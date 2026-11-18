// Package ipv6 translates profile-explicit IPv6 API requests. Device checks,
// persisted evidence and background jobs belong to the Agent IPv6 owner.
package ipv6

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/agents"
	core "configflow/internal/agents/ipv6"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

// Register installs all five methods of the IPv6 readiness blueprint.
func Register(r *httpx.Router) {
	handler := auth.Require(operation)
	r.GET("/api/agents/<agent_id>/ipv6-readiness", "agents.ipv6_readiness_operation", handler)
	r.GET("/api/agents/<agent_id>/ipv6-readiness/verify", "agents.ipv6_readiness_operation", handler)
	r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/ipv6-readiness/verification-settings", "agents.ipv6_readiness_operation", handler)
	r.POST("/api/agents/<agent_id>/ipv6-readiness/<operation>", "agents.ipv6_readiness_operation", handler)
}

type failureBody struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func rejected(code string, status int, message string) (*httpx.Response, error) {
	return httpx.JSONStatus(status, failureBody{false, code, message}).NoStore(), nil
}
func operation(c *httpx.Ctx) (*httpx.Response, error) {
	op := c.Param("operation")
	if op == "" {
		if strings.HasSuffix(c.Request.URL.Path, "/verification-settings") {
			op = "verification-settings"
		} else if strings.HasSuffix(c.Request.URL.Path, "/verify") {
			op = "verify"
		} else {
			op = "status"
		}
	}
	method := c.Request.Method
	allowed := op == "status" && method == "GET" || op == "prepare" && method == "POST" || op == "probe" && method == "POST" || op == "verify" && (method == "GET" || method == "POST") || op == "verification-settings" && (method == "GET" || method == "PUT")
	if !allowed {
		return rejected("NOT_ALLOWED", 404, msgNotAllowed)
	}
	if c.Request.ContentLength > 4096 {
		return rejected("INVALID_REQUEST", 400, msgTooLarge)
	}
	body := core.Document{}
	if method == "POST" || method == "PUT" {
		data, e := c.Data()
		if e != nil {
			return nil, e
		}
		if len(data) > 4096 {
			return rejected("INVALID_REQUEST", 400, msgTooLarge)
		}
		if !c.BindSilent(&body) || body == nil {
			return rejected("INVALID_REQUEST", 400, msgObject)
		}
	}
	selected := []string{}
	for _, value := range []string{c.Request.Header.Get("X-ConfigFlow-Profile"), c.Request.URL.Query().Get("profile_id")} {
		if value != "" {
			selected = append(selected, value)
		}
	}
	if b, ok := body["profile_id"]; ok {
		var p string
		if string(b) != "null" && string(b) != "false" && string(b) != "0" && string(b) != `""` {
			if json.Unmarshal(b, &p) != nil || p == "" {
				return rejected("PROFILE_REQUIRED", 400, msgProfile)
			}
			selected = append(selected, p)
		}
		delete(body, "profile_id")
	}
	if len(selected) == 0 {
		return rejected("PROFILE_REQUIRED", 400, msgProfile)
	}
	for _, p := range selected {
		if p != selected[0] {
			return rejected("PROFILE_REQUIRED", 400, msgProfile)
		}
	}
	profile, e := repo.ResolveProfileID(c.Context(), selected[0])
	if e != nil {
		return nil, e
	}
	m, e := agents.Get(c.Context())
	if e != nil {
		return nil, e
	}
	service := &core.Service{Manager: m, Checker: apiagents.ContentChecker}
	id := c.Param("agent_id")
	var data any
	switch op {
	case "status":
		refresh := c.Request.URL.Query().Get("refresh")
		data, e = service.Status(c.Context(), id, profile, refresh == "1" || refresh == "true")
	case "prepare":
		before, revision, err := m.Repository().GetPublishSnapshot(c.Context(), profile, true)
		if err != nil {
			return nil, err
		}
		var result *core.Report
		result, e = service.Prepare(c.Context(), id, profile, encode(body))
		if e == nil {
			after, current, err := m.Repository().GetPublishSnapshot(c.Context(), profile, true)
			if err != nil {
				return nil, err
			}
			if revision == current && same(before, after) {
				result.ConfigRevision = &revision
			}
			data = result
		}
	case "verify":
		if method == "GET" {
			data, e = service.Result(c.Context(), id, profile, c.Request.URL.Query().Get("deployment_id"))
		} else {
			data, e = service.Start(c.Context(), id, profile, encode(body), false)
		}
	case "verification-settings":
		var b json.RawMessage
		if method == "PUT" {
			b = encode(body)
		}
		data, e = service.VerificationSettings(c.Context(), id, profile, b)
	case "probe":
		if len(body) > 0 {
			return rejected("INVALID_REQUEST", 400, msgProbeBody)
		}
		data, e = service.Probe(c.Context(), id, profile)
	}
	status := 200
	if e != nil {
		var boundary *core.Error
		if !errors.As(e, &boundary) {
			return nil, e
		}
		status = boundary.Status
		data = failureBody{false, boundary.Code, boundary.Message}
	}
	public, e := service.Public(c.Context(), profile, data)
	if e != nil {
		return nil, e
	}
	return httpx.JSONStatus(status, public).NoStore(), nil
}
func encode(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func same(a, b any) bool           { return string(encode(a)) == string(encode(b)) }
