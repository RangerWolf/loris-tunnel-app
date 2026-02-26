package model

// Jumper is the SSH jumper configuration used by the frontend.
type Jumper struct {
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

// Tunnel is the SSH tunnel configuration used by the frontend.
type Tunnel struct {
	ID          int    `json:"id" toml:"id"`
	Name        string `json:"name" toml:"name"`
	Mode        string `json:"mode" toml:"mode"`
	JumperIDs   []int  `json:"jumperIds" toml:"jumper_ids"`
	LocalHost   string `json:"localHost" toml:"local_host"`
	LocalPort   int    `json:"localPort" toml:"local_port"`
	RemoteHost  string `json:"remoteHost" toml:"remote_host"`
	RemotePort  int    `json:"remotePort" toml:"remote_port"`
	AutoStart   bool   `json:"autoStart" toml:"auto_start"`
	Status      string `json:"status" toml:"status"`
	LastError   string `json:"lastError" toml:"last_error"`
	Description string `json:"description" toml:"description"`
	LatencyMs   int64  `json:"latencyMs,omitempty" toml:"-"`
}

// State is the full frontend state stored in config.
type State struct {
	Jumpers []Jumper `json:"jumpers"`
	Tunnels []Tunnel `json:"tunnels"`
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
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	JumperIDs   []int  `json:"jumperIds"`
	LocalHost   string `json:"localHost"`
	LocalPort   int    `json:"localPort"`
	RemoteHost  string `json:"remoteHost"`
	RemotePort  int    `json:"remotePort"`
	AutoStart   bool   `json:"autoStart"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

// TunnelConnectionTestResult is returned by TestTunnelConnection API.
type TunnelConnectionTestResult struct {
	LatencyMs int64 `json:"latencyMs"`
}
