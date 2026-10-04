package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type cliRunner func(context.Context, io.Writer, io.Writer, string, ...string) error

func execCLI(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

const cliUsage = `Usage: nulas [open|install|uninstall|remove|status|start|stop|restart|run|update|config [KEY [PORT]]]

  config [KEY [PORT]] Show all ports, or show/save one (1–65535; restart to apply).
                     Keys: port (web/API), ssr-port, dev-port.
                     config --json prints saved/default values as JSON.

  open     Open the configured Web/API URL in the default browser.
           Start Nulas first; requires a desktop browser session.
  run      Run both servers in foreground (quick installation required).
  update   Check/build/install updates (quick installation required).
           --check, --auto on|off|status, --watch
  install  Install the Linux user service for both frontend and backend
           (does not start it or enable boot startup; requires Python 3).
  uninstall Stop, disable and remove the Linux user service (requires Python 3).
  remove   Remove quick-installed software and CLI PATH entries; preserve data,
           core runtime and user configuration. Stop foreground servers first.
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
	if len(args) > 0 && args[0] == "open" {
		if len(args) != 1 {
			fmt.Fprint(stderr, cliUsage)
			return 2
		}
		return runOpen(runtime.GOOS, stdout, stderr, run)
	}
	if len(args) > 0 && (args[0] == "update" || args[0] == "run" || args[0] == "remove") {
		if args[0] == "remove" && len(args) != 1 {
			fmt.Fprint(stderr, cliUsage)
			return 2
		}
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
			return cliExit(errors.New("run/update/remove require a quick installation; use scripts/install.sh or scripts/install.ps1"), stderr)
		}
		if args[0] == "remove" && runtime.GOOS == "windows" {
			return cliExit(errors.New("use the installed nulas.cmd launcher for removal; run nulas update once to refresh an older launcher"), stderr)
		}
		script := filepath.Join(filepath.Dir(filepath.Dir(binary)), "scripts", "setup.py")
		python := env("NULAS_PYTHON", "python3")
		if runtime.GOOS == "windows" && os.Getenv("NULAS_PYTHON") == "" {
			python = "python"
		}
		forward := append([]string{script}, args...)
		forward = append(forward, "--home", home)
		ctx := context.Background()
		if (args[0] == "update" || args[0] == "remove") && !strings.Contains(strings.Join(args, " "), "--watch") {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 2*time.Hour)
			defer cancel()
		}
		return cliExit(run(ctx, stdout, stderr, python, forward...), stderr)
	}
	if len(args) != 1 || (args[0] != "install" && args[0] != "uninstall" && args[0] != "status" && args[0] != "start" && args[0] != "stop" && args[0] != "restart") {
		fmt.Fprint(stderr, cliUsage)
		return 2
	}
	if runtime.GOOS != "linux" {
		return cliExit(errors.New("service commands require Linux and user systemd"), stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if args[0] == "install" || args[0] == "uninstall" {
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
		forward := []string{installer}
		if args[0] == "uninstall" {
			forward = append(forward, "--uninstall")
		}
		return cliExit(run(ctx, stdout, stderr, env("NULAS_PYTHON", "python3"), forward...), stderr)
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
	if args[0] == "start" || args[0] == "restart" {
		addr, err := serverAddress()
		if err != nil {
			fmt.Fprintf(stderr, "无法读取浏览器入口：%v；请检查 nulas config port。\n", err)
		} else {
			fmt.Fprintf(stdout, "浏览器入口（当前 CLI 配置）：http://%s/\n", addr)
		}
		fmt.Fprintln(stdout, "请访问 Web/API 入口；SSR 端口仅供内部页面渲染，直接访问会导致 API 请求失败。")
		fmt.Fprintln(stdout, "若 service.env 覆盖了端口，请以服务启动日志为准；服务运行状态可用 nulas status 查看。")
	}
	return 0
}

func runOpen(platform string, stdout, stderr io.Writer, run cliRunner) int {
	addr, err := serverAddress()
	if err != nil {
		return cliExit(err, stderr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return cliExit(fmt.Errorf("invalid Nulas browser address: %w", err), stderr)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strings.ContainsAny(host, "/\\?#@%\"' \t\r\n") {
		return cliExit(errors.New("invalid Nulas browser address"), stderr)
	}
	// A wildcard listen address is not a browser destination.
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "::1"
	}
	url := "http://" + net.JoinHostPort(host, port) + "/"
	fmt.Fprintln(stdout, url)
	var name string
	var args []string
	switch platform {
	case "linux":
		name, args = "xdg-open", []string{url}
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32.exe", []string{"url.dll,FileProtocolHandler", url}
	default:
		return cliExit(fmt.Errorf("opening a browser is unsupported on %s; open the URL manually", platform), stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := run(ctx, stdout, stderr, name, args...); err != nil {
		return cliExit(fmt.Errorf("could not open the default browser; open the URL manually (a desktop session is required): %w", err), stderr)
	}
	fmt.Fprintln(stdout, "Browser open requested. Nulas must already be running.")
	return 0
}
