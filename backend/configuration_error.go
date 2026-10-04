package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

const configurationErrorLimit = 8 * 1024

var diagnosticURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)

// Mihomo's reload endpoint returns {"message": "<parse error>"}. Never
// publish an arbitrary response body: it can contain a complete configuration.
func configurationRejection(status int, body io.Reader, secret, document string) error {
	prefix := fmt.Sprintf("Mihomo rejected configuration (HTTP %d)", status)
	data, err := io.ReadAll(io.LimitReader(body, configurationErrorLimit+1))
	if err != nil || len(data) > configurationErrorLimit {
		return errors.New(prefix)
	}
	var response struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &response) != nil {
		return errors.New(prefix)
	}
	message := privateConfigurationDiagnostic(response.Message, secret, document)
	if message == "" {
		return errors.New(prefix)
	}
	return fmt.Errorf("%s: %s", prefix, message)
}

func privateConfigurationDiagnostic(message, secret, document string) string {
	values := map[string]bool{}
	add := func(value string) {
		if value != "" {
			values[value] = true
		}
	}
	add(secret)
	collectURL := func(value string) {
		for _, address := range diagnosticURL.FindAllString(value, -1) {
			add(address)
			u, err := url.Parse(address)
			if err != nil {
				continue
			}
			if u.User != nil {
				add(u.User.Username())
				password, _ := u.User.Password()
				add(password)
			}
			for _, entries := range u.Query() {
				for _, entry := range entries {
					add(entry)
				}
			}
		}
	}
	if document != "" {
		var root yaml.Node
		if yaml.Unmarshal([]byte(document), &root) != nil {
			// Without a parsed snapshot, credential redaction cannot be guaranteed.
			return ""
		}
		visited := map[*yaml.Node]uint8{}
		var collect func(*yaml.Node, bool)
		collect = func(node *yaml.Node, private bool) {
			flag := uint8(1)
			if private {
				flag = 2
			}
			if node == nil || visited[node]&flag != 0 {
				return
			}
			visited[node] |= flag
			if node.Kind == yaml.AliasNode {
				collect(node.Alias, private)
				return
			}
			if node.Kind == yaml.ScalarNode {
				collectURL(node.Value)
				if private {
					add(node.Value)
					// Core errors may quote just one line of a PEM key/certificate.
					for _, line := range strings.Split(node.Value, "\n") {
						add(strings.TrimSpace(line))
					}
				}
			}
			for i := 0; i < len(node.Content); i++ {
				if node.Kind == yaml.MappingNode {
					if private {
						add(node.Content[i].Value)
					}
					key := strings.ToLower(node.Content[i].Value)
					privateField := private || key == "url" || key == "users" || key == "username"
					for _, part := range []string{"password", "passwd", "secret", "token", "uuid", "auth", "credential", "key", "certificate"} {
						privateField = privateField || strings.Contains(key, part)
					}
					i++
					collect(node.Content[i], privateField)
				} else {
					collect(node.Content[i], private)
				}
			}
		}
		collect(&root, false)
	}
	// Account for the common quoting/encoding forms used in parser errors.
	originals := make([]string, 0, len(values))
	for value := range values {
		originals = append(originals, value)
	}
	for _, value := range originals {
		encoded, _ := json.Marshal(value)
		add(string(encoded[1 : len(encoded)-1]))
		add(strings.ReplaceAll(value, "'", "''"))
		add(url.QueryEscape(value))
		add(url.PathEscape(value))
	}
	ordered := make([]string, 0, len(values))
	for value := range values {
		ordered = append(ordered, value)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	// Hide URLs even when they originate from the core rather than the snapshot.
	message = diagnosticURL.ReplaceAllString(message, "[redacted URL]")
	if len(ordered) > 0 {
		pairs := make([]string, 0, len(ordered)*2)
		for _, value := range ordered {
			pairs = append(pairs, value, "[redacted]")
		}
		message = strings.NewReplacer(pairs...).Replace(message)
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	runes := []rune(message)
	if len(runes) > 1024 {
		message = string(runes[:1024]) + "…"
	}
	return message
}
