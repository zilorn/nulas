package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

func validateUpdateInterval(hours int) error {
	if hours < 0 || hours > 720 {
		return errors.New("更新间隔须为 0（关闭）或 1–720 小时")
	}
	return nil
}

func nextProfileUpdate(now time.Time, hours int) *time.Time {
	if hours == 0 {
		return nil
	}
	next := now.Add(time.Duration(hours) * time.Hour)
	return &next
}

func (a *App) profileUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/profiles/{id}/updates", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL                 *string `json:"url"`
			UpdateIntervalHours int     `json:"updateIntervalHours"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if err := validateUpdateInterval(body.UpdateIntervalHours); err != nil {
			fail(w, 400, err)
			return
		}
		if body.URL != nil {
			*body.URL = strings.TrimSpace(*body.URL)
			if _, err := validateImportURL(*body.URL); err != nil {
				fail(w, 400, err)
				return
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		for i, p := range a.state.Profiles {
			if p.ID != r.PathValue("id") {
				continue
			}
			if p.Source != "network" {
				fail(w, 409, errors.New("仅网络导入配置支持更新"))
				return
			}
			if body.URL != nil {
				p.RefreshURL = *body.URL
			}
			if p.RefreshURL == "" {
				fail(w, 400, errors.New("旧配置未保存更新地址，请重新填写"))
				return
			}
			p.UpdateIntervalHours = body.UpdateIntervalHours
			p.NextUpdate = nextProfileUpdate(time.Now().UTC(), body.UpdateIntervalHours)
			old := a.state.Profiles[i]
			a.state.Profiles[i] = p
			if err := a.persist(); err != nil {
				a.state.Profiles[i] = old
				fail(w, 500, err)
				return
			}
			reply(w, 200, publicProfile(p))
			return
		}
		fail(w, 404, errors.New("配置不存在"))
	})
	mux.HandleFunc("POST /api/profiles/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		for i, p := range a.state.Profiles {
			if p.ID != r.PathValue("id") {
				continue
			}
			if p.RefreshURL == "" {
				fail(w, 409, errors.New("请先设置网络更新地址"))
				return
			}
			job, err := a.queueProfileRefresh(i, time.Now().UTC())
			if err != nil {
				fail(w, 409, err)
				return
			}
			reply(w, 202, publicJob(job))
			return
		}
		fail(w, 404, errors.New("配置不存在"))
	})
}

// Caller holds a.mu. Persist the private URL snapshot and next attempt together
// before the worker can download. At most one background operation is active.
func (a *App) queueProfileRefresh(index int, now time.Time) (Job, error) {
	for _, j := range a.state.Jobs {
		if j.Status == "queued" || j.Status == "running" {
			return Job{}, errors.New("已有后台任务，请等待完成后重试")
		}
	}
	p := a.state.Profiles[index]
	if _, err := validateImportURL(p.RefreshURL); err != nil {
		return Job{}, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return Job{}, err
	}
	oldJobs := a.state.Jobs
	j := Job{ID: hex.EncodeToString(id), Action: "refresh-profile", Status: "queued", Message: "等待更新配置", Created: now, ProfileID: p.ID, RefreshURL: p.RefreshURL}
	a.state.Jobs = append(append([]Job(nil), oldJobs...), j)
	a.state.Profiles[index].NextUpdate = nextProfileUpdate(now, p.UpdateIntervalHours)
	a.state.Profiles[index].RefreshStatus = "queued"
	a.state.Profiles[index].RefreshMessage = j.Message
	if err := a.persist(); err != nil {
		a.state.Jobs = oldJobs
		a.state.Profiles[index] = p
		return Job{}, errors.New("保存更新任务失败，未执行下载")
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return j, nil
}

func (a *App) scheduleProfileUpdates(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, j := range a.state.Jobs {
		if j.Status == "queued" || j.Status == "running" {
			return
		}
	}
	index := -1
	for i, p := range a.state.Profiles {
		if p.RefreshURL == "" || p.UpdateIntervalHours <= 0 || p.UpdateIntervalHours > 720 || p.NextUpdate == nil || p.NextUpdate.After(now) {
			continue
		}
		if index < 0 || p.NextUpdate.Before(*a.state.Profiles[index].NextUpdate) {
			index = i
		}
	}
	if index >= 0 {
		if _, err := a.queueProfileRefresh(index, now); err != nil {
			old := a.state.Profiles[index]
			a.state.Profiles[index].RefreshStatus = "failed"
			a.state.Profiles[index].RefreshMessage = err.Error()
			a.state.Profiles[index].NextUpdate = nextProfileUpdate(now, old.UpdateIntervalHours)
			if a.persist() != nil {
				a.state.Profiles[index] = old
			}
		}
	}
}
