package model

type LicenseStatus struct {
	Active     bool   `json:"active"`
	ExpireTime string `json:"expire_time,omitempty"`
	IsLifetime bool   `json:"is_lifetime"`
	Code       string `json:"code,omitempty"`
}

type LicenseRedeemResult struct {
	Success    bool   `json:"success"`
	Active     bool   `json:"active"`
	ExpireTime string `json:"expire_time,omitempty"`
	AddedDays  int    `json:"added_days"`
	Message    string `json:"message"`
	Code       string `json:"code,omitempty"`
}

type UsageEventRequest struct {
	MachineID  string `json:"machine_id"`
	EventType  string `json:"event_type"`
	AppVersion string `json:"app_version,omitempty"`
	Platform   string `json:"platform,omitempty"`
	ClientTS   string `json:"client_ts,omitempty"`
}

type UsageEventResponse struct {
	Success bool `json:"success"`
}
