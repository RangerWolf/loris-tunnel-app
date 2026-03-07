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
