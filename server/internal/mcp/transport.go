// Package mcp implements ConfigFlow's stateless MCP JSON-RPC HTTP transport,
// administrative authentication and tool adapters. Business writes remain in
// their REST owners; this package never implements device or repository writes.
package mcp

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/version"
)

// Definition advertises one tool and its native JSON schema.
type Definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

//go:embed catalog.json
var catalog []byte

var logger = logx.Get("backend.mcp_server.server")

// Register installs all six explicit MCP transport entries.
func Register(r *httpx.Router) {
	for _, path := range []string{"/mcp", "/mcp/"} {
		r.POST(path, "mcp.handle_mcp", handle)
		r.Handle([]string{http.MethodGet, http.MethodDelete}, path, "mcp.unsupported_transport", unsupported)
	}
}
func unsupported(*httpx.Ctx) (*httpx.Response, error) {
	return httpx.JSONStatus(405, rpcError(nil, -32601, msgStateless)), nil
}

type rpcFault struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcFault       `json:"error,omitempty"`
}

func rpcError(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcFault{Code: code, Message: message}}
}
func rpcResult(id json.RawMessage, result json.RawMessage) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

type message struct {
	JSONRPC json.RawMessage `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  json.RawMessage `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type callParams struct {
	Name      json.RawMessage `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type toolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

func authenticate(c *httpx.Ctx) (bool, error) {
	r, err := repo.GetRepository(c.Context())
	if err != nil {
		return false, err
	}
	var settings json.RawMessage
	if err := r.View(c.Context(), func(doc *model.Document) error {
		raw, present, err := repo.SystemMemberJSON(&doc.System, "system_config")
		if err != nil {
			return err
		}
		if present {
			settings = raw
		}
		return nil
	}); err != nil {
		return false, err
	}
	damaged, err := r.SystemSettingsDamaged(c.Context())
	if err != nil {
		return false, err
	}
	if damaged {
		return false, nil
	}
	bearer, _ := auth.ParseBearerToken(c.Request.Header.Get("Authorization"), true)
	query, _ := c.Arg("token")
	if !auth.IsTokenWithinLength(query) {
		query = ""
	}
	internal, err := r.RuleProxyTokensForSanitization(c.Context())
	if err != nil {
		return false, err
	}
	if internal.Has(bearer) || internal.Has(query) {
		return false, nil
	}
	equal := func(a, b string) bool {
		aa, bb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
		return subtle.ConstantTimeCompare(aa[:], bb[:]) == 1
	}
	// Credential authority must not use R5's zero fallback or numeric text:
	// a damaged stored token must never become anonymous access or a new token.
	if !unset(settings) && bytes.TrimSpace(settings)[0] != '{' {
		return false, nil
	}
	token := ""
	malformed := false
	if raw, ok := asObject(settings)["config_token"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		malformed = json.Unmarshal(raw, &token) != nil
	}
	if !malformed && token != "" && (equal(token, bearer) || equal(token, query)) {
		return true, nil
	}
	if auth.IsAuthEnabled() {
		if bearer == "" {
			return false, nil
		}
		v, err := auth.VerifyToken(bearer)
		return err == nil && !v.Failed(), nil
	}
	return !malformed && token == "", nil
}

func handle(c *httpx.Ctx) (*httpx.Response, error) {
	ok, err := authenticate(c)
	if err != nil {
		return nil, err
	}
	if !ok {
		return httpx.JSONStatus(401, rpcError(nil, -32600, "Unauthorized")), nil
	}
	// Python reads raw JSON regardless of Content-Type. Use the shared strict
	// binder with a private header copy, preserving the actual incoming request.
	clone := *c
	clone.Request = c.Request.Clone(c.Context())
	clone.Request.Header.Set("Content-Type", "application/json")
	var payload json.RawMessage
	if clone.Bind(&payload) != nil {
		return httpx.JSONStatus(400, rpcError(nil, -32700, msgParse)), nil
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(trimmed, &batch) != nil {
			return httpx.JSONStatus(400, rpcError(nil, -32700, msgParse)), nil
		}
		if len(batch) == 0 {
			return httpx.JSONStatus(400, rpcError(nil, -32600, msgBatchEmpty)), nil
		}
		responses := []rpcResponse{}
		for _, raw := range batch {
			if b := bytes.TrimSpace(raw); len(b) > 0 && b[0] == '{' {
				if res := dispatch(c, raw); res != nil {
					responses = append(responses, *res)
				}
			}
		}
		if len(responses) == 0 {
			return httpx.Empty(202), nil
		}
		return httpx.JSON(responses), nil
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return httpx.JSONStatus(400, rpcError(nil, -32600, msgRoot)), nil
	}
	res := dispatch(c, trimmed)
	if res == nil {
		return httpx.Empty(202), nil
	}
	return httpx.JSON(res), nil
}
func dispatch(c *httpx.Ctx, raw json.RawMessage) (response *rpcResponse) {
	var m message
	if jsondecode.Unmarshal(raw, &m) != nil {
		return nil
	}
	if m.ID == nil {
		return nil
	} // Notifications are never executed, including writes.
	defer func() {
		if recover() != nil {
			logger.Error(msgInternal)
			r := rpcError(m.ID, -32603, msgInternal)
			response = &r
		}
	}()
	var method, protocol string
	valid := json.Unmarshal(m.Method, &method) == nil && method != "" && json.Unmarshal(m.JSONRPC, &protocol) == nil && protocol == "2.0"
	if !valid {
		r := rpcError(m.ID, -32600, msgInvalidRequest)
		return &r
	}
	var result json.RawMessage
	var err error
	switch method {
	case "initialize":
		if !unset(m.Params) && bytes.TrimSpace(m.Params)[0] != '{' {
			r := rpcError(m.ID, -32603, msgInternal)
			return &r
		}
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = jsondecode.Unmarshal(m.Params, &p)
		v := p.ProtocolVersion
		if v != "2025-06-18" && v != "2025-03-26" && v != "2024-11-05" {
			v = "2025-06-18"
		}
		result, err = json.Marshal(struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools struct {
					ListChanged bool `json:"listChanged"`
				} `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			Instructions string `json:"instructions"`
		}{ProtocolVersion: v, ServerInfo: struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}{"configflow", version.Version}, Instructions: instructions})
	case "ping":
		result = json.RawMessage(`{}`)
	case "tools/list":
		result = packed(object{"tools": catalog})
	case "tools/call":
		result, err = execute(c, m.Params)
	default:
		r := rpcError(m.ID, -32601, fmt.Sprintf(msgUnknownMethod, method))
		return &r
	}
	if err != nil {
		var invalid *invalidParams
		r := rpcError(m.ID, -32603, msgInternal)
		if errors.As(err, &invalid) {
			r = rpcError(m.ID, -32602, invalid.message)
		} else {
			logger.Error(msgInternal)
		}
		return &r
	}
	r := rpcResult(m.ID, result)
	return &r
}

type invalidParams struct{ message string }

func (e *invalidParams) Error() string { return e.message }
func execute(c *httpx.Ctx, params json.RawMessage) (json.RawMessage, error) {
	var p callParams
	if !unset(params) && jsondecode.Unmarshal(params, &p) != nil {
		return nil, errors.New(msgType)
	}
	var name string
	if json.Unmarshal(p.Name, &name) != nil || name == "" {
		return nil, &invalidParams{msgName}
	}
	argsRaw := p.Arguments
	if unset(argsRaw) {
		argsRaw = json.RawMessage(`{}`)
	}
	if trimmed := bytes.TrimSpace(argsRaw); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, &invalidParams{msgArguments}
	}
	if !knownTool(name) {
		return nil, &invalidParams{fmt.Sprintf(msgUnknownTool, name)}
	}
	// Detach once per tool, retaining the originating generation and repository.
	ctx := lifecycle.Detach(c.Context())
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	invocation, err := newInvocation(ctx, c.App(), argsRaw)
	var payload json.RawMessage
	if err == nil {
		payload, err = invocation.run(ctx, name)
	}
	failed := false
	if err != nil {
		var api *apiError
		if !errors.As(err, &api) {
			return nil, err
		}
		failed = true
		payload = text(fmt.Sprintf(msgCallFailed, api.status, api.message))
		// Python logs the business failure as a warning; internal tokens
		// never reach the log.
		if r, repoErr := repo.GetRepository(ctx); repoErr == nil {
			if logged, err := redact.RedactSecretsInText(ctx, r, api.message); err == nil {
				logger.Warningf(logToolFailed, name, logged)
			}
		}
	}
	content, err := publicText(ctx, payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(toolResult{Content: []textContent{{Type: "text", Text: content}}, IsError: failed})
}
func publicText(ctx context.Context, payload json.RawMessage) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	sysCfg, err := r.SystemConfig(ctx)
	if err != nil {
		return "", err
	}
	settings, err := json.Marshal(sysCfg)
	if err != nil {
		return "", err
	}
	public, err := redact.SanitizeExternalPayload(ctx, r, payload, json.RawMessage(settings))
	if err != nil {
		return "", err
	}
	// Python json.dumps(ensure_ascii=False): no HTML escaping of & < >.
	encoded, err := jsonresp.EncodeUTF8(public)
	if err != nil {
		return "", err
	}
	var s string
	if json.Unmarshal(encoded, &s) == nil {
		return s, nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", "  "); err != nil {
		return "", err
	}
	return pretty.String(), nil
}
