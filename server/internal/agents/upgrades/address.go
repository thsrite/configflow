package upgrades

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

// Constants describe address-follow checks and the existing operator guidance.
const (
	VerifyTimeout = 4
	HASteps       = "它是 DNS 高可用成员，面板没有修改记录：两台设备的主备配置和路由器的放行名单里写的都是旧地址。" +
		"请依次：1. 停用 DNS 高可用；2. 等这台设备的地址在这里自动更新（约两分钟内）；" +
		"3. 重新配对并开启高可用；4. 重新预览并发布上游路由。"
	HAEdit = "这台设备是 DNS 高可用成员，不能在这里修改地址：两台设备的主备配置和路由器的放行名单里写的都是当前地址。" +
		"请依次：1. 停用 DNS 高可用；2. 修改地址；3. 重新配对并开启高可用；4. 重新预览并发布上游路由。"
	EnvEdit    = "这个容器的地址由环境变量 AGENT_IP 指定，容器每次启动都会用它。请修改 AGENT_IP 后重建容器，面板记录会自动更新。"
	RouterHint = "如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。"
	// DefaultRetrySeconds is the production address-follow throttle.
	DefaultRetrySeconds = 120
)

// RetrySeconds controls address-follow retries; tests restore it after use.
var RetrySeconds float64 = DefaultRetrySeconds
var addressLog = logx.Get("backend.agents.address_follow")

type attempt struct {
	host   string
	member bool
	at     time.Time
}

var (
	addressMu   sync.Mutex
	attempts    = map[string]attempt{}
	reported    = map[string]bool{}
	startThread = func(ctx context.Context, name string, fn func(context.Context)) error {
		return lifecycle.SpawnerFrom(ctx).Spawn(ctx, name, fn)
	}
	reachesOverride func(*model.Agent, string) bool
	askOverride     func(*model.Agent, string, string, []byte) (int, model.Fields)
)

// ResetAddressState forgets retry throttles and pending-notice memory.
func ResetAddressState() {
	addressMu.Lock()
	defer addressMu.Unlock()
	attempts = map[string]attempt{}
	reported = map[string]bool{}
}

// Usable returns a reachable LAN IPv4 candidate, excluding local-only and Docker bridge addresses.
func Usable(text string) string {
	ip, err := netip.ParseAddr(text)
	if err != nil || !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return ""
	}
	b := ip.As4()
	if b[0] >= 240 || b[0] == 172 && b[1] >= 17 && b[1] <= 31 {
		return ""
	}
	return ip.String()
}

// FollowModel verifies a reported host in an originating-runtime background job.
func FollowModel(ctx context.Context, m *agents.Manager, a *model.Agent, reportedHost string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	id, ok := originalString(a.MemberJSON("id"))
	if !ok || id == "" {
		return agents.ErrAgentEndpoint
	}
	host := Usable(reportedHost)
	old, _ := originalString(a.MemberJSON("host"))
	if host == "" || host == old {
		addressMu.Lock()
		pending := reported[id]
		addressMu.Unlock()
		if host != "" && pending {
			resolve(ctx, m, id)
		}
		return nil
	}
	member, err := memberHA(a)
	if err != nil {
		return err
	}
	at := lifecycle.ClockFrom(ctx).Now()
	addressMu.Lock()
	if err := ctx.Err(); err != nil {
		addressMu.Unlock()
		return err
	}
	prev, exists := attempts[id]
	if exists && prev.host == host && prev.member == member && at.Sub(prev.at).Seconds() < RetrySeconds {
		addressMu.Unlock()
		return nil
	}
	attempts[id] = attempt{host, member, at}
	addressMu.Unlock()
	// Keep both repository values and the originating runtime; Background would
	// allow an old generation to start a new job after shutdown.
	base := repo.WithRepository(ctx, m.Repository())
	if err := startThread(base, "agent-address", func(worker context.Context) { followInBackground(worker, m, id, host) }); err != nil {
		addressMu.Lock()
		if attempts[id] == (attempt{host, member, at}) {
			delete(attempts, id)
		}
		addressMu.Unlock()
		return fmt.Errorf("start Agent address check: %w", err)
	}
	return nil
}

// ConfirmedModel returns a usable host only when the original stored host agrees.
func ConfirmedModel(a *model.Agent, reportedHost string) string {
	host := Usable(reportedHost)
	old, ok := originalString(a.MemberJSON("host"))
	if ok && host != "" && host == old {
		return host
	}
	return ""
}
func addressRequest(ctx context.Context, m *agents.Manager, a *model.Agent, host, method, path string, body []byte) (*agents.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	token, ok := originalString(a.MemberJSON("token"))
	port, present, err := a.MemberJSON("port")
	if !ok || !present || err != nil {
		return nil, agents.ErrAgentEndpoint
	}
	header := http.Header{"Authorization": {"Bearer " + token}}
	if len(body) > 0 {
		header.Set("Content-Type", "application/json")
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: "http://" + host + ":" + rawText(port) + path, Header: header, Body: body, Timeout: agents.Seconds(VerifyTimeout), Latin1Headers: true})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return response, err
}
func reachesModel(ctx context.Context, m *agents.Manager, a *model.Agent, host string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if reachesOverride != nil {
		return reachesOverride(a, host), nil
	}
	r, err := addressRequest(ctx, m, a, host, "GET", "/api/capabilities", nil)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if errors.Is(err, agents.ErrHeaderEncoding) || errors.Is(err, agents.ErrAgentEndpoint) {
			return false, err
		}
		return false, nil
	}
	return r.StatusCode == 200, nil
}
func askModel(ctx context.Context, m *agents.Manager, a *model.Agent, host, method string, body []byte) (int, model.Fields, error) {
	if err := ctx.Err(); err != nil {
		return 0, model.Fields{}, err
	}
	if askOverride != nil {
		status, data := askOverride(a, host, method, body)
		return status, data, nil
	}
	r, err := addressRequest(ctx, m, a, host, method, "/api/agent-address", body)
	if err != nil {
		if ctx.Err() != nil {
			return 0, model.Fields{}, ctx.Err()
		}
		if errors.Is(err, agents.ErrHeaderEncoding) || errors.Is(err, agents.ErrAgentEndpoint) {
			return 0, model.Fields{}, err
		}
		return 0, model.Fields{}, nil
	}
	var fields model.Fields
	if jsondecode.Unmarshal(responseData(r), &fields) != nil {
		fields = model.Fields{}
	}
	return r.StatusCode, fields, nil
}
func followInBackground(ctx context.Context, m *agents.Manager, id, host string) {
	defer func() {
		if recover() != nil {
			addressLog.Error("Could not follow the reported address of Agent " + id)
		}
	}()
	if err := followModel(ctx, m, id, host); err != nil && ctx.Err() == nil {
		addressLog.Exception("Could not follow the reported address of Agent "+id, err)
	}
}
func sameNamedAtModel(ctx context.Context, m *agents.Manager, a *model.Agent, id, host string) (bool, error) {
	name, valid := originalString(a.MemberJSON("name"))
	if !valid {
		return false, agents.ErrAgentEndpoint
	}
	rows, err := m.ReadAgents(ctx, false, nil)
	if err != nil {
		return false, err
	}
	for i := range rows {
		otherID, idOK := originalString(rows[i].MemberJSON("id"))
		otherName, nOK := originalString(rows[i].MemberJSON("name"))
		otherHost, hOK := originalString(rows[i].MemberJSON("host"))
		if idOK && nOK && hOK && otherID != id && otherName == name && otherHost == host {
			return true, nil
		}
	}
	return false, nil
}
func changeRecord(ctx context.Context, m *agents.Manager, id, host string) (bool, error) {
	data, err := json.Marshal(struct {
		Host string `json:"host"`
	}{host})
	if err != nil {
		return false, err
	}
	edits, err := agents.DecodeAgentEdits(data)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return m.EditAgent(ctx, id, edits)
}
func followModel(ctx context.Context, m *agents.Manager, id, host string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return err
	}
	if a == nil {
		return nil
	}
	old, _ := originalString(a.MemberJSON("host"))
	if old == host {
		return nil
	}
	label := agentLabel(a)
	member, err := memberHA(a)
	if err != nil {
		return err
	}
	if member {
		addressLog.Warning(fmt.Sprintf(msgFollowHA, label, old, host))
		report(ctx, m, a, host, msgHAUnchanged, HASteps)
		return nil
	}
	if taken, e := sameNamedAtModel(ctx, m, a, id, host); e != nil {
		return e
	} else if taken {
		addressLog.Warning(fmt.Sprintf(msgFollowDuplicate, label, host))
		return nil
	}
	ok, err := reachesModel(ctx, m, a, host)
	if err != nil {
		return err
	}
	if !ok {
		addressLog.Warning(fmt.Sprintf(msgFollowUnreachable, label, old, host))
		port, _, _ := a.MemberJSON("port")
		report(ctx, m, a, host, msgUnreachableUnchanged, fmt.Sprintf(msgFollowHelp, host, rawText(port), old))
		return nil
	}
	updated, err := changeRecord(ctx, m, id, host)
	var conflict *agents.DeploymentConflict
	if errors.As(err, &conflict) {
		addressLog.Info(fmt.Sprintf(msgFollowConflict, label, host, conflict))
		addressMu.Lock()
		delete(attempts, id)
		addressMu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}
	addressLog.Info(fmt.Sprintf(msgFollowUpdated, label, old, host))
	resolve(ctx, m, id)
	if err := ctx.Err(); err != nil {
		return err
	}
	notifications.Safely(func() error {
		return notifications.SaveNotice(ctx, m.Repository(), "notice:agent-address:"+id, label+msgAddressNoticeTitle, fmt.Sprintf(msgAddressNotice, old, host), notifications.Destination("/agents", msgViewAgent, map[string]string{"agent": id}))
	})
	return nil
}
func report(ctx context.Context, m *agents.Manager, a *model.Agent, host, label, message string) {
	if ctx.Err() != nil {
		return
	}
	addressMu.Lock()
	if ctx.Err() != nil {
		addressMu.Unlock()
		return
	}
	reported[a.ID] = true
	addressMu.Unlock()
	notifications.Safely(func() error { return notifications.SaveAgentAddress(ctx, m.Repository(), a, host, label, message) })
}
func resolve(ctx context.Context, m *agents.Manager, id string) {
	if ctx.Err() != nil {
		return
	}
	addressMu.Lock()
	if ctx.Err() != nil {
		addressMu.Unlock()
		return
	}
	delete(reported, id)
	addressMu.Unlock()
	notifications.Safely(func() error { return notifications.ResolveAgentAddress(ctx, m.Repository(), id) })
}
func addressMode(a *model.Agent, status int, info model.Fields) (string, string) {
	writable, _ := info.Get("writable")
	if status == 200 && string(writable) == "true" {
		return "agent", ""
	}
	configured, _ := info.Get("configured")
	if status == 200 && hasContent(configured) {
		return "env", EnvEdit
	}
	method, _ := originalString(a.MemberJSON("deployment_method"))
	if method == "docker" {
		return "record", msgDockerAddress
	}
	if status == 404 {
		return "record", msgLegacyAddress
	}
	return "record", ""
}

// AddressDescription is the existing address dialog response.
type AddressDescription struct {
	Host       string          `json:"host"`
	Port       json.RawMessage `json:"port"`
	Mode       string          `json:"mode"`
	Message    string          `json:"message"`
	Addresses  []string        `json:"addresses"`
	Configured string          `json:"configured"`
	Reachable  bool            `json:"reachable"`
}

// DescribeModel reads device address capabilities without modifying the record.
func DescribeModel(ctx context.Context, m *agents.Manager, a *model.Agent) (*AddressDescription, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	host, _ := originalString(a.MemberJSON("host"))
	port, _, _ := a.MemberJSON("port")
	if len(port) == 0 {
		port = []byte("null")
	}
	result := &AddressDescription{Host: host, Port: port, Addresses: []string{}}
	member, err := memberHA(a)
	if err != nil {
		return nil, err
	}
	if member {
		result.Mode = "ha"
		result.Message = HAEdit
		return result, nil
	}
	status, info, err := askModel(ctx, m, a, host, "GET", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 && status != 404 {
		result.Mode = "unknown"
		result.Message = fmt.Sprintf(msgCurrentUnreachable, host, rawText(port))
		return result, nil
	}
	result.Mode, result.Message = addressMode(a, status, info)
	result.Reachable = true
	if raw, ok := info.Get("addresses"); ok && result.Mode == "agent" {
		var listed []string
		if jsondecode.Unmarshal(raw, &listed) == nil {
			for _, address := range listed {
				if Usable(address) != "" {
					result.Addresses = append(result.Addresses, address)
				}
			}
		}
	}
	if raw, ok := info.Get("configured"); ok && hasContent(raw) {
		result.Configured = rawText(raw)
	}
	return result, nil
}

// AddressResult is the response of a verified address change.
type AddressResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Host    string `json:"host,omitempty"`
	Written *bool  `json:"written,omitempty"`
	Changed *bool  `json:"changed,omitempty"`
}

func refusalModel(message string) *AddressResult { return &AddressResult{Message: message} }

// ChangeModel verifies a host, updates writable device settings, then edits the registry.
func ChangeModel(ctx context.Context, m *agents.Manager, a *model.Agent, value string) (*AddressResult, int, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	host := Usable(strings.TrimSpace(value))
	if host == "" {
		return refusalModel(msgEnterIPv4), 400, nil
	}
	id, ok := originalString(a.MemberJSON("id"))
	if !ok || id == "" {
		return nil, 0, agents.ErrAgentEndpoint
	}
	old, _ := originalString(a.MemberJSON("host"))
	label := agentLabel(a)
	member, err := memberHA(a)
	if err != nil {
		return nil, 0, err
	}
	if member {
		return refusalModel(HAEdit), 409, nil
	}
	if taken, e := sameNamedAtModel(ctx, m, a, id, host); e != nil {
		return nil, 0, e
	} else if taken {
		return refusalModel(msgDuplicateAddress + host), 409, nil
	}
	status, info, err := askModel(ctx, m, a, host, "GET", nil)
	if err != nil {
		return nil, 0, err
	}
	if status == 404 {
		ok, e := reachesModel(ctx, m, a, host)
		if e != nil {
			return nil, 0, e
		}
		if !ok {
			status = 0
		}
	}
	raw, _, _ := a.MemberJSON("port")
	port := rawText(raw)
	if status == 0 {
		return refusalModel(fmt.Sprintf(msgNewUnreachable, host, port)), 502, nil
	}
	if status != 200 && status != 404 {
		return refusalModel(fmt.Sprintf(msgWrongAgent, host, port)), 502, nil
	}
	chosen, message := addressMode(a, status, info)
	if chosen == "env" {
		return refusalModel(message), 409, nil
	}
	written := false
	if chosen == "agent" {
		body, e := jsonresp.EncodeDevice(struct {
			Host string `json:"host"`
		}{host})
		if e != nil {
			return nil, 0, e
		}
		code, reply, e := askModel(ctx, m, a, host, "POST", body)
		if e != nil {
			return nil, 0, e
		}
		if code != 200 {
			text := ""
			if raw, ok := reply.Get("message"); ok && hasContent(raw) {
				text = rawText(raw)
			}
			if text == "" {
				answer := msgNoReply
				if code != 0 {
					answer = strconv.Itoa(code)
				}
				text = fmt.Sprintf(msgAddressRejected, answer)
			}
			status := 502
			if code == 400 {
				status = 400
			}
			return refusalModel(text), status, nil
		}
		written = true
	}
	if host != old {
		updated, e := changeRecord(ctx, m, id, host)
		var conflict *agents.DeploymentConflict
		if errors.As(e, &conflict) {
			return refusalModel(conflict.Error()), 409, nil
		}
		if e != nil {
			return nil, 0, e
		}
		if !updated {
			return refusalModel("Agent not found"), 404, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	addressMu.Lock()
	delete(attempts, id)
	addressMu.Unlock()
	resolve(ctx, m, id)
	note := ""
	if written {
		note = msgWrittenNote
	}
	addressLog.Info(fmt.Sprintf(msgManualAddress, label, old, host, note))
	text := msgAddressChanged + host
	if written {
		text += msgDeviceWritten
	} else {
		text += msgRecordOnly
	}
	if host != old {
		text += RouterHint
	}
	return &AddressResult{Success: true, Host: host, Written: &written, Changed: new(host != old), Message: text}, 200, nil
}
