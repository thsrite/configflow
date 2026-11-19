package upgrades

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/redact"
)

func originalString(raw []byte, present bool, err error) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if err != nil || !present || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var text string
	if jsondecode.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}
func optionalObject(r *model.Agent, key string) (model.Fields, error) {
	raw, present, err := r.MemberJSON(key)
	if err != nil {
		return model.Fields{}, err
	}
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return model.Fields{}, nil
	}
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return fields, &CorruptStateError{Field: key}
	}
	return fields, nil
}
func fieldString(fields model.Fields, key string) (string, bool) {
	raw, ok := fields.Get(key)
	return originalString(raw, ok, nil)
}
func rawText(raw json.RawMessage) string {
	if text, ok := originalString(raw, len(raw) > 0, nil); ok {
		return text
	}
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

func validVersion(text string) bool { return versionPattern.MatchString(text) }

// Info is the exact-key identity and migration proof from /api/upgrade-info.
type Info struct {
	AgentID          string `json:"agent_id"`
	DeploymentMethod string `json:"deployment_method"`
	Architecture     string `json:"architecture"`
	Version          string `json:"version"`
	MigrationReady   bool   `json:"migration_ready"`
}

// Worker is an update worker observation. It cannot alone prove upgrade success.
type Worker struct {
	UpdateID  string                 `json:"update_id"`
	AgentID   string                 `json:"agent_id"`
	Status    string                 `json:"status"`
	Error     json.RawMessage        `json:"error"`
	Message   *string                `json:"message"`
	Events    []model.OperationEvent `json:"events"`
	UpdatedAt *string                `json:"updated_at"`
}

// LegacyReport is the protocol-1 rollback report, bound to one stored update ID.
type LegacyReport struct {
	UpdateID string          `json:"update_id"`
	Status   string          `json:"status"`
	Error    json.RawMessage `json:"error"`
}

// decodeProtocol retains exact member names and rejects duplicate JSON evidence.
func decodeProtocol(data []byte, dst any) error {
	var fields model.Fields
	if err := jsondecode.Unmarshal(data, &fields); err != nil {
		return agents.ErrInvalidAgentReply
	}
	if err := jsondecode.Unmarshal(data, dst); err != nil {
		return agents.ErrInvalidAgentReply
	}
	return nil
}
func responseData(response *agents.Response) []byte {
	if response == nil {
		return nil
	}
	if response.Header.Get("Content-Type") == "" && len(response.Body) > 3 {
		if text, ok := agents.DecodeJSONBytes(response.Body); ok {
			return []byte(text)
		}
	}
	return []byte(response.Text())
}

var requestOverride func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error)

// requestModel deliberately bypasses environment proxies for update control calls.
func requestModel(ctx context.Context, m *agents.Manager, a *model.Agent, method, path string, payload any) (*agents.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil {
		return nil, agents.ErrAgentEndpoint
	}
	host, hOK := originalString(a.MemberJSON("host"))
	token, tOK := originalString(a.MemberJSON("token"))
	port, present, err := a.MemberJSON("port")
	if !hOK || !tOK || !present || err != nil {
		return nil, agents.ErrAgentEndpoint
	}
	header := http.Header{"Authorization": {"Bearer " + token}}
	var body []byte
	if payload != nil {
		body, err = jsonresp.EncodeDevice(payload)
		if err != nil {
			return nil, err
		}
		header.Set("Content-Type", "application/json")
	}
	if requestOverride != nil {
		return requestOverride(ctx, a, method, path, body)
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: "http://" + host + ":" + rawText(port) + path, Header: header, Body: body, Timeout: agents.Seconds(5), NoProxy: true, Latin1Headers: true})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil && !agents.IsTransportError(err) && !errors.Is(err, agents.ErrHeaderEncoding) {
		err = &agents.TransportError{Err: err}
	}
	return response, err
}
func agentSecrets(a *model.Agent) redact.Secrets {
	token, _ := originalString(a.MemberJSON("token"))
	return redact.NewSecrets([]string{token})
}
func heartbeatAfterModel(a *model.Agent, moment float64) (bool, error) {
	raw, present, err := a.MemberJSON("last_heartbeat")
	if err != nil {
		return false, err
	}
	if !present {
		return false, nil
	}
	text, ok := originalString(raw, true, nil)
	if !ok {
		return false, &CorruptStateError{Field: "last_heartbeat"}
	}
	ts, valid := agents.ISOTimestamp(text)
	return valid && ts > moment, nil
}

func agentLabel(a *model.Agent) string {
	name, _ := originalString(a.MemberJSON("name"))
	if name == "" {
		name, _ = originalString(a.MemberJSON("id"))
	}
	return name
}
func findModel(records []model.Agent, id string) (*model.Agent, error) {
	for i := range records {
		if text, ok := originalString(records[i].MemberJSON("id")); ok && text == id {
			return &records[i], nil
		}
	}
	return nil, valueError("Agent not found")
}
func activeHA(a *model.Agent) (bool, error) {
	fields, err := optionalObject(a, agents.HAField)
	if err != nil || fields.Len() == 0 {
		return false, err
	}
	status, ok := fieldString(fields, "status")
	if !ok {
		return false, &CorruptStateError{Field: agents.HAField + ".status"}
	}
	return status != "succeeded" && status != "failed" && status != "rollback_failed", nil
}
func memberHA(a *model.Agent) (bool, error) {
	fields, err := optionalObject(a, "oxidns_ha")
	if err != nil {
		return false, err
	}
	if raw, present := fields.Get("enabled"); present && string(raw) == "true" {
		return true, nil
	}
	return activeHA(a)
}
func displayVersion(text string) string {
	if strings.TrimSpace(text) == "" {
		return "?"
	}
	return text
}

func hasContent(raw json.RawMessage) bool {
	content, err := model.JSONHasContent(raw)
	return err == nil && content
}

func decodeWorker(data []byte) (Worker, error) {
	var fields model.Fields
	var out Worker
	if jsondecode.Unmarshal(data, &fields) != nil {
		return out, agents.ErrInvalidAgentReply
	}
	var ok bool
	out.UpdateID, ok = fieldString(fields, "update_id")
	if !ok {
		return out, agents.ErrInvalidAgentReply
	}
	out.AgentID, ok = fieldString(fields, "agent_id")
	if !ok {
		return out, agents.ErrInvalidAgentReply
	}
	out.Status, ok = fieldString(fields, "status")
	if !ok {
		return out, agents.ErrInvalidAgentReply
	}
	out.Error, _ = fields.Get("error")
	if message, valid := fieldString(fields, "message"); valid {
		out.Message = &message
	}
	if timestamp, valid := fieldString(fields, "updated_at"); valid {
		out.UpdatedAt = &timestamp
	}
	if raw, exists := fields.Get("events"); exists {
		_ = jsondecode.Unmarshal(raw, &out.Events)
	}
	return out, nil
}
func decodeInfo(data []byte) (Info, error) {
	var out Info
	var evidence model.Fields
	if jsondecode.Unmarshal(data, &evidence) != nil {
		return out, agents.ErrInvalidAgentReply
	}
	fields, err := jsondecode.Select(data, "agent_id", "version", "migration_ready")
	if err != nil {
		return out, agents.ErrInvalidAgentReply
	}
	var ok bool
	out.AgentID, ok = originalString(fields["agent_id"], fields["agent_id"] != nil, nil)
	if !ok {
		return out, agents.ErrInvalidAgentReply
	}
	out.Version, ok = originalString(fields["version"], fields["version"] != nil, nil)
	if !ok {
		return out, agents.ErrInvalidAgentReply
	}
	out.MigrationReady = string(fields["migration_ready"]) == "true"
	return out, nil
}
