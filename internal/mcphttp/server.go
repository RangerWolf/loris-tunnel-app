package mcphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type ExecuteFunc func(command string) (success bool, output string, errMsg string, callErr error)

type Server struct {
	srv     *http.Server
	baseURL string
}

func (s *Server) BaseURL() string {
	return s.baseURL
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

func Start(listenHost string, port int, token string, execFn ExecuteFunc) (*Server, error) {
	if strings.TrimSpace(listenHost) == "" {
		listenHost = "127.0.0.1"
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("mcp token is empty")
	}
	if port <= 0 {
		return nil, fmt.Errorf("invalid mcp port")
	}
	if execFn == nil {
		return nil, fmt.Errorf("execute function is nil")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Query().Get("token")) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeResponse(w, rpcResponse{JSONRPC: "2.0", ID: nil, Error: &rpcError{Code: -32700, Message: "parse error"}})
			return
		}

		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "loris-ssh-pilot",
					"version": "0.1.0",
				},
			}
		case "tools/list":
			resp.Result = map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "execute_bash",
						"description": "Execute one read-only bash command via SSH Pilot whitelist validation.",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"command": map[string]interface{}{
									"type":        "string",
									"description": "Single-line shell command.",
								},
							},
							"required": []string{"command"},
						},
					},
				},
			}
		case "tools/call":
			name, command := parseToolCall(req.Params)
			if name != "execute_bash" {
				resp.Error = &rpcError{Code: -32602, Message: "unsupported tool"}
				break
			}
			ok, output, errMsg, callErr := execFn(command)
			if callErr != nil {
				resp.Error = &rpcError{Code: -32000, Message: callErr.Error()}
				break
			}
			text := strings.TrimSpace(output)
			if strings.TrimSpace(errMsg) != "" {
				if text != "" {
					text += "\n"
				}
				text += strings.TrimSpace(errMsg)
			}
			resp.Result = map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": text},
				},
				"isError": !ok,
			}
		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found"}
		}
		writeResponse(w, resp)
	})

	addr := net.JoinHostPort(listenHost, fmt.Sprintf("%d", port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	out := &Server{
		srv:     srv,
		baseURL: fmt.Sprintf("http://%s/mcp?token=%s", addr, token),
	}
	go func() { _ = srv.Serve(ln) }()
	return out, nil
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func parseToolCall(params interface{}) (string, string) {
	raw, _ := json.Marshal(params)
	var payload struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	_ = json.Unmarshal(raw, &payload)
	cmd, _ := payload.Arguments["command"].(string)
	return strings.TrimSpace(payload.Name), strings.TrimSpace(cmd)
}

func writeResponse(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
