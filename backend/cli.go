package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type cliRunner func(context.Context, io.Writer, io.Writer, string, ...string) error

func execCLI(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

const cliUsage = `Usage: nulas [install|status|start|stop|restart|run|update|config port [PORT]]

  config port [PORT]  Show or save the web/API port (1–65535; restart to apply).

  run      Run both servers in foreground (quick installation required).
  update   Check/build/install updates (quick installation required).
           --check, --auto on|off|status, --watch
  install  Install the Linux user service for both frontend and backend
           (does not start it or enable boot startup; requires Python 3).
  status   Show the combined service's systemd status.
  start    Start both servers.
  stop     Stop both servers.
  restart  Restart both servers.

Run without arguments to serve the backend in the foreground.
Use scripts/start.sh to run both servers in the foreground.
`

func cliExit(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "nulas: %v\n", err)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}
	return 1
}

func runCLI(args []string, stdout, stderr io.Writer, run cliRunner, executable func() (string, error)) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, cliUsage)
		return 0
	}
	if len(args) > 0 && args[0] == "config" {
		return runPortConfig(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "update" || args[0] == "run") {
		binary, err := executable()
		if err != nil {
			return cliExit(err, stderr)
		}
		binary, err = filepath.EvalSymlinks(binary)
		if err != nil {
			return cliExit(err, stderr)
		}
		home := os.Getenv("NULAS_INSTALL_HOME")
		if home == "" {
			return cliExit(errors.New("run/update require a quick installation; use scripts/install.sh or scripts/install.ps1"), stderr)
		}
		script := filepath.Join(filepath.Dir(filepath.Dir(binary)), "scripts", "setup.py")
		python := env("NULAS_PYTHON", "python3")
		if runtime.GOOS == "windows" && os.Getenv("NULAS_PYTHON") == "" {
			python = "python"
		}
		forward := append([]string{script}, args...)
		forward = append(forward, "--home", home)
		ctx := context.Background()
		if args[0] == "update" && !strings.Contains(strings.Join(args, " "), "--watch") {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 2*time.Hour)
			defer cancel()
		}
		return cliExit(run(ctx, stdout, stderr, python, forward...), stderr)
	}
	if len(args) != 1 || (args[0] != "install" && args[0] != "status" && args[0] != "start" && args[0] != "stop" && args[0] != "restart") {
		fmt.Fprint(stderr, cliUsage)
		return 2
	}
	if runtime.GOOS != "linux" {
		return cliExit(errors.New("service commands require Linux and user systemd"), stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if args[0] == "install" {
		binary, err := executable()
		if err != nil {
			return cliExit(err, stderr)
		}
		binary, err = filepath.EvalSymlinks(binary)
		if err != nil {
			return cliExit(err, stderr)
		}
		installer := filepath.Join(filepath.Dir(filepath.Dir(binary)), "scripts", "install_service.py")
		if _, err := os.Stat(installer); err != nil {
			return cliExit(fmt.Errorf("keep the executable in the project's bin directory and run scripts/build.sh first: %w", err), stderr)
		}
		return cliExit(run(ctx, stdout, stderr, env("NULAS_PYTHON", "python3"), installer), stderr)
	}
	// Refuse to operate on unrelated same-name services, including system units.
	var description strings.Builder
	if err := run(ctx, &description, stderr, "systemctl", "--user", "show", serviceUnit, "--property=Description", "--value"); err != nil {
		fmt.Fprintln(stderr, "Cannot read the user service. Run nulas install first and check your user systemd session.")
		return cliExit(err, stderr)
	}
	if strings.TrimSpace(description.String()) != "Nulas frontend and backend" {
		return cliExit(errors.New("Nulas frontend/backend user service is not installed or a different service has this name; run nulas install"), stderr)
	}
	err := run(ctx, stdout, stderr, "systemctl", "--user", "--no-pager", args[0], serviceUnit)
	if ctx.Err() != nil {
		return cliExit(fmt.Errorf("service command timed out: %w; check nulas status", ctx.Err()), stderr)
	}
	if err != nil {
		return cliExit(err, stderr)
	}
	if args[0] != "status" {
		fmt.Fprintf(stdout, "Nulas frontend and backend: %s completed.\n", args[0])
	}
	return 0
}
