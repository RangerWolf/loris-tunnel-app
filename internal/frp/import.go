package frp

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

type ImportProxy struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	LocalIP    string   `json:"localIP"`
	LocalPort  int      `json:"localPort"`
	RemotePort int      `json:"remotePort"`
	Domain     string   `json:"domain"`
	Errors     []string `json:"errors"`
}
type ImportResult struct {
	Format           string        `json:"format"`
	Server           Server        `json:"server"`
	Proxies          []ImportProxy `json:"proxies"`
	Warnings         []string      `json:"warnings"`
	Errors           []string      `json:"errors"`
	MatchedServerIDs []string      `json:"matchedServerIds"`
}

func importMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func importString(v any) string      { s, _ := v.(string); return s }
func importInt(v any) int {
	n, _ := v.(float64)
	if n != float64(int(n)) {
		return -1
	}
	return int(n)
}
func importTypes(m map[string]any, schema map[string]string) bool {
	for key, kind := range schema {
		value, exists := m[key]
		if !exists {
			continue
		}
		valid := false
		switch kind {
		case "string":
			_, valid = value.(string)
		case "number":
			n, ok := value.(float64)
			valid = ok && n == float64(int(n))
		case "bool":
			_, valid = value.(bool)
		case "map":
			_, valid = value.(map[string]any)
		case "list":
			_, valid = value.([]any)
		}
		if !valid {
			return false
		}
	}
	return true
}

func importUnknown(m map[string]any, allowed string) bool {
	for k := range m {
		if !strings.Contains(" "+allowed+" ", " "+k+" ") {
			return true
		}
	}
	return false
}

// ParseImport never expands templates, reads includes, or creates configuration.
// Parser diagnostics deliberately omit source text because it may contain credentials.
func ParseImport(content, hint string) (*ImportResult, error) {
	if len(content) > 2*1024*1024 {
		return nil, fmt.Errorf("FRPC configuration exceeds 2 MiB")
	}
	content = strings.TrimPrefix(strings.TrimSpace(content), "\ufeff")
	if content == "" {
		return nil, fmt.Errorf("FRPC configuration is empty")
	}
	if strings.Contains(content, "{{") || strings.Contains(content, "${") {
		return nil, fmt.Errorf("FRPC environment templates must be resolved before importing")
	}
	format := strings.ToLower(strings.TrimPrefix(hint, "."))
	if format == "yml" {
		format = "yaml"
	}
	if format == "" {
		switch {
		case strings.HasPrefix(content, "{"):
			format = "json"
		case regexp.MustCompile(`(?m)^\s*\[common\]\s*(?:[;#].*)?$`).MatchString(content):
			format = "ini"
		case regexp.MustCompile(`(?m)^\s*[A-Za-z][A-Za-z0-9_.]*\s*=`).MatchString(content):
			format = "toml"
		default:
			format = "yaml"
		}
	}
	var raw map[string]any
	var err error
	switch format {
	case "json":
		err = json.Unmarshal([]byte(content), &raw)
	case "toml":
		err = toml.Unmarshal([]byte(content), &raw)
	case "yaml":
		decoder := yaml.NewDecoder(strings.NewReader(content))
		err = decoder.Decode(&raw)
		if err == nil {
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				err = fmt.Errorf("multiple documents")
			}
		}
	case "ini":
		raw, err = parseImportINI(content)
	default:
		return nil, fmt.Errorf("unsupported FRPC configuration format")
	}
	if err != nil || raw == nil {
		return nil, fmt.Errorf("invalid FRPC %s configuration", format)
	}
	// Normalize numeric representations from TOML, YAML and INI.
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid FRPC configuration")
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid FRPC configuration")
	}
	result := &ImportResult{Format: format, Proxies: []ImportProxy{}, Warnings: []string{}, Errors: []string{}}
	if importUnknown(raw, "serverAddr serverPort user auth transport proxies log webServer loginFailExit includes visitors") ||
		len(importMap(raw["webServer"])) > 0 {
		result.Errors = append(result.Errors, "Unsupported FRPC client options; remove them before importing")
	}
	for _, key := range []string{"includes", "visitors"} {
		if value, ok := raw[key]; ok && value != nil {
			if list, ok := value.([]any); !ok || len(list) > 0 {
				result.Errors = append(result.Errors, "FRPC includes and visitors are not supported")
			}
		}
	}
	if !importTypes(raw, map[string]string{"serverAddr": "string", "serverPort": "number", "user": "string", "auth": "map", "transport": "map", "proxies": "list"}) {
		result.Errors = append(result.Errors, "Invalid FRPC client option types")
	}
	auth := importMap(raw["auth"])
	transport := importMap(raw["transport"])
	tls := importMap(transport["tls"])
	if !importTypes(auth, map[string]string{"method": "string", "token": "string"}) || !importTypes(transport, map[string]string{"tls": "map"}) {
		result.Errors = append(result.Errors, "Invalid FRPC authentication or transport options")
	}
	if importUnknown(auth, "method token") || (importString(auth["method"]) != "" && importString(auth["method"]) != "token") {
		result.Errors = append(result.Errors, "Only FRPC token authentication is supported")
	}
	if importUnknown(transport, "tls") || importUnknown(tls, "enable") {
		result.Errors = append(result.Errors, "Custom FRPC transport or TLS options are not supported")
	}
	server := Server{Name: "frpc-import", Host: importString(raw["serverAddr"]), Port: 7000, HTTPPort: 80, User: importString(raw["user"]), Token: importString(auth["token"]), TLS: true}
	if value, ok := raw["serverPort"]; ok {
		server.Port = importInt(value)
		if server.Port == 0 {
			server.Port = -1
		}
	}
	if value, ok := tls["enable"]; ok {
		enabled, valid := value.(bool)
		if !valid {
			result.Errors = append(result.Errors, "Invalid FRPC TLS enable value")
		}
		server.TLS = enabled
	}
	if err := server.Normalize(); err != nil {
		result.Errors = append(result.Errors, "Invalid FRPC server address or port")
	}
	result.Server = server
	if _, ok := raw["log"]; ok {
		result.Warnings = append(result.Warnings, "FRPC logging settings are managed by Loris Tunnel")
	}
	proxies, ok := raw["proxies"].([]any)
	if !ok || len(proxies) == 0 {
		result.Errors = append(result.Errors, "No FRPC proxies found")
	}
	names := map[string]bool{}
	endpoints := map[string]bool{}
	for _, value := range proxies {
		p := importMap(value)
		proxy := ImportProxy{Name: importString(p["name"]), Type: importString(p["type"]), LocalIP: importString(p["localIP"]), LocalPort: importInt(p["localPort"]), RemotePort: importInt(p["remotePort"]), Errors: []string{}}
		if !importTypes(p, map[string]string{"name": "string", "type": "string", "localIP": "string", "localPort": "number", "remotePort": "number", "customDomains": "list"}) {
			proxy.Errors = append(proxy.Errors, "Invalid FRPC proxy option types")
		}
		if proxy.Type == "" {
			proxy.Type = "tcp"
		}
		if proxy.LocalIP == "" {
			proxy.LocalIP = "127.0.0.1"
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(proxy.Name) {
			proxy.Errors = append(proxy.Errors, "Proxy Name must be 1–64 letters, digits, underscores or hyphens")
		}
		if importUnknown(p, "name type localIP localPort remotePort customDomains") {
			proxy.Errors = append(proxy.Errors, "Unsupported proxy options (plugins, subdomains and advanced routing are not supported)")
		}
		if domains, exists := p["customDomains"]; exists {
			list, valid := domains.([]any)
			if !valid || len(list) != 1 {
				proxy.Errors = append(proxy.Errors, "Exactly one custom domain is supported")
			} else {
				proxy.Domain = strings.ToLower(importString(list[0]))
			}
		}
		if proxy.Type == "http" && proxy.RemotePort != 0 {
			proxy.Errors = append(proxy.Errors, "HTTP remotePort is not supported")
		}
		if proxy.Type == "tcp" && proxy.Domain != "" {
			proxy.Errors = append(proxy.Errors, "TCP customDomains is not supported")
		}
		if err := ValidateProxy(proxy.Type, proxy.LocalIP, proxy.LocalPort, proxy.RemotePort, proxy.Domain); err != nil {
			proxy.Errors = append(proxy.Errors, err.Error())
		}
		endpoint := fmt.Sprintf("%s:%d:%s", proxy.Type, proxy.RemotePort, proxy.Domain)
		if names[proxy.Name] || endpoints[endpoint] {
			proxy.Errors = append(proxy.Errors, "Duplicate proxy name or public endpoint in configuration")
		}
		if len(proxy.Errors) == 0 {
			names[proxy.Name] = true
			endpoints[endpoint] = true
		}
		result.Proxies = append(result.Proxies, proxy)
	}
	return result, nil
}

func parseImportINI(content string) (map[string]any, error) {
	cfg, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: false}, []byte(content))
	if err != nil {
		return nil, err
	}
	raw := map[string]any{"proxies": []any{}}
	common, err := cfg.GetSection("common")
	if err != nil {
		return nil, err
	}
	for _, section := range cfg.Sections() {
		if section.Name() == ini.DefaultSection {
			if len(section.Keys()) > 0 {
				return nil, fmt.Errorf("unexpected INI defaults")
			}
			continue
		}
		if section == common {
			continue
		}
		proxy := map[string]any{"name": section.Name()}
		for _, key := range section.Keys() {
			switch key.Name() {
			case "type":
				proxy["type"] = key.String()
			case "local_ip":
				proxy["localIP"] = key.String()
			case "local_port", "remote_port":
				n, err := key.Int()
				if err != nil {
					return nil, err
				}
				name := "localPort"
				if key.Name() == "remote_port" {
					name = "remotePort"
				}
				proxy[name] = n
			case "custom_domains":
				domains := []any{}
				for _, domain := range strings.Split(key.String(), ",") {
					domains = append(domains, strings.TrimSpace(domain))
				}
				proxy["customDomains"] = domains
			default:
				proxy["unsupported"] = true
			}
		}
		raw["proxies"] = append(raw["proxies"].([]any), proxy)
	}
	auth := map[string]any{}
	tls := map[string]any{}
	// Legacy INI defaults TLS off, unlike modern FRPC formats.
	tls["enable"] = false
	for _, key := range common.Keys() {
		switch key.Name() {
		case "server_addr":
			raw["serverAddr"] = key.String()
		case "server_port":
			n, err := key.Int()
			if err != nil {
				return nil, err
			}
			raw["serverPort"] = n
		case "user":
			raw["user"] = key.String()
		case "token":
			auth["token"] = key.String()
		case "authentication_method":
			auth["method"] = key.String()
		case "tls_enable":
			b, err := key.Bool()
			if err != nil {
				return nil, err
			}
			tls["enable"] = b
		case "log_file", "log_level", "log_max_days", "disable_log_color":
			raw["log"] = true
		default:
			raw["unsupported"] = true
		}
	}
	raw["auth"] = auth
	raw["transport"] = map[string]any{"tls": tls}
	return raw, nil
}
