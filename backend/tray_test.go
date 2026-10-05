package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSwitchPreferencesPersistWithQueuedJobs(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir, "http://127.0.0.1:9090", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"proxy-enable", "tun-disable"} {
		a.mu.Lock()
		a.state.Jobs = nil
		a.mu.Unlock()
		if w := request(a, "POST", "/api/jobs", `{"action":"`+action+`"}`); w.Code != 202 {
			t.Fatal(w.Body.String())
		}
	}
	reloaded, err := newApp(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.state.Preferences.Proxy == nil || !*reloaded.state.Preferences.Proxy || reloaded.state.Preferences.TUN == nil || *reloaded.state.Preferences.TUN {
		t.Fatal("lost switch preferences on restart")
	}
	// Running operations must still become failed, without reapplying saved preferences.
	reloaded.state.Jobs[0].Status = "running"
	if err := reloaded.persist(); err != nil {
		t.Fatal(err)
	}
	again, err := newApp(dir, "", "")
	if err != nil || again.state.Jobs[0].Status != "failed" {
		t.Fatal("interrupted network operation replayed")
	}
}

func TestTrayLifecycleAndSavedIntent(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 unavailable")
	}
	t.Setenv("NULAS_PYTHON", python)
	script := filepath.Join(t.TempDir(), "helper.py")
	if err := os.WriteFile(script, []byte("import sys\nprint('READY', flush=True)\nsys.stdin.buffer.read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULAS_TRAY_SCRIPT", script)
	a, err := newApp(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.trayContext, a.trayURL = ctx, "http://127.0.0.1:8080"
	defer func() { a.trayMu.Lock(); a.stopTray(); a.trayMu.Unlock() }()
	for _, body := range []string{`{}`, `{"enabled":"yes"}`, `{"enabled":true,"unknown":1}`, `{"enabled":true,"language":"fr"}`, `{"language":42}`} {
		if w := request(a, "PUT", "/api/runtime/tray", body); w.Code != 400 {
			t.Fatal("accepted invalid tray body")
		}
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"language":"zh-CN"}`); w.Code != 200 || a.trayStatus().Enabled || a.trayStatus().Running {
		t.Fatal("language update enabled tray", w.Body.String())
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":true}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if s := a.trayStatus(); !s.Enabled || !s.Running {
		t.Fatal(s)
	}
	a.mu.Lock()
	process := a.tray
	a.mu.Unlock()
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":true}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	same := a.tray == process
	a.mu.Unlock()
	if !same {
		t.Fatal("duplicate helper")
	}

	if got := process.cmd.Args; got[len(got)-2] != "--language" || got[len(got)-1] != "zh-CN" {
		t.Fatal("missing default tray language", got)
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"language":"en"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	<-process.done
	a.mu.Lock()
	process = a.tray
	a.mu.Unlock()
	if process == nil || process.cmd.Args[len(process.cmd.Args)-1] != "en" {
		t.Fatal("running tray did not switch to English")
	}
	restored, err := newApp(a.dir, "", "")
	if err != nil || restored.state.Preferences.TrayLanguage != "en" {
		t.Fatal("tray language not restored", err)
	}

	// Normal disable must close stdin and wait for removal before replying.
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":false}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-process.done:
	default:
		t.Fatal("disabled helper still alive")
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":true}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	process = a.tray
	a.mu.Unlock()
	cancel()
	select {
	case <-process.done:
	case <-time.After(3 * time.Second):
		t.Fatal("helper survived backend cancellation")
	}
	if s := a.trayStatus(); !s.Enabled || s.Running {
		t.Fatal("saved intent confused with actual state")
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":false}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reloaded, err := newApp(a.dir, "", "")
	if err != nil || reloaded.state.Preferences.Tray {
		t.Fatal("disable preference not persisted")
	}
}

func TestTrayFailureAndPersistenceFailure(t *testing.T) {
	a, err := newApp(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	a.trayContext = context.Background()
	a.trayURL = "http://127.0.0.1:8080"
	t.Setenv("NULAS_PYTHON", filepath.Join(t.TempDir(), "missing"))
	w := request(a, "PUT", "/api/runtime/tray", `{"enabled":true}`)
	var s TrayStatus
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil || !s.Enabled || s.Running || s.Message == "" {
		t.Fatal(w.Body.String())
	}
	reloaded, err := newApp(a.dir, "", "")
	if err != nil || !reloaded.state.Preferences.Tray {
		t.Fatal("failed launch lost saved choice")
	}
	a.dir = filepath.Join(t.TempDir(), "missing")
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":false}`); w.Code != 500 {
		t.Fatal("ignored failed save")
	}
	if !a.state.Preferences.Tray {
		t.Fatal("failed save changed in-memory preference")
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"language":"en"}`); w.Code != 500 || a.state.Preferences.TrayLanguage != "" {
		t.Fatal("failed save changed tray language or launched helper")
	}
	// A failed atomic job write must roll back its associated network preference too.
	if w := request(a, "POST", "/api/jobs", `{"action":"tun-enable"}`); w.Code != 500 {
		t.Fatal(w.Body.String())
	}
	if a.state.Preferences.TUN != nil {
		t.Fatal("network preference changed despite failed save")
	}
}

func TestStartupPreferencePersistsBeforeSystemOperation(t *testing.T) {
	a := testApp(t, "")
	enabled := false
	fakeSystem(a, map[string]string{}, &enabled, true, "")
	if w := request(a, "PUT", "/api/runtime/system", `{"startup":true}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	restored, err := newApp(a.dir, "", "")
	if err != nil || restored.state.Preferences.Startup == nil || !*restored.state.Preferences.Startup {
		t.Fatal("startup choice not persisted")
	}
	a.dir = filepath.Join(t.TempDir(), "missing")
	if w := request(a, "PUT", "/api/runtime/system", `{"startup":false}`); w.Code != 500 {
		t.Fatal(w.Body.String())
	}
	if !enabled || a.state.Preferences.Startup == nil || !*a.state.Preferences.Startup {
		t.Fatal("failed persistence altered system registration")
	}
}

func TestTrayInstallationProgressAndFailure(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python unavailable")
	}
	for _, outcome := range []string{"READY", "ERROR 安装失败"} {
		t.Run(outcome, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "helper.py")
			body := "import sys, time\nprint('STATUS 正在安装依赖', flush=True)\ntime.sleep(2.5)\nprint('" + outcome + "', flush=True)\n"
			if outcome == "READY" {
				body += "sys.stdin.buffer.read()\n"
			}
			if err := os.WriteFile(script, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NULAS_PYTHON", python)
			t.Setenv("NULAS_TRAY_SCRIPT", script)
			a, err := newApp(t.TempDir(), "", "")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.trayContext, a.trayURL = ctx, "http://127.0.0.1:8080"
			defer func() { a.trayMu.Lock(); a.stopTray(); a.trayMu.Unlock() }()
			w := request(a, "PUT", "/api/runtime/tray", `{"enabled":true}`)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if s := a.trayStatus(); s.Running || s.Message != "正在安装依赖" {
				t.Fatal(s)
			}
			if outcome == "READY" {
				if w := request(a, "PUT", "/api/runtime/tray", `{"language":"en"}`); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			deadline := time.Now().Add(6 * time.Second)
			for time.Now().Before(deadline) {
				s := a.trayStatus()
				if outcome == "READY" && s.Running {
					a.mu.Lock()
					english := a.tray != nil && a.tray.cmd.Args[len(a.tray.cmd.Args)-1] == "en"
					a.mu.Unlock()
					if english {
						return
					}
				}
				if outcome != "READY" && !s.Running && s.Message == "安装失败" {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal(a.trayStatus())
		})
	}
}

func TestTrayDesktopWaitCanBeCancelled(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python unavailable")
	}
	script := filepath.Join(t.TempDir(), "helper.py")
	if err := os.WriteFile(script, []byte("import sys\nprint('WAIT 正在等待桌面', flush=True)\nsys.stdin.buffer.read()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULAS_PYTHON", python)
	t.Setenv("NULAS_TRAY_SCRIPT", script)
	a := testApp(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.trayContext, a.trayURL = ctx, "http://127.0.0.1:8080"
	defer func() { a.trayMu.Lock(); a.stopTray(); a.trayMu.Unlock() }()
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":true}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if status := a.trayStatus(); !status.Enabled || status.Running || status.Message != "正在等待桌面" {
		t.Fatal(status)
	}
	a.mu.Lock()
	process := a.tray
	waiting := process != nil && process.waiting
	a.mu.Unlock()
	if !waiting {
		t.Fatal("desktop wait must suspend the startup timeout")
	}
	if w := request(a, "PUT", "/api/runtime/tray", `{"enabled":false}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-process.done:
	case <-time.After(time.Second):
		t.Fatal("disabled tray still waiting")
	}
	if status := a.trayStatus(); status.Enabled || status.Running {
		t.Fatal(status)
	}
}
