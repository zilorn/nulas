package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type installationStatus struct {
	Current string            `json:"current"`
	Commit  string            `json:"commit"`
	Latest  string            `json:"latest"`
	Checked float64           `json:"checked"`
	Error   string            `json:"error"`
	Tools   map[string]string `json:"tools"`
}

func isAppUpdate(action string) bool { return action == "check-app-update" || action == "update-app" }
func (a *App) configureUpdates() {
	a.updateHome = os.Getenv("NULAS_INSTALL_HOME")
	binary, err := os.Executable()
	if err != nil {
		return
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return
	}
	a.runningRelease = filepath.Dir(filepath.Dir(binary))
	a.updateScript = filepath.Join(a.runningRelease, "scripts", "setup.py")
}
func (a *App) installation() (installationStatus, error) {
	var m installationStatus
	if a.updateHome == "" {
		return m, errors.New("手工构建版本请自行更新并构建；页面更新需要快速安装")
	}
	if _, err := os.Stat(a.updateScript); err != nil {
		return m, errors.New("更新脚本不可用，请检查快速安装目录")
	}
	f, err := os.Open(filepath.Join(a.updateHome, "installation.json"))
	if err != nil {
		return m, errors.New("无法读取安装信息，请检查快速安装目录")
	}
	defer f.Close()
	if err := json.NewDecoder(io.LimitReader(f, 1024*1024)).Decode(&m); err != nil || m.Current == "" || m.Commit == "" {
		return m, errors.New("安装信息无效，请检查快速安装目录")
	}
	return m, nil
}
func (a *App) registerUpdates(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/updates", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		m, err := a.installation()
		message := "尚未检查更新"
		if err != nil {
			message = err.Error()
		} else if m.Latest != "" {
			message = "已是最新版本"
			if m.Latest != m.Commit {
				message = "发现新版本"
			}
		}
		var job *Job
		busy := false
		for _, j := range a.state.Jobs {
			if isAppUpdate(j.Action) {
				public := publicJob(j)
				job = &public
				if j.Status == "queued" || j.Status == "running" {
					busy = true
				}
			}
		}
		reply(w, 200, map[string]any{"supported": err == nil, "message": message, "installed": m.Commit, "latest": m.Latest, "checked": m.Checked, "available": m.Latest != "" && m.Latest != m.Commit, "restartRequired": err == nil && filepath.Clean(m.Current) != filepath.Clean(a.runningRelease), "busy": busy, "job": job, "error": updateFailureMessage(m.Error)})
	})
	mux.HandleFunc("POST /api/updates/{action}", func(w http.ResponseWriter, r *http.Request) {
		action := r.PathValue("action")
		if action != "check" && action != "install" {
			fail(w, 404, errors.New("未知更新操作"))
			return
		}
		var body struct {
			Automatic bool `json:"automatic"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		m, err := a.installation()
		if err != nil {
			fail(w, 409, err)
			return
		}
		for _, j := range a.state.Jobs {
			if isAppUpdate(j.Action) && (j.Status == "queued" || j.Status == "running") {
				reply(w, 202, publicJob(j))
				return
			}
		}
		if body.Automatic {
			if action != "check" {
				fail(w, 400, errors.New("安装更新需要手动触发"))
				return
			}
			last := time.Unix(int64(m.Checked), 0)
			for _, j := range a.state.Jobs {
				if isAppUpdate(j.Action) && j.Created.After(last) {
					last = j.Created
				}
			}
			if time.Since(last) < 6*time.Hour {
				reply(w, 200, map[string]bool{"skipped": true})
				return
			}
		}
		if len(a.state.Jobs) >= 1000 {
			fail(w, 409, errors.New("后台任务记录已满"))
			return
		}
		id := make([]byte, 8)
		if _, err := rand.Read(id); err != nil {
			fail(w, 500, err)
			return
		}
		name := "check-app-update"
		if action == "install" {
			name = "update-app"
		}
		job := Job{ID: hex.EncodeToString(id), Action: name, Status: "queued", Message: "等待执行 Nulas 更新操作", Created: time.Now().UTC()}
		a.state.Jobs = append(a.state.Jobs, job)
		if err := a.persist(); err != nil {
			a.state.Jobs = a.state.Jobs[:len(a.state.Jobs)-1]
			fail(w, 500, err)
			return
		}
		select {
		case a.wake <- struct{}{}:
		default:
		}
		reply(w, 202, publicJob(job))
	})
}
func (a *App) runAppUpdate(j Job) error {
	m, err := a.installation()
	if err != nil {
		return err
	}
	python := os.Getenv("NULAS_PYTHON")
	if python == "" {
		python = m.Tools["python"]
	}
	if python == "" {
		python = "python3"
		if runtime.GOOS == "windows" {
			python = "python"
		}
	}
	args := []string{a.updateScript, "update", "--home", a.updateHome}
	if j.Action == "check-app-update" {
		args = append(args, "--check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	run := a.updateCommand
	if run == nil {
		run = execCLI
	}
	// Build output can contain repository credentials; only expose a fixed error to browsers.
	if err := run(ctx, io.Discard, io.Discard, python, args...); err != nil {
		return errors.New("Nulas 更新失败：请检查网络、构建依赖或安装目录锁；当前版本继续运行，可在终端执行 nulas update 查看详情")
	}
	return nil
}

func updateFailureMessage(value string) string {
	if value != "" {
		return "上次自动更新失败，请在终端检查 nulas update --auto status"
	}
	return ""
}
