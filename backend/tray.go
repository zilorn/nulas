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
	cmd     *exec.Cmd
	input   io.WriteCloser
	done    chan struct{}
	ready   bool
	waiting bool
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
	language := a.state.Preferences.TrayLanguage
	if language != "en" {
		language = "zh-CN"
	}
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
	cmd := exec.CommandContext(ctx, env("NULAS_PYTHON", python), script, "--url", origin, "--language", language)
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
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 256), 1024)
		ok := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == "READY" {
				a.mu.Lock()
				if a.tray == process {
					process.ready = true
					a.trayError = ""
				}
				a.mu.Unlock()
				ok = true
				break
			}
			if strings.HasPrefix(line, "STATUS ") || strings.HasPrefix(line, "ERROR ") || strings.HasPrefix(line, "WAIT ") {
				a.mu.Lock()
				if a.tray == process {
					a.trayError = strings.SplitN(line, " ", 2)[1]
					process.waiting = strings.HasPrefix(line, "WAIT ")
				}
				a.mu.Unlock()
			}
		}
		ready <- ok
		if ok {
			// A language selection can arrive while dependencies are being prepared.
			go func() {
				a.trayMu.Lock()
				defer a.trayMu.Unlock()
				a.mu.Lock()
				selected := a.state.Preferences.TrayLanguage
				if selected != "en" {
					selected = "zh-CN"
				}
				refresh := a.tray == process && a.state.Preferences.Tray && selected != language
				a.mu.Unlock()
				if refresh {
					a.stopTray()
					a.startTray()
				}
			}()
		}
		io.Copy(io.Discard, output)
	}()
	go func() {
		<-scanned
		cmd.Wait()
		input.Close()
		a.mu.Lock()
		if a.tray == process {
			a.tray = nil
			if a.trayError == "" || strings.HasPrefix(a.trayError, "正在") {
				a.trayError = "托盘已退出；请检查依赖与桌面托盘支持后重新开启。"
			}
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
	case <-time.After(2 * time.Second):
		// Installation continues in the helper; GET exposes progress and readiness.
		go func() {
			deadline := time.Now().Add(10 * time.Minute)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case ok := <-ready:
					if ok {
						return
					}
				case <-process.done:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					a.mu.Lock()
					waiting := process.waiting
					a.mu.Unlock()
					if waiting {
						deadline = time.Now().Add(10 * time.Minute)
					}
					if time.Now().Before(deadline) {
						continue
					}
				}
				break
			}
			a.trayMu.Lock()
			a.mu.Lock()
			current := a.tray == process
			a.mu.Unlock()
			if current {
				a.stopTray()
				a.mu.Lock()
				if a.trayError == "" || strings.HasPrefix(a.trayError, "正在") {
					a.trayError = "托盘启动失败或超时，请检查依赖安装与桌面支持后重试。"
				}
				a.mu.Unlock()
			}
			a.trayMu.Unlock()
		}()
		return
	}
	a.stopTray()
	a.mu.Lock()
	if a.trayError == "" {
		a.trayError = "托盘启动失败：请检查 Python 依赖与桌面托盘支持。Linux 会自动安装缺失依赖。"
	}
	a.mu.Unlock()
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
			Enabled  *bool   `json:"enabled"`
			Language *string `json:"language"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Enabled == nil && body.Language == nil {
			fail(w, 400, errors.New("需要 enabled 布尔值或 language"))
			return
		}
		if body.Language != nil && *body.Language != "zh-CN" && *body.Language != "en" {
			fail(w, 400, errors.New("language 仅支持 zh-CN 或 en"))
			return
		}
		if body.Enabled != nil && *body.Enabled && !a.trayStatus().Supported {
			fail(w, 409, errors.New("此平台不支持桌面托盘"))
			return
		}
		a.trayMu.Lock()
		defer a.trayMu.Unlock()
		a.mu.Lock()
		old := a.state.Preferences
		if body.Enabled != nil {
			a.state.Preferences.Tray = *body.Enabled
		}
		if body.Language != nil {
			a.state.Preferences.TrayLanguage = *body.Language
		}
		languageChanged := old.TrayLanguage != a.state.Preferences.TrayLanguage
		running := a.tray != nil && a.tray.ready
		enabled := a.state.Preferences.Tray
		var err error
		if old != a.state.Preferences || body.Enabled != nil {
			err = a.persist()
		}
		if err != nil {
			a.state.Preferences = old
		}
		a.mu.Unlock()
		if err != nil {
			fail(w, 500, fmt.Errorf("保存托盘偏好失败：%w", err))
			return
		}
		// Language-only requests never enable a tray or retry a failed launch.
		if enabled {
			if languageChanged && running {
				a.stopTray()
			}
			if body.Enabled != nil || (languageChanged && running) {
				a.startTray()
			}
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
