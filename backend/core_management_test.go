package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type coreReleaseTransport func(*http.Request) (*http.Response, error)

func (f coreReleaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockCoreReleases(a *App, body string, status int) {
	a.client = &http.Client{Transport: coreReleaseTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}
func TestCoreReleaseHistoryAndLatestSnapshot(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	a.managedContext = context.Background()
	mockCoreReleases(a, `[{"tag_name":"v1.2.3"},{"tag_name":"Prerelease-Alpha","prerelease":true},{"tag_name":"v1.2.4","draft":true}]`, 200)
	w := request(a, "GET", "/api/core/releases?page=1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "v1.2.3") || strings.Contains(w.Body.String(), "Alpha") || strings.Contains(w.Body.String(), "v1.2.4") {
		t.Fatal(w.Body.String())
	}
	if w = request(a, "GET", "/api/core/releases?page=0", ""); w.Code != 400 {
		t.Fatal("invalid page accepted")
	}
	mockCoreReleases(a, `{"tag_name":"v1.2.3"}`, 200)
	if w = request(a, "POST", "/api/core/switch", `{"version":"latest"}`); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if a.state.Jobs[0].CoreVersion != "v1.2.3" {
		t.Fatal("latest version not snapshotted")
	}
	if w = request(a, "POST", "/api/core/switch", `{"version":"v1.2.2"}`); w.Code != 409 {
		t.Fatal("concurrent switch accepted")
	}
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Jobs[0].CoreVersion != "v1.2.3" {
		t.Fatal("version not durable")
	}
	b.state.Jobs[0].Status = "running"
	if err = b.persist(); err != nil {
		t.Fatal(err)
	}
	c, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.state.Jobs[0].Status != "failed" || c.process() {
		t.Fatal("interrupted switch replayed")
	}
}
func TestCoreSwitchValidationAndPersistence(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	if w := request(a, "POST", "/api/core/switch", `{"version":"v1.2.3"}`); w.Code != 409 {
		t.Fatal("external controller accepted")
	}
	a.managedContext = context.Background()
	if w := request(a, "POST", "/api/core/switch", `{"version":"../../evil"}`); w.Code != 400 {
		t.Fatal("invalid tag accepted")
	}
	mockCoreReleases(a, `{}`, 403)
	if w := request(a, "POST", "/api/core/switch", `{"version":"latest"}`); w.Code != 502 || len(a.state.Jobs) != 0 {
		t.Fatal("release failure queued")
	}
	if err := os.Remove(filepath.Join(a.dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.dir, "state.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if w := request(a, "POST", "/api/core/switch", `{"version":"v1.2.3"}`); w.Code != 500 || len(a.state.Jobs) != 0 {
		t.Fatal("persistence failure queued")
	}
}
func TestCoreSwitchFailurePreservesLegacyAndCachesDownload(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	a.managedContext = context.Background()
	dir := filepath.Dir(corePath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := corePath()
	if err := os.WriteFile(original, []byte("legacy core"), 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "installer.py")
	t.Setenv("NULAS_CORE_INSTALLER", script)
	t.Setenv("NULAS_PYTHON", "python3")
	if err := os.WriteFile(script, []byte("raise SystemExit('checksum mismatch')"), 0600); err != nil {
		t.Fatal(err)
	}
	job := Job{CoreVersion: "v1.2.3", Config: Config{Port: 9090}}
	if err := a.switchCore(job); err == nil {
		t.Fatal("failed download accepted")
	}
	if corePath() != original {
		t.Fatal("download failure changed version")
	}
	source := `import pathlib,sys,json
p=pathlib.Path(sys.argv[sys.argv.index('--output')+1])
f=p/'mihomo'
f.write_text('#!/usr/bin/env python3\nraise SystemExit(0)\n');f.chmod(0o700)
(p/'release.json').write_text(json.dumps({'tag':'v1.2.3'}))
`
	if err := os.WriteFile(script, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	// Both starts fail before launching: controller/proxy ports intentionally conflict.
	if err := a.switchCore(job); err == nil || !strings.Contains(err.Error(), "原版本恢复") {
		t.Fatalf("missing rollback: %v", err)
	}
	if corePath() != original {
		t.Fatal("original version not restored")
	}
	if data, err := os.ReadFile(original); err != nil || string(data) != "legacy core" {
		t.Fatal("legacy core modified")
	}
	if err := os.WriteFile(script, []byte("raise SystemExit('installer must not run')"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.switchCore(job); err == nil || strings.Contains(err.Error(), "installer must not run") {
		t.Fatalf("cache not reused: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "versions"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "v1.2.3" {
		t.Fatal("staging files retained")
	}
	if err := os.WriteFile(filepath.Join(dir, "active-version"), []byte("../invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := coreInstalled(); err == nil {
		t.Fatal("invalid pointer accepted")
	}
}
