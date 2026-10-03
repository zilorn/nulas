package main

import "time"

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
	if j.Document == "" && a.state.Applied != nil && a.state.Applied.Full {
		// PATCH changes core settings only; preserve the active nodes/rules/DNS.
		p.Full, p.Document = true, a.state.Applied.Document
		p.Name += "（保留完整配置）"
	}
	return &AppliedConfig{Profile: p, AppliedAt: time.Now().UTC(), JobID: j.ID}
}

// Build the managed process's startup file, rather than replaying an apply job.
// Restore the selected proxy listener and fresh server-only controller credentials.
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
	for key, value := range coreSettings(c) {
		fields[key] = value
	}
	fields["external-controller"], fields["secret"] = "127.0.0.1:9090", secret
	fields["tun"] = map[string]any{"enable": false}
	return fields, nil
}

// Proxy sharing never changes the controller or dashboard listener.
func proxyBindAddress(lan bool) string {
	if lan {
		return "*"
	}
	return "127.0.0.1"
}

func coreSettings(c Config) map[string]any {
	return map[string]any{
		"mixed-port": c.Port, "mode": c.Mode, "allow-lan": c.LAN,
		"ipv6": c.IPv6, "log-level": c.Log, "bind-address": proxyBindAddress(c.LAN),
	}
}
