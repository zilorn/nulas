package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mmdbFields(t *testing.T, content string) map[string]any {
	t.Helper()
	fields, err := parseFullDocument(content)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestMMDBRequirementsAndSources(t *testing.T) {
	for _, tc := range []struct {
		content string
		want    bool
	}{
		{"rules: ['GEOIP,telegram,Telegram,no-resolve']", true},
		{"rules: ['AND,((NETWORK,UDP),(GEOIP,CN)),DIRECT']", true},
		{"sub-rules: {telegram: ['GEOIP,telegram,DIRECT']}", true},
		{"dns: {enable: true, fallback: [8.8.8.8]}", true},
		{"dns: {enable: true, fallback: [8.8.8.8], fallback-filter: {geoip: false}}", false},
		{"dns: {enable: false, fallback: [8.8.8.8]}", false},
		{"geodata-mode: true\nrules: ['GEOIP,telegram,DIRECT']", false},
		{"rules: ['DOMAIN,example.com,DIRECT', 'MATCH,DIRECT']", false},
	} {
		if got := needsMMDB(mmdbFields(t, tc.content)); got != tc.want {
			t.Fatalf("%s: got %v", tc.content, got)
		}
	}
	for _, source := range []string{"https://private.invalid/db?token=secret", defaultMMDBURL + "?token=secret", ""} {
		fields := map[string]any{"geox-url": map[string]any{"mmdb": source}}
		sources := mmdbSources(fields)
		if len(sources) != 1 || sources[0] != source {
			t.Fatal("custom source was replaced or sent to a mirror")
		}
	}
	if sources := mmdbSources(map[string]any{}); len(sources) != 3 || sources[0] != defaultMMDBURL {
		t.Fatal(sources)
	}
}

func TestPrepareMMDBRetriesValidatesAndPreservesFiles(t *testing.T) {
	fields := mmdbFields(t, "rules: ['GEOIP,telegram,DIRECT']")
	ctx := context.Background()
	dir := t.TempDir()
	var addresses []string
	fetch := func(_ context.Context, address string) ([]byte, error) {
		addresses = append(addresses, address)
		if len(addresses) <= 2 {
			return nil, errors.New("EOF")
		}
		return []byte("valid mmdb"), nil
	}
	verify := func(_ context.Context, _ string, data []byte) error {
		if string(data) != "valid mmdb" {
			return errors.New("invalid")
		}
		return nil
	}
	if err := prepareMMDB(ctx, dir, fields, fetch, verify); err != nil {
		t.Fatal(err)
	}
	if len(addresses) != 3 || addresses[0] != addresses[1] || addresses[2] == addresses[0] {
		t.Fatal("retry/fallback not used")
	}
	data, err := os.ReadFile(filepath.Join(dir, "Country.mmdb"))
	if err != nil || string(data) != "valid mmdb" {
		t.Fatal("validated file was not installed")
	}
	if err := prepareMMDB(ctx, dir, fields, fetch, verify); err != nil || len(addresses) != 3 {
		t.Fatal("existing valid database downloaded again")
	}
	if err := os.WriteFile(filepath.Join(dir, "Country.mmdb"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareMMDB(ctx, dir, fields, fetch, verify); err == nil || !strings.Contains(err.Error(), "existing MMDB") {
		t.Fatal("invalid cache accepted")
	}
	data, _ = os.ReadFile(filepath.Join(dir, "Country.mmdb"))
	if string(data) != "partial" || len(addresses) != 3 {
		t.Fatal("existing runtime file overwritten")
	}

	dir = t.TempDir()
	if err := prepareMMDB(ctx, dir, fields, func(context.Context, string) ([]byte, error) { return []byte("html"), nil }, verify); err == nil {
		t.Fatal("invalid download accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("failed download published")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := prepareMMDB(cancelled, dir, fields, fetch, verify); err == nil || len(addresses) != 3 {
		t.Fatal("cancelled operation continued")
	}
}

func TestFetchMMDBBoundsAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "clash.meta" {
			t.Error("download headers wrong")
		}
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("database"))
		case "/partial":
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("partial"))
		case "/large":
			w.WriteHeader(200)
			block := make([]byte, 1024*1024)
			for i := 0; i < 65; i++ {
				if _, err := w.Write(block); err != nil {
					return
				}
			}
		case "/loop":
			http.Redirect(w, r, "/loop?token=private-token", 302)
		default:
			w.WriteHeader(502)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/ok", "/partial", "/large", "/loop", "/fail"} {
		data, err := fetchMMDB(context.Background(), server.URL+path+"?token=private-token")
		if path == "/ok" {
			if err != nil || string(data) != "database" {
				t.Fatal(err)
			}
		} else if err == nil || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), server.URL) {
			t.Fatalf("unsafe or missing error: %v", err)
		}
	}
}

func TestManagedMMDBFailurePreventsApplyAndIsDurable(t *testing.T) {
	var downloads, applies int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			applies++
		} else {
			downloads++
		}
		w.WriteHeader(502)
	}))
	defer server.Close()
	a := testApp(t, server.URL)
	a.managedContext = context.Background()
	document := fmt.Sprintf("rules: ['GEOIP,telegram,DIRECT']\ngeox-url: {mmdb: '%s/db?token=private-token'}", server.URL)
	p := createFullProfile(t, a, "content", document)
	if w := request(a, "POST", "/api/jobs", fmt.Sprintf(`{"action":"apply","profileId":%q}`, p.ID)); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if !a.process() || a.state.Jobs[0].Status != "failed" || a.state.Applied != nil || downloads != 2 || applies != 0 {
		t.Fatal("preparation failure not enforced")
	}
	b, err := newApp(a.dir, server.URL, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	response := request(b, "GET", "/api/jobs", "").Body.String()
	if !strings.Contains(response, "MMDB preparation failed") || strings.Contains(response, "private-token") || b.process() {
		t.Fatal("failure was hidden, leaked secrets, or replayed")
	}
}

func TestMMDBValidationUsesIsolatedTestMode(t *testing.T) {
	isolatedCore(t)
	if err := os.MkdirAll(filepath.Dir(corePath()), 0700); err != nil {
		t.Fatal(err)
	}
	source := `#!/usr/bin/env python3
import json, pathlib, sys
assert '-t' in sys.argv
directory = pathlib.Path(sys.argv[sys.argv.index('-d')+1])
config = json.load(open(sys.argv[sys.argv.index('-f')+1]))
assert config['geox-url']['mmdb'] == ''
assert config['rules'] == ['GEOIP,CN,DIRECT']
assert directory.name.startswith('.mmdb-check-')
assert 'secret' not in config and 'external-controller' not in config
sys.exit(0 if (directory/'Country.mmdb').read_bytes() == b'valid mmdb' else 1)
`
	if err := os.WriteFile(corePath(), []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := verifyMMDB(context.Background(), dir, []byte("valid mmdb")); err != nil {
		t.Fatal(err)
	}
	if err := verifyMMDB(context.Background(), dir, []byte("partial")); err == nil {
		t.Fatal("invalid database passed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("validation artifacts retained")
	}
}

func TestMMDBPublishPreservesConcurrentCache(t *testing.T) {
	dir := t.TempDir()
	if err := publishMMDB(dir, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := publishMMDB(dir, []byte("replacement")); err == nil {
		t.Fatal("existing file replaced")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "Country.mmdb"))
	entries, _ := os.ReadDir(dir)
	if string(data) != "original" || len(entries) != 1 {
		t.Fatal("original lost or staging file retained")
	}
}

func TestExternalMMDBRemainsControllerManaged(t *testing.T) {
	var applies int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Error("unexpected database request")
		}
		applies++
		w.WriteHeader(204)
	}))
	defer server.Close()
	a := testApp(t, server.URL)
	p := createFullProfile(t, a, "content", "rules: ['GEOIP,telegram,DIRECT']\ngeox-url: {mmdb: 'https://private.invalid/database'}")
	request(a, "POST", "/api/jobs", fmt.Sprintf(`{"action":"apply","profileId":%q}`, p.ID))
	if !a.process() || a.state.Jobs[0].Status != "succeeded" || applies != 1 {
		t.Fatal("external apply changed")
	}
	if _, err := os.Stat(filepath.Join(a.dir, "managed-core")); !os.IsNotExist(err) {
		t.Fatal("external core data directory created")
	}
}
