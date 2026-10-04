package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const tunDevice = "Nulas"
const tunCapabilities uint64 = 1<<12 | 1<<13 // CAP_NET_ADMIN and CAP_NET_RAW

type TUNStatus struct {
	Supported bool     `json:"supported"`
	Ready     bool     `json:"ready"`
	Enabled   bool     `json:"enabled"`
	Message   string   `json:"message"`
	Commands  []string `json:"commands"`
}

type tunConfigResponse struct {
	TUN *struct {
		Enable bool   `json:"enable"`
		Device string `json:"device"`
	} `json:"tun"`
}

func effectiveCapabilities(status string) uint64 {
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			value, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "CapEff:")), 16, 64)
			return value
		}
	}
	return 0
}

// Inspect the running child, not file capabilities: setcap only takes effect on exec.
func (a *App) tunPermission() TUNStatus {
	s := TUNStatus{Commands: []string{}}
	a.mu.Lock()
	managed := a.managedContext != nil
	pid := 0
	if a.coreCommand != nil {
		pid = a.coreCommand.Process.Pid
	}
	a.mu.Unlock()
	if runtime.GOOS != "linux" || !managed {
		s.Message = "TUN 管理目前仅支持 Linux 托管内核；外部内核请在其所在主机配置。"
		return s
	}
	s.Supported = true
	binary, err := filepath.Abs(corePath())
	if err != nil {
		s.Message = "无法确定内核路径"
		return s
	}
	// Single quotes safely preserve spaces and shell metacharacters in deployment paths.
	quoted := "'" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "'"
	s.Commands = []string{"sudo setcap cap_net_admin,cap_net_raw+ep " + quoted, "getcap " + quoted}
	if pid == 0 {
		s.Message = "请先启动托管内核。"
		return s
	}
	device, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		s.Message = "无法访问 /dev/net/tun，请在主机检查 TUN 设备与访问权限。"
		return s
	}
	device.Close()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil || effectiveCapabilities(string(data))&tunCapabilities != tunCapabilities {
		s.Message = "内核缺少网络权限。执行下方命令后，在启动服务的终端按 Ctrl+C，再以普通用户重新启动原服务；返回点击“检查并开启”。请勿以 root 运行整个网页服务。"
		return s
	}
	s.Ready = true
	s.Commands = []string{}
	s.Message = "内核权限已就绪。开启后将创建 TUN 网卡并接管本机路由；初始配置仅直连，节点与规则沿用当前内核配置。"
	return s
}

func (a *App) tunStatus(ctx context.Context) (TUNStatus, error) {
	s := a.tunPermission()
	if !s.Supported {
		return s, nil
	}
	a.mu.Lock()
	connected := a.controller != ""
	a.mu.Unlock()
	if !connected {
		return s, nil
	}
	var config tunConfigResponse
	if err := a.controllerRequest(ctx, "GET", "/configs", nil, &config); err != nil {
		return s, err
	}
	if config.TUN == nil {
		return s, errors.New("内核未返回 TUN 状态")
	}
	if !config.TUN.Enable && config.TUN.Device != "" {
		if iface, err := a.lookupTUNInterface(config.TUN.Device); err == nil && iface.Flags&net.FlagUp != 0 {
			return s, errors.New("TUN 配置已关闭，但网卡仍在运行，请检查内核日志")
		}
	}
	if config.TUN.Enable {
		iface, err := a.lookupTUNInterface(config.TUN.Device)
		deviceExists := a.hasTUNDevice(tunDevice)
		if config.TUN.Device != tunDevice || !deviceExists || err != nil || iface.Flags&net.FlagUp == 0 {
			return s, errors.New("内核报告 TUN 已开启，但未检测到运行中的 TUN 网卡，请检查内核日志")
		}
		s.Enabled = true
		s.Message = "TUN 已开启，内核配置与网卡已检查。节点与规则沿用当前内核配置；不劫持 DNS，不修改防火墙。"
	}
	return s, nil
}

func (a *App) tunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runtime/tun", func(w http.ResponseWriter, r *http.Request) {
		a.controlMu.Lock()
		defer a.controlMu.Unlock()
		status, err := a.tunStatus(r.Context())
		if err != nil {
			fail(w, 502, err)
			return
		}
		reply(w, 200, status)
	})
}

func (a *App) setTUN(enable bool) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	s := a.tunPermission()
	if !s.Supported {
		return errors.New(s.Message)
	}
	if enable && !s.Ready {
		return errors.New(s.Message)
	}
	ctx := a.managedContext
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return a.applyTUN(ctx, enable)
}

// Caller holds controlMu; kept separate so failed listener creation can be tested without privileges.
func (a *App) applyTUN(ctx context.Context, enable bool) error {
	// Disable only changes enable; it leaves all unrelated core settings intact.
	settings := map[string]any{"enable": enable}
	if enable {
		settings["device"] = tunDevice
		settings["stack"] = "gvisor"
		settings["auto-route"] = true
		settings["auto-detect-interface"] = true
		settings["auto-redirect"] = false
		settings["dns-hijack"] = []string{}
	}
	if err := a.controllerRequest(ctx, "PATCH", "/configs", map[string]any{"tun": settings}, nil); err != nil {
		return err
	}
	for {
		s, err := a.tunStatus(ctx)
		if err == nil && s.Enabled == enable {
			return nil
		}
		select {
		case <-ctx.Done():
			if enable {
				// PATCH may report success even when listener creation failed. Undo the request.
				rollback, stop := context.WithTimeout(context.Background(), 3*time.Second)
				rollbackErr := a.controllerRequest(rollback, "PATCH", "/configs", map[string]any{"tun": map[string]bool{"enable": false}}, nil)
				stop()
				if rollbackErr != nil {
					return fmt.Errorf("TUN 开启检查失败且关闭请求失败：%v；请检查内核与主机网络", rollbackErr)
				}
			}
			return errors.New("TUN 状态检查超时；请检查内核日志与主机网络，不能确认操作成功")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Per-app probes keep runtime verification real while allowing isolated tests.
func (a *App) lookupTUNInterface(name string) (*net.Interface, error) {
	if a.tunInterface != nil {
		return a.tunInterface(name)
	}
	return net.InterfaceByName(name)
}

func (a *App) hasTUNDevice(name string) bool {
	if a.tunDeviceExists != nil {
		return a.tunDeviceExists(name)
	}
	_, err := os.Stat(filepath.Join("/sys/class/net", name, "tun_flags"))
	return err == nil
}
