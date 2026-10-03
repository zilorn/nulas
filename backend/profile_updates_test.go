package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func networkProfile(t *testing.T, a *App, url string, hours int) Profile {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "Network", "url": url, "updateIntervalHours": hours})
	w := request(a, "POST", "/api/profiles", string(body))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p Profile
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), url) || p.RefreshURL != "" || !p.Refreshable {
		t.Fatal("private URL exposed or refresh unavailable")
	}
	return p
}

func TestProfileRefreshDurabilityAndSnapshots(t *testing.T) {
	content := "mode: direct"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "clash.meta" {
			t.Error("incorrect download credentials/format")
		}
		fmt.Fprint(w, content)
	}))
	defer server.Close()
	a := testApp(t, "")
	p := networkProfile(t, a, server.URL+"?token=private", 2)
	if p.NextUpdate == nil || p.UpdatedAt == nil {
		t.Fatal("missing initial timestamps")
	}
	a.state.Applied = &AppliedConfig{Profile: Profile{Config: p.Config}}
	content = "name: Changed remotely\nmode: global\nproxies:\n - name: Node\n   type: ss\n   server: example.com\n   port: 443\n   cipher: aes-128-gcm\n   password: private-password\n"
	w := request(a, "POST", "/api/profiles/"+p.ID+"/refresh", "{}")
	if w.Code != 202 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	if request(a, "POST", "/api/profiles/"+p.ID+"/refresh", "{}").Code != 409 {
		t.Fatal("duplicate refresh queued")
	}
	if request(a, "POST", "/api/jobs", `{"action":"generate"}`).Code != 409 {
		t.Fatal("refresh did not reserve worker")
	}
	// Changing the source after submission cannot change the durable job snapshot.
	if w := request(a, "PUT", "/api/profiles/"+p.ID+"/updates", `{"url":"http://127.0.0.1:1/unreachable","updateIntervalHours":0}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	b, err := newApp(a.dir, "", "controller-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !b.process() {
		t.Fatal("pending refresh not resumed")
	}
	updated := b.state.Profiles[0]
	if updated.Config.Mode != "global" || !updated.Full || updated.Name != p.Name || updated.RefreshStatus != "succeeded" || updated.UpdatedAt == nil || !updated.UpdatedAt.After(*p.UpdatedAt) {
		t.Fatalf("update: %+v", updated)
	}
	if b.state.Config.Mode != "rule" || b.state.Applied.Config.Mode != "direct" {
		t.Fatal("refresh altered editor or applied snapshot")
	}
	for _, path := range []string{"/api/profiles", "/api/jobs"} {
		response := request(b, "GET", path, "").Body.String()
		if strings.Contains(response, "private") || strings.Contains(response, "unreachable") || strings.Contains(response, server.URL) {
			t.Fatal("secrets exposed", path)
		}
	}
	c, err := newApp(a.dir, "", "")
	if err != nil || c.state.Profiles[0].Document != content || c.state.Jobs[0].Status != "succeeded" {
		t.Fatal("updated content not durable", err)
	}
}

func TestProfileSchedulesAndFailures(t *testing.T) {
	content := "mode: direct"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, content) }))
	defer server.Close()
	a := testApp(t, "")
	p := networkProfile(t, a, server.URL, 1)
	due := *p.NextUpdate
	a.scheduleProfileUpdates(due.Add(-time.Second))
	if len(a.state.Jobs) != 0 {
		t.Fatal("scheduled too early")
	}
	a.scheduleProfileUpdates(due)
	a.scheduleProfileUpdates(due)
	if len(a.state.Jobs) != 1 || !a.state.Profiles[0].NextUpdate.Equal(due.Add(time.Hour)) {
		t.Fatal("schedule duplicated or not advanced")
	}
	content = "secret-token-field: private"
	if !a.process() || a.state.Jobs[0].Status != "failed" || a.state.Profiles[0].RefreshStatus != "failed" || a.state.Profiles[0].Config.Mode != "direct" || !a.state.Profiles[0].UpdatedAt.Equal(*p.UpdatedAt) {
		t.Fatal("failed download replaced good config")
	}
	if strings.Contains(request(a, "GET", "/api/jobs", "").Body.String(), "secret-token") {
		t.Fatal("validation error leaked downloaded content")
	}
	// Overdue schedules survive restart and enqueue one attempt rather than catch-up bursts.
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	overdue := due.Add(10 * time.Hour)
	b.scheduleProfileUpdates(overdue)
	if len(b.state.Jobs) != 2 || !b.state.Profiles[0].NextUpdate.Equal(overdue.Add(time.Hour)) {
		t.Fatal("overdue schedule not resumed")
	}
	b.state.Jobs[1].Status = "running"
	if err := b.persist(); err != nil {
		t.Fatal(err)
	}
	c, err := newApp(a.dir, "", "")
	if err != nil || c.state.Jobs[1].Status != "failed" || c.state.Profiles[0].RefreshStatus != "failed" {
		t.Fatal("interrupted update replayed", err)
	}
	c.scheduleProfileUpdates(overdue)
	if len(c.state.Jobs) != 2 {
		t.Fatal("interrupted update automatically replayed")
	}
}

func TestProfileUpdateValidationAndRollback(t *testing.T) {
	a := testApp(t, "")
	// An old network profile needs an explicit source; local profiles cannot schedule.
	a.state.Profiles = []Profile{{ID: "legacy", Source: "network"}, {ID: "local", Source: "created"}}
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"PUT", "/api/profiles/legacy/updates", `{"updateIntervalHours":1}`, 400},
		{"PUT", "/api/profiles/legacy/updates", `{"url":"file:///tmp/test","updateIntervalHours":1}`, 400},
		{"PUT", "/api/profiles/legacy/updates", `{"url":"https://example.com","updateIntervalHours":-1}`, 400},
		{"PUT", "/api/profiles/legacy/updates", `{"url":"https://example.com","updateIntervalHours":721}`, 400},
		{"PUT", "/api/profiles/legacy/updates", `{"url":"https://example.com","updateIntervalHours":1.5}`, 400},
		{"PUT", "/api/profiles/local/updates", `{"url":"https://example.com","updateIntervalHours":1}`, 409},
		{"POST", "/api/profiles/legacy/refresh", `{}`, 409},
		{"POST", "/api/profiles/missing/refresh", `{}`, 404},
		{"PUT", "/api/profiles/missing/updates", `{"updateIntervalHours":0}`, 404},
	} {
		if w := request(a, tc.method, tc.path, tc.body); w.Code != tc.code {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	if w := request(a, "PUT", "/api/profiles/legacy/updates", `{"url":"https://example.com?token=private","updateIntervalHours":720}`); w.Code != 200 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	before := a.state.Profiles[0]
	if err := os.Remove(filepath.Join(a.dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.dir, "state.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if request(a, "PUT", "/api/profiles/legacy/updates", `{"updateIntervalHours":0}`).Code != 500 || a.state.Profiles[0].UpdateIntervalHours != 720 {
		t.Fatal("settings failure not rolled back")
	}
	if request(a, "POST", "/api/profiles/legacy/refresh", `{}`).Code != 409 || len(a.state.Jobs) != 0 || a.state.Profiles[0].RefreshStatus != before.RefreshStatus {
		t.Fatal("unpersisted refresh queued")
	}
}

func TestProfileRefreshHistoryBound(t *testing.T) {
	a := testApp(t, "")
	a.state.Profiles = []Profile{{ID: "network", RefreshURL: "https://example.com"}}
	a.state.Jobs = make([]Job, 1000)
	for i := range a.state.Jobs {
		a.state.Jobs[i] = Job{Action: "refresh-profile", Status: "succeeded"}
	}
	a.state.Jobs[0] = Job{ID: "apply-snapshot", Action: "apply", Status: "succeeded"}
	if _, err := a.queueProfileRefresh(0, time.Now().UTC()); err != nil || len(a.state.Jobs) != 1000 || a.state.Jobs[0].ID != "apply-snapshot" {
		t.Fatal("history unbounded or apply removed", err)
	}
}

func TestProfileRefreshCompletionPersistenceFailure(t *testing.T) {
	a := testApp(t, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The task is durable/running, but saving the downloaded result fails.
		if err := os.Remove(filepath.Join(a.dir, "state.json")); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(filepath.Join(a.dir, "state.json"), 0700); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, "mode: global")
	}))
	defer server.Close()
	a.state.Profiles = []Profile{{ID: "network", Source: "network", Config: Config{7890, "direct", false, false, "info"}, RefreshURL: server.URL}}
	if _, err := a.queueProfileRefresh(0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if !a.process() || a.state.Jobs[0].Status != "failed" || a.state.Profiles[0].RefreshStatus != "failed" || a.state.Profiles[0].Config.Mode != "direct" {
		t.Fatal("unpersisted update reported success or replaced old content")
	}
}

func TestWorkerRefreshesScheduledProfiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "mode: global") }))
	defer server.Close()
	a := testApp(t, "")
	p := networkProfile(t, a, server.URL, 1)
	overdue := time.Now().UTC().Add(-time.Minute)
	a.state.Profiles[0].NextUpdate = &overdue
	a.state.Profiles = append(a.state.Profiles, Profile{ID: "disabled", RefreshURL: server.URL, NextUpdate: &overdue})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.worker(ctx) }()
	defer func() { cancel(); <-done }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("worker did not refresh due profile")
		case <-ticker.C:
			a.mu.Lock()
			complete := a.state.Profiles[0].RefreshStatus == "succeeded"
			if complete && (len(a.state.Jobs) != 1 || a.state.Jobs[0].ProfileID != p.ID || a.state.Profiles[0].Config.Mode != "global") {
				a.mu.Unlock()
				t.Fatal("incorrect scheduled operation")
			}
			a.mu.Unlock()
			if complete {
				return
			}
		}
	}
}
