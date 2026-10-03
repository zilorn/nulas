package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func updateTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("NULAS_INSTALL_HOME", "")
	a, err := newApp(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	a.updateHome = t.TempDir()
	a.runningRelease = filepath.Join(a.updateHome, "old")
	a.updateScript = filepath.Join(a.updateHome, "setup.py")
	if err := os.WriteFile(a.updateScript, []byte("# mock"), 0600); err != nil {
		t.Fatal(err)
	}
	saveUpdateMetadata(t, a, a.runningRelease)
	return a
}
func saveUpdateMetadata(t *testing.T, a *App, current string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"current": current, "commit": "1111111", "latest": "2222222", "tools": map[string]string{"python": "test-python"}})
	if err := atomicWrite(filepath.Join(a.updateHome, "installation.json"), b); err != nil {
		t.Fatal(err)
	}
}
func TestAppUpdateDurabilityAndRestartStatus(t *testing.T) {
	a := updateTestApp(t)
	response := request(a, "POST", "/api/updates/install", `{}`)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := request(a, "POST", "/api/updates/check", `{}`); response.Code != 202 || len(a.state.Jobs) != 1 {
		t.Fatal("duplicate update queued")
	}
	called := false
	a.updateCommand = func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		called = true
		b, err := os.ReadFile(filepath.Join(a.dir, "state.json"))
		if err != nil || !strings.Contains(string(b), `"status": "running"`) {
			t.Fatal("side effects before durable running state")
		}
		if name != "test-python" || !strings.Contains(strings.Join(args, " "), "--home "+a.updateHome) {
			t.Fatal(name, args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded update")
		}
		saveUpdateMetadata(t, a, filepath.Join(a.updateHome, "new"))
		return nil
	}
	if !a.process() || !called || a.state.Jobs[0].Status != "succeeded" {
		t.Fatal(a.state.Jobs)
	}
	response = request(a, "GET", "/api/updates", "")
	if !strings.Contains(response.Body.String(), `"restartRequired":true`) || strings.Contains(response.Body.String(), a.updateHome) {
		t.Fatal(response.Body.String())
	}
}
func TestAppUpdateFailureAndRecovery(t *testing.T) {
	a := updateTestApp(t)
	a.updateCommand = func(context.Context, io.Writer, io.Writer, string, ...string) error {
		return errors.New("https://private-token@example.invalid")
	}
	request(a, "POST", "/api/updates/check", `{}`)
	a.process()
	response := request(a, "GET", "/api/updates", "")
	if a.state.Jobs[0].Status != "failed" || strings.Contains(response.Body.String(), "private-token") {
		t.Fatal(response.Body.String())
	}
	if w := request(a, "POST", "/api/updates/check", `{"automatic":true}`); w.Code != 200 || len(a.state.Jobs) != 1 {
		t.Fatal("automatic failure retried too soon")
	}
	a.state.Jobs[0].Status = "running"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, "", "")
	if err != nil || b.state.Jobs[0].Status != "failed" {
		t.Fatal("interrupted update replayed", err)
	}
}
func TestAppUpdateValidationAndPersistenceFailure(t *testing.T) {
	a := updateTestApp(t)
	for _, tc := range []struct {
		path, body string
		code       int
	}{
		{"/api/updates/bad", `{}`, 404},
		{"/api/updates/install", `{"automatic":true}`, 400},
		{"/api/updates/check", `{"extra":true}`, 400},
	} {
		if w := request(a, "POST", tc.path, tc.body); w.Code != tc.code {
			t.Fatal(tc, w.Code)
		}
	}
	a.dir = filepath.Join(t.TempDir(), "missing")
	if w := request(a, "POST", "/api/updates/install", `{}`); w.Code != 500 || len(a.state.Jobs) != 0 {
		t.Fatal("unpersisted update queued")
	}
	a.updateHome = ""
	if w := request(a, "POST", "/api/updates/check", `{}`); w.Code != 409 {
		t.Fatal("unmanaged update allowed")
	}
	if w := request(a, "GET", "/api/updates", ""); !strings.Contains(w.Body.String(), `"supported":false`) {
		t.Fatal(w.Body.String())
	}
}
func TestAppUpdateAutomaticCooldown(t *testing.T) {
	a := updateTestApp(t)
	a.state.Jobs = []Job{{Action: "check-app-update", Status: "failed", Created: time.Now().Add(-7 * time.Hour)}}
	if w := request(a, "POST", "/api/updates/check", `{"automatic":true}`); w.Code != 202 || len(a.state.Jobs) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
}
