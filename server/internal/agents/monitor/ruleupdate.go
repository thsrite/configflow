package monitor

// Port of backend/agents/oxidns_rule_update.py (A07): refresh deployed
// OxiDNS rule data through its registered Agent. This bridge exposes one
// fixed operation. Core addresses, credentials, plugin tags and download
// targets are resolved by the Agent from its deployed config. Newer Agents
// also accept a list of deployed rule files to refresh; they still resolve
// each file against the deployed download list themselves.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"configflow/internal/agents"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// Versions and limits of oxidns_rule_update.py.
const (
	RuleUpdateMinAgentVersion      = "1.8.30-go"
	RuleUpdatePathsMinAgentVersion = "1.8.35-go"
	RuleUpdateMaxPaths             = 1000
	RuleUpdateMaxResponseBytes     = 16 * 1024
)

// RuleUpdateFail is oxidns_rule_update._fail(code, status).
func RuleUpdateFail(code string, status int) *Error {
	if status == 0 {
		status = 502
	}
	message, ok := RuleUpdateMessages[code]
	if !ok {
		message = RuleUpdateMessages["RULE_UPDATE_FAILED"]
	}
	return &Error{Code: code, Status: status, Message: message}
}

var taskIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
var taskStates = []string{"idle", "running", "succeeded", "failed"}
var taskPhases = []string{"idle", "downloading", "reloading", "done"}

// RuleTask is the bounded progress DTO returned by an OxiDNS Agent.
type RuleTask struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	Downloaded    int64  `json:"downloaded"`
	DownloadTotal int64  `json:"download_total"`
	Reloaded      int64  `json:"reloaded"`
	ReloadTotal   int64  `json:"reload_total"`
	Code          string `json:"code,omitempty"`
	Message       string `json:"message,omitempty"`
}

// RuleUpdateResult carries either a direct task or the HA owner's opaque result.
type RuleUpdateResult struct {
	Success bool            `json:"success"`
	Task    *RuleTask       `json:"task"`
	Pair    json.RawMessage `json:"-"`
}

func decodeRuleTask(raw json.RawMessage) (*RuleTask, error) {
	invalid := RuleUpdateFail("INVALID_RESPONSE", 0)
	var fields model.Fields
	if json.Unmarshal(raw, &fields) != nil {
		return nil, invalid
	}
	task := &RuleTask{ID: fieldText(fields, "id"), Status: fieldText(fields, "status"), Phase: fieldText(fields, "phase")}
	if !contains(taskStates, task.Status) || !contains(taskPhases, task.Phase) {
		return nil, invalid
	}
	id, present := fields.Get("id")
	if _, valid := originalString(id, present, nil); !valid || task.Status == "idle" && task.ID != "" || task.Status != "idle" && !taskIDRE.MatchString(task.ID) {
		return nil, invalid
	}
	for _, field := range []struct {
		name  string
		value *int64
	}{
		{"downloaded", &task.Downloaded}, {"download_total", &task.DownloadTotal}, {"reloaded", &task.Reloaded}, {"reload_total", &task.ReloadTotal},
	} {
		raw, _ := fields.Get(field.name)
		var n *int64
		if json.Unmarshal(raw, &n) != nil || n == nil || *n < 0 || *n > 1_000_000 {
			return nil, invalid
		}
		*field.value = *n
	}
	if task.Downloaded > task.DownloadTotal || task.Reloaded > task.ReloadTotal || task.Status == "succeeded" && (task.Phase != "done" || task.Downloaded != task.DownloadTotal || task.Reloaded != task.ReloadTotal) {
		return nil, invalid
	}
	if task.Status == "failed" {
		task.Code = fieldText(fields, "code")
		if _, known := RuleUpdateMessages[task.Code]; !known {
			task.Code = "RULE_UPDATE_FAILED"
		}
		task.Message = RuleUpdateMessages[task.Code]
	}
	return task, nil
}

// CallRuleUpdate starts or reads a rule update with typed request and reply DTOs.
func CallRuleUpdate(ctx context.Context, m Registry, id, method string, paths []string) (*RuleUpdateResult, int, error) {
	if method != "GET" && method != "POST" || paths != nil && (method != "POST" || len(paths) == 0 || len(paths) > RuleUpdateMaxPaths) {
		return nil, 0, RuleUpdateFail("INVALID_REQUEST", 400)
	}
	for _, path := range paths {
		if path == "" {
			return nil, 0, RuleUpdateFail("INVALID_REQUEST", 400)
		}
	}
	if method == "POST" {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	agent, err := m.ReadAgent(ctx, id, false)
	if err != nil {
		return nil, 0, err
	}
	if agent == nil {
		return nil, 0, RuleUpdateFail("AGENT_NOT_FOUND", 404)
	}
	if originalText(agent.MemberJSON("service_type")) != "oxidns" {
		return nil, 0, RuleUpdateFail("WRONG_SERVICE", 400)
	}
	if !modelVersionAtLeast(agent, RuleUpdateMinAgentVersion) {
		return nil, 0, RuleUpdateFail("UPGRADE_REQUIRED", 409)
	}
	raw, present, err := agent.MemberJSON("oxidns_ha")
	var ha model.Fields
	if err != nil {
		return nil, 0, err
	}
	// A malformed stored HA config is neither an unpaired device nor authority
	// to send an unguarded write. Keep it an explicit native validation refusal.
	if present && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, &ha) != nil {
		return nil, 0, errors.New(msgHAConfigDamaged)
	}
	if method == "POST" {
		journal, present, err := agent.MemberJSON(agents.HAField)
		if err != nil {
			return nil, 0, err
		}
		if present && !bytes.Equal(bytes.TrimSpace(journal), []byte("null")) {
			var fields model.Fields
			if json.Unmarshal(journal, &fields) != nil {
				return nil, 0, errors.New(msgHAPublicationDamaged)
			}
			status, _ := fields.Get("status")
			if len(status) > 0 && (status[0] == '[' || status[0] == '{') {
				return nil, 0, errors.New(msgHAPublicationDamaged)
			}
		}
	}
	paired := agents.HAEnabledModel(agent)
	if method == "POST" && agents.HAActiveModel(agent) && !(paired && agent.OxiDNSHAPublication != nil && originalText(agent.OxiDNSHAPublication.MemberJSON("kind")) == "rules") {
		return nil, 0, NewError("SERVICE_BUSY", 409, msgHARulesBusy)
	}
	if paired {
		raw, status, err := pairRules(ctx, m, id, method)
		var busy *HABusyError
		if errors.As(err, &busy) {
			return nil, 0, NewError("SERVICE_BUSY", 409, busy.Message)
		}
		if err != nil {
			return nil, 0, err
		}
		return &RuleUpdateResult{Success: true, Pair: raw}, status, nil
	}
	if paths != nil && !modelVersionAtLeast(agent, RuleUpdatePathsMinAgentVersion) {
		return nil, 0, RuleUpdateFail("UPGRADE_REQUIRED", 409)
	}
	origin, err := Origin(agent)
	if err != nil {
		return nil, 0, err
	}
	token, valid := originalString(agent.MemberJSON("token"))
	if !valid {
		return nil, 0, RuleUpdateFail("AGENT_UNREACHABLE", 0)
	}
	header, err := monitorHeaders(token)
	if err != nil {
		return nil, 0, RuleUpdateFail("AGENT_UNREACHABLE", 0)
	}
	var body []byte
	if method == "POST" {
		header.Set("Content-Type", "application/json")
		body = []byte("{}")
		if paths != nil {
			body, err = jsonresp.EncodeDevice(struct {
				Paths []string `json:"paths"`
			}{paths})
			if err != nil {
				return nil, 0, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: origin + "/api/oxidns/rules/update", Header: header, Body: body,
		Timeout: agents.Pair(3, 15), NoProxy: true, NoRedirects: true, MaxBody: RuleUpdateMaxResponseBytes})
	if err != nil {
		return nil, 0, RuleUpdateFail("AGENT_UNREACHABLE", 0)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	answer, err := protocolAnswer(response, RuleUpdateMaxResponseBytes, RuleUpdateFail)
	if err != nil {
		return nil, 0, err
	}
	status := response.StatusCode
	if status == 404 {
		return nil, 0, RuleUpdateFail("UPGRADE_REQUIRED", 409)
	}
	success, present, valid := answerBool(answer, "success")
	if present && !valid {
		return nil, 0, RuleUpdateFail("INVALID_RESPONSE", 0)
	}
	if status >= 400 || present && !success {
		code := fieldText(answer, "code")
		if _, known := RuleUpdateMessages[code]; !known {
			code = "RULE_UPDATE_FAILED"
		}
		errStatus := 502
		switch status {
		case 400, 409, 503, 504:
			errStatus = status
		}
		return nil, 0, RuleUpdateFail(code, errStatus)
	}
	if status != 200 && status != 202 || !present || !success {
		return nil, 0, RuleUpdateFail("INVALID_RESPONSE", 0)
	}
	raw, _ = answer.Get("task")
	task, err := decodeRuleTask(raw)
	if err != nil {
		return nil, 0, err
	}
	return &RuleUpdateResult{Success: true, Task: task}, status, nil
}

// MarshalJSON preserves the HA owner's DTO or writes the direct Agent task.
func (r RuleUpdateResult) MarshalJSON() ([]byte, error) {
	if r.Pair != nil {
		return append([]byte(nil), r.Pair...), nil
	}
	return json.Marshal(struct {
		Success bool      `json:"success"`
		Task    *RuleTask `json:"task"`
	}{r.Success, r.Task})
}
