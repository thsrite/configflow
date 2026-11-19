package agents_test

// Port of backend/test_agent_address_follow.py: a renumbered Agent is
// followed only after it is reached at the new address; HA members are only
// reported. The background check runs inline (upgrades.RunInlineForTest) as
// the Python test replaces threading.Thread; _reaches and _ask are replaced
// through the upgrades test seams. The two cases that exercise _reaches and
// _ask themselves are in internal/agents/upgrades (address_test.go).

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
)

const oldHost, newHost = "192.0.2.2", "192.0.2.11"

type addressEnv struct {
	t         *testing.T
	s         *httpxtest.Server
	agent     *jsonx.Object
	reachable bool
	reached   [][2]string
}

func newAddressEnv(t *testing.T) *addressEnv {
	t.Helper()
	s := newServer(t)
	upgrades.Wire()
	upgrades.ResetAddressState()
	t.Cleanup(upgrades.ResetAddressState)
	t.Cleanup(upgrades.RunInlineForTest())
	e := &addressEnv{t: t, s: s, reachable: true}
	t.Cleanup(legacyUpgradeSetReachesForTest(func(record *jsonx.Object, host string) bool {
		e.reached = append(e.reached, [2]string{record.GetString("token"), host})
		return e.reachable
	}))
	e.agent = e.register(oldHost)
	return e
}

func (e *addressEnv) manager() *agents.Manager {
	m, err := agents.Get(e.s.Context())
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *addressEnv) register(host string) *jsonx.Object {
	e.t.Helper()
	agent, err := e.manager().RegisterAgent(e.s.Context(), jsonx.Obj("name", "dns", "host", host, "port", 8082, "service_type", "oxidns"), nil, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return agent
}

func (e *addressEnv) beat(pairs ...any) *httpxtest.Result {
	e.t.Helper()
	body := jsonx.Obj("version", "1.8.60-go")
	body.Update(jsonx.Obj(pairs...))
	return e.s.Post("/api/agents/"+e.agent.GetString("id")+"/heartbeat", httpxtest.JSON(body), httpxtest.Bearer(e.agent.GetString("token")))
}

func (e *addressEnv) host() any {
	e.t.Helper()
	agent, err := e.manager().GetAgentByID(e.s.Context(), e.agent.GetString("id"), true)
	if err != nil {
		e.t.Fatal(err)
	}
	return agent.Value("host")
}

func (e *addressEnv) revision() string { return jsonx.Str(system(e.t, e.s).Value("_revision")) }

// notes are the pending notification items of source ("" = all).
func (e *addressEnv) notes(source string) []*jsonx.Object {
	e.t.Helper()
	var out []*jsonx.Object
	err := notifications.Transaction(e.s.DataDir, func(items *jsonx.Object) error {
		for _, value := range items.Values() {
			item := value.(*jsonx.Object)
			if item.Value("state") == "pending" && (source == "" || item.Value("source") == source) {
				out = append(out, item.DeepCopy())
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
	_, err := e.s.Repo.UpdateSystemTransaction(e.s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		for _, item := range system.GetList("agents") {
			agent := item.(*jsonx.Object)
			if agent.Value("id") == e.agent.Value("id") {
				if enabled {
					agent.Set("oxidns_ha", jsonx.Obj("enabled", true, "vip", "192.0.2.53"))
				} else {
					agent.Delete("oxidns_ha")
				}
			}
		}
		return nil, nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func TestReplyAnnouncesTheFieldAndAnUnchangedOrAbsentAddressDoesNothing(t *testing.T) {
	e := newAddressEnv(t)
	if reply := e.beat().WantStatus(200).Object(); reply.Value("reads_host") != true {
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
	if e.host() != newHost || len(e.reached) != 1 || e.reached[0] != [2]string{e.agent.GetString("token"), newHost} {
		t.Fatalf("host %v reached %v", e.host(), e.reached)
	}
	notes := e.notes(notifications.NoticeSource)
	if len(notes) != 1 {
		t.Fatalf("notes %v", notes)
	}
	message := notes[0].GetString("message")
	if !strings.Contains(message, oldHost) || !strings.Contains(message, newHost) || !strings.Contains(message, "上游路由") ||
		!jsonx.Equal(notes[0].GetObject("target").Value("query"), jsonx.Obj("agent", e.agent.Value("id"))) {
		t.Fatalf("notice %s", jsonx.Repr(notes[0]))
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
	if len(notes) != 1 || !strings.Contains(notes[0].GetString("title"), newHost) ||
		!strings.Contains(notes[0].GetString("status_label"), "连不上") || !strings.Contains(notes[0].GetString("message"), "自动更新") {
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
	if len(notes) != 1 || !strings.Contains(notes[0].GetString("status_label"), "高可用") {
		t.Fatalf("notes %v", notes)
	}
	for _, step := range []string{"停用 DNS 高可用", "重新配对", "发布上游路由"} {
		if !strings.Contains(notes[0].GetString("message"), step) {
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
	if e.beat("host", newHost).Object().Has("confirmed_host") || e.host() != newHost {
		t.Fatal("first reply")
	}
	if got := e.beat("host", newHost).Object().Value("confirmed_host"); got != newHost {
		t.Fatalf("confirmed %v", got)
	}
	if e.beat().Object().Has("confirmed_host") {
		t.Fatal("confirmed without a report")
	}
}

func TestAnAddressThePanelDidNotAdoptIsNeverConfirmed(t *testing.T) {
	e := newAddressEnv(t)
	e.reachable = false
	e.beat("host", newHost)
	if e.beat("host", newHost).Object().Has("confirmed_host") {
		t.Fatal("confirmed")
	}
}

// device is the Agent's /api/agent-address as seen from the panel.
type device struct {
	addresses  []string
	configured string
	writable   bool
	status     int
	calls      []string
}

func newDevice(t *testing.T, addresses []string, configured string, writable bool, status int) *device {
	d := &device{addresses: addresses, configured: configured, writable: writable, status: status}
	t.Cleanup(legacyUpgradeSetAskForTest(d.ask))
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

func (d *device) ask(_ *jsonx.Object, host, method string, body *jsonx.Object) (int, *jsonx.Object) {
	call := method + " " + host
	if body != nil {
		call += " " + jsonx.Repr(body)
	}
	d.calls = append(d.calls, call)
	if !d.has(host) {
		return 0, jsonx.NewObject()
	}
	if d.status != 200 {
		return d.status, jsonx.NewObject()
	}
	if method == "GET" {
		addresses := []any{}
		for _, a := range d.addresses {
			addresses = append(addresses, a)
		}
		return 200, jsonx.Obj("success", true, "configured", d.configured, "addresses", addresses, "writable", d.writable)
	}
	if !d.writable {
		return 409, jsonx.Obj("success", false, "message", "container")
	}
	requested := body.GetString("host")
	if !d.has(requested) {
		return 400, jsonx.Obj("success", false, "message", "这台设备没有地址 "+requested)
	}
	d.configured = requested
	return 200, jsonx.Obj("success", true, "host", requested)
}

func (d *device) onlyGets() bool {
	for _, call := range d.calls {
		if !strings.HasPrefix(call, "GET ") {
			return false
		}
	}
	return true
}

func (e *addressEnv) getAddress() *jsonx.Object {
	return e.s.Get("/api/agents/" + e.agent.GetString("id") + "/address").WantStatus(200).Object().GetObject("data")
}

func (e *addressEnv) putAddress(host any) *httpxtest.Result {
	return e.s.Put("/api/agents/"+e.agent.GetString("id")+"/address", httpxtest.JSON(jsonx.Obj("host", host)))
}

func TestDialogListsTheDevicesOwnAddresses(t *testing.T) {
	e := newAddressEnv(t)
	newDevice(t, []string{oldHost, newHost}, oldHost, true, 200)
	want := jsonx.Obj("host", oldHost, "port", 8082, "mode", "agent", "message", "", "addresses", []any{oldHost, newHost},
		"configured", oldHost, "reachable", true)
	if got := e.getAddress(); !jsonx.Equal(got, want) {
		t.Fatalf("got %s", jsonx.Repr(got))
	}
}

func TestManualChangeWritesTheDeviceFirstThenTheRecord(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, oldHost, true, 200)
	body := e.putAddress(" " + newHost + " ").WantStatus(200).Object()
	if body.Value("host") != newHost || body.Value("written") != true || !strings.Contains(body.GetString("message"), "上游路由") {
		t.Fatalf("body %v", body)
	}
	if e.host() != newHost || d.configured != newHost {
		t.Fatal("not changed")
	}
	// Everything goes to the new address: the old one may already be gone.
	if strings.Join(d.calls, "|") != "GET "+newHost+"|POST "+newHost+" {'host': '"+newHost+"'}" {
		t.Fatalf("calls %v", d.calls)
	}
	if got := e.beat("host", newHost).Object().Value("confirmed_host"); got != newHost {
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
	body := e.putAddress(oldHost).Object()
	if body.Value("success") != true || body.Value("written") != true || body.Value("changed") != false || strings.Contains(body.GetString("message"), "上游路由") {
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
	if !strings.Contains(r.Object().GetString("message"), "连不上") {
		t.Fatal(r.Text())
	}
	d.addresses, d.status = []string{oldHost, newHost}, 401 // something else answers there
	r = e.putAddress(newHost).WantStatus(502)
	if !strings.Contains(r.Object().GetString("message"), "不是这台 Agent") {
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
	if data.Value("mode") != "env" || !strings.Contains(data.GetString("message"), "AGENT_IP") || len(data.GetList("addresses")) != 0 {
		t.Fatalf("data %v", data)
	}
	r := e.putAddress(newHost).WantStatus(409)
	if !strings.Contains(r.Object().GetString("message"), "AGENT_IP") || e.host() != oldHost || !d.onlyGets() {
		t.Fatal(r.Text())
	}
}

func TestContainerWithoutAgentIPChangesOnlyTheRecord(t *testing.T) {
	e := newAddressEnv(t)
	if _, err := e.manager().UpdateAgent(e.s.Context(), e.agent.GetString("id"), jsonx.Obj("deployment_method", "docker")); err != nil {
		t.Fatal(err)
	}
	d := newDevice(t, []string{oldHost, newHost}, "", false, 200)
	if e.getAddress().Value("mode") != "record" {
		t.Fatal("mode")
	}
	body := e.putAddress(newHost).Object()
	if body.Value("success") != true || body.Value("written") != false || !strings.Contains(body.GetString("message"), "只修改了面板记录") ||
		e.host() != newHost || !d.onlyGets() {
		t.Fatalf("body %v", body)
	}
}

func TestOlderAgentIsVerifiedWithItsTokenAndOnlyTheRecordChanges(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, "", true, 404)
	data := e.getAddress()
	if data.Value("mode") != "record" || !strings.Contains(data.GetString("message"), "版本较旧") {
		t.Fatalf("data %v", data)
	}
	e.reachable = false
	e.putAddress(newHost).WantStatus(502)
	if e.host() != oldHost {
		t.Fatal("changed")
	}
	e.reachable = true
	body := e.putAddress(newHost).Object()
	if body.Value("success") != true || body.Value("written") != false || e.host() != newHost {
		t.Fatalf("body %v", body)
	}
	if e.reached[len(e.reached)-1] != [2]string{e.agent.GetString("token"), newHost} || !d.onlyGets() {
		t.Fatal("not verified with the token")
	}
}

func TestHighAvailabilityMemberCannotBeChangedHere(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{oldHost, newHost}, "", true, 200)
	e.setHA(true)
	data := e.getAddress()
	if data.Value("mode") != "ha" || !strings.Contains(data.GetString("message"), "停用 DNS 高可用") {
		t.Fatalf("data %v", data)
	}
	r := e.putAddress(newHost).WantStatus(409)
	if !strings.Contains(r.Object().GetString("message"), "高可用") || e.host() != oldHost || len(d.calls) != 0 {
		t.Fatal(r.Text())
	}
}

func TestDialogStillOpensWhenTheCurrentAddressIsDead(t *testing.T) {
	e := newAddressEnv(t)
	d := newDevice(t, []string{newHost}, "", true, 200)
	data := e.getAddress()
	if data.Value("mode") != "unknown" || data.Value("reachable") != false || !strings.Contains(data.GetString("message"), oldHost) {
		t.Fatalf("data %v", data)
	}
	if e.putAddress(newHost).Object().Value("success") != true || e.host() != newHost || d.configured != newHost {
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
	if e.putAddress(newHost).Object().Value("success") != true || len(e.notes(notifications.AgentAddressSource)) != 0 {
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
