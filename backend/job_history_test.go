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
)

func TestJobHistoryLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	document := "rules: [MATCH,DIRECT]\n#" + strings.Repeat("x", 93800)
	state := State{Config: Config{7890, "rule", false, false, "info"}}
	state.Jobs = append(state.Jobs, Job{ID: "network-failure", Action: "proxy-enable", Status: "failed"})
	enabled := true
	state.Preferences.Proxy = &enabled
	for i := 0; i < 1100; i++ {
		state.Jobs = append(state.Jobs, Job{ID: fmt.Sprint(i), Action: "apply", Status: "succeeded", Document: document, Config: state.Config})
	}
	state.Jobs = append(state.Jobs, Job{ID: "interrupted", Action: "apply", Status: "running", Document: document}, Job{ID: "pending", Action: "generate", Status: "queued", Document: document, Config: state.Config})
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "state.json"), data); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.state.Jobs) != jobHistoryLimit || a.state.Jobs[0].ID != "network-failure" {
		t.Fatal("history bound/network policy lost")
	}
	if a.state.Applied == nil || a.state.Applied.Document != document {
		t.Fatal("legacy applied snapshot lost")
	}
	interrupted := a.state.Jobs[a.jobIndex("interrupted")]
	if interrupted.Status != "failed" || interrupted.Document != "" {
		t.Fatal("interrupted task retained document or will replay")
	}
	if a.state.Jobs[a.jobIndex("pending")].Document != document {
		t.Fatal("pending snapshot lost")
	}
	if len(a.networkRestore) != 0 {
		t.Fatal("failed network job restored automatically")
	}
	data, err = os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 700000 {
		t.Fatalf("historical documents still inflate state: %d", len(data))
	}
	if !a.process() || a.state.Jobs[a.jobIndex("pending")].Document != "" {
		t.Fatal("queued snapshot did not finish and compact")
	}
}

func TestJobHistorySubmissionAtLimit(t *testing.T) {
	a := testApp(t, "")
	for i := 0; i < jobHistoryLimit; i++ {
		a.state.Jobs = append(a.state.Jobs, Job{ID: fmt.Sprint(i), Action: "generate", Status: "succeeded"})
	}
	if w := request(a, "POST", "/api/jobs", `{"action":"generate"}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(a.state.Jobs) != jobHistoryLimit || a.state.Jobs[0].ID != "1" {
		t.Fatal("oldest completed record not evicted")
	}
	if !a.process() || a.state.Jobs[len(a.state.Jobs)-1].Status != "succeeded" {
		t.Fatal("worker failed at history limit")
	}
}

func TestJobFilesRetentionAndPersistenceFailure(t *testing.T) {
	a := testApp(t, "")
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("%032x", i)
		a.state.Jobs = append(a.state.Jobs, Job{ID: id, Action: "generate", Status: "succeeded", Document: "private", RefreshURL: "https://private"})
		if err := os.WriteFile(filepath.Join(a.dir, id+".yaml"), []byte("document"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	orphan := strings.Repeat("a", 32) + ".yaml"
	for _, name := range []string{orphan, "user.yaml"} {
		if err := os.WriteFile(filepath.Join(a.dir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(a.dir, "state.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.persist(); err == nil {
		t.Fatal("expected persistence failure")
	}
	if a.state.Jobs[0].Document != "private" {
		t.Fatal("failed write mutated source history")
	}
	if _, err := os.Stat(filepath.Join(a.dir, orphan)); err != nil {
		t.Fatal("failed write deleted output")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		_, err := os.Stat(filepath.Join(a.dir, fmt.Sprintf("%032x.yaml", i)))
		if i < 2 && !os.IsNotExist(err) || i >= 2 && err != nil {
			t.Fatalf("unexpected output retention %d: %v", i, err)
		}
		if a.state.Jobs[i].Document != "" || a.state.Jobs[i].RefreshURL != "" {
			t.Fatal("finished snapshot retained")
		}
	}
	if _, err := os.Stat(filepath.Join(a.dir, orphan)); !os.IsNotExist(err) {
		t.Fatal("orphan output retained")
	}
	if _, err := os.Stat(filepath.Join(a.dir, "user.yaml")); err != nil {
		t.Fatal("user file deleted")
	}
}

func TestJobHistoryWorkerSurvivesConcurrentCompaction(t *testing.T) {
	var a *App
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		for i := 0; i < jobHistoryLimit; i++ {
			a.state.Jobs = append(a.state.Jobs, Job{ID: fmt.Sprintf("later-%d", i), Action: "check-app-update", Status: "succeeded"})
		}
		if err := a.persist(); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	a = testApp(t, server.URL)
	a.state.Jobs = []Job{{ID: "old", Action: "generate", Status: "succeeded"}}
	if w := request(a, "POST", "/api/jobs", `{"action":"apply"}`); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	id := a.state.Jobs[1].ID
	if !a.process() {
		t.Fatal("worker did not execute")
	}
	if a.state.Applied == nil || a.state.Applied.JobID != id {
		t.Fatal("applied snapshot lost after index shifted")
	}
	// This task can be evicted at completion because newer records were inserted.
	if _, err := os.Stat(filepath.Join(a.dir, id+".yaml")); !os.IsNotExist(err) {
		t.Fatal("apply created redundant output")
	}
}

func TestJobHistoryPreservesAllLegacyPendingSnapshots(t *testing.T) {
	jobs := make([]Job, jobHistoryLimit+2)
	for i := range jobs {
		jobs[i] = Job{ID: fmt.Sprint(i), Status: "queued", Document: "snapshot"}
	}
	compact := compactJobs(jobs)
	if len(compact) != len(jobs) || compact[0].Document != "snapshot" {
		t.Fatal("active work discarded to meet history limit")
	}
}

func TestJobHistoryOtherQueuesAtLimit(t *testing.T) {
	fill := func(a *App) {
		for i := 0; i < jobHistoryLimit; i++ {
			a.state.Jobs = append(a.state.Jobs, Job{ID: fmt.Sprint(i), Action: "generate", Status: "succeeded"})
		}
	}
	t.Run("app-update", func(t *testing.T) {
		a := updateTestApp(t)
		fill(a)
		if w := request(a, "POST", "/api/updates/check", `{}`); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		if len(a.state.Jobs) != jobHistoryLimit {
			t.Fatal("unbounded queue")
		}
	})
	t.Run("core-install", func(t *testing.T) {
		isolatedCore(t)
		a := testApp(t, "")
		fill(a)
		if code, err := a.queueCore(); err != nil || code != 202 {
			t.Fatal(code, err)
		}
		if len(a.state.Jobs) != jobHistoryLimit {
			t.Fatal("unbounded queue")
		}
	})
	t.Run("core-switch", func(t *testing.T) {
		isolatedCore(t)
		a := testApp(t, "")
		a.managedContext = context.Background()
		fill(a)
		if w := request(a, "POST", "/api/core/switch", `{"version":"v1.2.3"}`); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	})
	t.Run("network-restore", func(t *testing.T) {
		a := testApp(t, "")
		fill(a)
		a.networkRestore = map[string]string{"proxy": "", "tun": ""}
		if err := a.queueNetworkRestore(); err != nil {
			t.Fatal(err)
		}
		if len(a.state.Jobs) != jobHistoryLimit || len(a.networkRestore) != 0 {
			t.Fatal("restoration queue not compacted")
		}
	})
}
