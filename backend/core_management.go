package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

var stableCoreTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

type coreRelease struct {
	Tag        string `json:"tag_name"`
	Published  string `json:"published_at"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func (a *App) fetchCoreReleases(ctx context.Context, endpoint string, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/MetaCubeX/mihomo/releases"+endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Nulas-core-manager")
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := a.client.Do(req)
	if err != nil {
		return errors.New("无法读取官方版本，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("官方版本读取失败（HTTP %d），可能已达到 GitHub 请求限额", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 5*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 5*1024*1024 {
		return errors.New("Release response exceeds size limit")
	}
	return json.Unmarshal(data, target)
}
func (a *App) coreReleases(w http.ResponseWriter, r *http.Request) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			fail(w, 400, errors.New("invalid page"))
			return
		}
		page = n
	}
	var releases []coreRelease
	if err := a.fetchCoreReleases(r.Context(), fmt.Sprintf("?per_page=20&page=%d", page), &releases); err != nil {
		fail(w, 502, err)
		return
	}
	stable := []coreRelease{}
	for _, release := range releases {
		if stableCoreTag.MatchString(release.Tag) && !release.Prerelease && !release.Draft {
			stable = append(stable, release)
		}
	}
	reply(w, 200, map[string]any{"releases": stable, "hasMore": len(releases) == 20})
}
func (a *App) coreManagement(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tag := ""
	data, err := os.ReadFile(filepath.Join(filepath.Dir(corePath()), "release.json"))
	if err == nil {
		var metadata struct {
			Tag string `json:"tag"`
		}
		if json.Unmarshal(data, &metadata) == nil {
			tag = metadata.Tag
		}
	}
	pending := false
	for _, j := range a.state.Jobs {
		if (j.Action == "switch-core" || j.Action == "install-core") && (j.Status == "queued" || j.Status == "running") {
			pending = true
		}
	}
	reply(w, 200, map[string]any{"version": tag, "core": a.coreStatus(), "managed": a.managedContext != nil, "busy": pending, "platform": runtime.GOOS + "/" + runtime.GOARCH})
}
func (a *App) queueCoreSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, 400, err)
		return
	}
	if body.Version != "latest" && !stableCoreTag.MatchString(body.Version) {
		fail(w, 400, errors.New("请选择官方稳定版本"))
		return
	}
	a.mu.Lock()
	managed := a.managedContext != nil
	a.mu.Unlock()
	if !managed {
		fail(w, 409, errors.New("仅支持 Nulas 托管内核；外部控制器需自行更新"))
		return
	}
	// Resolve latest before persisting so a queued job always selects the reviewed release.
	if body.Version == "latest" {
		var release coreRelease
		if err := a.fetchCoreReleases(r.Context(), "/latest", &release); err != nil {
			fail(w, 502, err)
			return
		}
		if !stableCoreTag.MatchString(release.Tag) || release.Prerelease || release.Draft {
			fail(w, 502, errors.New("未找到官方稳定版本"))
			return
		}
		body.Version = release.Tag
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, j := range a.state.Jobs {
		if (j.Action == "switch-core" || j.Action == "install-core") && (j.Status == "queued" || j.Status == "running") {
			fail(w, 409, errors.New("已有内核任务正在执行"))
			return
		}
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		fail(w, 500, err)
		return
	}
	j := Job{ID: hex.EncodeToString(id), Action: "switch-core", CoreVersion: body.Version, Status: "queued", Message: "等待下载、校验并切换内核", Created: time.Now().UTC(), Config: a.state.Config}
	a.state.Jobs = append(a.state.Jobs, j)
	if err := a.persist(); err != nil {
		a.state.Jobs = a.state.Jobs[:len(a.state.Jobs)-1]
		fail(w, 500, err)
		return
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	reply(w, 202, j)
}
func (a *App) stopManagedCore() error {
	a.mu.Lock()
	cmd, done := a.coreCommand, a.coreDone
	a.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("等待原内核退出超时")
	}
}
func (a *App) switchCore(j Job) error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	if !stableCoreTag.MatchString(j.CoreVersion) || a.managedContext == nil {
		return errors.New("Invalid managed core switch")
	}
	dir := env("NULAS_CORE_DIR", "../.runtime/core")
	if err := os.MkdirAll(filepath.Join(dir, "versions"), 0700); err != nil {
		return err
	}
	target := filepath.Join(dir, "versions", j.CoreVersion)
	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			return errors.New("Invalid cached core directory")
		}
		data, readErr := os.ReadFile(filepath.Join(target, "release.json"))
		if readErr != nil {
			return readErr
		}
		var release struct {
			Tag string `json:"tag"`
		}
		if json.Unmarshal(data, &release) != nil || release.Tag != j.CoreVersion {
			return errors.New("Cached release version mismatch")
		}
		ctx, cancel := context.WithTimeout(a.managedContext, 10*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, filepath.Join(target, filepath.Base(corePath())), "-v").Run(); err != nil {
			return fmt.Errorf("本地内核无法执行：%w", err)
		}
		return a.activateCore(j, dir)
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	// Download into a staging directory. Existing binaries and failed downloads remain separate.
	stage, err := os.MkdirTemp(filepath.Join(dir, "versions"), ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	ctx, cancel := context.WithTimeout(a.managedContext, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, env("NULAS_PYTHON", "python3"), env("NULAS_CORE_INSTALLER", "../scripts/install_core.py"), "--output", stage, "--version", j.CoreVersion)
	output := &limitedOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("内核下载或校验失败：%v；%s", err, output.data)
	}
	binary := filepath.Join(stage, filepath.Base(corePath()))
	probeCtx, probeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer probeCancel()
	probe := exec.CommandContext(probeCtx, binary, "-v")
	probe.Stdout, probe.Stderr = output, output
	if err = probe.Run(); err != nil {
		return fmt.Errorf("下载的内核无法执行：%w", err)
	}
	metadata, err := os.ReadFile(filepath.Join(stage, "release.json"))
	if err != nil {
		return err
	}
	var release struct {
		Tag string `json:"tag"`
	}
	if json.Unmarshal(metadata, &release) != nil || release.Tag != j.CoreVersion {
		return errors.New("Downloaded release version mismatch")
	}
	// Publish only a complete verified download; preserve older version directories.
	if err = os.Rename(stage, target); err != nil {
		return err
	}
	return a.activateCore(j, dir)
}
func (a *App) activateCore(j Job, dir string) error {
	if err := a.managedContext.Err(); err != nil {
		return err
	}
	pointer := filepath.Join(dir, "active-version")
	previous, err := os.ReadFile(pointer)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(previous) > 0 && !stableCoreTag.Match(previous) {
		return errors.New("Invalid active core version; inspect it before switching")
	}
	if err = a.stopManagedCore(); err != nil {
		return err
	}
	if err = atomicWrite(pointer, []byte(j.CoreVersion)); err != nil {
		restartErr := a.startCore(j.Config)
		return fmt.Errorf("版本保存失败：%v；原内核恢复：%v", err, restartErr)
	}
	if err = a.startCore(j.Config); err != nil {
		if restoreErr := atomicWrite(pointer, previous); restoreErr != nil {
			return fmt.Errorf("新内核启动失败：%v；恢复版本失败：%w", err, restoreErr)
		}
		restartErr := a.startCore(j.Config)
		return fmt.Errorf("新内核启动失败：%v；原版本恢复结果：%v", err, restartErr)
	}
	return nil
}
