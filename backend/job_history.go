package main

import (
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const jobHistoryLimit = 1000
const generatedFileLimit = 10

func completedJob(j Job) bool { return j.Status == "succeeded" || j.Status == "failed" }

// Keep active snapshots and the latest network outcomes (manual retry policy),
// then fill remaining slots with recent completed metadata. Never mutate the
// original slice: callers must be able to roll back a failed atomic write.
func compactJobs(jobs []Job) []Job {
	keep := make([]bool, len(jobs))
	count := 0
	network := map[string]bool{}
	for i := len(jobs) - 1; i >= 0; i-- {
		j := jobs[i]
		feature := ""
		switch j.Action {
		case "proxy-enable", "proxy-disable":
			feature = "proxy"
		case "tun-enable", "tun-disable":
			feature = "tun"
		}
		if !completedJob(j) || (feature != "" && !network[feature]) {
			keep[i] = true
			count++
		}
		if feature != "" {
			network[feature] = true
		}
	}
	for i := len(jobs) - 1; i >= 0 && count < jobHistoryLimit; i-- {
		if !keep[i] {
			keep[i] = true
			count++
		}
	}
	result := make([]Job, 0, count)
	for i, j := range jobs {
		if !keep[i] {
			continue
		}
		if completedJob(j) {
			j.Document = ""
			j.RefreshURL = ""
		}
		result = append(result, j)
	}
	return result
}

func (a *App) jobIndex(id string) int {
	for i, j := range a.state.Jobs {
		if j.ID == id {
			return i
		}
	}
	panic("active job missing from state")
}

// Only remove files in the reserved random job-ID namespace, after the state
// commit succeeds. Unrecognized user files and active job outputs stay intact.
// Failed deletions are logged and retried on the next persistence/startup.
func (a *App) cleanupJobFiles() {
	keep := map[string]bool{}
	generated := 0
	for i := len(a.state.Jobs) - 1; i >= 0; i-- {
		j := a.state.Jobs[i]
		if !completedJob(j) {
			keep[j.ID+".yaml"] = true
		} else if j.Action == "generate" && j.Status == "succeeded" && generated < generatedFileLimit {
			keep[j.ID+".yaml"] = true
			generated++
		}
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		log.Printf("list job outputs: %v", err)
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || keep[name] || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		if len(id) != 32 || strings.ToLower(id) != id {
			continue
		}
		if _, err := hex.DecodeString(id); err != nil {
			continue
		}
		if err := os.Remove(filepath.Join(a.dir, name)); err != nil && !os.IsNotExist(err) {
			log.Printf("remove obsolete job output %s: %v", name, err)
		}
	}
}
