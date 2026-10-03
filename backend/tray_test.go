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
	for _, body := range []string{`{}`, `{"enabled":"yes"}`, `{"enabled":true,"unknown":1}`} {
		if w := request(a, "PUT", "/api/runtime/tray", body); w.Code != 400 {
			t.Fatal("accepted invalid tray body")
		}
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
