package monitor

// Port of backend/test_oxidns_rule_update.py (call-level cases): rule
// refresh is a fixed, authenticated operation with truthful task results.
// The route cases are in internal/api/agents/panel_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

func ruleAgent(changes ...any) *model.Agent {
	a := map[string]any{
		"id": "dns", "name": "DNS", "host": "127.0.0.1", "port": 8082,
		"service_type": "oxidns", "version": "1.8.30-go", "token": "agent-secret",
	}
	for i := 0; i+1 < len(changes); i += 2 {
		a[changes[i].(string)] = changes[i+1]
	}
	return mustAgentMap(a)
}

func ruleTask(status string, fields ...any) map[string]any {
	phase := "done"
	if status == "running" {
		phase = "downloading"
	}
	task := map[string]any{
		"id": strings.Repeat("a", 32), "status": status, "phase": phase,
		"downloaded": 0, "download_total": 2, "reloaded": 0, "reload_total": 1,
	}
	for i := 0; i+1 < len(fields); i += 2 {
		task[fields[i].(string)] = fields[i+1]
	}
	return task
}

func ruleManager(t *testing.T, agent *model.Agent) (*fakeManager, *replyClient) {
	if agent == nil {
		agent = ruleAgent()
	}
	client := &replyClient{}
	client.add(jsonResponse(map[string]any{"success": true, "task": ruleTask("running")}, 202))
	return &fakeManager{agents: map[string]*model.Agent{"dns": agent}, dataDir: t.TempDir(), client: client}, client
}

func (c *replyClient) set(replies ...any) {
	c.mu.Lock()
	c.replies = append([]any(nil), replies...)
	c.mu.Unlock()
}

func dumps(t *testing.T, v any) string {
	t.Helper()
	return string(mustMarshal(t, v))
}

func TestStartUsesOnlyRegisteredAgentAndKeepsAcceptedRunning(t *testing.T) {
	m, client := ruleManager(t, nil)
	raw, status, err := CallRuleUpdate(ctx, m, "dns", "POST", nil)
	if err != nil || status != 202 || raw.Task.Status != "running" {
		t.Fatal(raw, status, err)
	}
	c := client.calls[0]
	if c.url != "http://127.0.0.1:8082/api/oxidns/rules/update" || string(c.req.Body) != "{}" ||
		c.header["Authorization"] != "Bearer agent-secret" || !c.req.NoProxy || !c.req.NoRedirects ||
		c.req.Timeout != agents.Pair(3, 15) || c.header["Content-Type"] != "application/json" {
		t.Fatal(c)
	}
	if strings.Contains(dumps(t, raw), "agent-secret") {
		t.Fatal(raw)
	}
}

func TestGetReturnsActualCompletedCountsAndStripsPrivateFields(t *testing.T) {
	m, client := ruleManager(t, nil)
	client.set(jsonResponse(map[string]any{"success": true, "task": ruleTask("succeeded", "downloaded", 2, "reloaded", 1,
		"token", "secret", "url", "https://secret.test")}, 200))
	raw, status, err := CallRuleUpdate(ctx, m, "dns", "GET", nil)
	if err != nil || status != 200 {
		t.Fatal(err)
	}
	task := asMap(toJSONMap(t, raw)["task"])
	if task["status"] != "succeeded" || asInt64(task["downloaded"]) != 2 || asInt64(task["reloaded"]) != 1 ||
		strings.Contains(dumps(t, raw), "secret") || client.calls[0].req.Body != nil {
		t.Fatal(raw)
	}
}

func TestFailedDownloadNeverBecomesSuccessAndHasSafeActionableMessage(t *testing.T) {
	m, client := ruleManager(t, nil)
	client.set(jsonResponse(map[string]any{"success": true, "task": ruleTask("failed", "code", "DOWNLOAD_FAILED",
		"downloaded", 1, "message", "http://user:secret@upstream.test")}, 200))
	raw, _, err := CallRuleUpdate(ctx, m, "dns", "GET", nil)
	if err != nil {
		t.Fatal(err)
	}
	task := asMap(toJSONMap(t, raw)["task"])
	if task["status"] != "failed" || task["code"] != "DOWNLOAD_FAILED" ||
		!strings.Contains(asStr(task["message"]), "未执行热重载") || strings.Contains(dumps(t, raw), "secret") {
		t.Fatal(raw)
	}
}

func TestMissingUpdateConfigPointsToDownloadableRulesNotScheduling(t *testing.T) {
	m, client := ruleManager(t, nil)
	client.set(jsonResponse(map[string]any{"success": false, "code": "RULE_UPDATE_NOT_CONFIGURED",
		"message": "Enable native rule updates and publish the configuration"}, 409))
	_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", nil)
	if e := wantMonitorError(t, err, "RULE_UPDATE_NOT_CONFIGURED", 409); e.Message != "请先配置可下载的规则并重新发布 OxiDNS 配置，然后重试。" {
		t.Fatal(e)
	}
}

func TestRuleUpdateRejectsUnsupportedTargetsBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		agent   *model.Agent
		agentID string
		code    string
	}{
		{ruleAgent(), "missing", "AGENT_NOT_FOUND"},
		{ruleAgent("service_type", "mihomo"), "dns", "WRONG_SERVICE"},
		{ruleAgent("version", "1.8.20-go"), "dns", "UPGRADE_REQUIRED"},
		{ruleAgent("host", "host/path?secret"), "dns", "AGENT_UNREACHABLE"},
	} {
		m, client := ruleManager(t, tc.agent)
		_, _, err := CallRuleUpdate(ctx, m, tc.agentID, "POST", nil)
		wantMonitorError(t, err, tc.code, 0)
		if client.count() != 0 {
			t.Fatal(tc.code)
		}
	}
}

func TestRejectsInvalidOrPrivateUpstreamResponses(t *testing.T) {
	for _, tc := range []struct {
		reply *agents.Response
		code  string
	}{
		{rawResponse(401, "secret"), "AGENT_AUTH_FAILED"},
		{rawResponse(302, "secret"), "INVALID_RESPONSE"},
		{rawResponse(404, "404 page not found"), "UPGRADE_REQUIRED"},
		{rawResponse(200, "not json secret"), "INVALID_RESPONSE"},
		{rawResponse(200, strings.Repeat("x", RuleUpdateMaxResponseBytes+1)), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": false, "code": "SERVICE_BUSY", "message": "secret"}, 409), "SERVICE_BUSY"},
		{jsonResponse(map[string]any{"success": false, "code": []any{"secret"}}, 409), "RULE_UPDATE_FAILED"},
		{jsonResponse(map[string]any{"message": "secret"}, 500), "RULE_UPDATE_FAILED"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("succeeded")}, 200), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("running", "downloaded", true)}, 200), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("running", "downloaded", 3)}, 200), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("running", "id", "http://secret")}, 200), "INVALID_RESPONSE"},
		// Beyond the Python cases: a JSON 404 is still UPGRADE_REQUIRED, floats are not ints.
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("running")}, 404), "UPGRADE_REQUIRED"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("running", "downloaded", json.Number("1.0"))}, 200), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("running")}, 201), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true, "task": ruleTask("idle", "phase", "idle")}, 200), "INVALID_RESPONSE"},
	} {
		m, client := ruleManager(t, nil)
		client.set(tc.reply)
		_, _, err := CallRuleUpdate(ctx, m, "dns", "GET", nil)
		if e := wantMonitorError(t, err, tc.code, 0); strings.Contains(e.Message, "secret") {
			t.Fatal(e)
		}
	}
}

func TestTransportFailureIsSanitized(t *testing.T) {
	m, client := ruleManager(t, nil)
	client.set(errors.New("http://user:secret@private.test"))
	_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", nil)
	if e := wantMonitorError(t, err, "AGENT_UNREACHABLE", 502); strings.Contains(e.Message, "secret") {
		t.Fatal(e)
	}
}

func TestTargetedPathsRequireNewAgentAndAreSentAsAList(t *testing.T) {
	m, client := ruleManager(t, nil)
	_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", []string{"rules/a.txt"})
	wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	if client.count() != 0 {
		t.Fatal(client.calls)
	}
	m.agents["dns"] = ruleAgent("version", "1.8.35-go")
	for _, invalid := range [][]string{{}, {""}} {
		if _, _, err := CallRuleUpdate(ctx, m, "dns", "POST", invalid); err == nil {
			t.Fatal(invalid)
		}
	}
	if _, _, err := CallRuleUpdate(ctx, m, "dns", "GET", []string{"rules/a.txt"}); err == nil {
		t.Fatal("GET with paths")
	}
	if client.count() != 0 {
		t.Fatal(client.calls)
	}
	result, status, err := CallRuleUpdate(ctx, m, "dns", "POST", []string{"rules/a.txt", "rules/b.txt"})
	if err != nil || status != 202 || result.Task.Status != "running" {
		t.Fatal(result, err)
	}
	if string(client.calls[0].req.Body) != `{"paths": ["rules/a.txt", "rules/b.txt"]}` {
		t.Fatal(string(client.calls[0].req.Body))
	}
}

func TestUnknownTargetIsReportedAsAConflict(t *testing.T) {
	m, client := ruleManager(t, ruleAgent("version", "1.8.35-go"))
	client.set(jsonResponse(map[string]any{"success": false, "code": "RULE_UPDATE_TARGET_UNKNOWN"}, 409))
	_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", []string{"rules/a.txt"})
	wantMonitorError(t, err, "RULE_UPDATE_TARGET_UNKNOWN", 409)
}

// The DNS HA branches (oxidns_ha_publish.FIELD / active / rules; rules is
// A18's and a hook here).
func TestRuleUpdateOfDNSHAPairs(t *testing.T) {
	busy := map[string]any{"status": "publishing", "kind": "deploy"}
	m, client := ruleManager(t, ruleAgent(agents.HAField, busy))
	_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", nil)
	if e := wantMonitorError(t, err, "SERVICE_BUSY", 409); e.Message != "DNS 主备正在同步，请等待两台发布完成后更新规则" {
		t.Fatal(e)
	}
	// A GET of a busy unpaired Agent still reads the Agent's task.
	if _, status, err := CallRuleUpdate(ctx, m, "dns", "GET", nil); err != nil || status != 202 {
		t.Fatal(err)
	}
	// A pair defers to oxidns_ha_publish.rules; not ported yet → an error (500).
	m.agents["dns"] = ruleAgent("oxidns_ha", map[string]any{"enabled": true}, agents.HAField, map[string]any{"status": "publishing", "kind": "rules"})
	if _, _, err := CallRuleUpdate(ctx, m, "dns", "POST", nil); !errors.Is(err, ErrHARulesNotPorted) {
		t.Fatal(err)
	}
	restore := SetHARulesJSON(func(_ context.Context, _ Registry, agentID, method string) (json.RawMessage, int, error) {
		if method == "POST" {
			return nil, 0, &HABusyError{"请先完整发布相同的主备配置，再更新规则"}
		}
		return mustMarshal(t, map[string]any{"success": true, "task": ruleTask("running")}), 202, nil
	})
	defer restore()
	_, _, err = CallRuleUpdate(ctx, m, "dns", "POST", nil)
	if e := wantMonitorError(t, err, "SERVICE_BUSY", 409); e.Message != "请先完整发布相同的主备配置，再更新规则" {
		t.Fatal(e)
	}
	if _, status, err := CallRuleUpdate(ctx, m, "dns", "GET", nil); err != nil || status != 202 {
		t.Fatal(status, err)
	}
	if client.count() != 1 {
		t.Fatal("a pair never calls the Agent directly", client.calls)
	}
	// A truthy oxidns_ha that is not a dict is Python's AttributeError.
	m.agents["dns"] = ruleAgent("oxidns_ha", "yes")
	var monitorErr *Error
	if _, _, err := CallRuleUpdate(ctx, m, "dns", "GET", nil); err == nil || errors.As(err, &monitorErr) {
		t.Fatal(err)
	}
}
