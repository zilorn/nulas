package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxImportBytes = 6000

const networkImportTimeout = 15 * time.Second

type Profile struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Config  Config    `json:"config"`
	Source  string    `json:"source"`
	Created time.Time `json:"created"`
}

// Imports deliberately support only the starter's core settings. Rejecting
// additional fields prevents silently discarding rules, proxies or credentials.
func importConfig(content string) (Config, error) {
	c := Config{7890, "rule", false, false, "info"}
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if len(content) > maxImportBytes {
		return c, errors.New("导入内容不能超过 6 KB")
	}
	if content == "" {
		return c, errors.New("配置内容不能为空")
	}
	var fields map[string]json.RawMessage
	if strings.HasPrefix(content, "{") {
		d := json.NewDecoder(strings.NewReader(content))
		if _, err := d.Token(); err != nil {
			return c, errors.New("JSON 配置格式不正确")
		}
		fields = make(map[string]json.RawMessage)
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return c, errors.New("JSON 配置格式不正确")
			}
			key, ok := token.(string)
			if !ok {
				return c, errors.New("JSON 配置格式不正确")
			}
			if _, exists := fields[key]; exists {
				return c, fmt.Errorf("重复字段：%s", key)
			}
			var value json.RawMessage
			if err := d.Decode(&value); err != nil {
				return c, errors.New("JSON 配置格式不正确")
			}
			fields[key] = value
		}
		if _, err := d.Token(); err != nil {
			return c, errors.New("JSON 配置格式不正确")
		}
		if d.Decode(&struct{}{}) != io.EOF {
			return c, errors.New("仅支持一个 JSON 配置对象")
		}
	} else {
		fields = make(map[string]json.RawMessage)
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSuffix(line, "\r")
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if line != strings.TrimLeft(line, " \t") {
				return c, errors.New("仅支持顶层核心参数 YAML，不支持嵌套配置")
			}
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok {
				return c, errors.New("YAML 配置格式不正确")
			}
			key = strings.TrimSpace(key)
			if _, exists := fields[key]; exists {
				return c, fmt.Errorf("重复字段：%s", key)
			}
			value = strings.TrimSpace(value)
			// Comments are accepted after plain scalar values; quoted values stay intact.
			if !strings.HasPrefix(value, "\"") && !strings.HasPrefix(value, "'") {
				if i := strings.Index(value, " #"); i >= 0 {
					value = strings.TrimSpace(value[:i])
				}
			}
			switch key {
			case "mode", "log-level":
				if strings.HasPrefix(value, "\"") {
					var v string
					if json.Unmarshal([]byte(value), &v) != nil {
						return c, errors.New("字符串格式不正确")
					}
					value = v
				} else if strings.HasPrefix(value, "'") {
					if len(value) < 2 || !strings.HasSuffix(value, "'") {
						return c, errors.New("字符串格式不正确")
					}
					value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
				}
				b, _ := json.Marshal(value)
				fields[key] = b
			default:
				fields[key] = json.RawMessage(value)
			}
		}
	}
	if len(fields) == 0 {
		return c, errors.New("配置必须包含至少一个核心参数")
	}
	for key, value := range fields {
		if strings.TrimSpace(string(value)) == "null" {
			return c, fmt.Errorf("字段 %s 不能为空", key)
		}
		var err error
		switch key {
		case "mixed-port":
			err = json.Unmarshal(value, &c.Port)
		case "mode":
			err = json.Unmarshal(value, &c.Mode)
		case "allow-lan":
			err = json.Unmarshal(value, &c.LAN)
		case "ipv6":
			err = json.Unmarshal(value, &c.IPv6)
		case "log-level":
			err = json.Unmarshal(value, &c.Log)
		default:
			return c, fmt.Errorf("不支持字段 %s；仅支持 mixed-port、mode、allow-lan、ipv6、log-level", strconv.Quote(key))
		}
		if err != nil {
			return c, fmt.Errorf("字段 %s 的类型不正确", key)
		}
	}
	return c, validate(c)
}

// URLs may contain tokens: never persist them or include them in errors.
func validateImportURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || len(raw) > 4096 {
		return nil, errors.New("请提供有效的 HTTP/HTTPS 配置地址（不含用户名、密码或片段）")
	}
	return u, nil
}

func fetchImportConfig(ctx context.Context, raw string) (Config, error) {
	u, err := validateImportURL(strings.TrimSpace(raw))
	if err != nil {
		return Config{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, networkImportTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Config{}, errors.New("配置地址无效")
	}
	req.Header.Set("Accept", "application/yaml, application/json, text/yaml, text/plain")
	// Separate direct client: no controller credentials or proxy dependency.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("重定向次数过多")
		}
		if _, err := validateImportURL(req.URL.String()); err != nil {
			return err
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("不允许 HTTPS 降级")
		}
		return nil
	}}
	response, err := client.Do(req)
	if err != nil {
		return Config{}, errors.New("配置下载失败，请检查地址、网络或重定向；下载须在 15 秒内完成")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Config{}, fmt.Errorf("配置下载失败：HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxImportBytes {
		return Config{}, errors.New("导入内容不能超过 6 KB")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxImportBytes+1))
	if err != nil {
		return Config{}, errors.New("配置下载未完成，请重试")
	}
	if len(content) > maxImportBytes {
		return Config{}, errors.New("导入内容不能超过 6 KB")
	}
	return importConfig(string(content))
}

func (a *App) profileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		profiles := a.state.Profiles
		if profiles == nil {
			profiles = []Profile{}
		}
		reply(w, 200, profiles)
	})
	mux.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name    string  `json:"name"`
			Config  *Config `json:"config"`
			Content *string `json:"content"`
			URL     *string `json:"url"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" || len([]rune(body.Name)) > 60 {
			fail(w, 400, errors.New("配置名称须为 1–60 个字符"))
			return
		}
		sources := 0
		if body.Config != nil {
			sources++
		}
		if body.Content != nil {
			sources++
		}
		if body.URL != nil {
			sources++
		}
		if sources != 1 {
			fail(w, 400, errors.New("请仅提供配置参数、导入内容或网络地址中的一项"))
			return
		}
		var c Config
		source := "created"
		if body.URL != nil {
			var err error
			c, err = fetchImportConfig(r.Context(), *body.URL)
			if err != nil {
				fail(w, 400, err)
				return
			}
			source = "network"
		} else if body.Content != nil {
			var err error
			c, err = importConfig(*body.Content)
			if err != nil {
				fail(w, 400, err)
				return
			}
			source = "imported"
		} else {
			c = *body.Config
		}
		if err := validate(c); err != nil {
			fail(w, 400, err)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(a.state.Profiles) >= 100 {
			fail(w, 409, errors.New("最多保存 100 份配置"))
			return
		}
		for _, p := range a.state.Profiles {
			if strings.EqualFold(p.Name, body.Name) {
				fail(w, 409, errors.New("配置名称已存在"))
				return
			}
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			fail(w, 500, err)
			return
		}
		p := Profile{hex.EncodeToString(id), body.Name, c, source, time.Now().UTC()}
		old := a.state.Profiles
		a.state.Profiles = append(a.state.Profiles, p)
		if err := a.persist(); err != nil {
			a.state.Profiles = old
			fail(w, 500, err)
			return
		}
		reply(w, 201, p)
	})
	mux.HandleFunc("POST /api/profiles/{id}/load", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, p := range a.state.Profiles {
			if p.ID != r.PathValue("id") {
				continue
			}
			old := a.state.Config
			a.state.Config = p.Config
			if err := a.persist(); err != nil {
				a.state.Config = old
				fail(w, 500, err)
				return
			}
			reply(w, 200, p.Config)
			return
		}
		fail(w, 404, errors.New("配置不存在"))
	})
}
