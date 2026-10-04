package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

//go:embed ports.json
var portDefaultsJSON []byte

var portKeys = []string{"port", "ssr-port", "dev-port"}

func defaultPorts() map[string]int {
	ports := make(map[string]int)
	if err := json.Unmarshal(portDefaultsJSON, &ports); err != nil {
		panic(err)
	}
	return ports
}

func serverConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nulas", "server.json"), nil
}

// Keep server settings separate from state.json so a running worker cannot
// overwrite CLI changes. The user config directory is independent of cwd.
func readServerConfig() (string, map[string]json.RawMessage, map[string]int, error) {
	path, err := serverConfigPath()
	if err != nil {
		return "", nil, nil, err
	}
	settings := make(map[string]json.RawMessage)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return path, settings, defaultPorts(), nil
	}
	if err != nil {
		return path, nil, nil, err
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return path, nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	if settings == nil {
		return path, nil, nil, errors.New("server config must be a JSON object")
	}
	ports := defaultPorts()
	for _, key := range portKeys {
		if value, ok := settings[key]; ok {
			port := 0
			if err = json.Unmarshal(value, &port); err != nil {
				return path, nil, nil, fmt.Errorf("invalid %s: %w", key, err)
			}
			ports[key] = port
		}
		if ports[key] < 1 || ports[key] > 65535 {
			return path, nil, nil, fmt.Errorf("%s must be between 1 and 65535", key)
		}
	}
	if _, err := lanEnabled(settings); err != nil {
		return path, nil, nil, err
	}
	return path, settings, ports, nil
}

func lanEnabled(settings map[string]json.RawMessage) (bool, error) {
	value, ok := settings["lan"]
	if !ok {
		return false, nil
	}
	value = bytes.TrimSpace(value)
	if string(value) != "true" && string(value) != "false" {
		return false, errors.New("lan must be true or false")
	}
	return string(value) == "true", nil
}

// Snapshot local private interface addresses at startup; never trust arbitrary
// private IPs or domain names supplied in a request's Host header.
func lanInterfaceHosts(addresses []net.Addr) []string {
	var hosts []string
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.IsPrivate() && !ip.IsLoopback() {
			hosts = append(hosts, ip.String())
		}
	}
	return hosts
}

func serverSSRURL() (string, error) {
	if value := os.Getenv("NULAS_SSR_URL"); value != "" {
		return value, nil
	}
	_, _, ports, err := readServerConfig()
	if err != nil {
		return "", err
	}
	return "http://127.0.0.1:" + strconv.Itoa(ports["ssr-port"]), nil
}

func serverAddress() (string, error) {
	if addr := os.Getenv("NULAS_ADDR"); addr != "" {
		return addr, nil
	}
	_, settings, ports, err := readServerConfig()
	if err != nil {
		return "", err
	}
	host := "127.0.0.1"
	if enabled, _ := lanEnabled(settings); enabled {
		host = "0.0.0.0"
	}
	return host + ":" + strconv.Itoa(ports["port"]), nil
}

func runPortConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && args[0] == "--json") {
		_, settings, ports, err := readServerConfig()
		if err != nil {
			return cliExit(err, stderr)
		}
		if len(args) == 1 {
			values := make(map[string]any)
			for key, port := range ports {
				values[key] = port
			}
			values["lan"], _ = lanEnabled(settings)
			return cliExit(json.NewEncoder(stdout).Encode(values), stderr)
		}
		for _, key := range portKeys {
			fmt.Fprintf(stdout, "%s: %d\n", key, ports[key])
		}
		enabled, _ := lanEnabled(settings)
		fmt.Fprintf(stdout, "lan: %t\n", enabled)
		return 0
	}
	valid := args[0] == "lan"
	for _, key := range portKeys {
		valid = valid || args[0] == key
	}
	if len(args) > 2 || !valid {
		fmt.Fprint(stderr, cliUsage)
		return 2
	}
	key := args[0]
	port := 0
	if len(args) == 2 && key == "lan" {
		if args[1] != "true" && args[1] != "false" {
			return cliExit(errors.New("lan must be true or false"), stderr)
		}
	}
	if len(args) == 2 && key != "lan" {
		// Accept decimal digits only, including no signs or whitespace.
		for _, c := range args[1] {
			if c < '0' || c > '9' {
				return cliExit(errors.New(key+" must be an integer between 1 and 65535"), stderr)
			}
		}
		var err error
		port, err = strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return cliExit(errors.New(key+" must be an integer between 1 and 65535"), stderr)
		}
	}
	path, settings, saved, err := readServerConfig()
	if err != nil {
		return cliExit(err, stderr)
	}
	if len(args) == 1 {
		if key == "lan" {
			enabled, _ := lanEnabled(settings)
			fmt.Fprintln(stdout, enabled)
		} else {
			fmt.Fprintln(stdout, saved[key])
		}
		return 0
	}
	value := strconv.Itoa(port)
	if key == "lan" {
		value = args[1]
	}
	settings[key] = json.RawMessage(value)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return cliExit(err, stderr)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return cliExit(err, stderr)
	}
	if err = atomicWrite(path, append(data, '\n')); err != nil {
		return cliExit(err, stderr)
	}
	fmt.Fprintf(stdout, "Saved %s %s. Restart Nulas to apply (nulas restart for the Linux service).\n", key, value)
	if key == "lan" && value == "true" {
		fmt.Fprintln(stdout, "LAN access exposes the dashboard and unauthenticated API to your network. Use only a trusted LAN; public deployment requires authentication and TLS.")
	}
	if (key == "port" || key == "lan") && os.Getenv("NULAS_ADDR") != "" {
		fmt.Fprintln(stdout, "NULAS_ADDR overrides the saved listen address and port; remove that override to use this setting.")
	}
	if key == "ssr-port" && os.Getenv("NULAS_SSR_URL") != "" {
		fmt.Fprintln(stdout, "NULAS_SSR_URL overrides the saved SSR port; remove that override to use this setting.")
	}
	return 0
}
