package monitor

// Port of backend/agents/oxidns_rule_update.py (A07): refresh deployed
// OxiDNS rule data through its registered Agent. This bridge exposes one
// fixed operation. Core addresses, credentials, plugin tags and download
// targets are resolved by the Agent from its deployed config. Newer Agents
// also accept a list of deployed rule files to refresh; they still resolve
// each file against the deployed download list themselves.

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// Versions and limits of oxidns_rule_update.py.
const (
	RuleUpdateMinAgentVersion      = "1.8.30-go"
	RuleUpdatePathsMinAgentVersion = "1.8.35-go"
	RuleUpdateMaxPaths             = 1000
	RuleUpdateMaxResponseBytes     = 16 * 1024
)

// RuleUpdateMessages is MESSAGES of oxidns_rule_update.py.
var RuleUpdateMessages = map[string]string{
	"UPGRADE_REQUIRED":           "请将此 Agent 升级到 " + RuleUpdateMinAgentVersion + " 或更新版本后使用更新规则。",
	"AGENT_NOT_FOUND":            "未找到这台 Agent。",
	"WRONG_SERVICE":              "更新规则仅适用于 OxiDNS Agent。",
	"AGENT_UNREACHABLE":          "暂时无法连接 Agent，请检查网络后重新查询更新状态。",
	"AGENT_AUTH_FAILED":          "Agent 认证失败，请检查注册状态。",
	"INVALID_REQUEST":            "更新规则请求无效。",
	"INVALID_RESPONSE":           "规则更新状态格式异常，请检查 Agent 和 OxiDNS 版本。",
	"API_NOT_CONFIGURED":         "OxiDNS 管理 API 未启用，请在设置中启用并发布一次。",
	"TLS_UNSUPPORTED":            "规则更新暂不支持当前 OxiDNS API 的 TLS 配置。",
	"CONFIG_UNSUPPORTED":         "当前 OxiDNS 配置无法用于更新规则，请检查管理 API 和规则更新配置。",
	"RULE_UPDATE_NOT_CONFIGURED": "请先配置可下载的规则并重新发布 OxiDNS 配置，然后重试。",
	"RULE_UPDATE_TARGET_UNKNOWN": "要更新的规则文件不在设备的下载清单中，请重新发布 OxiDNS 配置后重试。",
	"RULE_UPDATE_AMBIGUOUS":      "发现多条规则更新链，无法确定更新目标，请检查 OxiDNS 规则更新配置。",
	"CORE_UPDATE_UNSUPPORTED":    "当前 OxiDNS 核心不支持原生规则更新接口，请升级核心后重试。",
	"CORE_UNREACHABLE":           "暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。",
	"CORE_AUTH_FAILED":           "OxiDNS 管理 API 认证失败，请检查核心配置。",
	"SERVICE_BUSY":               "此设备正在发布、重启或更新，请等待当前操作结束。",
	"RULE_UPDATE_BUSY":           "此设备正在更新规则，请等待当前更新完成。",
	"CORE_BUSY":                  "OxiDNS 正在执行下载或重载，请等待当前操作结束。",
	"SNAPSHOT_CHANGED":           "运行中的配置与已部署文件不一致，请先确认配置已应用，再更新规则。",
	"DOWNLOAD_FAILED":            "规则下载未全部成功，本次未执行热重载，请检查 OxiDNS 日志。",
	"DOWNLOAD_TIMEOUT":           "规则下载等待超时，核心可能仍在下载；请稍后核对状态和日志。",
	"RELOAD_FAILED":              "部分规则未能热重载，已完成的更新不会自动回滚，请检查 OxiDNS 日志。",
	"RULE_UPDATE_TIMEOUT":        "规则更新等待超时，请检查 OxiDNS 运行状态和日志。",
	"RULE_UPDATE_UNKNOWN":        "暂时无法确认规则更新结果，请稍后核对状态和 OxiDNS 日志。",
	"RULE_UPDATE_FAILED":         "规则更新失败，请检查 OxiDNS 日志后重试。",
}

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

// HABusyError is the ValueError of oxidns_ha_publish.rules: the route
// answers SERVICE_BUSY 409 with its text.
type HABusyError struct{ Message string }

func (e *HABusyError) Error() string { return e.Message }

// ErrHARulesNotPorted is the default pair rule update until A18 (DNS HA)
// registers oxidns_ha_publish.rules with SetHARules.
var ErrHARulesNotPorted = errors.New("oxidns_ha_publish.rules is not ported yet (A18)")

// HARulesFunc is oxidns_ha_publish.rules(manager, agent_id, method): the
// task answer and its HTTP status; a *HABusyError for its ValueErrors.
type HARulesFunc func(ctx context.Context, m Manager, agentID, method string) (*jsonx.Object, int, error)

var (
	haRulesMu sync.RWMutex
	haRules   HARulesFunc = func(context.Context, Manager, string, string) (*jsonx.Object, int, error) {
		return nil, 0, ErrHARulesNotPorted
	}
)

// SetHARules installs oxidns_ha_publish.rules (A18) and returns a function
// restoring the previous one.
func SetHARules(fn HARulesFunc) (restore func()) {
	haRulesMu.Lock()
	previous := haRules
	haRules = fn
	haRulesMu.Unlock()
	return func() {
		haRulesMu.Lock()
		haRules = previous
		haRulesMu.Unlock()
	}
}

func currentHARules() HARulesFunc {
	haRulesMu.RLock()
	defer haRulesMu.RUnlock()
	return haRules
}

var haTerminal = map[string]bool{"succeeded": true, "failed": true, "rollback_failed": true}

// haActive is oxidns_ha_publish.active(agent) with Python's exceptions: a
// truthy non-dict publication record is the AttributeError of .get and an
// unhashable status the TypeError of the set membership test (both 500).
func haActive(agent *jsonx.Object) (bool, error) {
	job, err := orDict(agent.Value(agents.HAField))
	if err != nil || job.Len() == 0 {
		return false, err
	}
	status := job.Value("status")
	if unhashable(status) {
		return false, typeErrorUnhashable(status)
	}
	text, ok := status.(string)
	return !ok || !haTerminal[text], nil
}

// ruleUpdateAgent is oxidns_rule_update._agent.
func ruleUpdateAgent(ctx context.Context, m Manager, agentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, false)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 {
		return nil, RuleUpdateFail("AGENT_NOT_FOUND", 404)
	}
	if !pyEqualStr(agent.Value("service_type"), "oxidns") {
		return nil, RuleUpdateFail("WRONG_SERVICE", 400)
	}
	if !versionAtLeast(agent, RuleUpdateMinAgentVersion) {
		return nil, RuleUpdateFail("UPGRADE_REQUIRED", 409)
	}
	return agent, nil
}

// SupportsPaths is supports_paths(agent): whether the Agent can refresh
// selected files instead of every download.
func SupportsPaths(agent *jsonx.Object) bool {
	return versionAtLeast(agent, RuleUpdatePathsMinAgentVersion)
}

var (
	taskStates = []string{"idle", "running", "succeeded", "failed"}
	taskPhases = []string{"idle", "downloading", "reloading", "done"}
	taskIDRE   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
)

func strIn(value any, options []string) bool {
	s, ok := value.(string)
	return ok && contains(options, s)
}

// ruleUpdateTask is _task(value): the validated task of an Agent answer.
func ruleUpdateTask(value any) (*jsonx.Object, error) {
	invalid := RuleUpdateFail("INVALID_RESPONSE", 0)
	task, ok := value.(*jsonx.Object)
	if !ok || task == nil {
		return nil, invalid
	}
	state, phase, id := task.Value("status"), task.Value("phase"), task.Value("id")
	if !strIn(state, taskStates) || !strIn(phase, taskPhases) {
		return nil, invalid
	}
	idText, ok := id.(string)
	if !ok || (state == "idle" && idText != "") || (state != "idle" && !taskIDRE.MatchString(idText)) {
		return nil, invalid
	}
	result := jsonx.Obj("id", idText, "status", state, "phase", phase)
	counts := map[string]int64{}
	for _, key := range []string{"downloaded", "download_total", "reloaded", "reload_total"} {
		count := task.Value(key)
		if !pyIntIn(count, 0, 1_000_000) {
			return nil, invalid
		}
		counts[key] = pyInt64(count)
		result.Set(key, counts[key])
	}
	if counts["downloaded"] > counts["download_total"] || counts["reloaded"] > counts["reload_total"] {
		return nil, invalid
	}
	if state == "succeeded" && (phase != "done" || counts["downloaded"] != counts["download_total"] ||
		counts["reloaded"] != counts["reload_total"]) {
		return nil, invalid
	}
	if state == "failed" {
		code, ok := task.Value("code").(string)
		if _, known := RuleUpdateMessages[code]; !ok || !known {
			code = "RULE_UPDATE_FAILED"
		}
		result.Set("code", code)
		result.Set("message", RuleUpdateMessages[code])
	}
	return result, nil
}

// RuleUpdateCall is oxidns_rule_update.call(manager, agent_id, method,
// paths): start (POST) or read (GET) the Agent's rule update; paths nil is
// None. Returns the answer and its HTTP status.
func RuleUpdateCall(ctx context.Context, m Manager, agentID, method string, paths any) (*jsonx.Object, int, error) {
	invalidRequest := func() (*jsonx.Object, int, error) { return nil, 0, RuleUpdateFail("INVALID_REQUEST", 400) }
	if method != "GET" && method != "POST" {
		return invalidRequest()
	}
	if paths != nil {
		list, ok := paths.([]any)
		if method != "POST" || !ok || len(list) == 0 || len(list) > RuleUpdateMaxPaths {
			return invalidRequest()
		}
		for _, path := range list {
			if text, ok := path.(string); !ok || text == "" {
				return invalidRequest()
			}
		}
	}
	agent, err := ruleUpdateAgent(ctx, m, agentID)
	if err != nil {
		return nil, 0, err
	}
	ha, err := orDict(agent.Value("oxidns_ha"))
	if err != nil {
		return nil, 0, err
	}
	paired := ha.Value("enabled") == true
	active := false
	if method == "POST" {
		if active, err = haActive(agent); err != nil {
			return nil, 0, err
		}
	}
	if active {
		job, err := orDict(agent.Value(agents.HAField))
		if err != nil {
			return nil, 0, err
		}
		if !(paired && pyEqualStr(job.Value("kind"), "rules")) {
			return nil, 0, NewError("SERVICE_BUSY", 409, "DNS 主备正在同步，请等待两台发布完成后更新规则")
		}
	}
	if paired {
		data, status, err := currentHARules()(ctx, m, agentID, method)
		var busy *HABusyError
		if errors.As(err, &busy) {
			return nil, 0, NewError("SERVICE_BUSY", 409, busy.Message)
		}
		return data, status, err
	}
	if paths != nil && !SupportsPaths(agent) {
		return nil, 0, RuleUpdateFail("UPGRADE_REQUIRED", 409)
	}
	payload := []byte("{}")
	if paths != nil {
		if payload, err = jsonx.Default(jsonx.Obj("paths", paths)); err != nil {
			return nil, 0, err
		}
	}
	origin, err := AgentOrigin(agent)
	if err != nil {
		return nil, 0, err
	}
	token, ok := agent.Value("token").(string)
	if !ok {
		return nil, 0, RuleUpdateFail("AGENT_UNREACHABLE", 0) // KeyError / TypeError
	}
	encodedToken, ok := latin1(token)
	if !ok {
		return nil, 0, RuleUpdateFail("AGENT_UNREACHABLE", 0)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+encodedToken)
	header.Set("Accept", "application/json")
	var body []byte
	if method == "POST" {
		header.Set("Content-Type", "application/json")
		body = payload
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: origin + "/api/oxidns/rules/update",
		Header: header, Body: body, Timeout: agents.Pair(3, 15), NoProxy: true, NoRedirects: true,
		MaxBody: RuleUpdateMaxResponseBytes})
	if err != nil {
		return nil, 0, RuleUpdateFail("AGENT_UNREACHABLE", 0)
	}
	status := response.StatusCode
	data, err := agentAnswer(response, RuleUpdateMaxResponseBytes, RuleUpdateFail)
	if err != nil {
		return nil, 0, err
	}
	if status == 404 {
		return nil, 0, RuleUpdateFail("UPGRADE_REQUIRED", 409)
	}
	answer, ok := data.(*jsonx.Object)
	if !ok || answer == nil {
		return nil, 0, RuleUpdateFail("INVALID_RESPONSE", 0)
	}
	if status >= 400 || answer.Value("success") == false {
		code, isStr := answer.Value("code").(string)
		if _, known := RuleUpdateMessages[code]; !isStr || !known {
			code = "RULE_UPDATE_FAILED"
		}
		errStatus := 502
		switch status {
		case 400, 409, 503, 504:
			errStatus = status
		}
		return nil, 0, RuleUpdateFail(code, errStatus)
	}
	if (status != 200 && status != 202) || answer.Value("success") != true {
		return nil, 0, RuleUpdateFail("INVALID_RESPONSE", 0)
	}
	task, err := ruleUpdateTask(answer.Value("task"))
	if err != nil {
		return nil, 0, err
	}
	return jsonx.Obj("success", true, "task", task), status, nil
}
