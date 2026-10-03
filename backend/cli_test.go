package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCLIServiceCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NULAS_ADDR", "")
	if code := runPortConfig([]string{"port", "4769"}, io.Discard, io.Discard); code != 0 {
		t.Fatal("cannot save test port")
	}
	for _, action := range []string{"status", "start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			var calls [][]string
			var stdout, stderr bytes.Buffer
			run := func(ctx context.Context, out, errout io.Writer, name string, args ...string) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded command")
				}
				calls = append(calls, append([]string{name}, args...))
				if len(calls) == 1 {
					io.WriteString(out, "Nulas frontend and backend\n")
				}
				return nil
			}
			if code := runCLI([]string{action}, &stdout, &stderr, run, nil); code != 0 {
				t.Fatalf("%d: %s", code, &stderr)
			}
			want := [][]string{{"systemctl", "--user", "show", serviceUnit, "--property=Description", "--value"}, {"systemctl", "--user", "--no-pager", action, serviceUnit}}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls = %v", calls)
			}
			started := action == "start" || action == "restart"
			if bytes.Contains(stdout.Bytes(), []byte("http://127.0.0.1:4769/")) != started || bytes.Contains(stdout.Bytes(), []byte("SSR 端口仅供内部")) != started {
				t.Fatalf("unexpected browser guidance: %s", &stdout)
			}
		})
	}
}

func TestCLIStartAddressOverride(t *testing.T) {
	t.Setenv("NULAS_ADDR", "127.0.0.1:4869")
	var stdout, stderr bytes.Buffer
	run := func(_ context.Context, out, _ io.Writer, _ string, args ...string) error {
		if args[1] == "show" {
			io.WriteString(out, "Nulas frontend and backend\n")
		}
		return nil
	}
	if code := runCLI([]string{"start"}, &stdout, &stderr, run, nil); code != 0 || !bytes.Contains(stdout.Bytes(), []byte("http://127.0.0.1:4869/")) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}

func TestCLIRejectsUnrelatedService(t *testing.T) {
	for _, description := range []string{"", "Other service"} {
		var stdout, stderr bytes.Buffer
		calls := 0
		run := func(_ context.Context, out, _ io.Writer, _ string, _ ...string) error {
			calls++
			io.WriteString(out, description)
			return nil
		}
		if code := runCLI([]string{"stop"}, &stdout, &stderr, run, nil); code == 0 || calls != 1 {
			t.Fatalf("code=%d calls=%d", code, calls)
		}
	}
}

func TestCLIFailureIsVisible(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		var stdout, stderr bytes.Buffer
		calls := 0
		run := func(_ context.Context, out, _ io.Writer, _ string, _ ...string) error {
			calls++
			if calls == failAt {
				return errors.New("user manager unavailable")
			}
			io.WriteString(out, "Nulas frontend and backend")
			return nil
		}
		if code := runCLI([]string{"restart"}, &stdout, &stderr, run, nil); code == 0 {
			t.Fatal("failure reported success")
		}
		if stdout.Len() != 0 || !bytes.Contains(stderr.Bytes(), []byte("user manager unavailable")) {
			t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
		}
	}
}

func TestCLIHelpAndInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"-h"}, {"unknown"}, {"start", "extra"}, {"uninstall", "extra"}, {"remove", "extra"}} {
		var stdout, stderr bytes.Buffer
		want := 2
		if len(args) == 1 && args[0] != "unknown" {
			want = 0
		}
		if code := runCLI(args, &stdout, &stderr, nil, nil); code != want {
			t.Fatalf("%v: %d", args, code)
		}
	}
}

func TestCLIInstallUsesExecutableLocation(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"bin", "scripts"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(root, "bin", "nulas")
	installer := filepath.Join(root, "scripts", "install_service.py")
	for _, path := range []string{binary, installer} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "nulas-link")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULAS_PYTHON", "custom-python")
	var stdout, stderr bytes.Buffer
	run := func(_ context.Context, _, _ io.Writer, name string, args ...string) error {
		if name != "custom-python" || !reflect.DeepEqual(args, []string{installer}) {
			t.Fatalf("%s %v", name, args)
		}
		return nil
	}
	executable := func() (string, error) { return link, nil }
	if code := runCLI([]string{"install"}, &stdout, &stderr, run, executable); code != 0 {
		t.Fatalf("%d: %s", code, &stderr)
	}
	runUninstall := func(ctx context.Context, _, _ io.Writer, name string, args ...string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("uninstall lacks timeout")
		}
		if name != "custom-python" || !reflect.DeepEqual(args, []string{installer, "--uninstall"}) {
			t.Fatalf("%s %v", name, args)
		}
		return errors.New("stop failed")
	}
	if code := runCLI([]string{"uninstall"}, &stdout, &stderr, runUninstall, executable); code == 0 {
		t.Fatal("uninstall failure reported success")
	}
	if err := os.Remove(installer); err != nil {
		t.Fatal(err)
	}
	if code := runCLI([]string{"install"}, &stdout, &stderr, nil, executable); code == 0 {
		t.Fatal("missing installer reported success")
	}
}

func TestCLIUpdateDelegation(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "bin", "nulas")
	if err := os.WriteFile(binary, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULAS_INSTALL_HOME", root)
	t.Setenv("NULAS_PYTHON", "custom-python")
	var stdout, stderr bytes.Buffer
	run := func(ctx context.Context, _, _ io.Writer, name string, args ...string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("update lacks timeout")
		}
		want := []string{filepath.Join(root, "scripts", "setup.py"), "update", "--check", "--home", root}
		if name != "custom-python" || !reflect.DeepEqual(args, want) {
			t.Fatalf("%s %v", name, args)
		}
		return nil
	}
	if code := runCLI([]string{"update", "--check"}, &stdout, &stderr, run, func() (string, error) { return binary, nil }); code != 0 {
		t.Fatalf("%d: %s", code, &stderr)
	}
	t.Setenv("NULAS_INSTALL_HOME", "")
	if code := runCLI([]string{"update"}, &stdout, &stderr, nil, func() (string, error) { return binary, nil }); code == 0 {
		t.Fatal("unmanaged install allowed update")
	}
}

func TestCLIRemoveDelegation(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "nulas")
	if err := os.WriteFile(binary, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULAS_INSTALL_HOME", root)
	t.Setenv("NULAS_PYTHON", "custom-python")
	var stdout, stderr bytes.Buffer
	run := func(ctx context.Context, _, _ io.Writer, name string, args ...string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("remove lacks timeout")
		}
		want := []string{filepath.Join(filepath.Dir(root), "scripts", "setup.py"), "remove", "--home", root}
		if name != "custom-python" || !reflect.DeepEqual(args, want) {
			t.Fatalf("%s %v", name, args)
		}
		return errors.New("removal failed")
	}
	executable := func() (string, error) { return binary, nil }
	if code := runCLI([]string{"remove"}, &stdout, &stderr, run, executable); code == 0 {
		t.Fatal("removal failure reported success")
	}
	t.Setenv("NULAS_INSTALL_HOME", "")
	if code := runCLI([]string{"remove"}, &stdout, &stderr, nil, executable); code == 0 {
		t.Fatal("unmanaged installation allowed removal")
	}
}
