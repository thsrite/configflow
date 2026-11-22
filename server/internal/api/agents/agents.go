// Package agents holds the HTTP handlers of the "agents" blueprint
// (backend/routes/agents.py). Package #20 registers the pure registry routes:
// registration, heartbeat, manual creation, edit and delete of one Agent.
// The rest of the blueprint (listing with content sync, deployments, logs,
// upgrades, ...) is package #26; it extends Register here.
package agents

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

var logger = logx.Get("backend.routes.agents")

// HeartbeatMaxContentLength is _HEARTBEAT_MAX_CONTENT_LENGTH.
const HeartbeatMaxContentLength = 32 * 1024

var heartbeatFields = map[string]bool{"version": true, "config_version": true, "service_status": true, "system_metrics": true, "host": true}

var metricFields = map[string]map[string]bool{
	"cpu":     {"usage_percent": true, "core_count": true},
	"memory":  {"total": true, "used": true, "available": true, "used_percent": true},
	"disk":    {"total": true, "used": true, "free": true, "used_percent": true},
	"network": {"bytes_sent": true, "bytes_recv": true, "speed_sent": true, "speed_recv": true},
}

var registrationLogFields = []string{"name", "host", "port", "service_type", "version"}

// Register adds the registry routes.
func Register(r *httpx.Router) {
	r.POST("/api/agents/register", "agents.register_agent", registerAgent)
	r.POST("/api/agents/<agent_id>/heartbeat", "agents.agent_heartbeat", agentHeartbeat)
	r.Handle([]string{"GET", "POST"}, "/api/agents", "agents.handle_agents", auth.Require(createAgent))
	r.Handle([]string{"GET", "PUT", "DELETE"}, "/api/agents/<agent_id>", "agents.handle_agent_item", auth.Require(handleAgentItem))
	registerDNSFollowRoutes(r)
	registerDNSHARoutes(r)
	registerContentRoutes(r)
	registerDeployRoutes(r)
	registerRuleSync(r)
	registerMetrics(r)
	registerTraffic(r)
	registerOxidns(r)
	registerClientTools(r)
	registerPanel(r)
	registerGateway(r)
	registerInstaller(r)
	registerUpgrades(r)
	registerDockerHA(r)
	registerDownloads(r)
	registerGatewayInstallations(r)
}

func failure(status int, message string) *httpx.Response {
	return httpx.JSONStatus(status, map[string]any{"success": false, "message": message})
}

// RegistrationLogFields bounds printable scalar fields for registration diagnostics.
// It never treats projected strings as stored identity evidence.
func RegistrationLogFields(data json.RawMessage) map[string]string {
	fields := map[string]string{}
	members, err := jsondecode.Select(data, registrationLogFields...)
	if err != nil {
		return fields
	}
	for _, field := range registrationLogFields {
		raw, present := members[field]
		if !present {
			continue
		}
		var text string
		if len(raw) > 0 && raw[0] == '"' {
			if jsondecode.Unmarshal(raw, &text) != nil {
				continue
			}
		} else {
			var n json.Number
			if jsondecode.Unmarshal(raw, &n) != nil || string(raw) == "null" {
				continue
			}
			text = n.String()
		}
		var out []rune
		for _, r := range text {
			if !logx.IsPrintable(r) || len(out) == 128 {
				break
			}
			out = append(out, r)
		}
		fields[field] = string(out)
	}
	return fields
}
func registrationRejected(ctx context.Context, data map[string]string, reason string) {
	logger.Warningf("Agent 注册被拒绝：%s（%s，%s）：%s", data["name"], data["service_type"], data["host"], reason)
	notifications.Safely(func() error {
		repository, err := repo.GetRepository(ctx)
		if err != nil {
			return err
		}
		return notifications.RejectRegistration(ctx, repository, &model.Agent{Name: data["name"], ServiceType: data["service_type"], Host: data["host"]}, reason)
	})
}

// isJSONNull reports whether a body that decoded to nil was the literal null.
func isJSONNull(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return string(bytes.Trim(data, " \t\r\n")) == "null"
}

const unauthorizedReason = "面板里已有同名同地址的 Agent，但这次注册没有带它原来的凭据。" +
	"如果设备是重装的，请先在 Agent 页删除旧记录，设备会自动重新注册。"

func registerAgent(c *httpx.Ctx) (*httpx.Response, error) {
	var data map[string]string
	secrets := redact.NewSecrets([]string{os.Getenv("AGENT_REGISTRATION_KEY")})
	response, err := doRegister(c, &data, &secrets)
	if err == nil {
		return response, nil
	}
	var conflict *agents.DeploymentConflict
	var permission *agents.PermissionError
	var installation *agents.InstallationError
	switch {
	case errors.Is(err, agents.ErrRegistrationProfileType), errors.Is(err, agents.ErrRegistrationVersion):
		return failure(400, err.Error()), nil
	case errors.Is(err, agents.ErrRegistrationAuthorization):
		registrationRejected(c.Context(), data, RegistrationLocked)
		return failure(403, RegistrationLocked), nil
	case errors.As(err, &conflict):
		registrationRejected(c.Context(), data, conflict.Message)
		return failure(409, conflict.Message), nil
	case errors.As(err, &permission):
		registrationRejected(c.Context(), data, unauthorizedReason)
		return failure(401, "Unauthorized"), nil
	case errors.As(err, &installation):
		return failure(installation.Status, installation.Message), nil
	}
	message, _ := redact.SanitizeWithSecrets(err.Error(), secrets, false).(string)
	logger.Errorf("Agent注册失败: %s", message)
	logger.Errorf("错误详情: %s", message)
	return failure(500, message), nil
}

func doRegister(c *httpx.Ctx, out *map[string]string, secrets *redact.Secrets) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	var body json.RawMessage
	if err := bindForce(c, &body); err != nil {
		return nil, err
	}
	if isJSONNull(body) {
		logger.Error("Failed to parse JSON data")
		return failure(400, "Invalid JSON data"), nil
	}
	var data model.Fields
	if err := jsondecode.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("registration requires a JSON object: %w", err)
	}
	text := func(name string) string {
		raw, _ := data.Get(name)
		var value string
		if len(raw) > 0 && raw[0] == '"' {
			_ = jsondecode.Unmarshal(raw, &value)
		}
		return value
	}
	requestSecrets := []string{text("registration_token"), text("registration_key"), text("installation_token"), os.Getenv("AGENT_REGISTRATION_KEY")}
	header, present := c.Header("Authorization")
	existingToken, _ := auth.ParseBearerToken(header, present)
	requestSecrets = append(requestSecrets, existingToken)
	*secrets = redact.NewSecrets(requestSecrets)
	*out = safeRegistrationLogFields(body, *secrets)
	installationID, installationToken := text("installation_id"), text("installation_token")
	idRaw, idPresent := data.Get("installation_id")
	tokenRaw, tokenPresent := data.Get("installation_token")
	installing := idPresent && string(idRaw) != "null" || tokenPresent && string(tokenRaw) != "null"
	data.Delete("installation_id")
	data.Delete("installation_token")
	hooks := agents.CurrentHooks()
	if installing {
		if hooks.InstallationProfileTyped == nil {
			return nil, &agents.InstallationError{Message: "Gateway installation registration is not available yet", Status: 501}
		}
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		_, updated, err := hooks.InstallationProfileTyped(ctx, raw, installationID, installationToken, c.RemoteAddr())
		if err != nil {
			return nil, err
		}
		if err = jsondecode.Unmarshal(updated, &data); err != nil {
			return nil, err
		}
	}
	registrationKey, registrationToken := text("registration_key"), text("registration_token")
	data.Delete("registration_key")
	data.Delete("registration_token")
	key := os.Getenv("AGENT_REGISTRATION_KEY")
	admission := agents.RegistrationAuthorization{AllowNew: !auth.IsAuthEnabled() && key == ""}
	if key != "" && registrationKey != "" {
		left, right := sha256.Sum256([]byte(registrationKey)), sha256.Sum256([]byte(key))
		admission.AllowNew = subtle.ConstantTimeCompare(left[:], right[:]) == 1
	}
	if auth.IsAuthEnabled() && validPanelJWT(c) {
		admission.AllowNew = true
	}
	if auth.IsAuthEnabled() || key == "" {
		admission.Credential = registrationToken
	}
	host := text("host")
	admission.ReportedHost = host
	clientIP := c.RemoteAddr()
	if forwarded, ok := c.Header("X-Forwarded-For"); ok {
		clientIP = latin1(forwarded)
	}
	if strings.Contains(clientIP, ",") {
		clientIP = strings.TrimSpace(strings.Split(clientIP, ",")[0])
	}
	dockerIP := host == "localhost" || strings.HasPrefix(host, "127.")
	if address, err := netip.ParseAddr(host); err == nil && address.Is4() {
		octets := address.As4()
		dockerIP = dockerIP || octets[0] == 172 && octets[1] >= 17 && octets[1] <= 31
	}
	hostRaw, hostPresent := data.Get("host")
	hostEmpty := !hostPresent || string(hostRaw) == "null" || string(hostRaw) == `""` || string(hostRaw) == "false" || string(hostRaw) == "0" || string(hostRaw) == "[]" || string(hostRaw) == "{}"
	if !hostEmpty && (len(hostRaw) == 0 || hostRaw[0] != '"') {
		return nil, fmt.Errorf("registration host must be a string")
	}
	if !installing && (hostEmpty || dockerIP) {
		raw, _ := json.Marshal(clientIP)
		if err := data.Set("host", raw); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	*out = safeRegistrationLogFields(raw, *secrets)
	logger.Infof("Agent注册字段: %v", *out)
	var result json.RawMessage
	if installing {
		if hooks.InstallationRegisterTyped == nil {
			return nil, &agents.InstallationError{Message: "Gateway installation registration is not available yet", Status: 501}
		}
		result, err = hooks.InstallationRegisterTyped(ctx, manager, installationID, installationToken, raw, existingToken)
	} else {
		input, decodeErr := agents.DecodeRegistrationInput(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		registered, registerErr := manager.RegisterIdentity(ctx, input, agents.RegistrationOptions{ExistingToken: existingToken, Authorization: &admission})
		if registerErr != nil {
			return nil, registerErr
		}
		result, err = json.Marshal(registered)
	}
	if err != nil {
		return nil, err
	}
	var response map[string]json.RawMessage
	if err = jsondecode.Unmarshal(result, &response); err != nil {
		return nil, err
	}
	response["success"] = json.RawMessage("true")
	label := "Agent 重新注册"
	if string(response["is_new"]) == "true" {
		label = "新 Agent 注册成功"
	}
	logger.Infof("%s：%s（%s，%s）", label, (*out)["name"], (*out)["service_type"], (*out)["host"])
	return httpx.JSON(response), nil
}

// latin1 decodes a header value as WSGI does (ISO-8859-1).
func latin1(s string) string {
	runes := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		runes[i] = rune(s[i])
	}
	return string(runes)
}

// authenticatedAgent selects credentials only from their original JSON strings.
func authenticatedAgent(c *httpx.Ctx, manager *agents.Manager, id string) (*model.Agent, error) {
	a, err := manager.ReadAgent(c.Context(), id, false)
	if err != nil {
		return nil, err
	}
	if !agentTokenMatches(c, a) {
		return nil, nil
	}
	return a, nil
}

// ReadJSONBody returns bounded, native JSON with the route-specific error envelope.
func ReadJSONBody(c *httpx.Ctx, maxLength int64, invalidMessage string) (json.RawMessage, *httpx.Response) {
	return readBoundJSON(c, maxLength, invalidMessage)
}

// ValidateSystemMetrics validates protocol numbers without interpreting booleans,
// numeric strings or out-of-range integers as machine measurements.
func ValidateSystemMetrics(raw json.RawMessage) (bool, error) {
	var fields map[string]json.RawMessage
	if len(raw) > 16*1024 || jsondecode.Unmarshal(raw, &fields) != nil || fields == nil {
		return false, nil
	}
	for key := range fields {
		if _, known := metricFields[key]; !known && key != "collected_at" {
			return false, nil
		}
	}
	if collected, present := fields["collected_at"]; present && string(collected) != "null" {
		var text string
		if len(collected) == 0 || collected[0] != '"' || jsondecode.Unmarshal(collected, &text) != nil || len([]rune(text)) > 64 {
			return false, nil
		}
	}
	for section, allowed := range metricFields {
		raw, present := fields[section]
		if !present {
			continue
		}
		var values map[string]json.RawMessage
		if jsondecode.Unmarshal(raw, &values) != nil || values == nil {
			return false, nil
		}
		for field, raw := range values {
			if !allowed[field] {
				return false, nil
			}
			if len(raw) == 0 || raw[0] == '"' || string(raw) == "null" {
				return false, nil
			}
			var number json.Number
			if jsondecode.Unmarshal(raw, &number) != nil {
				return false, nil
			}
			if field == "usage_percent" || field == "used_percent" {
				n, err := number.Float64()
				if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || n < 0 || n > 100 {
					return false, nil
				}
			} else {
				if strings.ContainsAny(number.String(), ".eE") {
					return false, nil
				}
				n, err := strconv.ParseUint(number.String(), 10, 64)
				if err != nil || field == "core_count" && n > 65536 {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// ValidateHeartbeatPayload keeps the closed heartbeat schema and field limits.
func ValidateHeartbeatPayload(raw json.RawMessage) (bool, error) {
	var fields map[string]json.RawMessage
	if jsondecode.Unmarshal(raw, &fields) != nil || fields == nil {
		return false, nil
	}
	for key := range fields {
		if !heartbeatFields[key] {
			return false, nil
		}
	}
	for _, limit := range []struct {
		field string
		max   int
	}{{"version", 64}, {"config_version", 128}, {"service_status", 64}, {"host", 64}} {
		if raw, present := fields[limit.field]; present {
			var text string
			if len(raw) == 0 || raw[0] != '"' || jsondecode.Unmarshal(raw, &text) != nil || len([]rune(text)) > limit.max {
				return false, nil
			}
		}
	}
	if raw, present := fields["version"]; present {
		var version string
		if jsondecode.Unmarshal(raw, &version) != nil || !agents.ValidMachineVersion(version) {
			return false, nil
		}
	}
	if metrics, present := fields["system_metrics"]; present {
		return ValidateSystemMetrics(metrics)
	}
	return true, nil
}

// heartbeatFeature retains the existing optional feature flag semantics while
// the address hook consumes the already-read typed Agent snapshot. These are
// display/protocol preferences, never credential or ownership evidence.
func heartbeatFeature(agent *model.Agent, field string) bool {
	if agent == nil || agent.ServiceType != "mihomo" {
		return false
	}
	raw, present, err := model.EncodeMember(agent, field)
	if err != nil || !present {
		return false
	}
	enabled, err := model.JSONHasContent(raw)
	return err == nil && enabled
}

func agentHeartbeat(c *httpx.Ctx) (*httpx.Response, error) {
	response, err := heartbeat(c)
	if err != nil {
		return failure(500, err.Error()), nil
	}
	return response, nil
}

func heartbeat(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := manager.ReadAgent(ctx, agentID, false)
	if err != nil {
		return nil, err
	}
	if !agentTokenMatches(c, agent) {
		return failure(401, "Unauthorized"), nil
	}
	value, errorResponse := ReadJSONBody(c, HeartbeatMaxContentLength, "Invalid heartbeat data")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if valid, err := ValidateHeartbeatPayload(value); err != nil {
		return nil, err
	} else if !valid {
		return failure(400, "Invalid heartbeat data"), nil
	}
	var data model.Agent
	if err := jsondecode.Unmarshal(value, &data); err != nil {
		return failure(400, "Invalid heartbeat data"), nil
	}
	// The address is not part of the stored heartbeat; it only tells the panel
	// where the Agent now is.
	reported := data.Host
	data.Host = ""
	ok, err := manager.ObserveHeartbeat(ctx, agentID, &data)
	if err != nil {
		return nil, err
	}
	if !ok {
		return failure(404, "Agent not found"), nil
	}
	reply := map[string]any{"success": true, "domain_discovery_enabled": heartbeatFeature(agent, "domain_discovery_enabled"), "traffic_history_enabled": heartbeatFeature(agent, "traffic_history_enabled"), "reads_host": true}
	if host := reported; host != "" {
		hooks := agents.CurrentHooks()
		if hooks.AddressFollowTyped != nil {
			if err := hooks.AddressFollowTyped(ctx, manager, agent, host); err != nil {
				return nil, err
			}
		}
		if hooks.AddressConfirmedTyped != nil {
			if confirmed := hooks.AddressConfirmedTyped(agent, host); confirmed != "" {
				reply["confirmed_host"] = confirmed
			}
		}
	}

	return httpx.JSON(reply), nil
}

func createAgent(c *httpx.Ctx) (*httpx.Response, error) {
	if c.Method() == "GET" {
		return listContentAgents(c)
	}
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	var body json.RawMessage
	if err := c.Bind(&body); err != nil {
		return failure(500, err.Error()), nil
	}
	input, err := agents.DecodeRegistrationInput(body)
	if err != nil {
		return failure(500, err.Error()), nil
	}
	result, err := manager.RegisterIdentity(ctx, input, agents.RegistrationOptions{})
	if errors.Is(err, agents.ErrRegistrationVersion) {
		return failure(400, err.Error()), nil
	}
	if err != nil {
		var conflict *agents.DeploymentConflict
		if errors.As(err, &conflict) {
			return failure(409, conflict.Message), nil
		}
		return failure(500, err.Error()), nil
	}
	return httpx.JSON(map[string]any{"success": true, "data": result}), nil
}

func handleAgentItem(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	if c.Method() == "GET" {
		return getContentAgent(c, manager)
	}
	var ok bool
	if c.Method() == "PUT" {
		var body json.RawMessage
		if err := c.Bind(&body); err != nil {
			return failure(500, err.Error()), nil
		}
		edits, decodeErr := agents.DecodeAgentEdits(body)
		if decodeErr != nil {
			return failure(500, decodeErr.Error()), nil
		}
		ok, err = manager.EditAgent(ctx, agentID, edits)
		if err == nil && ok {
			return httpx.JSON(map[string]any{"success": true, "data": true}), nil
		}
	} else {
		ok, err = manager.DeleteAgent(ctx, agentID)
		if err == nil && ok {
			for _, clear := range agents.AgentDeletedHooks() {
				if err = clear(ctx, agentID); err != nil {
					break
				}
			}
			if err == nil {
				return httpx.JSON(map[string]bool{"success": true}), nil
			}
		}
	}
	if err != nil {
		var conflict *agents.DeploymentConflict
		if errors.As(err, &conflict) {
			return failure(409, conflict.Message), nil
		}
		return failure(500, err.Error()), nil
	}
	return failure(404, "Agent not found"), nil
}

// validPanelJWT recognizes only a panel-issued JWT, never an Agent token.
func validPanelJWT(c *httpx.Ctx) bool {
	if c.IsInternalCall() {
		return true
	}
	header, present := c.Header("Authorization")
	token, ok := auth.ParseBearerToken(header, present)
	if !ok {
		return false
	}
	verification, err := auth.VerifyToken(token)
	return err == nil && !verification.Failed()
}

func safeRegistrationLogFields(data json.RawMessage, secrets redact.Secrets) map[string]string {
	// Redact before truncating: a long credential must not leave a matching prefix.
	var fields map[string]json.RawMessage
	if jsondecode.Unmarshal(data, &fields) != nil {
		return map[string]string{}
	}
	for _, name := range registrationLogFields {
		raw, present := fields[name]
		if !present || len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var value string
		if jsondecode.Unmarshal(raw, &value) != nil {
			continue
		}
		sanitized := redact.DiagnosticText(value, secrets, 0)
		fields[name], _ = json.Marshal(sanitized)
	}
	raw, _ := json.Marshal(fields)
	return RegistrationLogFields(raw)
}
