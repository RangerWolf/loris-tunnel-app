package license

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"loris-tunnel/internal/model"
)

const (
	devBackendAPIBaseURL  = "http://localhost:8000/api/v1"
	prodBackendAPIBaseURL = "https://loris-tunnel-prod.flyml.net/api/v1"
	requestTimeout        = 10 * time.Second
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type redeemRequest struct {
	Code      string `json:"code"`
	MachineID string `json:"machine_id"`
}

type apiErrorResponse struct {
	Detail  string `json:"detail"`
	Message string `json:"message"`
}

func NewDefaultClient() *Client {
	return NewClientByBuildType("production")
}

func NewClientByBuildType(buildType string) *Client {
	baseURL := resolveBaseURLByBuildType(buildType)

	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

func resolveBaseURLByBuildType(buildType string) string {
	switch strings.ToLower(strings.TrimSpace(buildType)) {
	case "dev":
		return strings.TrimRight(devBackendAPIBaseURL, "/")
	default:
		// Any non-dev build type (e.g. production/debug) uses production backend.
		return strings.TrimRight(prodBackendAPIBaseURL, "/")
	}
}

func (c *Client) GetStatus(ctx context.Context, machineID string) (model.LicenseStatus, error) {
	if c == nil {
		return model.LicenseStatus{}, fmt.Errorf("license client is nil")
	}
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return model.LicenseStatus{}, fmt.Errorf("machine_id is empty")
	}

	query := url.Values{}
	query.Set("machine_id", machineID)
	path := c.baseURL + "/license/status?" + query.Encode()
	req, err := http.NewRequestWithContext(ensureContext(ctx), http.MethodGet, path, nil)
	if err != nil {
		return model.LicenseStatus{}, fmt.Errorf("create status request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return model.LicenseStatus{}, fmt.Errorf("request license status failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.LicenseStatus{}, fmt.Errorf("read status response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.LicenseStatus{}, buildAPIError(resp.StatusCode, body, "license status request failed")
	}

	var result model.LicenseStatus
	if err := json.Unmarshal(body, &result); err != nil {
		return model.LicenseStatus{}, fmt.Errorf("parse status response failed: %w", err)
	}
	return result, nil
}

func (c *Client) Redeem(ctx context.Context, machineID, code string) (model.LicenseRedeemResult, error) {
	if c == nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("license client is nil")
	}
	machineID = strings.TrimSpace(machineID)
	code = strings.TrimSpace(code)
	if machineID == "" {
		return model.LicenseRedeemResult{}, fmt.Errorf("machine_id is empty")
	}
	if code == "" {
		return model.LicenseRedeemResult{}, fmt.Errorf("code is empty")
	}

	payload, err := json.Marshal(redeemRequest{
		Code:      code,
		MachineID: machineID,
	})
	if err != nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("marshal redeem payload failed: %w", err)
	}

	path := c.baseURL + "/license/redeem"
	req, err := http.NewRequestWithContext(ensureContext(ctx), http.MethodPost, path, bytes.NewReader(payload))
	if err != nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("create redeem request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("request license redeem failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("read redeem response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.LicenseRedeemResult{}, buildAPIError(resp.StatusCode, body, "license redeem request failed")
	}

	var result model.LicenseRedeemResult
	if err := json.Unmarshal(body, &result); err != nil {
		return model.LicenseRedeemResult{}, fmt.Errorf("parse redeem response failed: %w", err)
	}
	return result, nil
}

func (c *Client) ReportUsageEvent(ctx context.Context, payload model.UsageEventRequest) (model.UsageEventResponse, error) {
	if c == nil {
		return model.UsageEventResponse{}, fmt.Errorf("license client is nil")
	}
	payload.MachineID = strings.TrimSpace(payload.MachineID)
	payload.EventType = strings.TrimSpace(payload.EventType)
	if payload.MachineID == "" {
		return model.UsageEventResponse{}, fmt.Errorf("machine_id is empty")
	}
	if payload.EventType == "" {
		return model.UsageEventResponse{}, fmt.Errorf("event_type is empty")
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return model.UsageEventResponse{}, fmt.Errorf("marshal usage event payload failed: %w", err)
	}

	path := c.baseURL + "/client/usage/events"
	req, err := http.NewRequestWithContext(ensureContext(ctx), http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return model.UsageEventResponse{}, fmt.Errorf("create usage event request failed: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return model.UsageEventResponse{}, fmt.Errorf("request usage event failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.UsageEventResponse{}, fmt.Errorf("read usage event response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.UsageEventResponse{}, buildAPIError(resp.StatusCode, body, "usage event request failed")
	}

	var result model.UsageEventResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return model.UsageEventResponse{}, fmt.Errorf("parse usage event response failed: %w", err)
	}
	return result, nil
}

func ensureContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func buildAPIError(statusCode int, body []byte, fallback string) error {
	var payload apiErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Detail); msg != "" {
			return fmt.Errorf("%s: %s", fallback, msg)
		}
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return fmt.Errorf("%s: %s", fallback, msg)
		}
	}
	return fmt.Errorf("%s (HTTP %d)", fallback, statusCode)
}
