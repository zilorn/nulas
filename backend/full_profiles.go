package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Full documents stay on the server: list/create/job responses contain metadata
// only. Preserve the original text for durable snapshots and generated files.
func importProfileConfig(content string) (importedConfig, error) {
	original := content
	c, coreErr := importNamedConfig(content)
	if coreErr == nil {
		return c, nil
	}
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if err := unsupportedImportFormat(content); err != nil {
		return c, err
	}
	if !strings.Contains(content, ":") {
		return c, coreErr
	}
	fields, err := parseFullDocument(content)
	if err != nil {
		return c, err
	}
	full := false
	for _, key := range []string{"proxies", "proxy-groups", "rules", "proxy-providers", "rule-providers", "dns", "hosts", "sniffer"} {
		switch value := fields[key].(type) {
		case []any:
			full = full || len(value) > 0
		case map[string]any:
			full = full || len(value) > 0
		}
	}
	if !full {
		normalized, err := json.Marshal(fields)
		if err != nil {
			return c, errors.New("配置字段类型不正确")
		}
		return importNamedConfig(string(normalized))
	}
	c = importedConfig{Config: Config{7890, "rule", false, false, "info"}}
	for key, target := range map[string]any{"name": &c.Name, "mixed-port": &c.Port, "mode": &c.Mode, "allow-lan": &c.LAN, "ipv6": &c.IPv6, "log-level": &c.Log} {
		if value, exists := fields[key]; exists {
			b, err := json.Marshal(value)
			if err != nil || value == nil || json.Unmarshal(b, target) != nil {
				return c, fmt.Errorf("字段 %s 的类型不正确", key)
			}
		}
	}
	c.Name = strings.TrimSpace(c.Name)
	if len([]rune(c.Name)) > 60 {
		return c, errors.New("文件内的配置名称不能超过 60 个字符")
	}
	if err := validate(c.Config); err != nil {
		return c, err
	}
	for _, key := range []string{"proxies", "proxy-groups", "rules"} {
		if value, exists := fields[key]; exists {
			if _, ok := value.([]any); !ok {
				return c, fmt.Errorf("字段 %s 必须是列表", key)
			}
		}
	}
	for _, key := range []string{"proxy-providers", "rule-providers", "dns", "hosts", "sniffer"} {
		if value, exists := fields[key]; exists {
			if _, ok := value.(map[string]any); !ok {
				return c, fmt.Errorf("字段 %s 必须是对象", key)
			}
		}
	}
	c.Document = original
	return c, nil
}

func parseFullDocument(content string) (map[string]any, error) {
	var fields map[string]any
	decoder := yaml.NewDecoder(strings.NewReader(content))
	if err := decoder.Decode(&fields); err != nil || len(fields) == 0 {
		// Parser errors can quote scalar values containing credentials.
		return nil, errors.New("YAML/JSON 配置格式不正确：需要一个顶层对象，且不能包含重复字段")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("仅支持一个 YAML/JSON 配置文档")
	}
	return fields, nil
}

func publicProfile(p Profile) Profile {
	p.Document = ""
	p.Refreshable = p.RefreshURL != ""
	p.RefreshURL = ""
	return p
}
func publicJob(j Job) Job { j.Document = ""; j.RefreshURL = ""; return j }

// Applying a full profile is explicit. Original configuration is preserved for
// export; only the applied copy follows allow-lan for proxy listeners and disables privileged
// interception. Controller settings are not taken from imported files.
func fullApplyPayload(content, namespace string) ([]byte, error) {
	fields, err := parseFullDocument(content)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"name", "external-controller", "external-controller-tls", "external-controller-unix", "external-controller-pipe", "secret", "external-ui", "external-ui-url", "external-ui-name", "external-controller-cors", "listeners", "tunnels", "interface-name", "routing-mark", "redir-port", "tproxy-port", "authentication", "skip-auth-prefixes", "lan-allowed-ips", "lan-disallowed-ips", "tuic-server", "ss-config", "vmess-config"} {
		delete(fields, key)
	}
	lan, _ := fields["allow-lan"].(bool)
	fields["allow-lan"] = lan
	fields["bind-address"] = proxyBindAddress(lan)
	fields["tun"] = map[string]any{"enable": false}
	fields["iptables"] = map[string]any{"enable": false}
	if dns, ok := fields["dns"].(map[string]any); ok {
		delete(dns, "listen")
	}
	if ntp, ok := fields["ntp"].(map[string]any); ok {
		ntp["write-to-system"] = false
	}
	// Give HTTP provider caches private, per-job relative paths rather than
	// overwriting paths supplied by an imported subscription.
	for _, kind := range []string{"proxy-providers", "rule-providers"} {
		if providers, ok := fields[kind].(map[string]any); ok {
			index := 0
			for _, value := range providers {
				if provider, ok := value.(map[string]any); ok && provider["type"] == "http" {
					provider["path"] = fmt.Sprintf(".nulas/%s/%s-%d.yaml", kind, namespace, index)
					index++
				}
			}
		}
	}
	return yaml.Marshal(fields)
}

func validateApplyPorts(content, controller string) error {
	fields, err := parseFullDocument(content)
	if err != nil {
		return err
	}
	// Do not allow imported listeners to collide with the current controller.
	u, err := url.Parse(controller)
	if err != nil {
		return errors.New("控制接口地址无效")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	for _, key := range []string{"port", "socks-port", "mixed-port"} {
		if value, ok := fields[key]; ok && fmt.Sprint(value) == port {
			return errors.New("配置代理端口不能与控制接口端口相同")
		}
	}
	return nil
}
