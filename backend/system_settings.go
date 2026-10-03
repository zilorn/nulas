package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const serviceUnit = "nulas.service"

type SystemFeature struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Recovery  bool   `json:"recovery"`
	Message   string `json:"message"`
}
type SystemSettings struct {
	Proxy   SystemFeature `json:"proxy"`
	Startup SystemFeature `json:"startup"`
}

func (a *App) command(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if a.systemCommand != nil {
		return a.systemCommand(ctx, name, args...)
	}
	// Commands are fixed system tools; no shell or user-supplied command is executed.
	output := &limitedOutput{}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	out := output.data
	if err != nil {
		return "", fmt.Errorf("%s 操作失败：%w", name, err)
	}
	if len(out) >= 8192 {
		return "", errors.New("系统命令返回内容过大")
	}
	return strings.TrimSpace(string(out)), nil
}
func (a *App) proxyGet(ctx context.Context, key string) (string, error) {
	parts := strings.SplitN(key, "/", 2)
	return a.command(ctx, "gsettings", "get", "org.gnome.system.proxy"+parts[0], parts[1])
}
func (a *App) proxySet(ctx context.Context, key, value string) error {
	parts := strings.SplitN(key, "/", 2)
	if _, err := a.command(ctx, "gsettings", "set", "org.gnome.system.proxy"+parts[0], parts[1], value); err != nil {
		return err
	}
	got, err := a.proxyGet(ctx, key)
	if err != nil {
		return err
	}
	if got != value {
		return fmt.Errorf("系统代理 %s 回读不一致", key)
	}
	return nil
}

var proxyKeys = []string{"/mode", ".http/host", ".http/port", ".http/use-authentication", ".https/host", ".https/port", ".socks/host", ".socks/port"}

func (a *App) proxyStatus(ctx context.Context) SystemFeature {
	s := SystemFeature{Message: "仅支持后端所在主机的 Linux GNOME 桌面代理；无桌面服务器不支持系统代理。"}
	if runtime.GOOS != "linux" || (a.systemCommand == nil && (os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" || os.Getenv("XDG_CURRENT_DESKTOP") == "")) {
		return s
	}
	mode, err := a.proxyGet(ctx, "/mode")
	if err != nil {
		s.Message = err.Error()
		return s
	}
	s.Supported = true
	a.mu.Lock()
	owned := len(a.state.ProxyBackup) > 0
	port := a.state.ProxyPort
	a.mu.Unlock()
	s.Recovery = owned
	s.Enabled = owned && mode == "'manual'"
	if s.Enabled {
		for _, key := range proxyKeys[1:] {
			value, err := a.proxyGet(ctx, key)
			if err != nil {
				s.Enabled = false
				s.Message = err.Error()
				return s
			}
			if (strings.HasSuffix(key, "/port") && value != strconv.Itoa(port)) || (strings.HasSuffix(key, "/use-authentication") && value != "false") || (strings.HasSuffix(key, "/host") && value != "'127.0.0.1'") {
				s.Enabled = false
			}
		}
	}
	s.Message = "修改后端用户的 GNOME 桌面代理，仅影响遵循此设置的应用；关闭会恢复开启前的设置。"
	if owned && !s.Enabled {
		s.Message = "已保存原设置，但系统代理被外部修改或操作中断；可关闭开关恢复。"
	}
	return s
}
func (a *App) startupStatus(ctx context.Context) SystemFeature {
	s := SystemFeature{Message: "需要 Linux 用户级 systemd 服务。先运行 python3 scripts/install_service.py 安装，再为服务用户启用 linger。"}
	if runtime.GOOS != "linux" {
		return s
	}
	loaded, err := a.command(ctx, "systemctl", "--user", "show", serviceUnit, "--property=LoadState", "--value")
	if err != nil || loaded != "loaded" {
		return s
	}
	description, err := a.command(ctx, "systemctl", "--user", "show", serviceUnit, "--property=Description", "--value")
	if err != nil || description != "Nulas frontend and backend" {
		s.Message = "发现同名服务，但不是 Nulas 前后端服务，请检查安装。"
		return s
	}
	enabled, err := a.command(ctx, "systemctl", "--user", "is-enabled", serviceUnit)
	registered := err == nil && enabled == "enabled"
	linger, err := a.command(ctx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value")
	s.Supported = err == nil && linger == "yes"
	s.Enabled = registered && s.Supported
	s.Recovery = registered && !s.Supported
	if s.Recovery {
		s.Message = "服务已注册自启，但尚未启用 linger，无法保证开机运行；可关闭开关取消注册。"
	}
	if s.Supported {
		s.Message = "开机时由用户级 systemd 同时启动 Go 后台与 Node 前端；关闭仅取消下次自启，不停止当前服务。"
	}
	return s
}
func (a *App) systemSettings(ctx context.Context) SystemSettings {
	return SystemSettings{Proxy: a.proxyStatus(ctx), Startup: a.startupStatus(ctx)}
}
func (a *App) systemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runtime/system", func(w http.ResponseWriter, r *http.Request) {
		a.controlMu.Lock()
		defer a.controlMu.Unlock()
		reply(w, 200, a.systemSettings(r.Context()))
	})
	mux.HandleFunc("PUT /api/runtime/system", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Startup *bool `json:"startup"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Startup == nil {
			fail(w, 400, errors.New("需要 startup 布尔值"))
			return
		}
		a.controlMu.Lock()
		defer a.controlMu.Unlock()
		if body.Startup != nil {
			status := a.startupStatus(r.Context())
			if !status.Supported && *body.Startup {
				fail(w, 409, errors.New(status.Message))
				return
			}
			action := "disable"
			if *body.Startup {
				action = "enable"
			}
			if _, err := a.command(r.Context(), "systemctl", "--user", action, serviceUnit); err != nil {
				fail(w, 502, err)
				return
			}
			if a.startupStatus(r.Context()).Enabled != *body.Startup {
				fail(w, 502, errors.New("自启状态回读不一致"))
				return
			}
		}
		reply(w, 200, a.systemSettings(r.Context()))
	})
}
func (a *App) setSystemProxy(enable bool) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if s := a.proxyStatus(ctx); !s.Supported {
		return errors.New(s.Message)
	}
	a.mu.Lock()
	backup := a.state.ProxyBackup
	controller := a.controller
	a.mu.Unlock()
	if !enable {
		if len(backup) == 0 {
			return errors.New("没有 Nulas 保存的代理快照，拒绝修改其他代理设置")
		}
		return a.restoreProxy(ctx, backup)
	}
	if len(backup) > 0 {
		return errors.New("已有系统代理快照，请先关闭恢复后重试")
	}
	u, err := url.Parse(controller)
	if err != nil || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return errors.New("系统代理仅支持本机回环控制器")
	}
	var config struct {
		Port int `json:"mixed-port"`
	}
	if err := a.controllerRequest(ctx, "GET", "/configs", nil, &config); err != nil {
		return err
	}
	if config.Port < 1 || config.Port > 65535 {
		return errors.New("内核未提供有效混合端口")
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(config.Port)))
	if err != nil {
		return errors.New("内核代理端口不可达，未更改系统代理")
	}
	conn.Close()
	backup = map[string]string{}
	for _, key := range proxyKeys {
		v, err := a.proxyGet(ctx, key)
		if err != nil {
			return err
		}
		backup[key] = v
	}
	// Persist recovery data before any external side effect. Interrupted jobs are never replayed.
	a.mu.Lock()
	a.state.ProxyBackup = backup
	a.state.ProxyPort = config.Port
	err = a.persist()
	if err != nil {
		a.state.ProxyBackup = nil
		a.state.ProxyPort = 0
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	for _, key := range proxyKeys[1:] {
		value := "'127.0.0.1'"
		if strings.HasSuffix(key, "/use-authentication") {
			value = "false"
		}
		if strings.HasSuffix(key, "/port") {
			value = strconv.Itoa(config.Port)
		}
		err = a.proxySet(ctx, key, value)
		if err != nil {
			break
		}
	}
	if err == nil {
		err = a.proxySet(ctx, "/mode", "'manual'")
	}
	if err != nil {
		rollbackCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if rollbackErr := a.restoreProxy(rollbackCtx, backup); rollbackErr != nil {
			return fmt.Errorf("开启失败：%v；恢复失败：%v，请关闭重试", err, rollbackErr)
		}
		return err
	}
	return nil
}
func (a *App) restoreProxy(ctx context.Context, backup map[string]string) error {
	// Disable while restoring endpoints, then restore the original mode last.
	if err := a.proxySet(ctx, "/mode", "'none'"); err != nil {
		return err
	}
	for _, key := range proxyKeys[1:] {
		v, ok := backup[key]
		if !ok {
			return errors.New("代理快照不完整，无法恢复")
		}
		if err := a.proxySet(ctx, key, v); err != nil {
			return err
		}
	}
	if err := a.proxySet(ctx, "/mode", backup["/mode"]); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	oldPort := a.state.ProxyPort
	a.state.ProxyBackup = nil
	a.state.ProxyPort = 0
	if err := a.persist(); err != nil {
		a.state.ProxyBackup = backup
		a.state.ProxyPort = oldPort
		return err
	}
	return nil
}
