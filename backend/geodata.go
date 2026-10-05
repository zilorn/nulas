package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const mmdbDownloadLimit = 64 * 1024 * 1024

const defaultMMDBURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country.mmdb"

var geoIPRule = regexp.MustCompile(`(?:^|[,(])\s*GEOIP\s*,`)

func needsMMDB(fields map[string]any) bool {
	if fields["geodata-mode"] == true {
		return false
	}
	var hasRule func(any) bool
	hasRule = func(value any) bool {
		switch v := value.(type) {
		case string:
			return geoIPRule.MatchString(v)
		case []any:
			for _, entry := range v {
				if hasRule(entry) {
					return true
				}
			}
		case map[string]any:
			for _, entry := range v {
				if hasRule(entry) {
					return true
				}
			}
		}
		return false
	}
	if hasRule(fields["rules"]) || hasRule(fields["sub-rules"]) {
		return true
	}
	if dns, ok := fields["dns"].(map[string]any); ok && dns["enable"] == true {
		fallback, _ := dns["fallback"].([]any)
		filter, _ := dns["fallback-filter"].(map[string]any)
		// Mihomo defaults fallback-filter.geoip to true.
		return len(fallback) > 0 && filter["geoip"] != false
	}
	return false
}

// Mirrors are used only for exact, public upstream assets. Never send a custom
// download URL (which may contain credentials) to a mirror or substitute its data.
func mmdbSources(fields map[string]any) []string {
	source := defaultMMDBURL
	if urls, ok := fields["geox-url"].(map[string]any); ok {
		if value, ok := urls["mmdb"].(string); ok {
			source = value
		}
	}
	for _, asset := range []string{"country.mmdb", "geoip.metadb", "geoip.db"} {
		for _, known := range []string{
			"https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/" + asset,
			"https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/release/" + asset,
			"https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + asset,
			"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + asset,
		} {
			if source == known {
				result := []string{source}
				for _, mirror := range []string{
					"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + asset,
					"https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + asset,
				} {
					if mirror != source {
						result = append(result, mirror)
					}
				}
				return result
			}
		}
	}
	return []string{source}
}

func (a *App) prepareManagedMMDB(content string) error {
	if a.managedContext == nil {
		return nil
	}
	fields, err := parseFullDocument(content)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.managedContext, 2*time.Minute)
	defer cancel()
	return prepareMMDB(ctx, filepath.Join(a.dir, "managed-core"), fields, fetchMMDB, verifyMMDB)
}

func prepareMMDB(ctx context.Context, dir string, fields map[string]any,
	fetch func(context.Context, string) ([]byte, error), verify func(context.Context, string, []byte) error) error {
	if !needsMMDB(fields) {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		for _, name := range []string{"country.mmdb", "geoip.metadb", "geoip.db"} {
			if strings.EqualFold(entry.Name(), name) {
				// Verify without modifying the live file. An incomplete core download
				// must not bypass preparation or be silently overwritten.
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() || info.Size() > mmdbDownloadLimit {
					return errors.New("existing MMDB is not a regular file within 64 MiB; inspect the managed-core data directory before retrying")
				}
				data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
				if err != nil {
					return err
				}
				if err = verify(ctx, dir, data); err != nil {
					return errors.New("existing MMDB failed validation; move the invalid Country.mmdb / geoip.metadb / geoip.db out of the managed-core data directory, then retry applying manually")
				}
				return nil
			}
		}
	}
	var last error
	for _, source := range mmdbSources(fields) {
		for attempt := 0; attempt < 2; attempt++ {
			if err := ctx.Err(); err != nil {
				return mmdbPreparationError(err)
			}
			data, err := fetch(ctx, source)
			if err == nil {
				err = verify(ctx, dir, data)
			}
			if err == nil {
				if err := ctx.Err(); err != nil {
					return mmdbPreparationError(err)
				}
				return publishMMDB(dir, data)
			}
			last = err
		}
	}
	return mmdbPreparationError(last)
}

// Publish a complete synced file without replacing a cache that appeared while
// downloading. Hard links are atomic and keep the destination private (0600).
func publishMMDB(dir string, data []byte) error {
	f, err := os.CreateTemp(dir, ".mmdb-download-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), filepath.Join(dir, "Country.mmdb"))
}

func mmdbPreparationError(err error) error {
	return fmt.Errorf("MMDB preparation failed: %v; check network access or geox-url.mmdb, or place a compatible Country.mmdb in the managed-core data directory, then retry applying manually", err)
}

func fetchMMDB(parent context.Context, source string) ([]byte, error) {
	if _, err := validateImportURL(source); err != nil {
		return nil, errors.New("invalid MMDB download address")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		if _, err := validateImportURL(req.URL.String()); err != nil {
			return err
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade rejected")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, errors.New("invalid MMDB download address")
	}
	req.Header.Set("User-Agent", "clash.meta")
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("MMDB download connection failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MMDB download HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, mmdbDownloadLimit+1))
	if err != nil {
		return nil, errors.New("MMDB download was interrupted")
	}
	if len(data) == 0 || len(data) > mmdbDownloadLimit {
		return nil, errors.New("MMDB download is empty or exceeds 64 MiB")
	}
	return data, nil
}

// Use the installed core's own parser in an isolated directory. Test mode opens
// no proxy listeners; the empty source prevents a corrupt file triggering a download.
func verifyMMDB(parent context.Context, dir string, data []byte) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".mmdb-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, "Country.mmdb"), data, 0600); err != nil {
		return err
	}
	config := filepath.Join(stage, "check.json")
	if err = os.WriteFile(config, []byte(`{"geodata-mode":false,"geox-url":{"mmdb":""},"rules":["GEOIP,CN,DIRECT"],"log-level":"silent"}`), 0600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	binary, err := filepath.Abs(corePath())
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binary, "-t", "-d", stage, "-f", config)
	if err = cmd.Run(); err != nil {
		return errors.New("downloaded MMDB failed Mihomo validation")
	}
	return nil
}
