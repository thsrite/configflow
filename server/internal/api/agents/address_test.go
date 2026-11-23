package agents_test

// Port of backend/test_agent_address_follow.py: a renumbered Agent is
// followed only after it is reached at the new address; HA members are only
// reported. The background check runs inline (upgrades.RunInlineForTest) as
// the Python test replaces threading.Thread; _reaches and _ask are replaced
// through the upgrades test seams. The two cases that exercise _reaches and
// _ask themselves are in internal/agents/upgrades (address_test.go).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/agents"
	agentcore "configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/testjson"
)

const oldHost, newHost = "192.0.2.2", "192.0.2.11"

type addressEnv struct {
	t          *testing.T
	s          *httpxtest.Server
	agentID    string
	agentToken string
	reachable  bool
	reached    [][2]string
}

func newAddressEnv(t *testing.T) *addressEnv {
	t.Helper()
	s := newServer(t)
	upgrades.ResetAddressState()
	t.Cleanup(upgrades.ResetAddressState)
	t.Cleanup(upgrades.RunInlineForTest())
	e := &addressEnv{t: t, s: s, reachable: true}
	t.Cleanup(upgrades.SetReachesForTest(func(record *model.Agent, host string) bool {
		e.reached = append(e.reached, [2]string{record.Token, host})
		return e.reachable
	}))
	e.agentID, e.agentToken = e.register(oldHost)
	return e
}

func (e *addressEnv) manager() *agents.Manager {
	m, err := agents.Get(e.s.Context())
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *addressEnv) register(host string) (string, string) {
	e.t.Helper()
	return registerAgentForTest(e.t, e.s.Context(), e.manager(), map[string]any{
		"name": "dns", "host": host, "port": 8082, "service_type": "oxidns",
	})
}

func (e *addressEnv) beat(pairs ...any) *httpxtest.Result {
	e.t.Helper()
	body := map[string]any{"version": "1.8.60-go"}
	for i := 0; i+1 < len(pairs); i += 2 {
		body[pairs[i].(string)] = pairs[i+1]
	}
	return e.s.Post("/api/agents/"+e.agentID+"/heartbeat", httpxtest.JSON(body), httpxtest.Bearer(e.agentToken))
}

func (e *addressEnv) host() string {
	e.t.Helper()
	agent, err := e.manager().ReadAgent(e.s.Context(), e.agentID, true)
	if err != nil {
		e.t.Fatal(err)
	}
	return agent.Host
}

func (e *addressEnv) revision() int64 {
	e.t.Helper()
	sys, err := e.s.Repo.System(e.s.Context())
	if err != nil {
		e.t.Fatal(err)
	}
	return int64(sys.Revision)
}

// notes are the pending notification items of source ("" = all).
func (e *addressEnv) notes(source string) []map[string]any {
	e.t.Helper()
	var out []map[string]any
	err := notifications.UpdateItems(e.s.Context(), e.s.DataDir, func(items *notifications.Items) error {
		for _, value := range parseJSONMap(e.t, mustRawJSON(e.t, items)) {
			item := value.(map[string]any)
			if item["state"] == "pending" && (source == "" || item["source"] == source) {
				out = append(out, cloneMap(e.t, item))
			}
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *addressEnv) setHA(enabled bool) {
	e.t.Helper()
	err := e.s.Repo.UpdateSystem(e.s.Context(), func(_ context.Context, sys *model.System) error {
		doc := parseJSONMap(e.t, mustRawJSON(e.t, sys))
		agentsList := mapList(doc, "agents")
		for _, item := range agentsList {
			agent := item.(map[string]any)
			if agent["id"] == e.agentID {
				if enabled {
					agent["oxidns_ha"] = map[string]any{"enabled": true, "vip": "192.0.2.53"}
				} else {
					delete(agent, "oxidns_ha")
				}
			}
		}
		return sys.SetJSON("agents", mustRawJSON(e.t, agentsList))
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func TestReplyAnnouncesTheFieldAndAnUnchangedOrAbsentAddressDoesNothing(t *testing.T) {
	e := newAddressEnv(t)
	if reply := parseBodyMap(t, e.beat().WantStatus(200)); reply["reads_host"] != true {
		t.Fatalf("reply %v", reply)
	}
	e.beat("host", oldHost).WantStatus(200)
	if e.host() != oldHost || len(e.reached) != 0 || len(e.notes("")) != 0 {
		t.Fatal("changed")
	}
	// Fields the panel does not know are still refused, as before.
	e.beat("address", newHost).WantStatus(400)
}

func TestRecordFollowsAnAddressReachedWithTheAgentsOwnToken(t *testing.T) {
	e := newAddressEnv(t)
	before := e.revision()
	e.beat("host", newHost).WantStatus(200)
	if e.host() != newHost || len(e.reached) != 1 || e.reached[0] != [2]string{e.agentToken, newHost} {
		t.Fatalf("host %v reached %v", e.host(), e.reached)
	}
	notes := e.notes(notifications.NoticeSource)
	if len(notes) != 1 {
		t.Fatalf("notes %v", notes)
	}
	message := mapStr(notes[0], "message")
	if !strings.Contains(message, oldHost) || !strings.Contains(message, newHost) || !strings.Contains(message, "上游路由") ||
		!testjson.Equal(mapObj(notes[0], "target")["query"], map[string]any{"agent": e.agentID}) {
		t.Fatalf("notice %v", notes[0])
	}
	revision := e.revision()
	if revision == before {
		t.Fatal("revision unchanged")
	}
	e.beat("host", newHost)
	e.beat("host", newHost)
	if e.revision() != revision || len(e.reached) != 1 {
		t.Fatal("changed again")
	}
}

func TestUnreachableAddressIsReportedNotAdoptedAndRetriedLater(t *testing.T) {
	e := newAddressEnv(t)
	e.reachable = false
	e.beat("host", newHost)
	e.beat("host", newHost)
	if e.host() != oldHost || len(e.reached) != 1 { // one attempt per retry window
		t.Fatalf("host %v reached %v", e.host(), e.reached)
	}
	notes := e.notes(notifications.AgentAddressSource)
	if len(notes) != 1 || !strings.Contains(mapStr(notes[0], "title"), newHost) ||
		!strings.Contains(mapStr(notes[0], "status_label"), "连不上") || !strings.Contains(mapStr(notes[0], "message"), "自动更新") {
		t.Fatalf("notes %v", notes)
	}
	t.Cleanup(upgrades.SetRetrySecondsForTest(0))
	e.reachable = true
	e.beat("host", newHost)
	if e.host() != newHost || len(e.notes(notifications.AgentAddressSource)) != 0 || len(e.notes(notifications.NoticeSource)) != 1 {
		t.Fatal("not followed / warning not withdrawn")
	}
}

func TestHighAvailabilityMemberIsOnlyReportedWithTheSteps(t *testing.T) {
	e := newAddressEnv(t)
	e.setHA(true)
	e.beat("host", newHost)
	if e.host() != oldHost || len(e.reached) != 0 { // not even probed
		t.Fatal("followed")
	}
	notes := e.notes(notifications.AgentAddressSource)
	if len(notes) != 1 || !strings.Contains(mapStr(notes[0], "status_label"), "高可用") {
		t.Fatalf("notes %v", notes)
	}
	for _, step := range []string{"停用 DNS 高可用", "重新配对", "发布上游路由"} {
		if !strings.Contains(mapStr(notes[0], "message"), step) {
			t.Fatalf("missing %s", step)
		}
	}
	// After HA is disabled the same report is acted on at once.
	e.setHA(false)
	e.beat("host", newHost)
	if e.host() != newHost || len(e.notes(notifications.AgentAddressSource)) != 0 {
		t.Fatal("not followed after HA")
	}
}

func TestAddressesThePanelCannotUseAreIgnored(t *testing.T) {
	for _, value := range []string{"172.17.0.2", "172.31.255.1", "127.0.0.1", "169.254.1.1", "0.0.0.0", "224.0.0.1",
		"fe80::1", "dns.example", "192.0.2.11:8082", ""} {
		e := newAddressEnv(t)
		e.beat("host", value).WantStatus(200)
		if e.host() != oldHost || len(e.reached) != 0 || len(e.notes("")) != 0 {
			t.Fatalf("%q: acted on", value)
		}
	}
}

func TestAddressAlreadyUsedByASameNamedRecordIsNotTaken(t *testing.T) {
	e := newAddressEnv(t)
	e.register(newHost)
	e.beat("host", newHost)
	if e.host() != oldHost || len(e.reached) != 0 {
		t.Fatal("taken")
	}
}

func TestOversizedOrNonTextAddressIsRejected(t *testing.T) {
	e := newAddressEnv(t)
	e.beat("host", strings.Repeat("1", 65)).WantStatus(400)
	e.beat("host", []any{newHost}).WantStatus(400)
	if e.host() != oldHost {
		t.Fatal("changed")
	}
}

func TestTheAgentIsToldOnceTheRecordHoldsTheAddressItReports(t *testing.T) {
	e := newAddressEnv(t)
	// The record has not followed yet when this reply is built: no confirmation.
	if mapHas(parseBodyMap(t, e.beat("host", newHost)), "confirmed_host") || e.host() != newHost {
		t.Fatal("first reply")
	}
	if got := parseBodyMap(t, e.beat("host", newHost))["confirmed_host"]; got != newHost {
		t.Fatalf("confirmed %v", got)
	}
	if mapHas(parseBodyMap(t, e.beat()), "confirmed_host") {
		t.Fatal("confirmed without a report")
	}
}

func TestAnAddressThePanelDidNotAdoptIsNeverConfirmed(t *testing.T) {
	e := newAddressEnv(t)
	e.reachable = false
	e.beat("host", newHost)
	if mapHas(parseBodyMap(t, e.beat("host", newHost)), "confirmed_host") {
		t.Fatal("confirmed")
	}
}

type deviceCall struct {
	method string
	host   string
	body   map[string]any
}

// device is the Agent's /api/agent-address as seen from the panel.
type device struct {
	addresses  []string
	configured string
	writable   bool
	status     int
	calls      []deviceCall
}

func newDevice(t *testing.T, addresses []string, configured string, writable bool, status int) *device {
	d := &device{addresses: addresses, configured: configured, writable: writable, status: status}
	t.Cleanup(upgrades.SetAskForTest(d.ask))
	return d
}

func (d *device) has(host string) bool {
	for _, address := range d.addresses {
		if address == host {
			return true
		}
	}
	return false
}

func fieldsFromMap(v map[string]any) model.Fields {
	raw, _ := json.Marshal(v)
	var f model.Fields
	_ = json.Unmarshal(raw, &f)
	return f
}

func (d *device) ask(_ *model.Agent, host, method string, rawBody []byte) (int, model.Fields) {
	var body map[string]any
	if len(rawBody) > 0 {
		if v, err := decodeJSONAny(rawBody); err == nil {
			body, _ = v.(map[string]any)
		}
	}
	d.calls = append(d.calls, deviceCall{method: method, host: host, body: body})
	if !d.has(host) {
		return 0, fieldsFromMap(map[string]any{})
	}
	if d.status != 200 {
		return d.status, fieldsFromMap(map[string]any{})
	}
	if method == "GET" {
		addresses := []any{}
		for _, a := range d.addresses {
			addresses = append(addresses, a)
		}
		return 200, fieldsFromMap(map[string]any{"success": true, "configured": d.configured, "addresses": addresses, "writable": d.writable})
	}
	if !d.writable {
		return 409, fieldsFromMap(map[string]any{"success": false, "message": "container"})
	}
	requested := mapStr(body, "host")
	if !d.has(requested) {
		return 400, fieldsFromMap(map[string]any{"success": false, "message": "这台设备没有地址 " + requested})
	}
	d.configured = requested
	return 200, fieldsFromMap(map[string]any{"success": true, "host": requested})
}

func (d *device) onlyGets() bool {
	for _, call := range d.calls {
		if call.method != "GET" {
			return false
		}
	}
	return true
}

func (e *addressEnv) getAddress() map[string]any {
	return mapObj(parseBodyMap(e.t, e.s.Get("/api/agents/"+e.agentID+"/address").WantStatus(200)), "data")
}

func (e *addressEnv) putAddress(host any) *httpxtest.Result {
	return e.s.Put("/api/agents/"+e.agentID+"/address", httpxtest.JSON(map[string]any{"host": host}))
}

func TestDialogListsTheDevicesOwnAddresses(t *testing.T) {
	e := newAddressEnv(t)
	newDevice(t, []string{oldHost, newHost}, oldHost, true, 200)
	want := map[string]any{
		"host": oldHost, "port": 8082, "mode": "agent", "message": "", "addresses": []any{oldHost, newHost},
		"configured": oldHost, "reachable": true,
	}
	if got := e.getAddress(); !testjson.Equal(got, want) {
		t.Fatalf("got %v, diff: %s", got, testjson.Diff(want, got))
	}
}

func TestManualChangeWritesTheDeviceFirstThenTheRecord(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, oldHost, true, 200)
	body := parseBodyMap(t, e.putAddress(" "+newHost+" ").WantStatus(200))
	if body["host"] != newHost || body["written"] != true || !strings.Contains(mapStr(body, "message"), "上游路由") {
		t.Fatalf("body %v", body)
	}
	if e.host() != newHost || d.configured != newHost {
		t.Fatal("not changed")
	}
	// Everything goes to the new address: the old one may already be gone.
	if len(d.calls) != 2 || d.calls[0].method != "GET" || d.calls[0].host != newHost || d.calls[0].body != nil ||
		d.calls[1].method != "POST" || d.calls[1].host != newHost || !testjson.Equal(d.calls[1].body, map[string]any{"host": newHost}) {
		t.Fatalf("calls %+v", d.calls)
	}
	if got := parseBodyMap(t, e.beat("host", newHost))["confirmed_host"]; got != newHost {
		t.Fatalf("confirmed %v", got)
	}
	revision := e.revision()
	e.beat("host", newHost)
	e.beat("host", newHost)
	if e.revision() != revision || len(e.reached) != 0 {
		t.Fatal("touched again")
	}
}

func TestPinningTheAddressTheRecordAlreadyHasOnlyWritesTheDevice(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, "", true, 200)
	revision := e.revision()
	body := parseBodyMap(t, e.putAddress(oldHost))
	if body["success"] != true || body["written"] != true || body["changed"] != false || strings.Contains(mapStr(body, "message"), "上游路由") {
		t.Fatalf("body %v", body)
	}
	if d.configured != oldHost || e.revision() != revision {
		t.Fatal("record touched")
	}
}

func TestUnreachableOrForeignAddressChangesNothing(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost}, "", true, 200)
	r := e.putAddress(newHost).WantStatus(502)
	if !strings.Contains(mapStr(parseBodyMap(t, r), "message"), "连不上") {
		t.Fatal(r.Text())
	}
	d.addresses, d.status = []string{oldHost, newHost}, 401 // something else answers there
	r = e.putAddress(newHost).WantStatus(502)
	if !strings.Contains(mapStr(parseBodyMap(t, r), "message"), "不是这台 Agent") {
		t.Fatal(r.Text())
	}
	if e.host() != oldHost || d.configured != "" || !d.onlyGets() {
		t.Fatal("changed")
	}
}

func TestOnlyALanIPv4AddressIsAccepted(t *testing.T) {
	for _, value := range []any{"172.17.0.2", "127.0.0.1", "dns.example", "", nil, 12} {
		e := newAddressEnv(t)
		d := newDevice(t, []string{oldHost, newHost}, "", true, 200)
		e.putAddress(value).WantStatus(400)
		if len(d.calls) != 0 || e.host() != oldHost {
			t.Fatalf("%v accepted", value)
		}
	}
}

func TestContainerWithAgentIPIsSentToItsEnvironment(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, oldHost, false, 200)
	data := e.getAddress()
	if data["mode"] != "env" || !strings.Contains(mapStr(data, "message"), "AGENT_IP") || len(mapList(data, "addresses")) != 0 {
		t.Fatalf("data %v", data)
	}
	r := e.putAddress(newHost).WantStatus(409)
	if !strings.Contains(mapStr(parseBodyMap(t, r), "message"), "AGENT_IP") || e.host() != oldHost || !d.onlyGets() {
		t.Fatal(r.Text())
	}
}

func TestContainerWithoutAgentIPChangesOnlyTheRecord(t *testing.T) {
	e := newAddressEnv(t)
	if _, err := e.manager().EditAgent(e.s.Context(), e.agentID, agentEditsForTest(t, map[string]any{"deployment_method": "docker"})); err != nil {
		t.Fatal(err)
	}
	d := newDevice(t, []string{oldHost, newHost}, "", false, 200)
	if e.getAddress()["mode"] != "record" {
		t.Fatal("mode")
	}
	body := parseBodyMap(t, e.putAddress(newHost))
	if body["success"] != true || body["written"] != false || !strings.Contains(mapStr(body, "message"), "只修改了面板记录") ||
		e.host() != newHost || !d.onlyGets() {
		t.Fatalf("body %v", body)
	}
}

func TestOlderAgentIsVerifiedWithItsTokenAndOnlyTheRecordChanges(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, "", true, 404)
	data := e.getAddress()
	if data["mode"] != "record" || !strings.Contains(mapStr(data, "message"), "版本较旧") {
		t.Fatalf("data %v", data)
	}
	e.reachable = false
	e.putAddress(newHost).WantStatus(502)
	if e.host() != oldHost {
		t.Fatal("changed")
	}
	e.reachable = true
	body := parseBodyMap(t, e.putAddress(newHost))
	if body["success"] != true || body["written"] != false || e.host() != newHost {
		t.Fatalf("body %v", body)
	}
	if e.reached[len(e.reached)-1] != [2]string{e.agentToken, newHost} || !d.onlyGets() {
		t.Fatal("not verified with the token")
	}
}

func TestHighAvailabilityMemberCannotBeChangedHere(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, "", true, 200)
	e.setHA(true)
	data := e.getAddress()
	if data["mode"] != "ha" || !strings.Contains(mapStr(data, "message"), "停用 DNS 高可用") {
		t.Fatalf("data %v", data)
	}
	r := e.putAddress(newHost).WantStatus(409)
	if !strings.Contains(mapStr(parseBodyMap(t, r), "message"), "高可用") || e.host() != oldHost || len(d.calls) != 0 {
		t.Fatal(r.Text())
	}
}

func TestDialogStillOpensWhenTheCurrentAddressIsDead(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{newHost}, "", true, 200)
	data := e.getAddress()
	if data["mode"] != "unknown" || data["reachable"] != false || !strings.Contains(mapStr(data, "message"), oldHost) {
		t.Fatalf("data %v", data)
	}
	if parseBodyMap(t, e.putAddress(newHost))["success"] != true || e.host() != newHost || d.configured != newHost {
		t.Fatal("not changed")
	}
}

func TestManualChangeWithdrawsThePendingWarning(t *testing.T) {
	e := newAddressEnv(t)
	e.reachable = false
	e.beat("host", newHost)
	if len(e.notes(notifications.AgentAddressSource)) != 1 {
		t.Fatal("no warning")
	}
	newDevice(t, []string{oldHost, newHost}, "", true, 200)
	if parseBodyMap(t, e.putAddress(newHost))["success"] != true || len(e.notes(notifications.AgentAddressSource)) != 0 {
		t.Fatal("warning kept")
	}
}

func TestSameNamedRecordAtThatAddressBlocksTheChange(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, "", true, 200)
	e.register(newHost)
	e.putAddress(newHost).WantStatus(409)
	if len(d.calls) != 0 || e.host() != oldHost {
		t.Fatal("changed")
	}
}

func agentModelForTest(t testing.TB, value any) *model.Agent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func agentEditsForTest(t testing.TB, value any) agentcore.AgentEdits {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	edits, err := agentcore.DecodeAgentEdits(raw)
	if err != nil {
		t.Fatal(err)
	}
	return edits
}
