package model

import "strings"

const (
	ProviderFRP        = "frp"
	ProviderSSH        = "ssh"
	ProviderDevTunnel  = "devtunnel"
	ProviderCloudflare = "cloudflare"
)

// Jumper is the SSH jumper configuration used by the frontend.
type Jumper struct {
	NeedsCredentials       bool   `json:"needsCredentials,omitempty" toml:"needs_credentials,omitempty"`
	SyncID                 string `json:"syncId,omitempty" toml:"sync_id,omitempty"`
	ID                     int    `json:"id" toml:"id"`
	Name                   string `json:"name" toml:"name"`
	Host                   string `json:"host" toml:"host"`
	Port                   int    `json:"port" toml:"port"`
	User                   string `json:"user" toml:"user"`
	AuthType               string `json:"authType" toml:"auth_type"`
	KeyPath                string `json:"keyPath" toml:"key_path"`
	AgentSocketPath        string `json:"agentSocketPath" toml:"agent_socket_path"`
	Password               string `json:"password" toml:"password"`
	BypassHostVerification bool   `json:"bypassHostVerification" toml:"bypass_host_verification"`
	KeepAliveIntervalMs    int    `json:"keepAliveIntervalMs" toml:"keep_alive_interval_ms"`
	TimeoutMs              int    `json:"timeoutMs" toml:"timeout_ms"`
	HostKeyAlgorithms      string `json:"hostKeyAlgorithms" toml:"host_key_algorithms"`
	Notes                  string `json:"notes" toml:"notes"`
}

// TunnelGroup is a user-defined collection for organizing tunnels.
type TunnelGroup struct {
	SyncID string `json:"syncId,omitempty" toml:"sync_id,omitempty"`
	ID     int    `json:"id" toml:"id"`
	Name   string `json:"name" toml:"name"`
}

// DevTunnelConfig is the persisted Microsoft Dev Tunnel setup.
// Login tokens stay in the CLI credential store and are not written here.
// LastPublicURL caches the last successful host result.
// PublicURL is a live host result and is intentionally excluded from TOML.
type DevTunnelConfig struct {
	TunnelPort    int    `json:"tunnelPort,omitempty" toml:"tunnel_port,omitempty"`
	LastPublicURL string `json:"lastPublicUrl,omitempty" toml:"last_public_url,omitempty"`
	TunnelID      string `json:"tunnelId" toml:"tunnel_id"`
	// TunnelName is the requested custom id for a tunnel that does not exist
	// yet. After a successful create the actual id is stored in TunnelID and
	// this field is cleared.
	TunnelName string `json:"tunnelName,omitempty" toml:"tunnel_name,omitempty"`
	LocalHost  string `json:"localHost" toml:"local_host"`
	LocalPort  int    `json:"localPort" toml:"local_port"`
	Protocol   string `json:"protocol" toml:"protocol"`
	Access     string `json:"access" toml:"access"`
	Expiration string `json:"expiration,omitempty" toml:"expiration,omitempty"`
	Region     string `json:"region,omitempty" toml:"region,omitempty"`
	Account    string `json:"account,omitempty" toml:"account,omitempty"`
}

// CloudflareTunnelConfig is one locally-managed named tunnel.
// Credentials stay in ~/.cloudflared and are not written here.
type CloudflareTunnelConfig struct {
	TunnelID   string `json:"tunnelId" toml:"tunnel_id"`
	TunnelName string `json:"tunnelName,omitempty" toml:"tunnel_name,omitempty"`
	LocalHost  string `json:"localHost" toml:"local_host"`
	LocalPort  int    `json:"localPort" toml:"local_port"`
	Protocol   string `json:"protocol" toml:"protocol"`
	Hostname   string `json:"hostname" toml:"hostname"`
}

type FRPTunnelConfig struct {
	ServerID   string `json:"serverId" toml:"server_id"`
	ProxyName  string `json:"proxyName" toml:"proxy_name"`
	RemotePort int    `json:"remotePort" toml:"remote_port"`
	Domain     string `json:"domain" toml:"domain"`
}

// ReconnectInfo is transient runtime diagnostics, never configuration.
type ReconnectInfo struct {
	Kind         string   `json:"kind"`
	Attempt      int      `json:"attempt"`
	DelaySeconds float64  `json:"delaySeconds"`
	HostIDs      []string `json:"hostIds,omitempty"`
	Detail       string   `json:"detail,omitempty"`
}

// Tunnel is a persisted connection. Empty Provider means an older SSH config.
type Tunnel struct {
	Reconnect         *ReconnectInfo          `json:"reconnect,omitempty" toml:"-"`
	FRP               *FRPTunnelConfig        `json:"frp,omitempty" toml:"frp,omitempty"`
	OriginState       string                  `json:"originState,omitempty" toml:"-"`
	LatencyState      string                  `json:"latencyState,omitempty" toml:"-"`
	LatencyCheckedAt  string                  `json:"latencyCheckedAt,omitempty" toml:"-"`
	LatencyHTTPStatus int                     `json:"latencyHttpStatus,omitempty" toml:"-"`
	LatencyDetail     string                  `json:"latencyDetail,omitempty" toml:"-"`
	SyncID            string                  `json:"syncId,omitempty" toml:"sync_id,omitempty"`
	ID                int                     `json:"id" toml:"id"`
	Name              string                  `json:"name" toml:"name"`
	Provider          string                  `json:"provider,omitempty" toml:"provider,omitempty"`
	GroupID           int                     `json:"groupId" toml:"group_id"`
	Mode              string                  `json:"mode" toml:"mode"`
	JumperIDs         []int                   `json:"jumperIds" toml:"jumper_ids"`
	LocalHost         string                  `json:"localHost" toml:"local_host"`
	LocalPort         int                     `json:"localPort" toml:"local_port"`
	RemoteHost        string                  `json:"remoteHost" toml:"remote_host"`
	RemotePort        int                     `json:"remotePort" toml:"remote_port"`
	AutoStart         bool                    `json:"autoStart" toml:"auto_start"`
	ManuallyStopped   bool                    `json:"manuallyStopped" toml:"manually_stopped"`
	Status            string                  `json:"status" toml:"status"`
	LastError         string                  `json:"lastError" toml:"last_error"`
	Description       string                  `json:"description" toml:"description"`
	DevTunnel         *DevTunnelConfig        `json:"devTunnel,omitempty" toml:"dev_tunnel,omitempty"`
	Cloudflare        *CloudflareTunnelConfig `json:"cloudflare,omitempty" toml:"cloudflare,omitempty"`
	LatencyMs         int64                   `json:"latencyMs,omitempty" toml:"-"`
	PublicURL         string                  `json:"publicUrl,omitempty" toml:"-"`
}

// ProviderName treats a missing provider as SSH so historical configs keep working.
func (t Tunnel) ProviderName() string {
	switch strings.TrimSpace(t.Provider) {
	case "", ProviderSSH:
		return ProviderSSH
	default:
		return strings.TrimSpace(t.Provider)
	}
}

// State is the full frontend state stored in config.
type State struct {
	Jumpers         []Jumper      `json:"jumpers"`
	Groups          []TunnelGroup `json:"groups"`
	Tunnels         []Tunnel      `json:"tunnels"`
	ActiveSlots     int           `json:"activeSlots"`
	ActiveSlotLimit int           `json:"activeSlotLimit"`
}

// TunnelGroupPayload is used by create/update group APIs.
type TunnelGroupPayload struct {
	Name string `json:"name"`
}

// JumperPayload is used by create/update APIs.
type JumperPayload struct {
	Name                   string `json:"name"`
	Host                   string `json:"host"`
	Port                   int    `json:"port"`
	User                   string `json:"user"`
	AuthType               string `json:"authType"`
	KeyPath                string `json:"keyPath"`
	AgentSocketPath        string `json:"agentSocketPath"`
	Password               string `json:"password"`
	BypassHostVerification bool   `json:"bypassHostVerification"`
	KeepAliveIntervalMs    int    `json:"keepAliveIntervalMs"`
	TimeoutMs              int    `json:"timeoutMs"`
	HostKeyAlgorithms      string `json:"hostKeyAlgorithms"`
	Notes                  string `json:"notes"`
}

// TunnelPayload is used by create/update APIs.
type TunnelPayload struct {
	FRP              *FRPTunnelConfig        `json:"frp,omitempty"`
	Name             string                  `json:"name"`
	Provider         string                  `json:"provider"`
	GroupID          int                     `json:"groupId"`
	Mode             string                  `json:"mode"`
	JumperIDs        []int                   `json:"jumperIds"`
	LocalHost        string                  `json:"localHost"`
	LocalPort        int                     `json:"localPort"`
	RemoteHost       string                  `json:"remoteHost"`
	RemotePort       int                     `json:"remotePort"`
	AutoStart        bool                    `json:"autoStart"`
	Status           string                  `json:"status"`
	Description      string                  `json:"description"`
	DevTunnel        *DevTunnelConfig        `json:"devTunnel"`
	DevTunnelIDMode  string                  `json:"devTunnelIdMode,omitempty"`
	Cloudflare       *CloudflareTunnelConfig `json:"cloudflare,omitempty"`
	CloudflareIDMode string                  `json:"cloudflareIdMode,omitempty"`
}

// TunnelConnectionTestResult is returned by TestTunnelConnection API.
type TunnelConnectionTestResult struct {
	LatencyMs int64 `json:"latencyMs"`
}
