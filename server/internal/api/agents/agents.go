// Package agents holds the HTTP handlers of the "agents" blueprint
// (backend/routes/agents.py). Package #20 registers the pure registry routes:
// registration, heartbeat, manual creation, edit and delete of one Agent.
// The rest of the blueprint (listing with content sync, deployments, logs,
// upgrades, ...) is package #26; it extends Register here.
package agents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"math/big"
	"os"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
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
	r.POST("/api/agents", "agents.handle_agents", auth.Require(createAgent))
	r.Handle([]string{"PUT", "DELETE"}, "/api/agents/<agent_id>", "agents.handle_agent_item", auth.Require(handleAgentItem))
}

func failure(status int, message string) *httpx.Response {
	return httpx.JSONStatus(status, jsonx.Obj("success", false, "message", message))
}

// boundedLogValue is _bounded_log_value.
func boundedLogValue(value any) (string, bool) {
	switch value.(type) {
	case string, jsonx.Number:
	default:
		return "", false
	}
	text := jsonx.Str(value)
	var out []rune
	for _, r := range text {
		if !logx.IsPrintable(r) || len(out) == 128 {
			break
		}
		out = append(out, r)
	}
	return string(out), true
}

// RegistrationLogFields is _registration_log_fields(agent_data).
func RegistrationLogFields(data *jsonx.Object) *jsonx.Object {
	fields := jsonx.NewObject()
	for _, field := range registrationLogFields {
		value, ok := data.Get(field)
		if !ok {
			continue
		}
		if safe, ok := boundedLogValue(value); ok {
			fields.Set(field, safe)
		}
	}
	return fields
}

func registrationRejected(ctx context.Context, data *jsonx.Object, reason string) {
	fields := jsonx.NewObject()
	if data != nil {
		fields = RegistrationLogFields(data)
	}
	logger.Warningf("Agent 注册被拒绝：%s（%s，%s）：%s", fields.GetOr("name", ""), fields.GetOr("service_type", ""),
		fields.GetOr("host", ""), reason)
	notifications.Safely(func() error {
		repository, err := repo.GetRepository(ctx)
		if err != nil {
			return err
		}
		return notifications.RecordRegistrationRejection(repository, fields, reason)
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
	var data *jsonx.Object
	response, err := doRegister(c, &data)
	if err == nil {
		return response, nil
	}
	var conflict *agents.DeploymentConflict
	var permission *agents.PermissionError
	var installation *agents.InstallationError
	switch {
	case errors.As(err, &conflict):
		registrationRejected(c.Context(), data, conflict.Message)
		return failure(409, conflict.Message), nil
	case errors.As(err, &permission):
		registrationRejected(c.Context(), data, unauthorizedReason)
		return failure(401, "Unauthorized"), nil
	case errors.As(err, &installation):
		return failure(installation.Status, installation.Message), nil
	}
	logger.Errorf("Agent注册失败: %s", err)
	logger.Errorf("错误详情: %s", err)
	return failure(500, err.Error()), nil
}

func doRegister(c *httpx.Ctx, out **jsonx.Object) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	raw, _ := c.Data()
	value := c.JSONForce()
	if value == nil {
		if !isJSONNull(raw) {
			return nil, httpx.BadRequest("")
		}
		logger.Error("Failed to parse JSON data")
		return failure(400, "Invalid JSON data"), nil
	}
	data, ok := value.(*jsonx.Object)
	if !ok {
		if _, isList := value.([]any); isList {
			// list.pop('installation_id', None)
			return nil, errors.New("pop expected at most 1 argument, got 2")
		}
		return nil, errors.New("'" + httpx.PyTypeName(value) + "' object has no attribute 'pop'")
	}
	*out = data
	// A gateway installer gets a short-lived capability for exactly these two
	// service identities. Never reveal the global registration key.
	installationID, _ := data.Pop("installation_id")
	installationToken, _ := data.Pop("installation_token")
	var installationProfile any
	hooks := agents.CurrentHooks()
	installing := installationID != nil || installationToken != nil
	if installing {
		if hooks.InstallationProfile == nil {
			return nil, &agents.InstallationError{Message: "Gateway installation registration is not available yet", Status: 501}
		}
		profile, err := hooks.InstallationProfile(ctx, data, installationID, installationToken, c.RemoteAddr())
		if err != nil {
			return nil, err
		}
		installationProfile = profile
		data.Set("profile_id", profile)
	}
	if key := os.Getenv("AGENT_REGISTRATION_KEY"); key != "" && installationProfile == nil {
		if !jsonx.Equal(data.GetOr("registration_key", ""), key) {
			logger.Warning("Agent 注册被拒绝：注册密钥不匹配")
			return failure(403, "Invalid registration key"), nil
		}
	}
	// 移除注册密钥（不存入配置，并先于日志记录以防止泄露）
	data.Pop("registration_key")

	host := data.GetOr("host", "")
	clientIP := c.HeaderDefault("X-Forwarded-For", c.RemoteAddr())
	if clientIP != "" && strings.Contains(clientIP, ",") {
		clientIP = strings.TrimFunc(strings.Split(clientIP, ",")[0], isPySpace)
	}
	dockerIP := false
	if jsonx.Truthy(host) {
		text, isString := host.(string)
		if !isString {
			return nil, errors.New("'" + httpx.PyTypeName(host) + "' object has no attribute 'split'")
		}
		parts := strings.Split(text, ".")
		if len(parts) == 4 && parts[0] == "172" {
			if second, ok := httpx.PyParseInt(parts[1]); ok && second.IsInt64() && second.Int64() >= 17 && second.Int64() <= 31 {
				dockerIP = true
			}
		}
		if strings.HasPrefix(text, "127.") || text == "localhost" {
			dockerIP = true
		}
	}
	if installationProfile == nil && (!jsonx.Truthy(host) || dockerIP) {
		data.Set("host", clientIP)
	}
	logger.Infof("Agent注册字段: %s", jsonx.Repr(RegistrationLogFields(data)))

	header, present := c.Header("Authorization")
	var existingToken any
	if token, ok := auth.ParseBearerToken(header, present); ok {
		existingToken = token
	}
	var result *jsonx.Object
	if installationProfile != nil {
		if hooks.InstallationRegister == nil {
			return nil, &agents.InstallationError{Message: "Gateway installation registration is not available yet", Status: 501}
		}
		result, err = hooks.InstallationRegister(ctx, manager, installationID, installationToken, data, existingToken)
	} else {
		result, err = manager.RegisterAgent(ctx, data, existingToken, nil)
	}
	if err != nil {
		return nil, err
	}
	body := jsonx.Obj("success", true)
	body.Update(result)
	logged := RegistrationLogFields(data)
	label := "Agent 重新注册"
	if jsonx.Truthy(result.Value("is_new")) {
		label = "新 Agent 注册成功"
	}
	logger.Infof("%s：%s（%s，%s）", label, jsonx.Str(truthyOr(logged.Value("name"), result.Value("id"))),
		jsonx.Str(logged.GetOr("service_type", "mihomo")), jsonx.Str(logged.GetOr("host", "")))
	return httpx.JSON(body), nil
}

func truthyOr(value, fallback any) any {
	if jsonx.Truthy(value) {
		return value
	}
	return fallback
}

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x1f, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// authenticatedAgent is _authenticated_agent: the Agent whose Bearer token
// the request carries, nil otherwise.
func authenticatedAgent(c *httpx.Ctx, manager *agents.Manager, agentID string) (*jsonx.Object, error) {
	agent, err := manager.GetAgentByID(c.Context(), agentID, false)
	if err != nil {
		return nil, err
	}
	header, present := c.Header("Authorization")
	provided, ok := auth.ParseBearerToken(header, present)
	expected, isString := agent.GetOr("token", "").(string)
	if agent == nil {
		expected, isString = "", true
	}
	if !ok || !isString || expected == "" || !constantTimeEqual(provided, expected) {
		return nil, nil
	}
	return agent, nil
}

func constantTimeEqual(provided, expected string) bool {
	for _, s := range []string{provided, expected} {
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return false
			}
		}
	}
	if len(provided) != len(expected) {
		return false
	}
	var diff byte
	for i := 0; i < len(provided); i++ {
		diff |= provided[i] ^ expected[i]
	}
	return diff == 0
}

// ReadJSONBody is _read_json_body(max_length, invalid_message): the decoded
// body of at most maxLength bytes, or the 413 / 400 response. A body that
// decodes to null is returned as nil with no error response.
func ReadJSONBody(c *httpx.Ctx, maxLength int64, invalidMessage string) (any, *httpx.Response) {
	if length, ok := c.ContentLength(); ok && length > maxLength {
		return nil, failure(413, "Request body too large")
	}
	var raw []byte
	if stream := c.Stream(); stream != nil {
		raw, _ = io.ReadAll(io.LimitReader(stream, maxLength+1))
	}
	if int64(len(raw)) > maxLength {
		return nil, failure(413, "Request body too large")
	}
	if !c.IsJSON() {
		return nil, failure(400, invalidMessage)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	value := c.JSONForce()
	if value == nil && !isJSONNull(raw) {
		return nil, failure(400, invalidMessage)
	}
	return value, nil
}

func validMetricNumber(field string, value any) bool {
	number, ok := value.(jsonx.Number)
	if !ok {
		return false
	}
	if number.IsFloat() {
		f := number.Float64()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return false
		}
		if field == "usage_percent" || field == "used_percent" {
			return f >= 0 && f <= 100
		}
		return false
	}
	big, _ := number.BigInt()
	if field == "usage_percent" || field == "used_percent" {
		return big.Sign() >= 0 && big.Cmp(jsonxBig(100)) <= 0
	}
	if big.Sign() < 0 {
		return false
	}
	if field == "core_count" {
		return big.Cmp(jsonxBig(65536)) <= 0
	}
	limit, _ := jsonx.ParseNumber("18446744073709551615")
	max, _ := limit.BigInt()
	return big.Cmp(max) <= 0
}

func jsonxBig(n int64) *big.Int { return big.NewInt(n) }

// ValidateSystemMetrics is _validate_system_metrics.
func ValidateSystemMetrics(value any) bool {
	metrics, ok := value.(*jsonx.Object)
	if !ok || metrics == nil {
		return false
	}
	for _, key := range metrics.Keys() {
		if _, known := metricFields[key]; !known && key != "collected_at" {
			return false
		}
	}
	encoded, err := jsonx.Encode(metrics, jsonx.Options{})
	if err != nil || len(encoded) > 16*1024 {
		return false
	}
	if collected, ok := metrics.Get("collected_at"); ok && collected != nil {
		text, isString := collected.(string)
		if !isString || len([]rune(text)) > 64 {
			return false
		}
	}
	for section, allowed := range metricFields {
		item, ok := metrics.Get(section)
		if !ok {
			continue
		}
		values, isObject := item.(*jsonx.Object)
		if !isObject || values == nil {
			return false
		}
		for field, number := range values.All() {
			if !allowed[field] || !validMetricNumber(field, number) {
				return false
			}
		}
	}
	return true
}

// ValidateHeartbeatPayload is _validate_heartbeat_payload.
func ValidateHeartbeatPayload(value any) bool {
	payload, ok := value.(*jsonx.Object)
	if !ok || payload == nil {
		return false
	}
	for _, key := range payload.Keys() {
		if !heartbeatFields[key] {
			return false
		}
	}
	for _, limit := range []struct {
		field string
		max   int
	}{{"version", 128}, {"config_version", 128}, {"service_status", 64}, {"host", 64}} {
		if item, ok := payload.Get(limit.field); ok {
			text, isString := item.(string)
			if !isString || len([]rune(text)) > limit.max {
				return false
			}
		}
	}
	if metrics, ok := payload.Get("system_metrics"); ok {
		return ValidateSystemMetrics(metrics)
	}
	return true
}

func mihomoFlag(agent *jsonx.Object, field string) bool {
	return jsonx.Truthy(agent.Value(field)) && agent.Value("service_type") == "mihomo"
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
	agent, err := authenticatedAgent(c, manager, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return failure(401, "Unauthorized"), nil
	}
	value, errorResponse := ReadJSONBody(c, HeartbeatMaxContentLength, "Invalid heartbeat data")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !ValidateHeartbeatPayload(value) {
		return failure(400, "Invalid heartbeat data"), nil
	}
	data := value.(*jsonx.Object)
	// The address is not part of the stored heartbeat; it only tells the panel
	// where the Agent now is.
	reported, _ := data.Pop("host")
	ok, err := manager.UpdateHeartbeat(ctx, agentID, data)
	if err != nil {
		return nil, err
	}
	if !ok {
		return failure(404, "Agent not found"), nil
	}
	reply := jsonx.Obj("success", true, "domain_discovery_enabled", mihomoFlag(agent, "domain_discovery_enabled"),
		"traffic_history_enabled", mihomoFlag(agent, "traffic_history_enabled"), "reads_host", true)
	if host, _ := reported.(string); host != "" {
		hooks := agents.CurrentHooks()
		if hooks.AddressFollow != nil {
			if err := hooks.AddressFollow(ctx, manager, agent, host); err != nil {
				return nil, err
			}
		}
		// 记录里已经是 Agent 上报的地址时告诉它；Agent 据此把配置文件里失效的 agent_ip 改成这个地址。
		if hooks.AddressConfirmed != nil {
			if confirmed := hooks.AddressConfirmed(agent, host); confirmed != "" {
				reply.Set("confirmed_host", confirmed)
			}
		}
	}
	return httpx.JSON(reply), nil
}

func createAgent(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	value, err := c.JSON()
	if err != nil {
		return failure(500, err.Error()), nil
	}
	data, ok := value.(*jsonx.Object)
	if !ok || data == nil {
		return failure(500, httpx.NoAttributeGet(value)), nil
	}
	result, err := manager.RegisterAgent(ctx, data, nil, nil)
	if err != nil {
		var conflict *agents.DeploymentConflict
		if errors.As(err, &conflict) {
			return failure(409, conflict.Message), nil
		}
		return failure(500, err.Error()), nil
	}
	return httpx.JSON(jsonx.Obj("success", true, "data", result)), nil
}

// nonObjectUpdateError is the exception update_agent(agent_id, updates)
// raises for a JSON body that is not an object: `'profile_id' in updates`
// (a substring or membership test, or not iterable at all), then
// `updates['profile_id']` when that test is true, else updates.get(...).
func nonObjectUpdateError(value any) string {
	switch v := value.(type) {
	case string:
		if strings.Contains(v, "profile_id") {
			return "string indices must be integers, not 'str'"
		}
		return httpx.NoAttributeGet(value)
	case []any:
		for _, item := range v {
			if jsonx.Equal(item, "profile_id") {
				return "list indices must be integers or slices, not str"
			}
		}
		return httpx.NoAttributeGet(value)
	}
	return "argument of type '" + httpx.PyTypeName(value) + "' is not iterable"
}

func handleAgentItem(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	var ok bool
	if c.Method() == "PUT" {
		value, jsonErr := c.JSON()
		if jsonErr != nil {
			return failure(500, jsonErr.Error()), nil
		}
		data, isObject := value.(*jsonx.Object)
		if !isObject || data == nil {
			return failure(500, nonObjectUpdateError(value)), nil
		}
		ok, err = manager.UpdateAgent(ctx, agentID, data)
		if err == nil && ok {
			return httpx.JSON(jsonx.Obj("success", true, "data", true)), nil
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
				return httpx.JSON(jsonx.Obj("success", true)), nil
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
