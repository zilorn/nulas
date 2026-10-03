package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	return path, settings, ports, nil
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
	_, _, ports, err := readServerConfig()
	if err != nil {
		return "", err
	}
	return "127.0.0.1:" + strconv.Itoa(ports["port"]), nil
}

func runPortConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && args[0] == "--json") {
		_, _, ports, err := readServerConfig()
		if err != nil {
			return cliExit(err, stderr)
		}
		if len(args) == 1 {
			return cliExit(json.NewEncoder(stdout).Encode(ports), stderr)
		}
		for _, key := range portKeys {
			fmt.Fprintf(stdout, "%s: %d\n", key, ports[key])
		}
		return 0
	}
	valid := false
	for _, key := range portKeys {
		valid = valid || args[0] == key
	}
	if len(args) > 2 || !valid {
		fmt.Fprint(stderr, cliUsage)
		return 2
	}
	key := args[0]
	port := 0
	if len(args) == 2 {
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
		fmt.Fprintln(stdout, saved[key])
		return 0
	}
	settings[key] = json.RawMessage(strconv.Itoa(port))
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
	fmt.Fprintf(stdout, "Saved %s %d. Restart Nulas to apply (nulas restart for the Linux service).\n", key, port)
	if key == "port" && os.Getenv("NULAS_ADDR") != "" {
		fmt.Fprintln(stdout, "NULAS_ADDR overrides the saved port; remove that override to use this setting.")
	}
	if key == "ssr-port" && os.Getenv("NULAS_SSR_URL") != "" {
		fmt.Fprintln(stdout, "NULAS_SSR_URL overrides the saved SSR port; remove that override to use this setting.")
	}
	return 0
}
