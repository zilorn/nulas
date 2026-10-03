package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T, controller string) *App {
	t.Helper()
	a, e := newApp(t.TempDir(), controller, "test-secret")
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func request(a *App, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	return w
}
func TestConfigurationValidationAndPersistence(t *testing.T) {
	a := testApp(t, "")
	for _, body := range []string{`{"mixed-port":0,"mode":"rule","log-level":"info"}`, `{"mixed-port":7890,"mode":"bad","log-level":"info"}`, `{"mixed-port":7890,"mode":"rule","log-level":"bad"}`, `{} {}`} {
		if w := request(a, "PUT", "/api/config", body); w.Code != 400 {
			t.Fatalf("invalid config accepted: %d", w.Code)
		}
	}
	w := request(a, "PUT", "/api/config", `{"mixed-port":8888,"mode":"direct","allow-lan":true,"ipv6":true,"log-level":"warning"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	b, e := newApp(a.dir, "", "")
	if e != nil || b.state.Config.Port != 8888 {
		t.Fatalf("persistence: %v", e)
	}
}
func TestDurableBackgroundGeneration(t *testing.T) {
	a := testApp(t, "")
	w := request(a, "POST", "/api/jobs", `{"action":"generate"}`)
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if request(a, "POST", "/api/jobs", `{"action":"generate"}`).Code != 409 {
		t.Fatal("duplicate queued task accepted")
	}
	b, e := newApp(a.dir, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if !b.process() || b.state.Jobs[0].Status != "succeeded" {
		t.Fatal("pending job did not resume")
	}
	data, e := os.ReadFile(filepath.Join(b.dir, b.state.Jobs[0].ID+".yaml"))
	if e != nil {
		t.Fatal(e)
	}
	var c Config
	if json.Unmarshal(data, &c) != nil || c.Port != 7890 {
		t.Fatal("invalid generated config")
	}
	if b.process() {
		t.Fatal("job replayed")
	}
}
func TestApplyController(t *testing.T) {
	called := false
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "PATCH" || r.URL.Path != "/configs" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("incorrect controller request")
		}
		var c Config
		if json.NewDecoder(r.Body).Decode(&c) != nil || c.Mode != "rule" {
			t.Error("incorrect snapshot")
		}
		w.WriteHeader(204)
	}))
	defer core.Close()
	a := testApp(t, core.URL)
	if request(a, "POST", "/api/jobs", `{"action":"apply"}`).Code != 202 {
		t.Fatal("apply rejected")
	}
	a.process()
	if !called || a.state.Jobs[0].Status != "succeeded" {
		t.Fatal("controller not called")
	}
}
func TestApplyFailureAndRecovery(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer core.Close()
	a := testApp(t, core.URL)
	request(a, "POST", "/api/jobs", `{"action":"apply"}`)
	a.process()
	if a.state.Jobs[0].Status != "failed" {
		t.Fatal("controller error hidden")
	}
	a.state.Jobs[0].Status = "running"
	if e := a.persist(); e != nil {
		t.Fatal(e)
	}
	b, e := newApp(a.dir, core.URL, "")
	if e != nil {
		t.Fatal(e)
	}
	if b.state.Jobs[0].Status != "failed" || b.process() {
		t.Fatal("interrupted side effect replayed")
	}
	c := testApp(t, "")
	if request(c, "POST", "/api/jobs", `{"action":"apply"}`).Code != 409 {
		t.Fatal("unconfigured controller accepted")
	}
}
func TestCrossOriginAndLimits(t *testing.T) {
	a := testApp(t, "")
	r := httptest.NewRequest("POST", "http://localhost/api/jobs", strings.NewReader(`{"action":"generate"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted")
	}
	if request(a, "POST", "/api/jobs", strings.Repeat("x", 9000)).Code != 400 {
		t.Fatal("oversized body accepted")
	}
}

func TestHostedFrontend(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>Nulas</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULAS_WEB_DIR", dir)
	a := testApp(t, "")
	w := request(a, "GET", "/", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Nulas") {
		t.Fatal("frontend was not served")
	}
	if request(a, "GET", "/api/unknown", "").Code != 404 {
		t.Fatal("unknown API should not serve frontend")
	}
}
