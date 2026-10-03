package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Only expose display fields; controller configuration and credentials stay server-side.
type Proxy struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now,omitempty"`
	All  []string `json:"all,omitempty"`
}

func (a *App) controllerRequest(ctx context.Context, method, path string, body any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	a.mu.Lock()
	controller, secret := a.controller, a.secret
	a.mu.Unlock()
	if controller == "" {
		return errors.New("尚未连接 Mihomo 内核，请先启动内核或配置控制接口")
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, controller+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("无法创建内核请求")
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return errors.New("Mihomo 控制接口不可用，请检查内核连接")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Mihomo 拒绝操作 (HTTP %d)", res.StatusCode)
	}
	if result != nil {
		data, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
		if err != nil || len(data) > 2*1024*1024 {
			return errors.New("内核响应读取失败或超过 2 MB")
		}
		if json.Unmarshal(data, result) != nil {
			return errors.New("内核响应格式无效")
		}
	}
	return nil
}
func (a *App) proxies(ctx context.Context) (map[string]Proxy, error) {
	var data struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	err := a.controllerRequest(ctx, "GET", "/proxies", nil, &data)
	if err == nil && data.Proxies == nil {
		err = errors.New("内核没有返回有效的节点列表")
	}
	return data.Proxies, err
}
func (a *App) nodeRoutes(mux *http.ServeMux) {
	// A latency probe is transient and never changes selection or saved configuration.
	mux.HandleFunc("POST /api/nodes/delay", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Name == "" || len(body.Name) > 1024 {
			fail(w, 400, errors.New("请选择有效的测速节点"))
			return
		}
		select {
		case a.delaySlots <- struct{}{}:
			defer func() { <-a.delaySlots }()
		default:
			fail(w, 429, errors.New("测速请求过多，请稍后重试"))
			return
		}
		proxies, err := a.proxies(r.Context())
		if err != nil {
			fail(w, 502, err)
			return
		}
		proxy, ok := proxies[body.Name]
		if !ok || proxy.Type == "Reject" || proxy.Type == "RejectDrop" || proxy.Type == "Pass" {
			fail(w, 400, errors.New("该节点不存在或不支持测速，请刷新列表"))
			return
		}
		var result struct {
			Delay *int `json:"delay"`
		}
		query := url.Values{"url": {"https://www.gstatic.com/generate_204"}, "timeout": {"5000"}, "expected": {"204"}}
		if err := a.controllerRequest(r.Context(), "GET", "/proxies/"+url.PathEscape(body.Name)+"/delay?"+query.Encode(), nil, &result); err != nil {
			fail(w, 502, fmt.Errorf("测速失败（超时或目标不可达）：%w", err))
			return
		}
		if result.Delay == nil || *result.Delay <= 0 || *result.Delay > 65535 {
			fail(w, 502, errors.New("内核未返回有效的测速延迟"))
			return
		}
		reply(w, 200, map[string]any{"name": body.Name, "delay": *result.Delay})
	})
	mux.HandleFunc("GET /api/runtime/fallback", func(w http.ResponseWriter, r *http.Request) {
		var data struct {
			Rules []struct {
				Type  string `json:"type"`
				Proxy string `json:"proxy"`
				Extra struct {
					Disabled bool `json:"disabled"`
				} `json:"extra"`
			} `json:"rules"`
		}
		if err := a.controllerRequest(r.Context(), "GET", "/rules", nil, &data); err != nil {
			fail(w, 502, err)
			return
		}
		if data.Rules == nil {
			fail(w, 502, errors.New("内核没有返回有效的规则列表"))
			return
		}
		for _, rule := range data.Rules {
			if strings.EqualFold(rule.Type, "MATCH") && !rule.Extra.Disabled && rule.Proxy != "" && rule.Proxy != "PASS" && rule.Proxy != "PASS-RULE" {
				reply(w, 200, map[string]any{"target": rule.Proxy, "configured": true})
				return
			}
		}
		reply(w, 200, map[string]any{"target": "DIRECT", "configured": false})
	})
	mux.HandleFunc("GET /api/nodes", func(w http.ResponseWriter, r *http.Request) {
		proxies, err := a.proxies(r.Context())
		if err != nil {
			fail(w, 502, err)
			return
		}
		list := make([]Proxy, 0, len(proxies))
		for name, p := range proxies {
			p.Name = name
			list = append(list, p)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		reply(w, 200, list)
	})
	mux.HandleFunc("PUT /api/nodes/selection", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Group string `json:"group"`
			Name  string `json:"name"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Group == "" || body.Name == "" || len(body.Group) > 1024 || len(body.Name) > 1024 {
			fail(w, 400, errors.New("请选择有效的代理组和节点"))
			return
		}
		a.controlMu.Lock()
		defer a.controlMu.Unlock()
		proxies, err := a.proxies(r.Context())
		if err != nil {
			fail(w, 502, err)
			return
		}
		group, ok := proxies[body.Group]
		if !ok || group.Type != "Selector" {
			fail(w, 400, errors.New("仅支持手动选择代理组"))
			return
		}
		found := false
		for _, name := range group.All {
			if name == body.Name {
				found = true
				break
			}
		}
		if !found {
			fail(w, 400, errors.New("该节点不属于所选代理组，请刷新列表"))
			return
		}
		if err := a.controllerRequest(r.Context(), "PUT", "/proxies/"+url.PathEscape(body.Group), map[string]string{"name": body.Name}, nil); err != nil {
			fail(w, 502, err)
			return
		}
		reply(w, 200, map[string]string{"group": body.Group, "name": body.Name})
	})
	mux.HandleFunc("GET /api/runtime/mode", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := a.controllerRequest(r.Context(), "GET", "/configs", nil, &body); err != nil {
			fail(w, 502, err)
			return
		}
		if body.Mode != "rule" && body.Mode != "global" && body.Mode != "direct" {
			fail(w, 502, errors.New("内核返回了未知运行模式"))
			return
		}
		reply(w, 200, body)
	})
	mux.HandleFunc("PUT /api/runtime/mode", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Mode != "rule" && body.Mode != "global" && body.Mode != "direct" {
			fail(w, 400, errors.New("请选择规则、全局或直连模式"))
			return
		}
		a.controlMu.Lock()
		defer a.controlMu.Unlock()
		if err := a.controllerRequest(r.Context(), "PATCH", "/configs", body, nil); err != nil {
			fail(w, 502, err)
			return
		}
		reply(w, 200, body)
	})
}
