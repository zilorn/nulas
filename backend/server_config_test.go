package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedServerConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
	t.Setenv("NULAS_ADDR", "")
	path, err := serverConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPortConfigRoundTrip(t *testing.T) {
	path := isolatedServerConfig(t)
	var out, stderr bytes.Buffer
	if code := runCLI([]string{"config", "port"}, &out, &stderr, nil, nil); code != 0 || out.String() != "8080\n" {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("query created settings: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"future":{"keep":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"1", "9090", "65535"} {
		out.Reset()
		if code := runCLI([]string{"config", "port", port}, &out, &stderr, nil, nil); code != 0 {
			t.Fatalf("%d %s", code, &stderr)
		}
		addr, err := serverAddress()
		if err != nil || addr != "127.0.0.1:"+port {
			t.Fatalf("%s %v", addr, err)
		}
		out.Reset()
		if code := runCLI([]string{"config", "port"}, &out, &stderr, nil, nil); code != 0 || out.String() != port+"\n" {
			t.Fatalf("query: %d %s", code, &out)
		}
	}
	data, err := os.ReadFile(path)
	var settings struct{ Future struct{ Keep bool } }
	if err != nil || json.Unmarshal(data, &settings) != nil || !settings.Future.Keep {
		t.Fatalf("unknown settings lost: %s %v", data, err)
	}
}

func TestPortConfigInvalidAndFailures(t *testing.T) {
	path := isolatedServerConfig(t)
	for _, args := range [][]string{{"config"}, {"config", "unknown"}, {"config", "port", "80", "extra"}, {"config", "port", ""}, {"config", "port", "0"}, {"config", "port", "65536"}, {"config", "port", "-1"}, {"config", "port", "+80"}, {"config", "port", "8.0"}, {"config", "port", " 80"}, {"config", "port", "999999999999999999999"}} {
		var out, stderr bytes.Buffer
		if code := runCLI(args, &out, &stderr, nil, nil); code == 0 || out.Len() != 0 {
			t.Fatalf("%v: %d %s", args, code, &out)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid input wrote config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{broken`, `null`, `{"port":null}`, `{"port":0}`, `{"port":65536}`, `{"port":"80"}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := runCLI([]string{"config", "port", "9090"}, &out, &stderr, nil, nil); code == 0 {
			t.Fatalf("overwrote malformed settings: %s", data)
		}
		got, _ := os.ReadFile(path)
		if string(got) != data {
			t.Fatal("settings modified on failure")
		}
		if _, err := serverAddress(); err == nil {
			t.Fatal("startup accepted malformed port")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := runCLI([]string{"config", "port", "9090"}, &out, &stderr, nil, nil); code == 0 || out.Len() != 0 {
		t.Fatal("read failure reported success")
	}
}

func TestPortConfigEnvironmentOverride(t *testing.T) {
	isolatedServerConfig(t)
	t.Setenv("NULAS_ADDR", "127.0.0.1:8181")
	var out, stderr bytes.Buffer
	if code := runCLI([]string{"config", "port", "9090"}, &out, &stderr, nil, nil); code != 0 || !strings.Contains(out.String(), "overrides") {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	addr, err := serverAddress()
	if err != nil || addr != "127.0.0.1:8181" {
		t.Fatalf("%s %v", addr, err)
	}
	t.Setenv("NULAS_ADDR", "")
	addr, err = serverAddress()
	if err != nil || addr != "127.0.0.1:9090" {
		t.Fatalf("%s %v", addr, err)
	}
}
