package main

import (
	"encoding/json"
	"time"
)

// Document is private. The snapshot is independent of both the editor and library.
type AppliedConfig struct {
	Profile
	AppliedAt time.Time `json:"appliedAt"`
	JobID     string    `json:"jobId"`
}

// Caller holds the state lock (or is loading state before serving requests).
func (a *App) appliedSnapshot(j Job) *AppliedConfig {
	p := Profile{ID: j.ProfileID, Name: "快速配置", Config: j.Config, Source: "applied", Created: j.Created, Full: j.Document != "", Document: j.Document}
	for _, saved := range a.state.Profiles {
		if saved.ID == j.ProfileID {
			p.Name, p.Source, p.Created = saved.Name, saved.Source, saved.Created
			break
		}
	}
	if j.Document != "" {
		// Full application forces loopback listeners.
		p.Config.LAN = false
	} else if a.state.Applied != nil && a.state.Applied.Full {
		// PATCH changes core settings only; preserve the active nodes/rules/DNS.
		p.Full, p.Document = true, a.state.Applied.Document
		p.Name += "（保留完整配置）"
	}
	return &AppliedConfig{Profile: p, AppliedAt: time.Now().UTC(), JobID: j.ID}
}

// Build the managed process's startup file, rather than replaying an apply job.
// Keep the same safe listeners and fresh server-only controller credentials.
func (a *App) managedStartupConfig(c Config, secret string) (map[string]any, error) {
	fields := map[string]any{"rules": []string{"MATCH,DIRECT"}}
	if applied := a.state.Applied; applied != nil {
		c = applied.Config
		if applied.Document != "" {
			if err := validateApplyPorts(applied.Document, "http://127.0.0.1:9090"); err != nil {
				return nil, err
			}
			payload, err := fullApplyPayload(applied.Document, applied.JobID)
			if err != nil {
				return nil, err
			}
			fields, err = parseFullDocument(string(payload))
			if err != nil {
				return nil, err
			}
		}
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	for key, value := range settings {
		fields[key] = value
	}
	fields["allow-lan"], fields["bind-address"] = false, "127.0.0.1"
	fields["external-controller"], fields["secret"] = "127.0.0.1:9090", secret
	fields["tun"] = map[string]any{"enable": false}
	return fields, nil
}
