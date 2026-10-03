package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func publicProfile(p Profile) Profile { p.Document = ""; return p }
func publicJob(j Job) Job             { j.Document = ""; return j }

// Applying a full profile is explicit. Original configuration is preserved for
// export; only the applied copy uses loopback listeners and disables privileged
// interception. Controller settings are not taken from imported files.
func fullApplyPayload(content, namespace string) ([]byte, error) {
	fields, err := parseFullDocument(content)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"name", "external-controller", "external-controller-tls", "external-controller-unix", "external-controller-pipe", "secret", "external-ui", "external-ui-url", "external-ui-name", "external-controller-cors", "listeners", "tunnels", "interface-name", "routing-mark", "redir-port", "tproxy-port", "authentication", "skip-auth-prefixes", "lan-allowed-ips", "lan-disallowed-ips", "tuic-server", "ss-config", "vmess-config"} {
		delete(fields, key)
	}
	fields["allow-lan"] = false
	fields["bind-address"] = "127.0.0.1"
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

// Never turn off an existing TUN session as a side effect of a profile reload.
func (a *App) checkFullApply(controller, secret string) error {
	req, err := http.NewRequest(http.MethodGet, controller+"/configs", nil)
	if err != nil {
		return err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return errors.New("Mihomo controller unavailable")
	}
	defer res.Body.Close()
	var current struct {
		Tun struct {
			Enable *bool `json:"enable"`
		} `json:"tun"`
		Redir    int `json:"redir-port"`
		TProxy   int `json:"tproxy-port"`
		IPTables struct {
			Enable bool `json:"enable"`
		} `json:"iptables"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&current) != nil {
		return errors.New("无法检查内核运行配置，未应用完整配置")
	}
	if (current.Tun.Enable != nil && *current.Tun.Enable) || current.Redir != 0 || current.TProxy != 0 || current.IPTables.Enable {
		return errors.New("内核正在使用 TUN 或透明代理；为保留现有系统网络状态，未应用完整配置")
	}
	if current.Tun.Enable == nil {
		return errors.New("无法确认内核 TUN 状态，未应用完整配置")
	}
	return nil
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
