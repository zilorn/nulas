package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type CoreStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func corePath() string {
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(env("NULAS_CORE_DIR", "../.runtime/core"), name)
}
func coreInstalled() (bool, error) {
	info, err := os.Stat(corePath())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
		return false, errors.New("Core path exists but is not executable; inspect it before retrying")
	}
	return true, nil
}

// Caller holds the state lock.
func (a *App) coreStatus() CoreStatus {
	installed, err := coreInstalled()
	if err != nil {
		return CoreStatus{"failed", err.Error()}
	}
	for i := len(a.state.Jobs) - 1; i >= 0; i-- {
		j := a.state.Jobs[i]
		if j.Action != "install-core" {
			continue
		}
		if j.Status == "queued" || j.Status == "running" {
			return CoreStatus{"installing", j.Message}
		}
		if j.Status == "failed" && a.managedContext != nil {
			return CoreStatus{"failed", j.Message}
		}
		if installed {
			break
		}
		if j.Status == "failed" {
			return CoreStatus{"failed", j.Message}
		}
		break
	}
	if a.coreRuntime.Status != "" {
		return a.coreRuntime
	}
	if installed {
		return CoreStatus{"installed", "内核已安装；请单独启动并配置控制接口。"}
	}
	return CoreStatus{"missing", "尚未安装内核"}
}
func (a *App) ensureCore(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if err := decode(w, r, &body); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	code, err := a.queueCore()
	if err != nil {
		fail(w, code, err)
		return
	}
	reply(w, code, a.coreStatus())
}

// Caller holds the state lock. Failed/interrupted jobs require an explicit retry.
func (a *App) queueCore() (int, error) {
	status := a.coreStatus()
	if status.Status != "missing" && !(status.Status == "installed" && a.managedContext != nil) {
		return 200, nil
	}
	if len(a.state.Jobs) >= 1000 {
		return 409, errors.New("job history limit reached")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return 500, err
	}
	a.state.Jobs = append(a.state.Jobs, Job{ID: hex.EncodeToString(id), Action: "install-core", Status: "queued", Message: "Waiting to install and start Mihomo core", Created: time.Now().UTC(), Config: a.state.Config})
	if err := a.persist(); err != nil {
		a.state.Jobs = a.state.Jobs[:len(a.state.Jobs)-1]
		return 500, err
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return 202, nil
}

func (a *App) installCore() error {
	installed, err := coreInstalled()
	if err != nil || installed {
		return err
	}
	parent := a.managedContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, env("NULAS_PYTHON", "python3"), env("NULAS_CORE_INSTALLER", "../scripts/install_core.py"), "--output", filepath.Dir(corePath()))
	output := &limitedOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Core installation failed: %v: %s", err, strings.TrimSpace(string(output.data)))
	}
	installed, err = coreInstalled()
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("Installer completed without an executable core")
	}
	return nil
}

type limitedOutput struct{ data []byte }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 8192 - len(b.data); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
