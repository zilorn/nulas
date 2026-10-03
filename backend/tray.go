package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type trayProcess struct {
	cmd   *exec.Cmd
	input io.WriteCloser
	done  chan struct{}
	ready bool
}
type TrayStatus struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	Message   string `json:"message"`
}

func (a *App) trayStatus() TrayStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := TrayStatus{Supported: runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows", Enabled: a.state.Preferences.Tray, Running: a.tray != nil && a.tray.ready, Message: "托盘支持 Windows、macOS 和 Linux 桌面；关闭托盘不停止后台任务。"}
	if s.Enabled && !s.Running {
		s.Message = "已保存开启偏好，托盘未运行；请重新开启。"
	}
	if a.trayError != "" {
		s.Message = a.trayError
	}
	return s
}

// Caller holds trayMu. Readiness is acknowledged by the native helper, not inferred from Start.
func (a *App) startTray() {
	a.mu.Lock()
	if a.tray != nil || !a.state.Preferences.Tray {
		a.mu.Unlock()
		return
	}
	ctx, origin := a.trayContext, a.trayURL
	a.trayError = ""
	a.mu.Unlock()
	failure := func(message string) { a.mu.Lock(); a.trayError = message; a.mu.Unlock() }
	if ctx == nil || ctx.Err() != nil {
		failure("托盘未启动：后台服务未运行。")
		return
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		failure("托盘仅支持绑定回环地址的本机服务。")
		return
	}
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python"
	}
	script, err := filepath.Abs(env("NULAS_TRAY_SCRIPT", "../scripts/tray.py"))
	if err != nil {
		failure("无法确定托盘程序路径。")
		return
	}
	cmd := exec.CommandContext(ctx, env("NULAS_PYTHON", python), script, "--url", origin)
	// The helper only needs desktop/session variables, never controller credentials.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "MIHOMO_") && !strings.HasPrefix(value, "NULAS_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		failure("无法建立托盘通信。")
		return
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		failure("无法建立托盘通信。")
		return
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		failure("托盘启动失败，请检查 Python 与 NULAS_TRAY_SCRIPT 路径。")
		return
	}
	process := &trayProcess{cmd: cmd, input: input, done: make(chan struct{})}
	a.mu.Lock()
	a.tray = process
	a.mu.Unlock()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 256), 1024)
		ok := scanner.Scan() && scanner.Text() == "READY"
		ready <- ok
		io.Copy(io.Discard, output)
	}()
	go func() {
		cmd.Wait()
		input.Close()
		a.mu.Lock()
		if a.tray == process {
			a.tray = nil
			a.trayError = "托盘已退出；请检查桌面托盘支持，并安装 scripts/requirements-tray.txt 中的依赖后重新开启。"
		}
		a.mu.Unlock()
		close(process.done)
	}()
	select {
	case ok := <-ready:
		if ok {
			a.mu.Lock()
			if a.tray == process {
				process.ready = true
			}
			a.mu.Unlock()
			return
		}
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
	}
	a.stopTray()
	failure("托盘启动失败：请安装 scripts/requirements-tray.txt 中的依赖，并检查桌面托盘支持。Linux 需要 GTK/AppIndicator 与桌面托盘区域。")
}
func (a *App) stopTray() {
	a.mu.Lock()
	process := a.tray
	a.tray = nil
	a.mu.Unlock()
	if process == nil {
		return
	}
	process.input.Close()
	select {
	case <-process.done:
	case <-time.After(2 * time.Second):
		process.cmd.Process.Kill()
		<-process.done
	}
}
func (a *App) trayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runtime/tray", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, a.trayStatus()) })
	mux.HandleFunc("PUT /api/runtime/tray", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Enabled == nil {
			fail(w, 400, errors.New("需要 enabled 布尔值"))
			return
		}
		if *body.Enabled && !a.trayStatus().Supported {
			fail(w, 409, errors.New("此平台不支持桌面托盘"))
			return
		}
		a.trayMu.Lock()
		defer a.trayMu.Unlock()
		a.mu.Lock()
		old := a.state.Preferences.Tray
		a.state.Preferences.Tray = *body.Enabled
		err := a.persist()
		if err != nil {
			a.state.Preferences.Tray = old
		}
		a.mu.Unlock()
		if err != nil {
			fail(w, 500, fmt.Errorf("保存托盘偏好失败：%w", err))
			return
		}
		if *body.Enabled {
			a.startTray()
		} else {
			a.stopTray()
			a.mu.Lock()
			a.trayError = ""
			a.mu.Unlock()
		}
		// Saved intent and runtime readiness are separate; failure is visible in the response.
		reply(w, 200, a.trayStatus())
	})
}
