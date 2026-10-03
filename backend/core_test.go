package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func isolatedCore(t *testing.T) {
	t.Helper()
	t.Setenv("NULAS_CORE_DIR", filepath.Join(t.TempDir(), "core"))
}
func TestEnsureCoreDeduplicationAndRecovery(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			w := request(a, "POST", "/api/core/ensure", "{}")
			if w.Code != 202 && w.Code != 200 {
				t.Errorf("ensure: %s", w.Body.String())
			}
		}()
	}
	group.Wait()
	if len(a.state.Jobs) != 1 || a.coreStatus().Status != "installing" {
		t.Fatal("duplicate installation")
	}
	a.state.Jobs[0].Status = "running"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	request(b, "POST", "/api/core/ensure", "{}")
	if len(b.state.Jobs) != 1 || b.coreStatus().Status != "failed" || b.process() {
		t.Fatal("interrupted install replayed")
	}
	if request(b, "POST", "/api/jobs", `{"action":"install-core"}`).Code != 202 {
		t.Fatal("retry rejected")
	}
}
func TestCoreInstalledAndInvalidFile(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	if err := os.MkdirAll(filepath.Dir(corePath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corePath(), []byte("existing"), 0700); err != nil {
		t.Fatal(err)
	}
	request(a, "POST", "/api/core/ensure", "{}")
	if len(a.state.Jobs) != 0 || a.coreStatus().Status != "installed" {
		t.Fatal("existing core not preserved")
	}
	if err := os.Remove(corePath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(corePath(), 0700); err != nil {
		t.Fatal(err)
	}
	request(a, "POST", "/api/core/ensure", "{}")
	if len(a.state.Jobs) != 0 || a.coreStatus().Status != "failed" {
		t.Fatal("invalid core path overwritten")
	}
}
func TestInstallerVerificationAndFailure(t *testing.T) {
	isolatedCore(t)
	a := testApp(t, "")
	script := filepath.Join(t.TempDir(), "installer.py")
	t.Setenv("NULAS_CORE_INSTALLER", script)
	t.Setenv("NULAS_PYTHON", "python3")
	for _, test := range []struct {
		source  string
		success bool
	}{
		{"raise SystemExit('download failed')", false},
		{"pass", false},
		{"import pathlib,sys\np=pathlib.Path(sys.argv[2]);p.mkdir(parents=True);f=p/'mihomo';f.write_text('fake test core');f.chmod(0o700)", true},
	} {
		if err := os.WriteFile(script, []byte(test.source), 0600); err != nil {
			t.Fatal(err)
		}
		if request(a, "POST", "/api/jobs", `{"action":"install-core"}`).Code != 202 {
			t.Fatal("install rejected")
		}
		if !a.process() {
			t.Fatal("not processed")
		}
		job := a.state.Jobs[len(a.state.Jobs)-1]
		if (job.Status == "succeeded") != test.success {
			t.Fatalf("unexpected result: %+v", job)
		}
	}
}
