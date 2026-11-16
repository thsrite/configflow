package upgrades

// Port of backend/agents/address_follow.py: follow an Agent whose host was
// renumbered.
//
// An Agent reports the address it should be reached at with every heartbeat.
// The record follows it once ConfigFlow has reached that address with the
// Agent's own token, so a wrong interface or an unreachable address never
// replaces a working one. Members of a DNS high-availability pair are only
// reported: both devices' VRRP configuration and the router's exemptions
// name the old address, and changing the record alone would hide a broken
// pair.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/pystr"
)

// Constants of address_follow.py.
const (
	VerifyTimeout = 4
	HASteps       = "它是 DNS 高可用成员，面板没有修改记录：两台设备的主备配置和路由器的放行名单里写的都是旧地址。" +
		"请依次：1. 停用 DNS 高可用；2. 等这台设备的地址在这里自动更新（约两分钟内）；" +
		"3. 重新配对并开启高可用；4. 重新预览并发布上游路由。"
	HAEdit = "这台设备是 DNS 高可用成员，不能在这里修改地址：两台设备的主备配置和路由器的放行名单里写的都是当前地址。" +
		"请依次：1. 停用 DNS 高可用；2. 修改地址；3. 重新配对并开启高可用；4. 重新预览并发布上游路由。"
	EnvEdit    = "这个容器的地址由环境变量 AGENT_IP 指定，容器每次启动都会用它。请修改 AGENT_IP 后重建容器，面板记录会自动更新。"
	RouterHint = "如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。"
)

// RetrySeconds is RETRY_SECONDS (a variable: tests set it to 0 as the
// Python tests monkeypatch it).
var RetrySeconds float64 = 120

var addressLog = logx.Get("backend.agents.address_follow")

type attempt struct {
	host   string
	member bool
	at     float64
}

var (
	addressMu sync.Mutex
	attempts  = map[string]attempt{} // agent id -> last attempt
	reported  = map[string]bool{}    // agent ids with a pending "address changed" notice
	started   = time.Now()
)

// Test seams: startThread runs the background check (Python's daemon
// thread), reachesHook and askHook replace _reaches and _ask.
var (
	startThread = func(fn func()) { go fn() }
	reachesHook func(agent *jsonx.Object, host string) bool
	askHook     func(agent *jsonx.Object, host, method string, body *jsonx.Object) (int, *jsonx.Object)
)

func monotonic() float64 { return time.Since(started).Seconds() }

// ResetAddressState forgets the attempts and pending notices (tests).
func ResetAddressState() {
	addressMu.Lock()
	defer addressMu.Unlock()
	attempts = map[string]attempt{}
	reported = map[string]bool{}
}

// Usable is usable(value): a LAN IPv4 address ConfigFlow can connect to
// ("" for None): not loopback, link-local, multicast, unspecified,
// reserved or a Docker bridge address (172.17.0.0–172.31.255.255).
func Usable(value any) string {
	text, ok := value.(string)
	if !ok { // IPv4Address(12) is 0.0.0.12
		return ""
	}
	octets, ok := parseIPv4(text)
	if !ok {
		return ""
	}
	a, b := octets[0], octets[1]
	switch {
	case a == 127, // is_loopback
		a == 169 && b == 254,           // is_link_local
		a >= 224 && a <= 239,           // is_multicast
		octets == [4]int{0, 0, 0, 0},   // is_unspecified
		a >= 240,                       // is_reserved
		a == 172 && b >= 17 && b <= 31: // in 172.16.0.0/12 and 17 <= packed[1] <= 31
		return ""
	}
	return strconv.Itoa(a) + "." + strconv.Itoa(b) + "." + strconv.Itoa(octets[2]) + "." + strconv.Itoa(octets[3])
}

// parseIPv4 is ipaddress.IPv4Address(text) of CPython 3.11: four ASCII
// decimal octets of at most three digits, no leading zeros, each ≤ 255.
func parseIPv4(text string) ([4]int, bool) {
	var out [4]int
	if text == "" || strings.Contains(text, "/") {
		return out, false
	}
	parts := strings.Split(text, ".")
	if len(parts) != 4 {
		return out, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 3 {
			return out, false
		}
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				return out, false
			}
		}
		if part != "0" && part[0] == '0' {
			return out, false
		}
		n, _ := strconv.Atoi(part)
		if n > 255 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func agentIDOf(agent *jsonx.Object) (string, error) {
	id, ok := agent.Get("id")
	if !ok {
		return "", keyError("id")
	}
	return jsonx.Str(id), nil
}

// Follow is follow(manager, agent, reported): called for every heartbeat
// that reports a host; returns at once and verifies in the background.
func Follow(_ context.Context, m *agents.Manager, agent *jsonx.Object, reportedHost string) error {
	host := Usable(reportedHost)
	agentID, err := agentIDOf(agent)
	if err != nil {
		return err
	}
	if host == "" || jsonx.Equal(host, agent.Value("host")) {
		addressMu.Lock()
		pending := reported[agentID]
		addressMu.Unlock()
		if host != "" && pending {
			resolve(m, agentID)
		}
		return nil
	}
	member, err := haMember(agent)
	if err != nil {
		return err
	}
	now := monotonic()
	addressMu.Lock()
	previous, ok := attempts[agentID]
	if ok && previous.host == host && previous.member == member && now-previous.at < RetrySeconds {
		addressMu.Unlock()
		return nil
	}
	attempts[agentID] = attempt{host, member, now}
	addressMu.Unlock()
	startThread(func() { followInBackground(m, agentID, host) })
	return nil
}

// Confirmed is confirmed(agent, reported): the reported address once the
// record holds it ("" otherwise); the Agent may then store it in its config.
func Confirmed(agent *jsonx.Object, reportedHost string) string {
	host := Usable(reportedHost)
	if host != "" && jsonx.Equal(host, agent.Value("host")) {
		return host
	}
	return ""
}

// agentPrefix is f"http://{host}:{agent['port']}" with the token header;
// KeyError when the record has no port or token.
func agentRequest(agent *jsonx.Object, host string) (string, http.Header, error) {
	port, ok := agent.Get("port")
	if !ok {
		return "", nil, keyError("port")
	}
	token, ok := agent.Get("token")
	if !ok {
		return "", nil, keyError("token")
	}
	return "http://" + host + ":" + jsonx.Str(port), http.Header{"Authorization": {"Bearer " + jsonx.Str(token)}}, nil
}

// encodingFailure is the UnicodeEncodeError of a token http.client cannot
// send as latin-1: not a RequestException, so it leaves _reaches/_ask (a
// logged failure in _follow, a 500 in describe/change).
func encodingFailure(err error) error {
	var call *agents.CallError
	if errors.As(err, &call) {
		return err
	}
	return nil
}

// reaches is _reaches(agent, host): only the Agent holding this record's
// token answers this authenticated endpoint. requests.get with the
// environment's proxies (trust_env).
func reaches(m *agents.Manager, agent *jsonx.Object, host string) (bool, error) {
	if reachesHook != nil {
		return reachesHook(agent, host), nil
	}
	base, header, err := agentRequest(agent, host)
	if err != nil {
		return false, err
	}
	response, err := m.Client().Do(context.Background(), agents.Request{Method: "GET", URL: base + "/api/capabilities",
		Header: header, Timeout: agents.Seconds(VerifyTimeout), Latin1Headers: true})
	if err != nil {
		return false, encodingFailure(err) // every other failure is a RequestException here
	}
	return response.StatusCode == 200, nil
}

// ask is _ask(agent, host, method, body): the Agent's own address endpoint;
// status 0 is None (unreachable), the body a dict ({} otherwise).
func ask(m *agents.Manager, agent *jsonx.Object, host, method string, body *jsonx.Object) (int, *jsonx.Object, error) {
	if askHook != nil {
		status, data := askHook(agent, host, method, body)
		return status, data, nil
	}
	base, header, err := agentRequest(agent, host)
	if err != nil {
		return 0, nil, err
	}
	var payload []byte
	if body != nil {
		if payload, err = jsonx.Default(body); err != nil {
			return 0, nil, err
		}
		header.Set("Content-Type", "application/json")
	}
	response, err := m.Client().Do(context.Background(), agents.Request{Method: method, URL: base + "/api/agent-address",
		Header: header, Body: payload, Timeout: agents.Seconds(VerifyTimeout), Latin1Headers: true})
	if err != nil {
		if failure := encodingFailure(err); failure != nil {
			return 0, nil, failure
		}
		return 0, jsonx.NewObject(), nil
	}
	data, err := response.JSON()
	object, ok := data.(*jsonx.Object)
	if err != nil || !ok || object == nil {
		object = jsonx.NewObject()
	}
	return response.StatusCode, object, nil
}

func followInBackground(m *agents.Manager, agentID, host string) {
	defer func() {
		if p := recover(); p != nil {
			addressLog.Error("Could not follow the reported address of Agent " + agentID)
		}
	}()
	if err := follow(context.Background(), m, agentID, host); err != nil {
		addressLog.Exception("Could not follow the reported address of Agent "+agentID, err)
	}
}

// sameNamedAt reports another record with the Agent's name at host.
func sameNamedAt(ctx context.Context, m *agents.Manager, agent *jsonx.Object, agentID, host string) (bool, error) {
	all, err := m.GetAllAgents(ctx, true)
	if err != nil {
		return false, err
	}
	for _, other := range all {
		id, ok := other.Get("id")
		if !ok {
			return false, keyError("id")
		}
		if !jsonx.Equal(id, agentID) && jsonx.Equal(other.Value("name"), agent.Value("name")) && jsonx.Equal(other.Value("host"), host) {
			return true, nil
		}
	}
	return false, nil
}

func oldHost(agent *jsonx.Object) string {
	if old := agent.Value("host"); jsonx.Truthy(old) {
		return jsonx.Str(old)
	}
	return ""
}

// follow is _follow(manager, agent_id, host).
func follow(ctx context.Context, m *agents.Manager, agentID, host string) error {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return err
	}
	if agent == nil || agent.Len() == 0 || jsonx.Equal(agent.Value("host"), host) {
		return nil
	}
	old, label := oldHost(agent), agents.AgentLabel(agent)
	member, err := haMember(agent)
	if err != nil {
		return err
	}
	if member {
		addressLog.Warning("Agent " + label + " 上报地址由 " + old + " 变为 " + host + "；它是 DNS 高可用成员，记录未修改")
		report(m, agent, host, "是 DNS 高可用成员，记录未修改", HASteps)
		return nil
	}
	if taken, err := sameNamedAt(ctx, m, agent, agentID, host); err != nil {
		return err
	} else if taken {
		addressLog.Warning("Agent " + label + " 上报地址 " + host + " 与另一条同名记录相同，记录未修改")
		return nil
	}
	ok, err := reaches(m, agent, host)
	if err != nil {
		return err
	}
	if !ok {
		addressLog.Warning("Agent " + label + " 上报地址由 " + old + " 变为 " + host + "，但面板用它的凭据连不上新地址，记录未修改")
		report(m, agent, host, "面板连不上新地址，记录未修改",
			"面板用这台 Agent 的凭据访问 "+host+":"+jsonx.Str(agent.Value("port"))+" 没有成功，所以仍按 "+old+" 联系它。"+
				"请确认面板所在的机器能访问新地址和 Agent 端口；能访问后会自动更新。")
		return nil
	}
	updated, err := m.UpdateAgent(ctx, agentID, jsonx.Obj("host", host))
	var conflict *agents.DeploymentConflict
	if errors.As(err, &conflict) {
		// A publication or update is in flight; the next heartbeat tries again.
		addressLog.Info("Agent " + label + " 的地址暂未更新为 " + host + "：" + conflict.Error())
		addressMu.Lock()
		delete(attempts, agentID)
		addressMu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	addressLog.Info("Agent " + label + " 的地址已由 " + old + " 更新为 " + host + "（Agent 上报，已用其凭据核实可连接）")
	resolve(m, agentID)
	name := agent.Value("name")
	if !jsonx.Truthy(name) {
		name = agentID
	}
	repository := m.Repository()
	notifications.Safely(func() error {
		return notifications.RecordNotice(repository, "notice:agent-address:"+agentID,
			jsonx.Str(name)+" · Agent 地址已自动更新",
			"设备的地址由 "+old+" 变为 "+host+"，面板已核实能用新地址连上它并更新了记录。"+
				"如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。",
			notifications.Target("/agents", "查看 Agent", "agent", agentID))
	})
	return nil
}

func report(m *agents.Manager, agent *jsonx.Object, host, label, message string) {
	id, _ := agentIDOf(agent)
	addressMu.Lock()
	reported[id] = true
	addressMu.Unlock()
	repository := m.Repository()
	notifications.Safely(func() error { return notifications.RecordAgentAddress(repository, agent, host, label, message) })
}

func resolve(m *agents.Manager, agentID string) {
	addressMu.Lock()
	delete(reported, agentID)
	addressMu.Unlock()
	repository := m.Repository()
	notifications.Safely(func() error { return notifications.ResolveAgentAddress(repository, agentID) })
}

// mode is _mode(agent, status, info): what changing the address changes.
func mode(agent *jsonx.Object, status int, info *jsonx.Object) (string, string) {
	if status == 200 && info.Value("writable") == true {
		return "agent", ""
	}
	if status == 200 && jsonx.Truthy(info.Value("configured")) {
		return "env", EnvEdit
	}
	if jsonx.Equal(agent.Value("deployment_method"), "docker") {
		return "record", "容器没有指定 AGENT_IP，这里只修改面板记录的地址（填宿主机的局域网地址）。"
	}
	if status == 404 {
		return "record", "这台 Agent 版本较旧，只修改面板记录的地址；升级 Agent 后可同时写入设备上的配置文件。"
	}
	return "record", ""
}

// iterate is `for value in x` over a JSON value: list items, dict keys,
// string characters; TypeError for numbers and booleans.
func iterate(value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return v, nil
	case *jsonx.Object:
		out := make([]any, 0, v.Len())
		for _, key := range v.Keys() {
			out = append(out, key)
		}
		return out, nil
	case string:
		out := []any{}
		for _, r := range v {
			out = append(out, string(r))
		}
		return out, nil
	}
	return nil, typeError("'" + typeName(value) + "' object is not iterable")
}

// Describe is describe(agent): what the 修改地址 dialog shows before anything
// is changed.
func Describe(_ context.Context, m *agents.Manager, agent *jsonx.Object) (*jsonx.Object, error) {
	host := agent.Value("host")
	if !jsonx.Truthy(host) {
		host = ""
	}
	result := func(mode, message string, addresses []any, configured string, reachable bool) *jsonx.Object {
		return jsonx.Obj("host", host, "port", agent.Value("port"), "mode", mode, "message", message,
			"addresses", addresses, "configured", configured, "reachable", reachable)
	}
	member, err := haMember(agent)
	if err != nil {
		return nil, err
	}
	if member {
		return result("ha", HAEdit, []any{}, "", false), nil
	}
	status, info, err := ask(m, agent, jsonx.Str(agent.Value("host")), "GET", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 && status != 404 {
		return result("unknown", "面板用当前地址 "+jsonx.Str(agent.Value("host"))+":"+jsonx.Str(agent.Value("port"))+
			" 连不上这台 Agent。填写它现在的地址，保存时会先核实能连上。", []any{}, "", false), nil
	}
	chosen, message := mode(agent, status, info)
	listed := info.Value("addresses")
	if !jsonx.Truthy(listed) {
		listed = []any{}
	}
	values, err := iterate(listed)
	if err != nil {
		return nil, err
	}
	addresses := []any{}
	for _, value := range values {
		if text, ok := value.(string); ok && Usable(text) != "" {
			addresses = append(addresses, value)
		}
	}
	if chosen != "agent" {
		addresses = []any{}
	}
	configured := info.Value("configured")
	if !jsonx.Truthy(configured) {
		configured = ""
	}
	return result(chosen, message, addresses, jsonx.Str(configured), true), nil
}

func refusal(message any) *jsonx.Object { return jsonx.Obj("success", false, "message", message) }

// Change is change(manager, agent, value): set the address by hand — reach
// the Agent there first, store it on the device, then in the record. It
// returns the answer and its HTTP status.
func Change(ctx context.Context, m *agents.Manager, agent *jsonx.Object, value any) (*jsonx.Object, int, error) {
	if text, ok := value.(string); ok {
		value = pystr.Strip(text)
	}
	host := Usable(value)
	if host == "" {
		return refusal("请输入这台设备的局域网 IPv4 地址"), 400, nil
	}
	agentID, err := agentIDOf(agent)
	if err != nil {
		return nil, 0, err
	}
	old, label := oldHost(agent), agents.AgentLabel(agent)
	member, err := haMember(agent)
	if err != nil {
		return nil, 0, err
	}
	if member {
		return refusal(HAEdit), 409, nil
	}
	if taken, err := sameNamedAt(ctx, m, agent, agentID, host); err != nil {
		return nil, 0, err
	} else if taken {
		return refusal("另一条同名的 Agent 记录已经在用 " + host), 409, nil
	}
	status, info, err := ask(m, agent, host, "GET", nil)
	if err != nil {
		return nil, 0, err
	}
	if status == 404 {
		// An Agent without the address endpoint: prove it is this Agent another way.
		ok, err := reaches(m, agent, host)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			status = 0
		}
	}
	port := jsonx.Str(agent.Value("port"))
	if status == 0 {
		return refusal("面板连不上 " + host + ":" + port + "，没有修改。请确认地址正确，且面板所在的机器能访问它。"), 502, nil
	}
	if status != 200 && status != 404 {
		return refusal(host + ":" + port + " 有应答，但不是这台 Agent（凭据不匹配），没有修改。"), 502, nil
	}
	chosen, message := mode(agent, status, info)
	if chosen == "env" {
		return refusal(message), 409, nil
	}
	written := false
	if chosen == "agent" {
		status, reply, err := ask(m, agent, host, "POST", jsonx.Obj("host", host))
		if err != nil {
			return nil, 0, err
		}
		if status != 200 {
			text := reply.Value("message")
			if !jsonx.Truthy(text) {
				answer := "无应答"
				if status != 0 {
					answer = strconv.Itoa(status)
				}
				text = "设备没有接受这个地址（" + answer + "），没有修改。"
			}
			code := 502
			if status == 400 {
				code = 400
			}
			return refusal(text), code, nil
		}
		written = true
	}
	if host != old {
		updated, err := m.UpdateAgent(ctx, agentID, jsonx.Obj("host", host))
		var conflict *agents.DeploymentConflict
		if errors.As(err, &conflict) {
			return refusal(conflict.Error()), 409, nil
		}
		if err != nil {
			return nil, 0, err
		}
		if !updated {
			return refusal("Agent not found"), 404, nil
		}
	}
	addressMu.Lock()
	delete(attempts, agentID)
	addressMu.Unlock()
	resolve(m, agentID)
	note := ""
	if written {
		note = "，已写入设备配置"
	}
	addressLog.Info("Agent " + label + " 的地址已由 " + old + " 改为 " + host + "（手动修改，已核实可连接" + note + "）")
	text := "地址已改为 " + host
	if written {
		text += "，并写入了设备上的 Agent 配置。"
	} else {
		text += "（只修改了面板记录）。"
	}
	if host != old {
		text += RouterHint
	}
	return jsonx.Obj("success", true, "host", host, "written", written, "changed", host != old, "message", text), 200, nil
}
