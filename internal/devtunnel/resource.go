package devtunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	ResourceReady       = "ready"
	ResourceAuth        = "auth"
	ResourceNetwork     = "network"
	ResourceNotFound    = "not_found"
	ResourcePermission  = "permission"
	ResourceError       = "error"
	ResourceUnavailable = "unavailable"
)

// ErrNoPorts means the remote tunnel exists and has no port yet.
var ErrNoPorts = errors.New("dev tunnel has no ports")

// Port is one remote port returned by the CLI. URLs are copied from CLI fields only.
type Port struct {
	Number   int    `json:"port"`
	Protocol string `json:"protocol"`
}

// Resource is one Microsoft Dev Tunnel visible to the signed-in CLI account.
type Resource struct {
	ID              string   `json:"id"`
	Ports           []Port   `json:"ports"`
	PortsKnown      bool     `json:"portsKnown"`
	PortsError      string   `json:"portsError,omitempty"`
	Access          string   `json:"access"`
	Expiration      string   `json:"expiration"`
	URLs            []string `json:"urls"`
	HostConnections int      `json:"hostConnections"`
}

// ResourceList is a CLI query result. A failed query is never an empty ready list.
type ResourceList struct {
	State  string     `json:"state"`
	Detail string     `json:"detail"`
	Items  []Resource `json:"items"`
}

// ResourceDetail is one tunnel lookup. Not-found is distinct from a network failure.
type ResourceDetail struct {
	State    string   `json:"state"`
	Detail   string   `json:"detail"`
	Resource Resource `json:"resource"`
}

// CreateResourceInput creates a persistent tunnel immediately, without a port.
// Region is a cluster id. Empty means Microsoft chooses the cluster.
// TunnelName is an optional custom tunnel id; empty means a random id.
type CreateResourceInput struct {
	Access     string `json:"access"`
	Expiration string `json:"expiration"`
	Region     string `json:"region"`
	TunnelName string `json:"tunnelName,omitempty"`
}

// DeleteResourceResult reports whether the remote tunnel was removed.
type DeleteResourceResult struct {
	State      string `json:"state"`
	Detail     string `json:"detail"`
	TunnelName string `json:"tunnelName"`
}

type parsedResource struct {
	Resource
	portsKnown bool
}

// ValidTunnelID reports whether id is safe to pass as a single CLI argument.
func ValidTunnelID(id string) bool {
	return validTunnelID(strings.TrimSpace(id))
}

// SameTunnelID treats a short id and its cluster-qualified form as one resource.
func SameTunnelID(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aShort, aCluster, aHas := strings.Cut(a, ".")
	bShort, bCluster, bHas := strings.Cut(b, ".")
	if aShort == "" || aShort != bShort {
		return false
	}
	if !aHas || !bHas {
		return true
	}
	return aCluster == bCluster
}

// MatchPort checks the requested service port without rejecting unrelated ports.
func MatchPort(resource Resource, port int, protocol string) error {
	for _, got := range resource.Ports {
		if got.Number != port {
			continue
		}
		if protocol != "auto" && !strings.EqualFold(got.Protocol, protocol) {
			return fmt.Errorf("dev tunnel port %d/%s does not match local %d/%s", got.Number, got.Protocol, port, protocol)
		}
		return nil
	}
	return ErrNoPorts
}

// ListResources reads the signed-in account's tunnels. It does not create or delete anything.
func (c *Client) ListResources(ctx context.Context) ResourceList {
	path, _, err := c.resolve()
	if err != nil {
		return ResourceList{State: ResourceUnavailable, Detail: "dev tunnel cli is not installed", Items: []Resource{}}
	}
	out, err := c.run(ctx, path, "list", "-j")
	if state, detail := classifyCLIFailure(out, err); state != "" {
		return ResourceList{State: state, Detail: detail, Items: []Resource{}}
	}
	if err != nil {
		return ResourceList{State: ResourceError, Detail: clip(out+" "+err.Error(), 240), Items: []Resource{}}
	}
	items, parseErr := ParseResourceList(out)
	if parseErr != nil {
		return ResourceList{State: ResourceError, Detail: parseErr.Error(), Items: []Resource{}}
	}
	// List summaries may omit ports and access. Fill missing fields with bounded concurrency.
	var pending sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i := range items {
		if items[i].PortsKnown && items[i].Access != "unknown" {
			continue
		}
		slots <- struct{}{}
		pending.Add(1)
		go func(item *Resource) {
			defer pending.Done()
			defer func() { <-slots }()
			if item.Access == "unknown" {
				item.Access = c.readAccess(ctx, path, item.ID)
			}
			if item.PortsKnown {
				return
			}
			out, err := c.run(ctx, path, "port", "list", item.ID, "-j")
			if state, detail := classifyCLIFailure(out, err); state != "" {
				item.PortsError = detail
				return
			}
			if err != nil {
				item.PortsError = err.Error()
				return
			}
			ports, urls, err := ParsePortList(out)
			if err != nil {
				item.PortsError = err.Error()
				return
			}
			item.Ports = nonNilPorts(ports)
			item.URLs = mergeURLs(item.URLs, urls)
			item.PortsKnown = true
		}(&items[i])
	}
	pending.Wait()
	return ResourceList{State: ResourceReady, Items: items}
}

// readAccess queries the ACL omitted from CLI list summaries.
func (c *Client) readAccess(ctx context.Context, path, id string) string {
	out, err := c.run(ctx, path, "access", "list", id, "-j")
	if err != nil {
		return "unknown"
	}
	var entries []any
	if json.Unmarshal([]byte(out), &entries) == nil {
		return accessFrom(map[string]any{"accessControlEntries": entries})
	}
	var object map[string]any
	if json.Unmarshal([]byte(out), &object) != nil {
		return "unknown"
	}
	return accessFrom(object)
}

// GetResource reads one tunnel. A failed lookup does not mean the id should be recreated.
func (c *Client) GetResource(ctx context.Context, id string) ResourceDetail {
	id = strings.TrimSpace(id)
	if !ValidTunnelID(id) {
		return ResourceDetail{State: ResourceError, Detail: "invalid dev tunnel id"}
	}
	path, _, err := c.resolve()
	if err != nil {
		return ResourceDetail{State: ResourceUnavailable, Detail: "dev tunnel cli is not installed"}
	}
	out, err := c.run(ctx, path, "show", id, "-j")
	if state, detail := classifyCLIFailure(out, err); state != "" {
		return ResourceDetail{State: state, Detail: detail}
	}
	if err != nil {
		return ResourceDetail{State: ResourceError, Detail: clip(out+" "+err.Error(), 240)}
	}
	parsed, parseErr := parseResourceOutput(out)
	if parseErr != nil || parsed.ID == "" {
		return ResourceDetail{State: ResourceError, Detail: "dev tunnel show did not return a tunnel"}
	}
	if !parsed.portsKnown {
		portOut, portErr := c.run(ctx, path, "port", "list", parsed.ID, "-j")
		if state, detail := classifyCLIFailure(portOut, portErr); state != "" {
			return ResourceDetail{State: state, Detail: detail}
		}
		if portErr != nil {
			return ResourceDetail{State: ResourceError, Detail: clip(portOut+" "+portErr.Error(), 240)}
		}
		ports, urls, portsErr := ParsePortList(portOut)
		if portsErr != nil {
			return ResourceDetail{State: ResourceError, Detail: portsErr.Error()}
		}
		parsed.Ports = ports
		parsed.URLs = mergeURLs(parsed.URLs, urls)
	}
	if parsed.Access == "unknown" {
		parsed.Access = c.readAccess(ctx, path, parsed.ID)
	}
	parsed.PortsKnown = true
	parsed.Ports = nonNilPorts(parsed.Ports)
	parsed.URLs = nonNilStrings(parsed.URLs)
	return ResourceDetail{State: ResourceReady, Resource: parsed.Resource}
}

// CreateResource creates a persistent tunnel now and returns it without a port.
func (c *Client) CreateResource(ctx context.Context, input CreateResourceInput) ResourceDetail {
	access := strings.TrimSpace(input.Access)
	if access == "" {
		access = "private"
	}
	if access != "private" && access != "public" {
		return ResourceDetail{State: ResourceError, Detail: "unsupported dev tunnel access"}
	}
	id, err := c.CreatePersistent(ctx, access == "public", input.Expiration, input.Region, input.TunnelName)
	if err != nil {
		if errors.Is(err, ErrTunnelNameConflict) {
			return ResourceDetail{State: ResourceError, Detail: ErrTunnelNameConflict.Error()}
		}
		state, detail := classifyCLIFailure(err.Error(), err)
		if state == "" {
			state = ResourceError
			detail = err.Error()
		}
		return ResourceDetail{State: state, Detail: detail}
	}
	expiration, _ := NormalizeExpiration(input.Expiration)
	return ResourceDetail{
		State: ResourceReady,
		Resource: Resource{
			ID:         id,
			Ports:      []Port{},
			PortsKnown: true,
			Access:     access,
			Expiration: expiration,
			URLs:       []string{},
		},
	}
}

func classifyCLIFailure(output string, err error) (string, string) {
	text := strings.TrimSpace(output)
	if err != nil {
		extra := strings.TrimSpace(err.Error())
		if extra != "" && !strings.Contains(text, extra) {
			text = strings.TrimSpace(text + " " + extra)
		}
	}
	lower := strings.ToLower(text)
	if text == "" && err == nil {
		return "", ""
	}
	switch {
	case strings.Contains(lower, "login token expired"),
		strings.Contains(lower, "login required"),
		strings.Contains(lower, "not logged"),
		strings.Contains(lower, "not signed"),
		strings.Contains(lower, "logged out"):
		return ResourceAuth, clip(text, 240)
	}
	if err == nil {
		return "", ""
	}
	switch {
	case strings.Contains(lower, "not found"),
		strings.Contains(lower, "does not exist"),
		strings.Contains(lower, "no tunnel"):
		return ResourceNotFound, clip(text, 240)
	case strings.Contains(lower, "forbidden"),
		strings.Contains(lower, "permission"),
		strings.Contains(lower, "not authorized"),
		strings.Contains(lower, "access denied"):
		return ResourcePermission, clip(text, 240)
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "timed out"),
		strings.Contains(lower, "deadline exceeded"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no such host"),
		strings.Contains(lower, "network"),
		strings.Contains(lower, "tls"):
		return ResourceNetwork, clip(text, 240)
	default:
		return ResourceError, clip(text, 240)
	}
}

// ParseResourceList reads `devtunnel list -j`.
func ParseResourceList(output string) ([]Resource, error) {
	raw, err := decodeCLIJSON(output)
	if err != nil {
		return nil, err
	}
	nodes := resourceNodes(raw)
	items := make([]Resource, 0, len(nodes))
	for _, node := range nodes {
		parsed, ok := resourceFromMap(node)
		if !ok || parsed.ID == "" {
			continue
		}
		parsed.Ports = nonNilPorts(parsed.Ports)
		parsed.URLs = nonNilStrings(parsed.URLs)
		items = append(items, parsed.Resource)
	}
	return items, nil
}

// ParsePortList reads `devtunnel port list -j`.
func ParsePortList(output string) ([]Port, []string, error) {
	raw, err := decodeCLIJSON(output)
	if err != nil {
		return nil, nil, err
	}
	switch typed := raw.(type) {
	case []any:
		ports, urls := portsFromList(typed)
		return ports, urls, nil
	case map[string]any:
		if list, ok := firstArray(typed, "ports", "items", "value"); ok {
			ports, urls := portsFromList(list)
			return ports, urls, nil
		}
		if port, urls, ok := portFromMap(typed); ok {
			return []Port{port}, urls, nil
		}
		return []Port{}, []string{}, nil
	default:
		return nil, nil, fmt.Errorf("dev tunnel port list did not return json")
	}
}

func parseResourceOutput(output string) (parsedResource, error) {
	raw, err := decodeCLIJSON(output)
	if err != nil {
		return parsedResource{}, err
	}
	nodes := resourceNodes(raw)
	if len(nodes) == 0 {
		return parsedResource{}, fmt.Errorf("dev tunnel show did not return a tunnel")
	}
	parsed, ok := resourceFromMap(nodes[0])
	if !ok {
		return parsedResource{}, fmt.Errorf("dev tunnel show did not return a tunnel")
	}
	return parsed, nil
}

func decodeCLIJSON(output string) (any, error) {
	payload := extractJSONValue(output)
	if payload == "" {
		return nil, fmt.Errorf("dev tunnel command did not return json")
	}
	var raw any
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return nil, fmt.Errorf("dev tunnel command did not return json")
	}
	return raw, nil
}

func resourceNodes(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []any:
		return mapsOnly(typed)
	case map[string]any:
		if list, ok := firstArray(typed, "tunnels", "items", "value"); ok {
			return mapsOnly(list)
		}
		if inner, ok := typed["tunnel"].(map[string]any); ok {
			return []map[string]any{inner}
		}
		if _, ok := typed["tunnelId"].(string); ok {
			return []map[string]any{typed}
		}
	}
	return nil
}

func resourceFromMap(object map[string]any) (parsedResource, bool) {
	id := qualifyTunnelID(firstString(object, "tunnelId", "id"), firstString(object, "clusterId"))
	if id == "" {
		return parsedResource{}, false
	}
	ports, urls, known := portsField(object)
	urls = mergeURLs(urls, urlValues(object, "uri", "tunnelUri", "url"))
	return parsedResource{
		Resource: Resource{
			ID:              id,
			Ports:           ports,
			PortsKnown:      known,
			Access:          accessFrom(object),
			Expiration:      expirationFrom(object),
			URLs:            preferURLList(urls),
			HostConnections: intField(object, "hostConnections", "hostConnectionCount"),
		},
		portsKnown: known,
	}, true
}

func qualifyTunnelID(id, cluster string) string {
	id = strings.TrimSpace(id)
	cluster = strings.TrimSpace(cluster)
	if id == "" {
		return ""
	}
	if cluster == "" || strings.Contains(id, ".") {
		return id
	}
	return id + "." + cluster
}

func portsField(object map[string]any) ([]Port, []string, bool) {
	raw, ok := object["ports"]
	if !ok || raw == nil {
		return []Port{}, nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		return []Port{}, nil, false
	}
	ports, urls := portsFromList(list)
	return ports, urls, true
}

func portsFromList(list []any) ([]Port, []string) {
	ports := make([]Port, 0, len(list))
	var urls []string
	for _, item := range list {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		port, found, ok := portFromMap(object)
		if !ok {
			continue
		}
		ports = append(ports, port)
		urls = mergeURLs(urls, found)
	}
	return ports, urls
}

func portFromMap(object map[string]any) (Port, []string, bool) {
	number := intField(object, "portNumber", "port", "localPort")
	if number < 1 || number > 65535 {
		return Port{}, nil, false
	}
	protocol := strings.ToLower(firstString(object, "protocol"))
	return Port{Number: number, Protocol: protocol}, urlValues(object, "uri", "portUri", "url"), true
}

func accessFrom(object map[string]any) string {
	if allowed, ok := object["allowAnonymous"].(bool); ok {
		if allowed {
			return "public"
		}
		return "private"
	}
	entries, ok := firstArray(object, "accessControlEntries", "accessControl", "entries")
	if !ok {
		if control, nested := object["accessControl"].(map[string]any); nested {
			entries, ok = firstArray(control, "entries")
		}
	}
	if !ok {
		return "unknown"
	}
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(item, "type", "kind"))
		if !strings.Contains(kind, "anonymous") {
			continue
		}
		// Older CLI responses omit scopes; explicit scopes must include connect.
		scopes, scoped := item["scopes"].([]any)
		if !scoped {
			return "public"
		}
		for _, scope := range scopes {
			if value, ok := scope.(string); ok && strings.EqualFold(value, "connect") {
				return "public"
			}
		}
	}
	return "private"
}

func expirationFrom(object map[string]any) string {
	if value := firstString(object, "expiration"); value != "" {
		return value
	}
	return ""
}

func urlValues(object map[string]any, keys ...string) []string {
	var urls []string
	for _, key := range keys {
		value := firstString(object, key)
		if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
			urls = append(urls, value)
		}
	}
	return urls
}

func preferURLList(urls []string) []string {
	urls = mergeURLs(nil, urls)
	if len(urls) == 0 {
		return []string{}
	}
	best := preferBrowserURL(strings.Join(urls, ", "))
	if best == "" {
		return urls
	}
	ordered := []string{best}
	for _, item := range urls {
		if item != best {
			ordered = append(ordered, item)
		}
	}
	return ordered
}

func mergeURLs(base, extra []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(extra))
	for _, item := range append(append([]string{}, base...), extra...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func intField(object map[string]any, keys ...string) int {
	for _, key := range keys {
		switch value := object[key].(type) {
		case float64:
			return int(value)
		case int:
			return value
		}
	}
	return 0
}

func firstArray(object map[string]any, keys ...string) ([]any, bool) {
	for _, key := range keys {
		if list, ok := object[key].([]any); ok {
			return list, true
		}
	}
	return nil, false
}

func mapsOnly(list []any) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}
	return out
}

func nonNilPorts(ports []Port) []Port {
	if ports == nil {
		return []Port{}
	}
	return ports
}

func nonNilStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func extractJSONValue(text string) string {
	startObj := strings.Index(text, "{")
	startArr := strings.Index(text, "[")
	if startObj < 0 && startArr < 0 {
		return ""
	}
	start := startObj
	endMark := byte('}')
	if startArr >= 0 && (startObj < 0 || startArr < startObj) {
		start = startArr
		endMark = ']'
	}
	end := strings.LastIndexByte(text, endMark)
	if start < 0 || end <= start {
		return ""
	}
	return text[start : end+1]
}
