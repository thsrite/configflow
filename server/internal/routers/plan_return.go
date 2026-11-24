package routers

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/netipx"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ReturnAllocation is selected by the Agent, never by the release planner.
type ReturnAllocation struct {
	Version  int    `json:"version"`
	Reused   bool   `json:"reused"`
	Table    int    `json:"table"`
	Priority int    `json:"priority"`
	Mark     string `json:"mark"`
}

// ReturnSpec is the negotiated return-routing protocol payload.
type ReturnSpec struct {
	RouterIP      string    `json:"router_ip"`
	Table         int       `json:"table"`
	Priority      int       `json:"priority"`
	Mark          string    `json:"mark"`
	SafetyVersion int       `json:"safety_version"`
	ProxyDst      *[]string `json:"proxy_dst,omitempty"`
}

// ReturnManaged describes the Agent's owned return-routing resources.
type ReturnManaged struct {
	Active       bool            `json:"active"`
	TablePresent bool            `json:"table_present"`
	Spec         json.RawMessage `json:"spec"`
	rest         map[string]json.RawMessage
}

// ReturnCleanup is an owned return spec eligible for last-stage cleanup.
type ReturnCleanup struct {
	Spec       json.RawMessage `json:"spec"`
	SharedWith []string        `json:"shared_with"`
	Complete   bool            `json:"complete"`
	Safety     json.RawMessage `json:"safety"`
}

// ReturnStatus preserves the Agent response, including unknown evidence fields.
type ReturnStatus struct {
	Status      string            `json:"status,omitempty"`
	Message     string            `json:"message,omitempty"`
	Cleanup     *ReturnCleanup    `json:"cleanup,omitempty"`
	Managed     *ReturnManaged    `json:"managed,omitempty"`
	Safety      json.RawMessage   `json:"safety,omitempty"`
	FWMarkRules []json.RawMessage `json:"fwmark_rules,omitempty"`
	rest        map[string]json.RawMessage
}

// UnmarshalJSON retains the complete body while exposing the protocol views.
func (s *ReturnStatus) UnmarshalJSON(b []byte) error {
	type plain ReturnStatus
	var p plain
	if err := jsondecode.Unmarshal(b, &p); err != nil {
		return err
	}
	*s = ReturnStatus(p)
	if err := json.Unmarshal(b, &s.rest); err != nil {
		return err
	}
	delete(s.rest, "success")
	return nil
}

// MarshalJSON preserves presence and unknown Agent fields.
func (s ReturnStatus) MarshalJSON() ([]byte, error) {
	type plain ReturnStatus
	b, err := json.Marshal(plain(s))
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err = json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	for _, key := range []string{"managed", "fwmark_rules", "cleanup"} {
		if _, present := s.rest[key]; !present {
			continue
		}
		switch key {
		case "managed":
			out[key], err = json.Marshal(s.Managed)
		case "fwmark_rules":
			out[key], err = json.Marshal(s.FWMarkRules)
		case "cleanup":
			out[key], err = json.Marshal(s.Cleanup)
		}
		if err != nil {
			return nil, err
		}
	}
	for k, v := range s.rest {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	delete(out, "success")
	return json.Marshal(out)
}

// ReturnAgent is the owned Agent return-routing transport.
type ReturnAgent interface {
	Status(context.Context) (*ReturnStatus, error)
	Apply(context.Context, json.RawMessage) error
	Remove(context.Context, bool) error
}

var returnMarkPattern = regexp.MustCompile(`^0[xX][0-9a-fA-F]{1,8}$`)

func returnSafety(s *ReturnStatus) map[string]json.RawMessage {
	var v map[string]json.RawMessage
	if s != nil {
		_ = json.Unmarshal(s.Safety, &v)
	}
	return v
}

// ReturnAllocationOf validates the Agent's complete, strict allocation contract.
func ReturnAllocationOf(s *ReturnStatus) *ReturnAllocation {
	b := returnSafety(s)["allocation"]
	var fields map[string]json.RawMessage
	if json.Unmarshal(b, &fields) != nil {
		return nil
	}
	for _, k := range []string{"version", "reused", "table", "priority", "mark"} {
		if len(fields[k]) == 0 || bytes.Equal(fields[k], []byte("null")) {
			return nil
		}
	}
	var a ReturnAllocation
	if jsondecode.Unmarshal(b, &a) != nil || a.Version != 1 || a.Table < 1 || a.Table > 252 || a.Priority < 1 || a.Priority > 32765 || !returnMarkPattern.MatchString(a.Mark) {
		return nil
	}
	n, err := strconv.ParseUint(a.Mark[2:], 16, 32)
	if err != nil || n == 0 || n&(n-1) != 0 {
		return nil
	}
	a.Mark = fmt.Sprintf("0x%x", n)
	return &a
}

// ReturnProtocol is the newest explicitly advertised spec protocol.
func ReturnProtocol(s *ReturnStatus) int {
	var values []json.RawMessage
	_ = json.Unmarshal(returnSafety(s)["protocols"], &values)
	for _, v := range values {
		if len(v) == 0 || v[0] == '"' {
			continue
		}
		var n json.Number
		if json.Unmarshal(v, &n) == nil {
			f, e := n.Float64()
			if e == nil && f == 2 {
				return 2
			}
		}
	}
	return 1
}

// ReturnSafetyBlockers validates ownership safety even for cleanup. Cleanup can
// ignore occupancy blockers but still requires a valid ownership-aware protocol.
func ReturnSafetyBlockers(s *ReturnStatus, requireSafe bool) []string {
	v := returnSafety(s)
	var version int
	if json.Unmarshal(v["version"], &version) != nil || version != 1 {
		return []string{messageReturnUpgrade}
	}
	var ok bool
	var blockers []string
	if bytes.Equal(v["ok"], []byte("null")) || json.Unmarshal(v["ok"], &ok) != nil || len(v["blockers"]) == 0 || bytes.Equal(v["blockers"], []byte("null")) || json.Unmarshal(v["blockers"], &blockers) != nil {
		return []string{messageReturnIncomplete}
	}
	for _, b := range blockers {
		if strings.TrimSpace(b) == "" {
			return []string{messageReturnIncomplete}
		}
	}
	if requireSafe && (!ok || len(blockers) > 0) {
		if len(blockers) > 0 {
			return blockers
		}
		return []string{messageReturnUnsafe}
	}
	if requireSafe && ReturnAllocationOf(s) == nil {
		return []string{messageReturnNoAllocation}
	}
	return []string{}
}

// RouterIP returns the literal IPv4 endpoint host, without DNS resolution.
func RouterIP(endpoint string) string {
	u, e := url.Parse(endpoint)
	if e != nil {
		return ""
	}
	a, e := netip.ParseAddr(u.Hostname())
	if e != nil || !a.Is4() {
		return ""
	}
	return a.String()
}

// returnUpstreamProblem refuses a management address outside the Mihomo
// gateway's directly connected network (lab L5-D2): the Agent uses it as the
// return next hop and rejects it at apply time when it is not on its LAN.
// Without connected-route evidence (iKuai, unread routes, gateway not on a
// connected network) the Agent's apply check stays the only guard.
func returnUpstreamProblem(upstream, gateway string, ctx *ros.NatContext) string {
	router, ok := netipx.ParseAddr4(upstream)
	if !ok {
		return ""
	}
	next, ok := netipx.ParseAddr4(gateway)
	if !ok || ctx == nil || !ros.RowsOK(ctx.Routes) {
		return ""
	}
	var network netip.Prefix
	for _, row := range ctx.Routes {
		if table := row.Text("routing-table"); table != "" && table != "main" {
			continue
		}
		if !row.Enabled() || !row.Flag("connect") || !row.Flag("active") {
			continue
		}
		n, ok := netipx.ParseNetwork4(row.Text("dst-address"), false)
		if ok && n.Contains(next) && (!network.IsValid() || n.Bits() > network.Bits()) {
			network = n
		}
	}
	if !network.IsValid() || network.Contains(router) {
		return ""
	}
	return fmt.Sprintf(messageReturnUpstreamNetwork, router, network, network)
}
func returnScope(spec *ReturnSpec) string {
	if spec.SafetyVersion == 2 {
		return messageReturnScopeRouter
	}
	n := 0
	if spec.ProxyDst != nil {
		n = len(*spec.ProxyDst)
	}
	return fmt.Sprintf(messageReturnScopeDest, n)
}
func sameReturnSpec(raw json.RawMessage, want *ReturnSpec) bool {
	var actual ReturnSpec
	if jsondecode.Unmarshal(raw, &actual) != nil {
		return false
	}
	return actual.RouterIP == want.RouterIP && actual.Table == want.Table && actual.Priority == want.Priority && actual.Mark == want.Mark && actual.SafetyVersion == want.SafetyVersion && sameStrings(returnDest(&actual), returnDest(want))
}
func returnDest(s *ReturnSpec) []string {
	if s.ProxyDst == nil {
		return nil
	}
	return *s.ProxyDst
}
func sameStrings(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}
func checkReturnAllocation(status *ReturnStatus, spec *ReturnSpec) error {
	if blockers := ReturnSafetyBlockers(status, true); len(blockers) > 0 {
		return device.Error(strings.Join(blockers, "；"))
	}
	a := ReturnAllocationOf(status)
	if a.Table != spec.Table || a.Priority != spec.Priority || a.Mark != spec.Mark {
		return device.Error(messageReturnChanged)
	}
	if spec.SafetyVersion > ReturnProtocol(status) {
		return device.Error(messageReturnProtocolChanged)
	}
	return nil
}
func returnRuleMatches(raw json.RawMessage, want *ReturnSpec) bool {
	var r struct {
		Priority json.RawMessage `json:"priority"`
		Table    json.RawMessage `json:"table"`
		Mark     string          `json:"mark"`
	}
	if jsondecode.Unmarshal(raw, &r) != nil {
		return false
	}
	number := func(v json.RawMessage) (uint64, error) {
		var s string
		if len(v) > 0 && v[0] == '"' {
			if e := json.Unmarshal(v, &s); e != nil {
				return 0, e
			}
		} else {
			s = string(v)
		}
		base := 10
		if strings.HasPrefix(strings.ToLower(s), "0x") {
			base = 16
			s = s[2:]
		}
		return strconv.ParseUint(s, base, 32)
	}
	p, e := number(r.Priority)
	if e != nil || int(p) != want.Priority {
		return false
	}
	t, e := number(r.Table)
	if e != nil || int(t) != want.Table {
		return false
	}
	parts := strings.Split(r.Mark, "/")
	if len(parts) != 2 {
		return false
	}
	mark, e := number(json.RawMessage(strconv.Quote(parts[0])))
	if e != nil {
		return false
	}
	mask, e := number(json.RawMessage(strconv.Quote(parts[1])))
	if e != nil {
		return false
	}
	expected, e := number(json.RawMessage(strconv.Quote(want.Mark)))
	return e == nil && mark == mask && mask == expected
}

// UnmarshalJSON retains unknown managed-resource evidence and key presence.
func (m *ReturnManaged) UnmarshalJSON(b []byte) error {
	type plain ReturnManaged
	var p plain
	if err := jsondecode.Unmarshal(b, &p); err != nil {
		return err
	}
	*m = ReturnManaged(p)
	return json.Unmarshal(b, &m.rest)
}

// MarshalJSON retains unknown managed-resource evidence without inventing flags.
func (m ReturnManaged) MarshalJSON() ([]byte, error) {
	type plain ReturnManaged
	b, err := json.Marshal(plain(m))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	if m.rest != nil {
		for _, key := range []string{"active", "table_present"} {
			if _, ok := m.rest[key]; !ok && string(fields[key]) == "false" {
				delete(fields, key)
			}
		}
		for k, v := range m.rest {
			if _, ok := fields[k]; !ok && k != "active" && k != "table_present" {
				fields[k] = v
			}
		}
	}
	return json.Marshal(fields)
}
