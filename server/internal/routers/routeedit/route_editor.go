// Package routeedit implements conservative manual IPv4 static-route editing
// for RouterOS and iKuai devices.
package routeedit

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"

	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
	"configflow/internal/redact"
)

// EditableRoute is an ordinary route plus its complete stable vendor snapshot.
// Snapshots are open device records, never client-supplied release operations.
type EditableRoute struct {
	RouteFields
	Snapshot RouteSnapshot `json:"snapshot"`
}

// RouteSnapshot is the vendor's complete route record (a RouterOS or an
// iKuai row); it encodes as that record.
type RouteSnapshot struct {
	ros   ros.Row
	ikuai ikuai.Row
}

// MarshalJSON encodes the vendor record.
func (s RouteSnapshot) MarshalJSON() ([]byte, error) {
	if s.ikuai != nil {
		return json.Marshal(s.ikuai)
	}
	return json.Marshal(s.ros)
}

func (s RouteSnapshot) empty() bool { return s.ros == nil && s.ikuai == nil }

// RouteFields is the public route editor view, without device configuration.
type RouteFields struct {
	ID       string `json:"id"`
	CIDR     string `json:"cidr"`
	Gateway  string `json:"gateway"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment"`
	Table    string `json:"table"`
	Editable bool   `json:"editable"`
	Reason   string `json:"reason"`
	Managed  bool   `json:"managed"`
}

// PublicRoute is a route view bound to every stable configuration field.
type PublicRoute struct {
	RouteFields
	Revision string `json:"revision"`
}

// Public returns the snapshot-free view and its opaque revision.
func (r *EditableRoute) Public() *PublicRoute {
	if r == nil {
		return nil
	}
	return &PublicRoute{r.RouteFields, Digest(struct {
		Fields   RouteFields   `json:"fields"`
		Snapshot RouteSnapshot `json:"snapshot"`
	}{r.RouteFields, r.Snapshot})}
}

// RouteValues are the four editable fields of a manual IPv4 route.
type RouteValues struct {
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	Enabled bool   `json:"enabled"`
	Comment string `json:"comment"`
}

// RouteIntent is validated user intent; Fingerprint is deliberately not part of it.
type RouteIntent struct {
	Action   string       `json:"action"`
	ID       string       `json:"id,omitempty"`
	Revision string       `json:"revision,omitempty"`
	Values   *RouteValues `json:"values,omitempty"`
}

// RoutePlan binds a preview to a connection, intent and stable route snapshot.
type RoutePlan struct {
	Action      string        `json:"action"`
	ID          *string       `json:"id"`
	Values      *RouteValues  `json:"values"`
	Before      *PublicRoute  `json:"before"`
	After       *PlannedRoute `json:"after"`
	Summary     string        `json:"summary"`
	Warnings    []string      `json:"warnings"`
	Fingerprint string        `json:"fingerprint"`
	Empty       bool          `json:"empty"`
}

// PlannedRoute is the route a plan leaves: the edited fields over the current
// public view (keeping its editability and revision), or a bare main-table
// route for an addition.
type PlannedRoute struct {
	ID       string  `json:"id"`
	CIDR     string  `json:"cidr"`
	Gateway  string  `json:"gateway"`
	Enabled  bool    `json:"enabled"`
	Comment  string  `json:"comment"`
	Table    string  `json:"table"`
	Editable *bool   `json:"editable,omitempty"`
	Reason   *string `json:"reason,omitempty"`
	Managed  bool    `json:"managed"`
	Revision string  `json:"revision,omitempty"`
}

// RouteMutation is the vendor's proven result; a failure returns no success result.
type RouteMutation struct {
	Changed bool           `json:"changed"`
	Entry   *EditableRoute `json:"entry"`
}

// RouteResult is the public result of a manual edit.
type RouteResult struct {
	Changed bool         `json:"changed"`
	Entry   *PublicRoute `json:"entry"`
}

// RouteEditor is implemented by drivers supporting conservative manual edits.
type RouteEditor interface {
	ReadRoutes(context.Context) ([]*EditableRoute, error)
	MutateRoute(context.Context, string, *EditableRoute, *RouteValues) (*RouteMutation, error)
}

// ReadRoutes reads a complete list and refuses ambiguous identities. A nil
// editor (a driver without manual route editing) is refused.
func ReadRoutes(ctx context.Context, editor RouteEditor) ([]*EditableRoute, error) {
	if editor == nil {
		return nil, device.Error(messageEditUnsupported)
	}
	rows, err := editor.ReadRoutes(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row == nil || row.ID == "" || row.Snapshot.empty() || seen[row.ID] {
			return nil, device.Error(messageEditListIdentity)
		}
		seen[row.ID] = true
	}
	return rows, nil
}

func Digest(value any) string {
	data, _ := json.Marshal(value) // all callers contain JSON-safe records read from the device
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// NormalizeRouteRequest validates the strict HTTP intent shape before any I/O.
func NormalizeRouteRequest(data json.RawMessage) (RouteIntent, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return RouteIntent{}, device.Invalid(device.MessageEditFormat)
	}
	for key := range raw {
		if key != "action" && key != "id" && key != "revision" && key != "values" && key != "fingerprint" {
			return RouteIntent{}, device.Invalid(device.MessageEditFormat)
		}
	}
	var intent RouteIntent
	if json.Unmarshal(raw["action"], &intent.Action) != nil || (intent.Action != "add" && intent.Action != "update" && intent.Action != "delete") {
		return intent, device.Invalid(messageEditAction)
	}
	if intent.Action != "add" {
		if json.Unmarshal(raw["id"], &intent.ID) != nil || intent.ID == "" || utf8.RuneCountInString(intent.ID) > 128 || strings.ContainsFunc(intent.ID, func(r rune) bool { return r < 32 }) {
			return intent, device.Invalid(messageEditID)
		}
		if json.Unmarshal(raw["revision"], &intent.Revision) != nil || len(intent.Revision) != 64 || strings.ContainsFunc(intent.Revision, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) {
			return intent, device.Invalid(messageEditRevision)
		}
	} else {
		for _, key := range []string{"id", "revision"} {
			if value, exists := raw[key]; exists && strings.TrimSpace(string(value)) != "null" {
				text, valid := device.RequestString(value)
				if !valid || text != "" {
					return intent, device.Invalid(messageEditAddIdentity)
				}
			}
		}
	}
	if intent.Action == "delete" {
		if v, ok := raw["values"]; ok && strings.TrimSpace(string(v)) != "null" {
			return intent, device.Invalid(messageEditDeleteValues)
		}
		return intent, nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw["values"], &values) != nil || len(values) != 4 {
		return intent, device.Invalid(messageEditValues)
	}
	for _, key := range []string{"cidr", "gateway", "enabled", "comment"} {
		if _, ok := values[key]; !ok {
			return intent, device.Invalid(messageEditValues)
		}
	}
	v := new(RouteValues)
	if !routeJSONString(values["cidr"], &v.CIDR) || !routeJSONString(values["gateway"], &v.Gateway) {
		return intent, device.Invalid(messageEditAddressText)
	}
	network, ok := netipx.ParseNetwork4(strings.TrimSpace(v.CIDR), false)
	gateway, e := netip.ParseAddr(strings.TrimSpace(v.Gateway))
	if !ok || e != nil || !network.Addr().Is4() || !gateway.Is4() {
		return intent, device.Invalid(messageEditAddress)
	}
	if gateway.IsUnspecified() || gateway.IsMulticast() || gateway.IsLoopback() || gateway.String() == "255.255.255.255" {
		return intent, device.Invalid(messageEditGateway)
	}
	if string(values["enabled"]) != "true" && string(values["enabled"]) != "false" {
		return intent, device.Invalid(messageEditEnabled)
	}
	_ = json.Unmarshal(values["enabled"], &v.Enabled)
	if !routeJSONString(values["comment"], &v.Comment) || utf8.RuneCountInString(v.Comment) > 512 || strings.ContainsRune(v.Comment, 0) {
		return intent, device.Invalid(messageEditComment)
	}
	v.CIDR = network.Masked().String()
	v.Gateway = gateway.String()
	v.Comment = strings.TrimSpace(v.Comment)
	intent.Values = v
	return intent, nil
}

func routeJSONString(data []byte, target *string) bool {
	value, ok := device.RequestString(data)
	if ok {
		*target = value
	}
	return ok
}

// BuildRoutePlan builds a preview without device writes or repository mutations.
func BuildRoutePlan(router spec.Router, rows []*EditableRoute, data json.RawMessage) (*RoutePlan, error) {
	intent, err := NormalizeRouteRequest(data)
	if err != nil {
		return nil, err
	}
	var current *EditableRoute
	if intent.Action != "add" {
		current = editableByID(rows, intent.ID)
		if current == nil {
			return nil, device.KindError(device.KindConflict, messageEditMissing)
		}
		if current.Public().Revision != intent.Revision {
			return nil, device.KindError(device.KindConflict, messageEditDrift)
		}
		if !current.Editable {
			return nil, device.KindError(device.KindConflict, device.FirstNonEmpty(current.Reason, messageEditReadonly))
		}
	}
	v := intent.Values
	if v != nil {
		for _, row := range rows {
			if row.CIDR == v.CIDR && row.Table == "main" && (current == nil || row.ID != current.ID) {
				return nil, device.KindError(device.KindConflict, fmt.Sprintf(messageEditDuplicate, v.CIDR))
			}
		}
	}
	plan := &RoutePlan{Action: intent.Action, Values: v, Before: current.Public(), Warnings: []string{}}
	if current != nil {
		plan.ID = &intent.ID
	}
	if v != nil {
		after := &PlannedRoute{Table: "main"}
		if b := plan.Before; b != nil {
			editable, reason := b.Editable, b.Reason
			after = &PlannedRoute{ID: b.ID, Table: b.Table, Editable: &editable, Reason: &reason, Revision: b.Revision}
		}
		after.CIDR, after.Gateway, after.Enabled, after.Comment = v.CIDR, v.Gateway, v.Enabled, v.Comment
		after.Managed = device.IsManaged(v.Comment)
		plan.After = after
		plan.Empty = current != nil && routeValuesEqual(current, v)
	}
	cidr, gateway := "", ""
	if current != nil {
		cidr, gateway = current.CIDR, current.Gateway
	}
	if v != nil {
		cidr, gateway = v.CIDR, v.Gateway
	}
	label := map[string]string{"add": messageEditAdd, "update": messageEditUpdate, "delete": messageEditDelete}[intent.Action]
	plan.Summary = fmt.Sprintf(messageEditSummary, label, cidr, gateway)
	if (current != nil && current.Managed) || (v != nil && device.IsManaged(v.Comment)) {
		plan.Warnings = append(plan.Warnings, messageEditManaged)
	}
	type routeConnection struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		Endpoint  string `json:"endpoint"`
		Username  string `json:"username"`
		VerifyTLS bool   `json:"verify_tls"`
	}
	connection := routeConnection{router.ID, router.Type, router.Endpoint, router.Username, router.VerifyTLS}
	plan.Fingerprint = Digest(struct {
		Protocol   int             `json:"protocol"`
		Connection routeConnection `json:"connection"`
		Intent     RouteIntent     `json:"intent"`
		Before     *PublicRoute    `json:"before"`
		After      *PlannedRoute   `json:"after"`
	}{1, connection, intent, plan.Before, plan.After})
	return plan, nil
}

// ExecuteRoutePlan completes an edit and all its readbacks/compensation even if the request disconnects.
// Callers own the connection lock and rebuild the plan from fresh rows before calling.
func ExecuteRoutePlan(ctx context.Context, editor RouteEditor, rows []*EditableRoute, plan *RoutePlan) (*RouteResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, device.Error(device.MessageEditFormat)
	}
	if plan.Empty {
		return &RouteResult{Entry: plan.Before}, nil
	}
	if editor == nil {
		return nil, device.Error(messageEditUnsupported)
	}
	var expected *EditableRoute
	if plan.ID != nil {
		expected = editableByID(rows, *plan.ID)
	}
	result, err := editor.MutateRoute(ctx, plan.Action, expected, plan.Values)
	if err != nil {
		return nil, err
	}
	return &RouteResult{Changed: result.Changed, Entry: result.Entry.Public()}, nil
}

func editableByID(rows []*EditableRoute, id string) *EditableRoute {
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	return nil
}
func routeValuesEqual(row *EditableRoute, v *RouteValues) bool {
	return row.CIDR == v.CIDR && row.Gateway == v.Gateway && row.Enabled == v.Enabled && row.Comment == v.Comment
}
func routeIdentityEqual(a, b *EditableRoute) bool {
	return a != nil && b != nil && a.ID == b.ID && a.CIDR == b.CIDR && a.Gateway == b.Gateway && a.Enabled == b.Enabled && a.Comment == b.Comment && a.Table == b.Table && reflect.DeepEqual(a.Snapshot, b.Snapshot)
}

// A device may echo the password in its own error text. Keep the error kind but
// never expose that text, including encoded passwords from URL userinfo.
// Usernames are not secrets (the router form shows them) and often equal words
// of the fixed diagnostics, such as a RouterOS user named "api". The RouterOS
// transport already replaces a device detail echoing the password with
// redact.Redacted (safeDeviceText); such an error is hidden the same way.
func SafeError(err error, endpoint, password string) error {
	if err == nil {
		return nil
	}
	secrets := []string{password}
	if u, e := url.Parse(endpoint); e == nil && u.User != nil {
		if p, ok := u.User.Password(); ok {
			secrets = append(secrets, p)
		}
	}
	text := err.Error()
	if !redact.ContainsEncodedSecret(text, redact.NewSecrets(secrets)) && !strings.Contains(text, redact.Redacted) {
		return err
	}
	kind := device.KindRouter
	if e, ok := errors.AsType[*device.RouterError](err); ok {
		kind = e.Kind
	}
	message := MessageEditSecret
	if kind == device.KindRollback {
		message = messageEditSecretRollback
	}
	if kind == device.KindUncertainWrite {
		message = messageEditSecretUncertain
	}
	return device.KindError(kind, message)
}
