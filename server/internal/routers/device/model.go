// Package device defines vendor-neutral router types, errors, plan operation
// headers, static route indexing, DHCP DNS checks, and shared helpers. It
// performs no device I/O and contains no vendor-specific logic.
package device

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"configflow/internal/devhttp"
)

// ErrorKind tells RouterError's variants apart.
type ErrorKind int

const (
	KindRouter           ErrorKind = iota // a router request failed
	KindConflict                          // the device changed; a fresh preview is required
	KindRollback                          // a partial write could not be fully undone
	KindUncertainWrite                    // a write may have committed but its result is unknown
	KindMissingResource                   // the requested RouterOS resource is unavailable
	KindPingNotPermitted                  // RouterOS account lacks the test policy
	KindRejected                          // the device answered and refused the write: nothing changed
	KindInvalidResponse                   // the device response is malformed; retain failed probe evidence
)

// RouterError is a failure whose message is safe to show to users (use errors.As).
type RouterError struct {
	Kind    ErrorKind
	Message string
	// Undo, when set, reverts what the failed operation may still have
	// changed (a write whose reply was lost). The release records it like the
	// undo of a finished operation and runs it on rollback.
	Undo json.RawMessage
}

func (e *RouterError) Error() string { return e.Message }

// UndoOf is the undo a failed operation carries (nil when there is none).
func UndoOf(err error) json.RawMessage {
	if e, ok := errors.AsType[*RouterError](err); ok {
		return e.Undo
	}
	return nil
}

// Error constructs a KindRouter RouterError with message.
func Error(message string) error { return &RouterError{Message: message} }

// KindError constructs a RouterError with the given kind and message.
func KindError(kind ErrorKind, message string) error {
	return &RouterError{Kind: kind, Message: message}
}

// RollbackError constructs a KindRollback RouterError with message.
func RollbackError(message string) error { return KindError(KindRollback, message) }

// IsRouterError reports whether err wraps a *RouterError.
func IsRouterError(err error) bool {
	var target *RouterError
	return errors.As(err, &target)
}

// RouterErrorKind returns the ErrorKind of err if it wraps a *RouterError, or KindRouter otherwise.
func RouterErrorKind(err error) ErrorKind {
	var target *RouterError
	if errors.As(err, &target) {
		return target.Kind
	}
	return KindRouter
}

// ValidationError is an invalid router setting (HTTP 400 with the message).
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// Invalid constructs a *ValidationError with message.
func Invalid(message string) error { return &ValidationError{Message: message} }

// ErrNotPorted: a feature of a module not ported yet is needed (never a result).
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

// DeviceHTTP is the device transport seam. Production uses devhttp; tests use
// a loopback HTTP server and can inject a transport without touching globals.
type DeviceHTTP interface {
	Do(context.Context, string, string, any, time.Duration) (*devhttp.Response, error)
	Close()
}

// RouterInfo is what a probe reports.
type RouterInfo struct {
	Vendor       string   `json:"vendor"`
	Version      string   `json:"version"`
	Model        string   `json:"model"`
	Capabilities []string `json:"capabilities"`
}

// RouteEntry is a static route; Active/Distance are nil without runtime evidence.
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

// DhcpDnsEntry is one DHCP network's DNS setting. Python defaulted Enabled
// and DNSExplicit to true: drivers set both explicitly.
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

// ReportPart is a feature module's section (policy routing, DNS guard, IPv6):
// Statuses are added to the summary, InSync joins in_sync and RetireExtra are
// route ids that leave routes.extra (a migration owns them).
type ReportPart struct {
	Key         string
	Section     any
	Statuses    []string
	InSync      bool
	RetireExtra []string
}

// IsManaged reports a ConfigFlow-owned comment.
func IsManaged(comment string) bool {
	text := strings.ToLower(strings.TrimSpace(comment))
	return text == ManagedPrefix || strings.HasPrefix(text, ManagedPrefix+":") || strings.HasPrefix(text, ManagedPrefix+"-")
}

// ManagedComment tags a comment as managed, keeping the user's text: "tg" -> "cf-tg".
func ManagedComment(comment string) string {
	text := strings.TrimSpace(comment)
	if text == "" || IsManaged(text) {
		return cmp.Or(text, ManagedPrefix)
	}
	return ManagedPrefix + "-" + text
}

// PolicyRouteOK: enabled, any protocol, every source except the gateway, next hop the gateway, highest priority.
func PolicyRouteOK(e *PolicyRouteEntry, gateway string) bool {
	return e.Enabled && gateway != "" && e.Gateway == gateway && len(e.Src) == 1 && e.Src[0] == gateway &&
		e.SrcInvert && !e.DstInvert && e.Protocol == "any" && e.Priority != nil && *e.Priority == PolicyPriority
}
