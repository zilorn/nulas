package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNetworkStartupRestoreDurabilityAndOrdering(t *testing.T) {
	a := testApp(t, "")
	enabled := true
	a.state.Preferences.TUN, a.state.Preferences.Proxy = &enabled, &enabled
	a.state.Jobs = []Job{{ID: "old-tun", Action: "tun-enable", Status: "succeeded"}, {ID: "old-proxy", Action: "proxy-enable", Status: "succeeded"}}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Core startup must finish before any restoration jobs are scheduled.
	b.state.Jobs = append(b.state.Jobs, Job{ID: "core", Action: "install-core", Status: "queued"})
	if err := b.queueNetworkRestore(); err != nil || len(b.state.Jobs) != 3 {
		t.Fatal("restore overtook core startup", err)
	}
	b.state.Jobs[2].Status = "succeeded"
	if err := b.queueNetworkRestore(); err != nil {
		t.Fatal(err)
	}
	if len(b.state.Jobs) != 5 || b.state.Jobs[3].Action != "tun-enable" || b.state.Jobs[4].Action != "proxy-enable" {
		t.Fatal(b.state.Jobs)
	}
	for _, job := range b.state.Jobs[3:] {
		if !job.Restore || job.Status != "queued" {
			t.Fatal(job)
		}
	}
	if err := b.queueNetworkRestore(); err != nil || len(b.state.Jobs) != 5 {
		t.Fatal("duplicate restoration", err)
	}
	c, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.networkRestore) != 0 || len(c.state.Jobs) != 5 {
		t.Fatal("pending restore duplicated after restart")
	}
}

func TestNetworkStartupRestoreSkipsUnsafeOrSupersededIntent(t *testing.T) {
	for _, status := range []string{"failed", "running", "queued", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			a := testApp(t, "")
			enabled := true
			a.state.Preferences.TUN = &enabled
			a.state.Jobs = []Job{{ID: "old", Action: "tun-enable", Status: status}}
			if err := a.persist(); err != nil {
				t.Fatal(err)
			}
			b, err := newApp(a.dir, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if status == "succeeded" {
				// A new user action while startup is in progress cancels automatic restore.
				b.state.Jobs = append(b.state.Jobs, Job{ID: "new", Action: "tun-disable", Status: "succeeded"})
			}
			before := len(b.state.Jobs)
			if err := b.queueNetworkRestore(); err != nil || len(b.state.Jobs) != before {
				t.Fatal("unsafe network operation retried", err)
			}
		})
	}
	a := testApp(t, "")
	disabled := false
	a.state.Preferences.TUN = &disabled
	a.prepareNetworkRestore()
	if err := a.queueNetworkRestore(); err != nil || len(a.state.Jobs) != 0 {
		t.Fatal("unset/disabled preferences restored", err)
	}
}

func TestNetworkStartupRestorePersistenceFailure(t *testing.T) {
	a := testApp(t, "")
	enabled := true
	a.state.Preferences.Proxy = &enabled
	a.prepareNetworkRestore()
	dir := a.dir
	a.dir = filepath.Join(t.TempDir(), "missing")
	if err := a.queueNetworkRestore(); err == nil || len(a.state.Jobs) != 0 || len(a.networkRestore) != 1 {
		t.Fatal("failed save did not roll back restoration")
	}
	a.dir = dir
	if err := a.queueNetworkRestore(); err != nil || len(a.state.Jobs) != 1 {
		t.Fatal("restore scheduling could not recover", err)
	}
}

func TestWorkerRestoresProxyAndPreservesOriginalBackup(t *testing.T) {
	a, values := proxyFixture(t, "")
	original := make(map[string]string)
	for k, v := range values {
		original[k] = v
	}
	request(a, "POST", "/api/jobs", `{"action":"proxy-enable"}`)
	a.process()
	backup := a.state.ProxyBackup
	b, err := newApp(a.dir, a.controller, "")
	if err != nil {
		t.Fatal(err)
	}
	// The desktop reset its settings during reboot.
	values["/mode"] = "'none'"
	enabled := false
	fakeSystem(b, values, &enabled, true, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := b.systemCommand
	b.systemCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		result, err := command(ctx, name, args...)
		if name == "gsettings" && args[0] == "set" && args[2] == "mode" && args[3] == "'manual'" {
			cancel()
		}
		return result, err
	}
	done := make(chan struct{})
	go func() { defer close(done); b.worker(ctx) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not restore proxy")
	}
	if len(b.state.Jobs) != 2 || !b.state.Jobs[1].Restore || b.state.Jobs[1].Status != "succeeded" || values["/mode"] != "'manual'" {
		t.Fatal(b.state.Jobs, values)
	}
	if !reflect.DeepEqual(b.state.ProxyBackup, backup) {
		t.Fatal("original proxy recovery snapshot overwritten")
	}
	if err := b.setSystemProxy(false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, original) {
		t.Fatal("original desktop proxy settings lost")
	}
	// Verification uses only fake desktop commands and loopback fixtures.
	if _, err := os.Stat(filepath.Join(b.dir, "state.json")); err != nil {
		t.Fatal(err)
	}
}

func TestStartupCorePrecedesPendingNetworkRestore(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	a.managedContext = context.Background()
	a.state.Jobs = []Job{{ID: "restore", Action: "tun-enable", Status: "queued", Restore: true}}
	if code, err := a.queueCoreWithPriority(true); err != nil || code != 202 {
		t.Fatal(code, err)
	}
	if len(a.state.Jobs) != 2 || a.state.Jobs[0].Action != "install-core" || a.state.Jobs[1].ID != "restore" {
		t.Fatal("pending network operation overtook core", a.state.Jobs)
	}
}

func TestFailedStartupRestoreRemainsVisibleAndRequiresManualRetry(t *testing.T) {
	a, values := proxyFixture(t, "")
	enabled := true
	a.state.Preferences.Proxy = &enabled
	a.prepareNetworkRestore()
	fakeSystem(a, values, &enabled, true, "/mode")
	if err := a.queueNetworkRestore(); err != nil {
		t.Fatal(err)
	}
	a.process()
	if a.state.Jobs[0].Status != "failed" || a.state.Jobs[0].Message == "" {
		t.Fatal("restoration failure hidden")
	}
	b, err := newApp(a.dir, a.controller, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.queueNetworkRestore(); err != nil || len(b.state.Jobs) != 1 {
		t.Fatal("failed restoration replayed", err)
	}
}
