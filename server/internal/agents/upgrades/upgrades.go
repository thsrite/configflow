// Package upgrades is the port of backend/agents/upgrades.py (durable,
// version-confirmed Agent updates: acceptance is never success) and
// backend/agents/address_follow.py (following a renumbered Agent).
package upgrades

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

var logger = logx.Get("backend.agents.upgrades")

// Terminal is TERMINAL.
var Terminal = map[string]bool{"succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}

// terminalNotRollbackFailed is TERMINAL - {'rollback_failed'}.
var terminalNotRollbackFailed = map[string]bool{"succeeded": true, "failed": true, "rolled_back": true}

// Stages is STAGES.
var Stages = map[string]bool{"queued": true, "downloading": true, "verifying": true, "backing_up": true, "migrating": true,
	"replacing": true, "restarting": true, "checking": true, "rolling_back": true,
	"succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}

var stagesOrUnknown = func() map[string]bool {
	out := map[string]bool{"unknown": true}
	for key := range Stages {
		out[key] = true
	}
	return out
}()

type resultLevel struct {
	level logx.Level
	text  string
}

var resultLevels = map[string]resultLevel{
	"succeeded":       {logx.Info, "成功"},
	"failed":          {logx.Warning, "失败"},
	"rolled_back":     {logx.Warning, "失败，已回滚到原版本"},
	"rollback_failed": {logx.Error, "失败，且回滚失败"},
}

// Test seams.
var (
	now           = agents.NowTimestamp
	artifactsFunc = Artifacts
	newUpdateID   = func() string {
		var b [16]byte
		rand.Read(b[:])
		b[6] = b[6]&0x0f | 0x40
		b[8] = b[8]&0x3f | 0x80
		return hex.EncodeToString(b[:])
	}
	// requestHook, when set, replaces _request (tests).
	requestHook func(agent *jsonx.Object, method, path string, payload *jsonx.Object) (*agents.Response, error)
)

// request is _request(agent, method, path, json=payload): a control-plane
// call that does not use proxies from the environment (trust_env=False),
// timeout 5 s. Transport failures are *agents.TransportError.
func request(ctx context.Context, m *agents.Manager, agent *jsonx.Object, method, path string, payload *jsonx.Object) (*agents.Response, error) {
	for _, key := range []string{"host", "port"} {
		if !agent.Has(key) {
			return nil, keyError(key)
		}
	}
	token, ok := agent.Get("token")
	if !ok {
		return nil, keyError("token")
	}
	text, ok := token.(string)
	if !ok {
		return nil, typeError("can only concatenate str (not \"" + typeName(token) + "\") to str")
	}
	if requestHook != nil {
		return requestHook(agent, method, path, payload)
	}
	header := http.Header{"Authorization": {"Bearer " + text}}
	var body []byte
	if payload != nil {
		encoded, err := jsonx.Default(payload)
		if err != nil {
			return nil, err
		}
		body = encoded
		header.Set("Content-Type", "application/json")
	}
	// Latin1Headers: http.client sends header values as latin-1; a token it
	// cannot encode is a UnicodeEncodeError (a ValueError, not a
	// RequestException).
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: agents.AgentURL(agent, path), Header: header,
		Body: body, Timeout: agents.Seconds(5), NoProxy: true, Latin1Headers: true})
	var call *agents.CallError
	if err != nil && !agents.IsTransportError(err) && !errors.As(err, &call) {
		err = &agents.TransportError{Err: err}
	}
	return response, err
}

// responseJSON is response.json(): a decoding failure is requests'
// JSONDecodeError (a RequestException and a ValueError).
func responseJSON(response *agents.Response) (any, error) {
	value, err := response.JSON()
	if err != nil {
		return nil, &agents.TransportError{Err: err}
	}
	return value, nil
}

// requestFailed reports a requests.RequestException.
func requestFailed(err error) bool { return agents.IsTransportError(err) }

func tokenSecrets(agent *jsonx.Object) []any { return []any{agent.Value("token")} }

// save is _save(manager, agent_id, update_id, patch, only_queued=...).
func save(ctx context.Context, m *agents.Manager, agentID string, updateID any, patch *jsonx.Object, onlyQueued bool) (*jsonx.Object, error) {
	type finishedState struct {
		name  any
		state *jsonx.Object
	}
	var finished []finishedState
	value, err := m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		agent, err := findAgent(*list, agentID)
		if err != nil {
			return nil, err
		}
		state, err := orDict(agent.Value("latest_upgrade"))
		if err != nil {
			return nil, err
		}
		before := state.Value("status")
		if !jsonx.Equal(state.Value("update_id"), updateID) {
			return nil, valueError("Upgrade ownership changed")
		}
		done, err := in(state.Value("status"), terminalNotRollbackFailed)
		if err != nil {
			return nil, err
		}
		if done {
			return state.Clone(), nil
		}
		if onlyQueued && !jsonx.Equal(state.Value("status"), "queued") {
			return state.Clone(), nil
		}
		reportedAt := ""
		if text, ok := patch.Value("agent_updated_at").(string); ok {
			reportedAt, _ = agents.EventTime(text)
		}
		if reportedAt != "" && jsonx.Truthy(state.Value("agent_updated_at")) {
			previous, ok := state.Value("agent_updated_at").(string)
			if !ok {
				return nil, typeError("'<' not supported between instances of 'str' and '" + typeName(state.Value("agent_updated_at")) + "'")
			}
			if reportedAt < previous {
				return state.Clone(), nil
			}
		}
		secrets := tokenSecrets(agent)
		incoming := agents.CleanEvents(patch.Value("events"), secrets, stagesOrUnknown)
		events := agents.MergeEvents(state.Value("events"), incoming, secrets, stagesOrUnknown)
		if jsonx.Truthy(patch.Value("status")) && !patch.Has("message") {
			state.Delete("message")
		}
		for key, value := range patch.All() {
			switch key {
			case "events", "agent_updated_at":
			case "error", "message":
				state.Set(key, agents.FailureText(value, secrets, 2048))
			default:
				state.Set(key, value)
			}
		}
		if reportedAt != "" {
			state.Set("agent_updated_at", reportedAt)
		}
		status := state.Value("status")
		lastStatus := func(list []any) any { return list[len(list)-1].(*jsonx.Object).Value("status") }
		if len(incoming) == 0 || !jsonx.Equal(lastStatus(incoming), status) || len(events) > 0 && !jsonx.Equal(lastStatus(events), status) {
			message := state.Value("message")
			if !jsonx.Truthy(message) {
				message = state.Value("error")
			}
			statusText, _ := status.(string)
			events = agents.AppendEvent(events, statusText, message, "", secrets)
		}
		state.Set("events", events)
		state.Set("updated_at", jsonx.Float(now()))
		agent.Set("latest_upgrade", state)
		if !jsonx.Equal(status, before) {
			if text, ok := status.(string); ok {
				if _, known := resultLevels[text]; known {
					name := agent.Value("name")
					if !jsonx.Truthy(name) {
						name = agentID
					}
					finished = append(finished, finishedState{name, state.Clone()})
				}
			}
		}
		return state.Clone(), nil
	})
	if err != nil {
		return nil, err
	}
	if len(finished) > 0 {
		last := finished[len(finished)-1]
		state := last.state
		detail := state.Value("error")
		if !jsonx.Truthy(detail) {
			detail = state.Value("message")
		}
		if !jsonx.Truthy(detail) {
			detail = ""
		}
		level := resultLevels[state.GetString("status")]
		suffix := ""
		if jsonx.Truthy(detail) && state.GetString("status") != "succeeded" {
			suffix = "（" + logx.LogText(detail, 300) + "）"
		}
		logger.Log(level.level, "Agent "+logx.LogText(last.name, 120)+" 升级 "+logx.LogText(orText(state.Value("previous_version")), 40)+
			" → "+logx.LogText(orText(state.Value("target_version")), 40)+" "+level.text+suffix)
	}
	return jsonx.DeepCopy(value).(*jsonx.Object), nil
}

// orText is `value or '?'`.
func orText(value any) any {
	if jsonx.Truthy(value) {
		return value
	}
	return "?"
}

// Start is start(manager, agent_id): reserve a durable update and submit it
// to the Agent. ValueErrors (IsValueError) are the user-facing refusals.
func Start(ctx context.Context, m *agents.Manager, agentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 {
		return nil, valueError("Agent not found")
	}
	if jsonx.Equal(agent.Value("deployment_method"), "docker") {
		return nil, valueError("Docker Agent 请更新镜像并重建容器；网页二进制更新不适用于 Docker")
	}
	cannotConnect := valueError("无法连接 Agent 并确认更新能力")
	infoResponse, err := request(ctx, m, agent, "GET", "/api/upgrade-info", nil)
	if err != nil {
		// except (requests.RequestException, ValueError)
		if requestFailed(err) || IsValueError(err) {
			return nil, cannotConnect
		}
		return nil, err
	}
	var info any
	if infoResponse.StatusCode != 404 {
		if infoResponse.StatusCode >= 400 && infoResponse.StatusCode < 600 { // raise_for_status
			return nil, cannotConnect
		}
		if info, err = responseJSON(infoResponse); err != nil {
			return nil, cannotConnect
		}
	}
	hasInfo := jsonx.Truthy(info)
	var arch any
	if hasInfo {
		agentField, err := get(info, "agent_id")
		if err != nil {
			return nil, err
		}
		method, _ := get(info, "deployment_method")
		if !jsonx.Equal(agentField, agentID) || !jsonx.Equal(method, "shell") {
			return nil, valueError("Agent 身份或安装方式与网页记录不符")
		}
	}
	if !hasInfo && !jsonx.Equal(agent.Value("deployment_method"), "shell") {
		return nil, valueError("旧 Agent 未报告 Shell 安装方式，无法安全选择更新流程")
	}
	available, err := artifactsFunc()
	if err != nil {
		return nil, err
	}
	if hasInfo {
		arch, _ = get(info, "architecture")
	}
	notReady := false
	if jsonx.Truthy(arch) {
		if unhashable(arch) {
			return nil, typeError("unhashable type: '" + typeName(arch) + "'")
		}
		text, ok := arch.(string)
		notReady = !ok || !available.Has(text)
	}
	if !notReady && !hasInfo {
		notReady = available.Len() != len(Arches)
		for _, name := range Arches {
			if !available.Has(name) {
				notReady = true
			}
		}
	}
	if notReady {
		return nil, valueError("更新文件未准备就绪，请先更新 ConfigFlow 主服务镜像")
	}
	target := agents.GetLatestVersion()
	protocol := 1
	if hasInfo {
		protocol = 2
	}
	startedAt := now()
	updatedAt := now()
	state := jsonx.Obj("update_id", newUpdateID(), "target_version", target,
		"previous_version", agent.GetOr("version", ""), "status", "queued",
		"agent_id", agentID, "started_at", jsonx.Float(startedAt), "updated_at", jsonx.Float(updatedAt),
		"protocol", jsonx.Int(int64(protocol)), "artifacts", available)
	message := "正在提交 Agent " + target + " 更新请求"
	if jsonx.Truthy(arch) {
		message += "（" + jsonx.Str(arch) + "）"
	}
	state.Set("message", message)
	state.Set("events", agents.AppendEvent([]any{}, "queued", message, "", nil))
	updateID := state.Value("update_id")

	_, err = m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		current, err := findAgent(*list, agentID)
		if err != nil {
			return nil, err
		}
		active, err := haActive(current)
		if err != nil {
			return nil, err
		}
		if active {
			return nil, valueError("DNS 主备正在同步，请等待两台发布完成后升级")
		}
		previous, err := orDict(current.Value("latest_upgrade"))
		if err != nil {
			return nil, err
		}
		previousUpgrade, err := orDict(agent.Value("latest_upgrade"))
		if err != nil {
			return nil, err
		}
		if !jsonx.Equal(previous.Value("update_id"), previousUpgrade.Value("update_id")) {
			return nil, valueError("当前更新任务已改变，请刷新后查看最新结果")
		}
		if previous.Len() > 0 {
			terminal, err := in(previous.Value("status"), Terminal)
			if err != nil {
				return nil, err
			}
			if !terminal || jsonx.Equal(previous.Value("status"), "rollback_failed") {
				return nil, valueError("已有更新正在执行或等待恢复，请先查询结果")
			}
		}
		deployment, err := orDict(current.Value("latest_deployment"))
		if err != nil {
			return nil, err
		}
		if deployment.Len() > 0 {
			finished, err := in(deployment.Value("status"), terminalNotRollbackFailed)
			if err != nil {
				return nil, err
			}
			if !finished {
				return nil, valueError("存在未结束的配置发布，请先完成或恢复")
			}
		}
		current.Set("latest_upgrade", state.Clone())
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	name := agent.Value("name")
	if !jsonx.Truthy(name) {
		name = agentID
	}
	logger.Info("开始升级 Agent " + logx.LogText(name, 120) + "：" + logx.LogText(orText(state.Value("previous_version")), 40) +
		" → " + logx.LogText(target, 40))
	payload := jsonx.Obj("version", target, "update_id", updateID)
	if hasInfo {
		text, ok := arch.(string)
		if !ok {
			if unhashable(arch) {
				return nil, typeError("unhashable type: '" + typeName(arch) + "'")
			}
			return nil, &PyError{Class: "KeyError", Message: jsonx.Repr(arch)}
		}
		files, ok := available.Get(text)
		if !ok {
			return nil, &PyError{Class: "KeyError", Message: jsonx.Repr(arch)}
		}
		payload.Update(files.(*jsonx.Object))
	}
	response, err := request(ctx, m, agent, "POST", "/api/update", payload)
	if err != nil {
		if requestFailed(err) {
			// The response may have been lost after acceptance. Keep the same ID
			// pending and reconcile with the Agent instead of resubmitting.
			return save(ctx, m, agentID, updateID, jsonx.Obj("status", "checking", "error", "请求结果待确认，正在查询 Agent"), true)
		}
		return nil, err
	}
	if response.StatusCode != 200 && response.StatusCode != 202 {
		var message any = "Agent rejected upgrade"
		if value, err := response.JSON(); err == nil {
			got, err := get(value, "message")
			if err != nil {
				return nil, err
			}
			if object := value.(*jsonx.Object); object.Has("message") {
				message = got
			}
		}
		return save(ctx, m, agentID, updateID, jsonx.Obj("status", "failed", "error", message), false)
	}
	// Acceptance alone does not prove that the independent worker is already
	// downloading. A simultaneous poll may also have observed a later stage.
	return save(ctx, m, agentID, updateID, jsonx.Obj("status", "queued",
		"message", "Agent "+target+" 更新请求已送达，等待设备处理"), true)
}

// number is a stored number as a float (int, float or bool) for
// `value + 600` (op "+") or `confirmed > value` (op ">"), with Python's
// TypeError texts otherwise.
func number(value any, op string) (float64, error) {
	switch v := value.(type) {
	case jsonx.Number:
		return v.Float64(), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	}
	if op == ">" {
		return 0, typeError("'>' not supported between instances of 'float' and '" + typeName(value) + "'")
	}
	switch value.(type) {
	case string, []any:
		name := typeName(value)
		return 0, typeError("can only concatenate " + name + " (not \"int\") to " + name)
	}
	return 0, typeError("unsupported operand type(s) for +: '" + typeName(value) + "' and 'int'")
}

// heartbeatTimestamp is datetime.fromisoformat(agent.get('last_heartbeat',
// ”)).timestamp(): ok false for Python's ValueError, an error for the
// TypeError of a non-string.
func heartbeatTimestamp(agent *jsonx.Object) (float64, bool, error) {
	value := agent.GetOr("last_heartbeat", "")
	text, isString := value.(string)
	if !isString {
		return 0, false, typeError("fromisoformat: argument must be str")
	}
	ts, ok := agents.ISOTimestamp(text)
	return ts, ok, nil
}

// Poll is poll(manager, agent_id): reconcile the recorded update with the
// Agent's worker and identity.
func Poll(ctx context.Context, m *agents.Manager, agentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 || !jsonx.Truthy(agent.Value("latest_upgrade")) {
		return nil, valueError("No upgrade recorded")
	}
	copied, err := pyDict(agent.Value("latest_upgrade"))
	if err != nil {
		return nil, err
	}
	state := copied.DeepCopy()
	status, err := item(state, "status")
	if err != nil {
		return nil, err
	}
	if done, err := in(status, terminalNotRollbackFailed); err != nil {
		return nil, err
	} else if done {
		return state, nil
	}
	if jsonx.Truthy(state.Value("recovery_reported_at")) {
		previous, err := item(state, "previous_version")
		if err != nil {
			return nil, err
		}
		if jsonx.Equal(agent.Value("version"), previous) {
			confirmed, ok, err := heartbeatTimestamp(agent)
			if err != nil {
				return nil, err
			}
			if ok {
				reported, err := number(state.Value("recovery_reported_at"), ">")
				if err != nil {
					return nil, err
				}
				if confirmed > reported {
					saved, err := save(ctx, m, agentID, state.Value("update_id"), jsonx.Obj("status", "rolled_back"), false)
					if err == nil || !IsValueError(err) {
						return saved, err
					}
				}
			}
		}
	}
	result, done, err := pollAgent(ctx, m, agent, agentID, state)
	if done || err != nil {
		return result, err
	}
	startedAt, err := item(state, "started_at")
	if err != nil {
		return nil, err
	}
	started, err := number(startedAt, "+")
	if err != nil {
		return nil, err
	}
	deadline := started + 600
	if now() > deadline {
		// The Agent did not report this job in progress. A heartbeat sent after
		// the deadline that still carries the old version proves the update
		// never took effect, so release the lock and allow a retry.
		previous, err := item(state, "previous_version")
		if err != nil {
			return nil, err
		}
		if jsonx.Equal(agent.Value("version"), previous) {
			after, err := heartbeatAfter(agent, deadline)
			if err != nil {
				return nil, err
			}
			if after {
				return save(ctx, m, agentID, state.Value("update_id"), jsonx.Obj("status", "failed",
					"error", "更新未生效：Agent 仍在运行 "+jsonx.Str(previous)+"，可重新更新；详情请查看 Agent 日志"), false)
			}
		}
		// Do not claim rollback when the old executable cannot be reached.
		if !jsonx.Equal(status, "unknown") {
			return save(ctx, m, agentID, state.Value("update_id"), jsonx.Obj("status", "unknown",
				"error", "升级结果未能确认，正在继续查询；请查看 Agent 日志"), false)
		}
	}
	return state, nil
}

// pollAgent is the try block of poll: done=false when Python falls through
// to the deadline check (a RequestException or ValueError, or nothing
// conclusive).
func pollAgent(ctx context.Context, m *agents.Manager, agent *jsonx.Object, agentID string, state *jsonx.Object) (*jsonx.Object, bool, error) {
	// caught is `except (requests.RequestException, ValueError): pass`.
	caught := func(err error) (*jsonx.Object, bool, error) {
		if requestFailed(err) || IsValueError(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	var worker *jsonx.Object
	var response *agents.Response
	var result any
	protocol, err := item(state, "protocol")
	if err != nil {
		return caught(err)
	}
	if jsonx.Equal(protocol, jsonx.Int(2)) {
		if response, err = request(ctx, m, agent, "GET", "/api/update", nil); err != nil {
			return caught(err)
		}
	}
	if response != nil && response.StatusCode == 200 {
		if result, err = responseJSON(response); err != nil {
			return caught(err)
		}
		updateID, err := get(result, "update_id")
		if err != nil {
			return caught(err)
		}
		stateUpdateID, err := item(state, "update_id")
		if err != nil {
			return caught(err)
		}
		matches := jsonx.Equal(updateID, stateUpdateID)
		if matches {
			id, _ := get(result, "agent_id")
			matches = jsonx.Equal(id, agentID)
		}
		if matches {
			status, _ := get(result, "status")
			if matches, err = in(status, Stages); err != nil {
				return caught(err)
			}
		}
		if matches {
			object := result.(*jsonx.Object)
			worker = jsonx.Obj("status", object.Value("status"), "error", truncate(jsonx.Str(object.GetOr("error", "")), 2048),
				"events", object.Value("events"), "agent_updated_at", object.Value("updated_at"))
			if message, ok := object.Value("message").(string); ok {
				worker.Set("message", message)
			}
			if !jsonx.Equal(object.Value("status"), "succeeded") {
				saved, err := save(ctx, m, agentID, stateUpdateID, worker, false)
				if err != nil {
					return caught(err)
				}
				return saved, true, nil
			}
		}
	}
	infoResponse, err := request(ctx, m, agent, "GET", "/api/upgrade-info", nil)
	if err != nil {
		return caught(err)
	}
	if infoResponse.StatusCode == 200 {
		info, err := responseJSON(infoResponse)
		if err != nil {
			return caught(err)
		}
		id, err := get(info, "agent_id")
		if err != nil {
			return caught(err)
		}
		matches := jsonx.Equal(id, agentID)
		if matches {
			target, err := item(state, "target_version")
			if err != nil {
				return caught(err)
			}
			version, _ := get(info, "version")
			ready, _ := get(info, "migration_ready")
			matches = jsonx.Equal(version, target) && ready == true
		}
		if matches {
			// Protocol 2 also needs the independent worker's final result.
			confirmed := jsonx.Equal(protocol, jsonx.Int(1))
			if !confirmed {
				if response == nil {
					return caught(noAttribute(nil, "status_code"))
				}
				if response.StatusCode == 200 {
					object := result.(*jsonx.Object)
					confirmed = jsonx.Equal(object.Value("update_id"), state.Value("update_id")) && jsonx.Equal(object.Value("status"), "succeeded")
				}
			}
			if confirmed {
				patch := jsonx.NewObject()
				if worker != nil {
					patch = worker.Clone()
				}
				patch.Set("status", "succeeded")
				patch.Set("error", "")
				saved, err := save(ctx, m, agentID, state.Value("update_id"), patch, false)
				if err != nil {
					return caught(err)
				}
				return saved, true, nil
			}
		}
	}
	if worker != nil && jsonx.Equal(worker.Value("status"), "succeeded") {
		worker.Set("status", "checking")
		worker.Set("message", "Agent 已报告更新完成，正在确认版本、身份与迁移结果")
		events := []any{}
		for _, entry := range agents.CleanEvents(worker.Value("events"), nil, Stages) {
			if !Terminal[entry.(*jsonx.Object).GetString("status")] {
				events = append(events, entry)
			}
		}
		worker.Set("events", events)
		saved, err := save(ctx, m, agentID, state.Value("update_id"), worker, false)
		if err != nil {
			return caught(err)
		}
		return saved, true, nil
	}
	return nil, false, nil
}

// heartbeatAfter is _heartbeat_after(agent, moment).
func heartbeatAfter(agent *jsonx.Object, moment float64) (bool, error) {
	ts, ok, err := heartbeatTimestamp(agent)
	if err != nil || !ok {
		return false, err
	}
	return ts > moment, nil
}

// ReportLegacy is report_legacy(manager, agent_id, payload): a protocol 1
// Agent reports that it is rolling back. The route answers 400 for
// ValueError, TypeError and AttributeError (ErrorClass).
func ReportLegacy(ctx context.Context, m *agents.Manager, agentID string, payload any) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, noAttribute(nil, "get")
	}
	state, err := orDict(agent.Value("latest_upgrade"))
	if err != nil {
		return nil, err
	}
	invalid := valueError("Invalid legacy upgrade report")
	if !jsonx.Equal(state.Value("protocol"), jsonx.Int(1)) {
		return nil, invalid
	}
	updateID, err := get(payload, "update_id")
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(updateID, state.Value("update_id")) {
		return nil, invalid
	}
	if status, _ := get(payload, "status"); !jsonx.Equal(status, "rolling_back") {
		return nil, invalid
	}
	stateUpdateID, err := item(state, "update_id")
	if err != nil {
		return nil, err
	}
	reportedError := payload.(*jsonx.Object).GetOr("error", "Migration failed")
	return save(ctx, m, agentID, stateUpdateID, jsonx.Obj("status", "rolling_back",
		"recovery_reported_at", jsonx.Float(now()), "error", truncate(jsonx.Str(reportedError), 2048)), false)
}
