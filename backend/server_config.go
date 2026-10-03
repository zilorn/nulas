package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const defaultWebPort = 8080

func serverConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nulas", "server.json"), nil
}

// Keep server settings separate from state.json so a running worker cannot
// overwrite CLI changes. The user config directory is independent of cwd.
func readServerConfig() (string, map[string]json.RawMessage, int, error) {
	path, err := serverConfigPath()
	if err != nil {
		return "", nil, 0, err
	}
	settings := make(map[string]json.RawMessage)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return path, settings, defaultWebPort, nil
	}
	if err != nil {
		return path, nil, 0, err
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return path, nil, 0, fmt.Errorf("read %s: %w", path, err)
	}
	if settings == nil {
		return path, nil, 0, errors.New("server config must be a JSON object")
	}
	port := defaultWebPort
	if value, ok := settings["port"]; ok {
		port = 0
		if err = json.Unmarshal(value, &port); err != nil {
			return path, nil, 0, fmt.Errorf("invalid web port: %w", err)
		}
	}
	if port < 1 || port > 65535 {
		return path, nil, 0, errors.New("web port must be between 1 and 65535")
	}
	return path, settings, port, nil
}

func serverAddress() (string, error) {
	if addr := os.Getenv("NULAS_ADDR"); addr != "" {
		return addr, nil
	}
	_, _, port, err := readServerConfig()
	if err != nil {
		return "", err
	}
	return "127.0.0.1:" + strconv.Itoa(port), nil
}

func runPortConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 || args[0] != "port" {
		fmt.Fprint(stderr, cliUsage)
		return 2
	}
	port := 0
	if len(args) == 2 {
		// Accept decimal digits only, including no signs or whitespace.
		for _, c := range args[1] {
			if c < '0' || c > '9' {
				return cliExit(errors.New("web port must be an integer between 1 and 65535"), stderr)
			}
		}
		var err error
		port, err = strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			return cliExit(errors.New("web port must be an integer between 1 and 65535"), stderr)
		}
	}
	path, settings, saved, err := readServerConfig()
	if err != nil {
		return cliExit(err, stderr)
	}
	if len(args) == 1 {
		fmt.Fprintln(stdout, saved)
		return 0
	}
	settings["port"] = json.RawMessage(strconv.Itoa(port))
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
	fmt.Fprintf(stdout, "Saved web/API port %d. Restart Nulas to apply (nulas restart for the Linux service).\n", port)
	if os.Getenv("NULAS_ADDR") != "" {
		fmt.Fprintln(stdout, "NULAS_ADDR overrides the saved port; remove that override to use this setting.")
	}
	return 0
}
