package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// Capture startup intent before serving requests. Failed, interrupted and pending
// operations require their existing recovery path, never a new automatic retry.
func (a *App) prepareNetworkRestore() {
	a.networkRestore = map[string]string{}
	for feature, saved := range map[string]*bool{"tun": a.state.Preferences.TUN, "proxy": a.state.Preferences.Proxy} {
		if saved == nil || !*saved {
			continue
		}
		last := a.latestNetworkJob(feature)
		if last == nil {
			a.networkRestore[feature] = ""
		} else if last.Action == feature+"-enable" && last.Status == "succeeded" {
			a.networkRestore[feature] = last.ID
		}
	}
}

// Caller holds mu (or is loading state).
func (a *App) latestNetworkJob(feature string) *Job {
	for i := len(a.state.Jobs) - 1; i >= 0; i-- {
		if strings.HasPrefix(a.state.Jobs[i].Action, feature+"-") {
			return &a.state.Jobs[i]
		}
	}
	return nil
}

// Called by the single worker after startup jobs, including core readiness.
// Persist fresh restoration jobs before performing any networking operations.
func (a *App) queueNetworkRestore() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.networkRestore) == 0 {
		return nil
	}
	for _, j := range a.state.Jobs {
		if j.Status == "queued" || j.Status == "running" {
			return nil
		}
	}
	var jobs []Job
	for _, feature := range []string{"tun", "proxy"} {
		baseline, wanted := a.networkRestore[feature]
		if !wanted {
			continue
		}
		last := a.latestNetworkJob(feature)
		if last != nil && last.ID != baseline {
			continue // A more recent user operation takes precedence.
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return err
		}
		jobs = append(jobs, Job{ID: hex.EncodeToString(id), Action: feature + "-enable", Restore: true, Status: "queued", Message: "等待启动恢复已保存的开启偏好", Created: time.Now().UTC(), Config: a.state.Config})
	}
	if len(jobs) == 0 {
		a.networkRestore = nil
		return nil
	}
	previous := len(a.state.Jobs)
	a.state.Jobs = append(a.state.Jobs, jobs...)
	if err := a.persist(); err != nil {
		a.state.Jobs = a.state.Jobs[:previous]
		return err
	}
	a.networkRestore = nil
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return nil
}
