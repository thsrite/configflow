// Package routers is the upstream-router integration (backend/routers):
// router records, desired state, drift report, release. Modules land one at
// a time (server/parity/PROGRESS-routers.md); R01 holds the shared model,
// router records (store), DHCP DNS and the desired-state computation.
//
// Model fields keep the JSON names Python used (reports and fingerprints
// carry them). For optional lists a nil slice is "not reported" (Python
// None) and an empty non-nil slice is "reported empty".
package routers

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// ErrorKind tells RouterError's variants apart.
type ErrorKind int

const (
	KindRouter   ErrorKind = iota // a router request failed
	KindConflict                  // the device changed; a fresh preview is required
	KindRollback                  // a partial write could not be fully undone
)

// RouterError is a failure whose message is safe to show to users.
type RouterError struct {
	Kind    ErrorKind
	Message string
}

func (e *RouterError) Error() string { return e.Message }

func routerError(message string) error { return &RouterError{Message: message} }

// AsRouterError reports whether err is (wraps) a RouterError.
func AsRouterError(err error) (*RouterError, bool) {
	var r *RouterError
	ok := errors.As(err, &r)
	return r, ok
}

// ValidationError is an invalid router setting (HTTP 400 with the message).
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

// ErrNotPorted is returned where a feature of a module that is not ported
// yet is needed; it never stands for a result.
var ErrNotPorted = errors.New("routers: depends on a module that is not ported yet")

// ManagedPrefix marks generated objects ("cf", "cf-…", "cf:…").
const ManagedPrefix = "cf"

// Policy route comments and priority.
const (
	PolicyAutoComment   = ManagedPrefix + "-mihomo"
	PolicyManualComment = ManagedPrefix + "-mihomo-manual"
	RestoredComment     = "mihomo-manual" // hand-made CIDRs restored from PolicyManualComment
	PolicyPriority      = 0               // highest 端口分流 priority
)

// RouterInfo is what a probe reports.
type RouterInfo struct {
	Vendor       string   `json:"vendor"`
	Version      string   `json:"version"`
	Model        string   `json:"model"`
	Capabilities []string `json:"capabilities"`
}

// RouteEntry is a static route. Active and Distance are nil when the
// driver has no runtime evidence. Table is "main" unless the device says
// otherwise.
type RouteEntry struct {
	ID       string `json:"id"`
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment"`
	Table    string `json:"table"`
	Active   *bool  `json:"active"`
	Distance *int64 `json:"distance"`
}

// NewRoute is a main-table route without runtime evidence.
func NewRoute(id, dst, gateway string, enabled bool, comment string) *RouteEntry {
	return &RouteEntry{ID: id, Dst: dst, Gateway: gateway, Enabled: enabled, Comment: comment, Table: "main"}
}

// PolicyRouteEntry is a source-aware forwarding rule (iKuai 端口分流 with a
// next hop); it can exclude the gateway itself as a source.
type PolicyRouteEntry struct {
	ID        string   `json:"id"`
	Dst       []string `json:"dst"`
	Gateway   string   `json:"gateway"` // empty when the rule targets a WAN line
	Enabled   bool     `json:"enabled"`
	Comment   string   `json:"comment"`
	Src       []string `json:"src"`
	SrcInvert bool     `json:"src_invert"`
	DstInvert bool     `json:"dst_invert"`
	Protocol  string   `json:"protocol"`
	Priority  *int64   `json:"priority"`
}

// NatBypassEntry exempts traffic from source NAT.
type NatBypassEntry struct {
	ID         string          `json:"id"`
	Dst        []string        `json:"dst"` // empty means any
	Src        []string        `json:"src"`
	Enabled    bool            `json:"enabled"`
	Comment    string          `json:"comment"`
	Attributes json.RawMessage `json:"attributes"` // vendor matchers
}

// DhcpDnsEntry is one DHCP network's DNS setting.
type DhcpDnsEntry struct {
	ID         string          `json:"id"`
	Network    string          `json:"network"`
	DNS        []string        `json:"dns"`
	Enabled    bool            `json:"enabled"`
	Attributes json.RawMessage `json:"attributes"`
	Blockers   []string        `json:"blockers"`
	// False when a vendor's empty DNS slot could inherit an unlisted server.
	DNSExplicit bool `json:"dns_explicit"`
}

// DhcpDnsState is the DHCP DNS readback.
type DhcpDnsState struct {
	Entries  []*DhcpDnsEntry `json:"entries"`
	Evidence json.RawMessage `json:"evidence"`
}

// RouterState is one readback of the device. Later modules add the fields
// they own (NAT context, address lists, policy/IPv6/DNS-guard plans, iKuai
// groups).
type RouterState struct {
	Routes      []*RouteEntry       `json:"routes"`
	NatBypass   []*NatBypassEntry   `json:"nat_bypass"`
	AddressSets map[string][]string `json:"address_sets"`
	DhcpDNS     *DhcpDnsState       `json:"dhcp_dns"`
	// nil: proxy CIDRs go by static routes; a list: by policy rules.
	PolicyRoutes []*PolicyRouteEntry `json:"policy_routes"`
	// Our policy rules being withdrawn (RetirePolicyRoutes).
	RetiredPolicyRoutes []*PolicyRouteEntry `json:"retired_policy_routes"`
	// Routes the router actually forwards with (iKuai 当前路由表); nil when unknown.
	ActiveRoutes []*RouteEntry `json:"active_routes"`
}

// Driver talks to one router.
type Driver interface {
	Vendor() string
	SupportsWrite() bool // false: stages are exported as a script
	Probe() (*RouterInfo, error)
	ReadState() (*RouterState, error)
	ReadDHCPDNS() (*DhcpDnsState, error) // only when a DNS Agent is selected
	// Apply applies one release operation and returns the one that undoes it.
	Apply(op json.RawMessage) (json.RawMessage, error)
	RenderScript(ops []json.RawMessage) (string, error)
	Close()
}

// IsManaged reports a ConfigFlow-owned comment.
func IsManaged(comment string) bool {
	text := strings.ToLower(strings.TrimSpace(comment))
	return text == ManagedPrefix || strings.HasPrefix(text, ManagedPrefix+":") || strings.HasPrefix(text, ManagedPrefix+"-")
}

// ManagedComment tags a comment as managed, keeping the user's text:
// "tg" -> "cf-tg".
func ManagedComment(comment string) string {
	text := strings.TrimSpace(comment)
	switch {
	case IsManaged(text):
		return text
	case text != "":
		return ManagedPrefix + "-" + text
	}
	return ManagedPrefix
}

// PolicyRouteOK: enabled, any protocol, every source except the gateway,
// next hop the gateway, highest priority.
func PolicyRouteOK(e *PolicyRouteEntry, gateway string) bool {
	return e.Enabled && gateway != "" && e.Gateway == gateway && len(e.Src) == 1 && e.Src[0] == gateway &&
		e.SrcInvert && !e.DstInvert && e.Protocol == "any" && e.Priority != nil && *e.Priority == PolicyPriority
}

// PolicyRoute is the first policy route carrying comment.
func PolicyRoute(state *RouterState, comment string) *PolicyRouteEntry {
	for _, e := range state.PolicyRoutes {
		if e.Comment == comment {
			return e
		}
	}
	return nil
}

// RetirePolicyRoutes routes proxy CIDRs by static routes and withdraws our
// policy rules (they need managed return routing).
func RetirePolicyRoutes(state *RouterState) {
	if state.PolicyRoutes == nil {
		return
	}
	retired := []*PolicyRouteEntry{}
	for _, e := range state.PolicyRoutes {
		if e.Comment == PolicyAutoComment || e.Comment == PolicyManualComment {
			retired = append(retired, e)
		}
	}
	state.RetiredPolicyRoutes, state.PolicyRoutes = retired, nil
}

// StaleRoutes are active routes to the gateway that no configured static
// route explains (iKuai keeps kernel routes after edits).
func StaleRoutes(state *RouterState, gateway string) []*RouteEntry {
	out := []*RouteEntry{}
	if gateway == "" || state.ActiveRoutes == nil {
		return out
	}
	configured := map[string]bool{}
	for _, r := range state.Routes {
		if r.Enabled && r.Gateway == gateway {
			configured[r.Dst] = true
		}
	}
	for _, r := range state.ActiveRoutes {
		if r.Gateway == gateway && !configured[r.Dst] {
			out = append(out, r)
		}
	}
	return out
}

// StaleRouteWarning explains StaleRoutes to the user.
func StaleRouteWarning(routes []*RouteEntry, gateway string) string {
	dsts := make([]string, len(routes))
	for i, r := range routes {
		dsts[i] = r.Dst
	}
	return "路由器当前路由表中有 " + strconv.Itoa(len(routes)) + " 条指向 " + gateway + " 的路由不在静态路由配置里" +
		"（" + strings.Join(dsts, ", ") + "）：这是修改或删除静态路由后残留的内核路由，" +
		"优先于分流规则，会让 " + gateway + " 自身的流量回环。界面和接口都无法删除，需要重启路由器清除。"
}
