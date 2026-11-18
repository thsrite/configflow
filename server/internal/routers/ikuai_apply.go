package routers

// iKuai release operations (ikuai.py apply and helpers).

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"

	"configflow/internal/devhttp"
	"configflow/internal/netipx"
)

// ikuaiID is an operation's object id as text (plans send strings, records
// keep iKuai's numbers).
type ikuaiID string

func (id *ikuaiID) UnmarshalJSON(data []byte) error {
	var v any
	if err := devhttp.DecodeJSON(data, &v); err != nil {
		return err
	}
	*id = ikuaiID(replyText(v))
	return nil
}

func (id ikuaiID) String() string { return string(id) }

// ikuaiOp is one release operation or undo record for iKuai. Fields are
// those of the plan's operations and of the records Apply returns.
type ikuaiOp struct {
	Kind        string            `json:"kind"`
	ID          ikuaiID           `json:"id"`
	CIDR        string            `json:"cidr"`
	Gateway     string            `json:"gateway"`
	Comment     string            `json:"comment"`
	Src         []string          `json:"src"`
	Dst         []string          `json:"dst"`
	AddDst      []string          `json:"add_dst"`
	Groups      *[]string         `json:"groups"` // present: destinations are these IP groups
	KeepForeign bool              `json:"keep_foreign"`
	Record      Row               `json:"record"`
	Func        string            `json:"func"`
	Match       Row               `json:"match"`
	Confirmed   bool              `json:"confirmed"`
	Name        string            `json:"name"`
	Role        string            `json:"role"`
	Values      []string          `json:"values"`
	Notes       map[string]string `json:"notes"`
	Mark        bool              `json:"mark"`
	Entries     *[]GroupEntry     `json:"entries"`
	Previous    string            `json:"previous"`
	Expected    json.RawMessage   `json:"expected"` // a group digest, or the DHCP form
	DNS         json.RawMessage   `json:"dns"`
	Scope       json.RawMessage   `json:"scope"`
}

func (op ikuaiOp) digest() string {
	var s string
	_ = json.Unmarshal(op.Expected, &s)
	return s
}

// Undo records Apply returns (JSON keys as the release stores them).
type (
	ikuaiRecord struct {
		Kind   string `json:"kind"`
		Record Row    `json:"record"`
	}
	ikuaiIDRecord struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	ikuaiRouteAdd struct {
		Kind    string `json:"kind"`
		CIDR    string `json:"cidr"`
		Gateway string `json:"gateway"`
		Comment string `json:"comment"`
	}
	// ikuaiAddUndo finds an added object again by its unique tagname.
	ikuaiAddUndo struct {
		Kind      string `json:"kind"`
		Func      string `json:"func"`
		Match     Row    `json:"match"`
		Confirmed bool   `json:"confirmed"` // false: the add may never have happened
		CIDR      string `json:"cidr,omitempty"`
	}
	ikuaiGroupUndo struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		Expected  string `json:"expected"`
		Confirmed bool   `json:"confirmed"`
	}
	ikuaiGroupRestore struct {
		Kind     string       `json:"kind"`
		Name     string       `json:"name"`
		Role     string       `json:"role"`
		Values   []string     `json:"values"`
		Entries  []GroupEntry `json:"entries"`
		Previous string       `json:"previous"`
		Expected string       `json:"expected"`
	}
	ikuaiGroupReadd struct {
		Kind    string       `json:"kind"`
		Name    string       `json:"name"`
		Role    string       `json:"role"`
		Values  []string     `json:"values"`
		Entries []GroupEntry `json:"entries"`
	}
)

// noUndo is the record of an operation that needs no undo.
var noUndo = struct{}{}

// withUndo attaches the undo of a write that may have happened to err.
func withUndo(err error, undo any) error {
	e, ok := errors.AsType[*RouterError](err)
	if !ok {
		return err
	}
	data, mErr := json.Marshal(undo)
	if mErr != nil {
		return err
	}
	copied := *e
	copied.Undo = data
	return &copied
}

func absentKey(funcName, name string) string { return funcName + "\x00" + name }

// --- batch -------------------------------------------------------------------------

// ikuaiBatch is the state shared by the writes of one release stage or rollback.
type ikuaiBatch struct {
	rows           map[string]Row // static_rt by id; nil: read again before use
	stale          map[string]bool
	added          []*ikuaiAddUndo // new routes not read back yet
	addedSinceRead bool
	rollback       bool
	groups         []*IkuaiGroup // nil: read again before use
	groupsStale    map[string]bool
	groupsWritten  map[string]ikuaiWrittenGroup
}

type ikuaiWrittenGroup struct {
	expected string
	added    bool
}

// Batch runs fn as one release stage (rollback false) or one rollback. Deletes
// take their rows from one complete static_rt snapshot kept in step with the
// driver's own writes; edits still read the latest row, so a concurrent hand
// edit is never overwritten. New routes are proven by one read-back before the
// next other write (a stage may delete what they replace) and at the end; a
// rollback attempts every undo even after a failed re-add, so it proves its
// re-adds only at the end. Nothing carries over to the next batch. A nested
// call just runs fn. The final read-back runs even when ctx was cancelled.
func (d *Ikuai) Batch(ctx context.Context, rollback bool, fn func(context.Context) error) error {
	if d.batch != nil {
		return fn(ctx)
	}
	d.batch = &ikuaiBatch{stale: map[string]bool{}, rollback: rollback, groupsStale: map[string]bool{},
		groupsWritten: map[string]ikuaiWrittenGroup{}}
	defer func() { d.batch = nil }()
	if err := fn(ctx); err != nil {
		return err
	}
	return d.confirmAdded(context.WithoutCancel(ctx), d.batch.added)
}

func rowsByID(rows []Row) map[string]Row {
	out := make(map[string]Row, len(rows))
	for _, row := range rows {
		out[row.text("id")] = row
	}
	return out
}

// snapshot is the batch's static_rt rows (read again when fresh tags are
// needed after adds).
func (d *Ikuai) snapshot(ctx context.Context, freshTags bool) (map[string]Row, error) {
	b := d.batch
	if b.rows == nil || (freshTags && b.addedSinceRead) {
		rows, err := d.ListAll(ctx, "static_rt", "", true, "")
		if err != nil {
			return nil, err
		}
		b.rows, b.stale, b.addedSinceRead = rowsByID(rows), map[string]bool{}, false
	}
	return b.rows, nil
}

// matchesRow compares every ownership field, ignoring only static_rt's
// netmask display suffix. Other tables and all other fields remain exact.
func matchesRow(funcName string, row, match Row) bool {
	for key, value := range match {
		actual := row[key]
		if funcName == "static_rt" && key == "netmask" {
			if mask, ok := actual.(string); ok {
				actual = ikuaiRouteMask(mask)
			}
			if mask, ok := value.(string); ok {
				value = ikuaiRouteMask(mask)
			}
		}
		if !jsonEqual(actual, value) {
			return false
		}
	}
	return true
}

func taggedRows(rows []Row, tag any) []Row {
	var out []Row
	for _, row := range rows {
		if jsonEqual(row["tagname"], tag) {
			out = append(out, row)
		}
	}
	return out
}

// confirmAdded proves every route added in the batch exists, with one
// complete read-back that also serves as the next snapshot.
func (d *Ikuai) confirmAdded(ctx context.Context, added []*ikuaiAddUndo) error {
	if len(added) == 0 {
		return nil
	}
	listed, err := d.ListAll(ctx, "static_rt", "", true, "")
	if err != nil {
		return err
	}
	if d.batch != nil {
		d.batch.rows, d.batch.stale, d.batch.addedSinceRead = rowsByID(listed), map[string]bool{}, false
	}
	byName := map[string][]Row{}
	for _, row := range decodeRows(listed) {
		if tag, ok := row["tagname"].(string); ok {
			byName[tag] = append(byName[tag], row)
		}
	}
	var missing []string
	for _, undo := range added {
		tagged := byName[undo.Match.text("tagname")]
		if len(tagged) != 1 || !matchesRow("static_rt", tagged[0], undo.Match) {
			missing = append(missing, undo.CIDR)
			// A complete read without the (unique) name proves nothing was created.
			undo.Confirmed = len(tagged) > 0
			if !undo.Confirmed {
				d.absent[absentKey("static_rt", undo.Match.text("tagname"))] = true
			}
		}
	}
	if len(missing) > 0 {
		return routerError(messageIkuaiRoutesMissing + strings.Join(missing, "、"))
	}
	return nil
}

// --- writes ------------------------------------------------------------------------

// staticWrite writes static_rt and keeps the batch snapshot true to it.
func (d *Ikuai) staticWrite(ctx context.Context, action string, param Row, undo *ikuaiAddUndo) error {
	if _, err := d.Write(ctx, "static_rt", action, param, 0); err != nil {
		if isRejected(err) {
			return err
		}
		if d.batch != nil {
			d.batch.rows = nil
		}
		if undo != nil {
			// The reply was lost, not refused: the route may exist.
			lost := *undo
			lost.Confirmed = false
			return withUndo(err, lost)
		}
		return err
	}
	if d.batch == nil || d.batch.rows == nil {
		return nil
	}
	switch action {
	case "del":
		delete(d.batch.rows, param.text("id"))
	case "edit":
		d.batch.stale[param.text("id")] = true
	default:
		d.batch.addedSinceRead = true
	}
	return nil
}

// row is the current (decoded) row with id; static routes come from the
// batch snapshot unless fresh or edited since.
func (d *Ikuai) row(ctx context.Context, funcName, id string, fresh bool) (Row, error) {
	var row Row
	if funcName == "static_rt" && d.batch != nil {
		if fresh || d.batch.stale[id] {
			d.batch.rows = nil
		}
		rows, err := d.snapshot(ctx, false)
		if err != nil {
			return nil, err
		}
		row = rows[id]
	} else {
		rows, err := d.ListAll(ctx, funcName, "", true, "")
		if err != nil {
			return nil, err
		}
		if i := slices.IndexFunc(rows, func(r Row) bool { return r.text("id") == id }); i >= 0 {
			row = rows[i]
		}
	}
	if row == nil {
		return nil, routerError(fmt.Sprintf(messageIkuaiRowMissing, funcName, id))
	}
	return decodeIkuaiRow(row), nil
}

// add writes a named object and returns the undo that finds it again by its
// unique name: iKuai refuses a duplicate name (code 3001, 4.0.314), so the
// name identifies the new object even when its ID is never read back.
func (d *Ikuai) add(ctx context.Context, funcName string, payload, match Row, timeout float64) (*ikuaiAddUndo, error) {
	if tag, ok := payload["tagname"].(string); !ok || tag == "" {
		// Without a name the undo could not find the object again (or would
		// take a row without a name for it). Python failed here (KeyError).
		return nil, routerError(messageIkuaiOperationFormat)
	}
	undo := &ikuaiAddUndo{Kind: "add_undo", Func: funcName, Match: Row{"tagname": payload["tagname"]}, Confirmed: true}
	maps.Copy(undo.Match, match)
	delete(d.absent, absentKey(funcName, undo.Match.text("tagname")))
	if funcName == "static_rt" {
		return undo, d.staticWrite(ctx, "add", payload, undo)
	}
	if _, err := d.Write(ctx, funcName, "add", payload, timeout); err != nil {
		if isRejected(err) {
			return nil, err
		}
		lost := *undo
		lost.Confirmed = false
		return nil, withUndo(err, lost)
	}
	return undo, nil
}

// newID is the ID of an object just added; a failure carries its undo.
func (d *Ikuai) newID(ctx context.Context, funcName string, undo *ikuaiAddUndo) (string, error) {
	rows, err := d.ListAll(ctx, funcName, "", true, "")
	if err != nil {
		return "", withUndo(err, undo)
	}
	tagged := taggedRows(decodeRows(rows), undo.Match["tagname"])
	if len(tagged) == 1 && matchesRow(funcName, tagged[0], undo.Match) {
		return tagged[0].text("id"), nil
	}
	lost := *undo
	lost.Confirmed = len(tagged) > 0
	return "", withUndo(routerError(fmt.Sprintf(messageIkuaiAddUnconfirmed, funcName)), lost)
}

// revertAdd deletes an object this driver added, found by its name, only
// while every recorded field still matches.
func (d *Ikuai) revertAdd(ctx context.Context, op ikuaiOp) (any, error) {
	if tag, ok := op.Match["tagname"].(string); !ok || tag == "" {
		// Only the unique name proves ownership; never match rows without one.
		return nil, routerError(messageIkuaiOperationFormat)
	}
	var rows []Row
	if op.Func == "static_rt" && d.batch != nil {
		snapshot, err := d.snapshot(ctx, true)
		if err != nil {
			return nil, err
		}
		rows = slices.Collect(maps.Values(snapshot))
	} else {
		var err error
		if rows, err = d.ListAll(ctx, op.Func, "", true, ""); err != nil {
			return nil, err
		}
	}
	tag := op.Match.text("tagname")
	tagged := taggedRows(decodeRows(rows), op.Match["tagname"])
	if len(tagged) == 0 {
		if op.Confirmed && !d.absent[absentKey(op.Func, tag)] {
			return nil, routerError(fmt.Sprintf(messageIkuaiAddGone, op.Func, tag))
		}
		return noUndo, nil
	}
	if len(tagged) != 1 || !matchesRow(op.Func, tagged[0], op.Match) {
		return nil, routerError(fmt.Sprintf(messageIkuaiAddChanged, op.Func, tag))
	}
	param := Row{"id": tagged[0]["id"]}
	if op.Func == "static_rt" {
		return noUndo, d.staticWrite(ctx, "del", param, nil)
	}
	_, err := d.Write(ctx, op.Func, "del", param, 0)
	return noUndo, err
}

// sameObject refuses a restore when the row now holding the record's id is
// not the object the record was read from: its name changed, or iKuai gave
// the id of a deleted object to a new one. The before-image is then not
// written over it (Python wrote it to whatever row had the id).
func sameObject(funcName string, current, record Row) error {
	if !jsonEqual(current["tagname"], record["tagname"]) {
		return routerError(fmt.Sprintf(messageIkuaiRestoreReplaced, funcName, current.text("id")))
	}
	return nil
}

// edit writes a whole rule; a lost reply carries the idempotent undo (the full old row).
func (d *Ikuai) edit(ctx context.Context, funcName string, payload Row, undo any, timeout float64) error {
	_, err := d.Write(ctx, funcName, "edit", payload, timeout)
	if err != nil && !isRejected(err) {
		return withUndo(err, undo)
	}
	return err
}

// --- forms ---------------------------------------------------------------------------

func pick(row Row, fields []string) Row {
	out := Row{}
	for _, key := range fields {
		if value, ok := row[key]; ok {
			out[key] = value
		}
	}
	return out
}

func anyList(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// IkuaiRoutePayload is the complete static_rt edit form of a decoded row with
// changes applied; the netmask loses the display suffix iKuai adds.
func IkuaiRoutePayload(row, changes Row) Row {
	payload := pick(row, IkuaiRouteFields)
	maps.Copy(payload, changes)
	payload["netmask"] = ikuaiRouteMask(nonEmptyText(payload["netmask"]))
	return payload
}

func ikuaiNATPayload(row, changes Row) Row {
	payload := pick(row, ikuaiNATFields)
	for _, key := range []string{"src_addr", "dst_addr", "src_port", "dst_port"} {
		payload[key] = ikuaiAddressField(payload[key])
	}
	maps.Copy(payload, changes)
	return payload
}

func ikuaiTarget(dst []string, refs []any, useRefs bool) map[string]any {
	if useRefs {
		return map[string]any{"custom": []any{}, "object": refs}
	}
	return map[string]any{"custom": anyList(dst), "object": []any{}}
}

// ikuaiNewNAT is a NAT bypass rule; with refs its destination is those IP
// groups and dst only names it.
func ikuaiNewNAT(src, dst []string, comment string, refs []any, useRefs bool) Row {
	return ikuaiNATPayload(Row{}, Row{"enabled": "yes", "tagname": IkuaiTagname("nat", strings.Join(dst, ",")),
		"action": "filter", "protocol": "any", "iinterface": "any", "ointerface": "any",
		"src_addr": map[string]any{"custom": anyList(src), "object": []any{}}, "dst_addr": ikuaiTarget(dst, refs, useRefs),
		"nat_addr": "", "nat_port": "", "src_addr_inv": 0, "dst_addr_inv": 0, "comment": comment})
}

// ikuaiPolicyPayload is a complete 端口分流 form: a field left out would reset
// the rule's other settings.
func ikuaiPolicyPayload(row, changes Row) Row {
	payload := pick(row, ikuaiPolicyFields)
	for _, key := range []string{"src_addr", "dst_addr", "src_port", "dst_port", "time"} {
		payload[key] = ikuaiAddressField(payload[key])
	}
	maps.Copy(payload, changes)
	return payload
}

// ikuaiPolicyTarget makes a rule route dst to the gateway for every source
// but the gateway itself (with refs: those IP groups, nothing listed literally).
func ikuaiPolicyTarget(gateway string, dst []string, comment string, refs []any, useRefs bool) Row {
	return Row{"enabled": "yes", "type": ikuaiPolicyNexthop, "nexthop": gateway, "protocol": "any",
		"prio": PolicyPriority, "src_addr": map[string]any{"custom": []any{gateway}, "object": []any{}},
		"src_addr_inv": 1, "dst_addr": ikuaiTarget(dst, refs, useRefs), "dst_addr_inv": 0, "comment": comment}
}

func ikuaiNewPolicy(gateway string, dst []string, comment string, refs []any, useRefs bool) Row {
	changes := ikuaiPolicyTarget(gateway, dst, comment, refs, useRefs)
	maps.Copy(changes, Row{"tagname": IkuaiTagname("pr", comment), "interface": "", "iface_band": 0,
		"time": ikuaiPolicyAlways(), "mode": 0, "area_code": "", "dst_type": 0})
	return ikuaiPolicyPayload(Row{}, changes)
}

// ipv4Mask is the dotted netmask of an IPv4 prefix length.
func ipv4Mask(bits int) string { return net.IP(net.CIDRMask(bits, 32)).String() }

// --- apply ---------------------------------------------------------------------------

// Apply applies one release operation and returns the one that undoes it. A
// write that may have happened before failing returns an error carrying its
// undo (UndoOf). Once started it runs to the end even if ctx is cancelled
// (PORTING rule 4b); each device request has its own timeout.
func (d *Ikuai) Apply(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	ctx = context.WithoutCancel(ctx)
	var op ikuaiOp
	if err := devhttp.DecodeJSON(raw, &op); err != nil {
		return nil, routerError(messageIkuaiOperationFormat)
	}
	if b := d.batch; op.Kind != "route_add" && b != nil && len(b.added) > 0 && !b.rollback {
		// As if each add had been read back at once: nothing else (e.g. the
		// 端口分流 rule or covered route a new route replaces) is written
		// until the new routes exist.
		added := b.added
		b.added = nil
		if err := d.confirmAdded(ctx, added); err != nil {
			return nil, err
		}
	}
	result, err := d.apply(ctx, op)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("routers: iKuai undo record: %w", err)
	}
	return data, nil
}

func (d *Ikuai) apply(ctx context.Context, op ikuaiOp) (any, error) {
	switch {
	case op.Kind == "dhcp_dns_update" || op.Kind == "dhcp_dns_restore":
		return d.dhcpApply(ctx, op)
	case strings.HasPrefix(op.Kind, "ikuai_group_"):
		return d.applyGroup(ctx, op)
	case strings.HasPrefix(op.Kind, "policy_route_"):
		return d.applyPolicy(ctx, op)
	}
	switch op.Kind {
	case "add_undo":
		return d.revertAdd(ctx, op)
	case "route_add":
		return d.addRoute(ctx, op.CIDR, op.Gateway, op.Comment)
	case "route_delete":
		row, err := d.row(ctx, "static_rt", op.ID.String(), false)
		if err != nil {
			return nil, err
		}
		// Python deleted first and crashed on an unreadable network, leaving
		// no undo; the undo is now known before the delete.
		network, ok := ikuaiNetwork(row)
		if !ok {
			return nil, routerError(fmt.Sprintf(messageIkuaiRouteNetwork, op.ID))
		}
		if err := d.staticWrite(ctx, "del", Row{"id": row["id"]}, nil); err != nil {
			return nil, err
		}
		return ikuaiRouteAdd{Kind: "route_add", CIDR: network, Gateway: row.text("gateway"), Comment: row.text("comment")}, nil
	case "route_comment", "route_restore":
		// Edits send the whole row: read just before writing, never from the snapshot.
		id := op.ID.String()
		if op.Kind == "route_restore" {
			id = op.Record.text("id")
		}
		row, err := d.row(ctx, "static_rt", id, true)
		if err != nil {
			return nil, err
		}
		payload := IkuaiRoutePayload(op.Record, nil)
		if op.Kind == "route_comment" {
			payload = IkuaiRoutePayload(row, Row{"comment": op.Comment})
		} else if err := sameObject("static_rt", row, op.Record); err != nil {
			return nil, err
		}
		undo := ikuaiRecord{Kind: "route_restore", Record: row}
		if err := d.staticWrite(ctx, "edit", payload, nil); err != nil {
			if op.Kind == "route_comment" && !isRejected(err) {
				// Python attached no undo: a lost reply could leave the
				// user's route marked as ours.
				return nil, withUndo(err, undo)
			}
			return nil, err
		}
		return undo, nil
	case "nat_update":
		row, err := d.row(ctx, "nat_rule", op.ID.String(), false)
		if err != nil {
			return nil, err
		}
		dst := ikuaiAddressField(row["dst_addr"])
		refs, useRefs, err := d.targetRefs(ctx, op, dst)
		if err != nil {
			return nil, err
		}
		if useRefs {
			// Destinations move into our groups; the user's groups and the sources stay.
			dst = ikuaiTarget(nil, refs, true)
		} else {
			existing := dst["custom"].([]any)
			custom := slices.Clone(existing)
			for _, cidr := range op.AddDst {
				// Values are compared as strings: a decoded list may hold maps,
				// and == on those would panic.
				if !slices.ContainsFunc(existing, func(v any) bool { s, ok := v.(string); return ok && s == cidr }) {
					custom = append(custom, cidr)
				}
			}
			dst["custom"] = custom
		}
		undo := ikuaiRecord{Kind: "nat_restore", Record: row}
		if err := d.edit(ctx, "nat_rule", ikuaiNATPayload(row, Row{"dst_addr": dst, "comment": op.Comment}), undo, 0); err != nil {
			return nil, err
		}
		return undo, nil
	case "nat_restore":
		row, err := d.row(ctx, "nat_rule", op.Record.text("id"), false)
		if err != nil {
			return nil, err
		}
		if err := sameObject("nat_rule", row, op.Record); err != nil {
			return nil, err
		}
		record, err := d.rebind(ctx, op.Record)
		if err != nil {
			return nil, err
		}
		payload := ikuaiNATPayload(record, nil)
		undo := ikuaiRecord{Kind: "nat_restore", Record: row}
		if err := d.edit(ctx, "nat_rule", payload, undo, ikuaiRestoreTimeout(payload)); err != nil {
			return nil, err
		}
		return undo, nil
	case "nat_add":
		refs, useRefs, err := d.targetRefs(ctx, op, nil)
		if err != nil {
			return nil, err
		}
		payload := ikuaiNewNAT(op.Src, op.Dst, op.Comment, refs, useRefs)
		return d.addAndFind(ctx, "nat_rule", "nat_delete", payload, Row{"action": "filter", "comment": op.Comment}, 0)
	case "nat_readd":
		record, err := d.rebind(ctx, op.Record)
		if err != nil {
			return nil, err
		}
		payload := ikuaiNATPayload(record, nil)
		delete(payload, "id")
		return d.addAndFind(ctx, "nat_rule", "nat_delete", payload, Row{"action": "filter", "comment": payload["comment"]},
			ikuaiRestoreTimeout(payload))
	case "nat_delete":
		return d.deleteRule(ctx, "nat_rule", "nat_readd", op.ID.String())
	}
	return nil, routerError(fmt.Sprintf(messageUnsupportedOperation, op.Kind))
}

// addAndFind adds a rule and returns the record deleting it by its new ID.
func (d *Ikuai) addAndFind(ctx context.Context, funcName, undoKind string, payload, match Row, timeout float64) (any, error) {
	undo, err := d.add(ctx, funcName, payload, match, timeout)
	if err != nil {
		return nil, err
	}
	id, err := d.newID(ctx, funcName, undo)
	if err != nil {
		return nil, err
	}
	return ikuaiIDRecord{Kind: undoKind, ID: id}, nil
}

// deleteRule deletes a rule and returns the record re-adding it whole
// (group references included).
func (d *Ikuai) deleteRule(ctx context.Context, funcName, undoKind, id string) (any, error) {
	row, err := d.row(ctx, funcName, id, false)
	if err != nil {
		return nil, err
	}
	if _, err := d.Write(ctx, funcName, "del", Row{"id": row["id"]}, 0); err != nil {
		return nil, err
	}
	return ikuaiRecord{Kind: undoKind, Record: row}, nil
}

func (d *Ikuai) addRoute(ctx context.Context, cidr, gateway, comment string) (any, error) {
	network, ok := netipx.ParseNetwork4(cidr, true)
	if !ok {
		return nil, routerError(messageIkuaiOperationFormat)
	}
	address, mask := network.Addr().String(), ipv4Mask(network.Bits())
	undo, err := d.add(ctx, "static_rt", Row{"enabled": "yes", "tagname": IkuaiTagname("rt", cidr), "ip_type": "4",
		"interface": "auto", "dst_addr": address, "gateway": gateway, "netmask": mask, "prio": 1, "comment": comment},
		Row{"dst_addr": address, "netmask": mask, "gateway": gateway, "comment": comment}, 0)
	if err != nil {
		return nil, err
	}
	undo.CIDR = cidr
	if d.batch != nil {
		// Proven with every other new route before the next other write or at the end.
		d.batch.added = append(d.batch.added, undo)
		return undo, nil
	}
	if err := d.confirmAdded(ctx, []*ikuaiAddUndo{undo}); err != nil {
		return nil, withUndo(err, undo)
	}
	return undo, nil
}

func (d *Ikuai) applyPolicy(ctx context.Context, op ikuaiOp) (any, error) {
	switch op.Kind {
	case "policy_route_add":
		refs, useRefs, err := d.targetRefs(ctx, op, nil)
		if err != nil {
			return nil, err
		}
		payload := ikuaiNewPolicy(op.Gateway, op.Dst, op.Comment, refs, useRefs)
		return d.addAndFind(ctx, "stream_ipport", "policy_route_delete", payload, Row{"comment": payload["comment"]}, 0)
	case "policy_route_readd":
		record, err := d.rebind(ctx, op.Record)
		if err != nil {
			return nil, err
		}
		payload := ikuaiPolicyPayload(record, nil)
		delete(payload, "id")
		return d.addAndFind(ctx, "stream_ipport", "policy_route_delete", payload, Row{"comment": payload["comment"]},
			ikuaiRestoreTimeout(payload))
	case "policy_route_delete":
		return d.deleteRule(ctx, "stream_ipport", "policy_route_readd", op.ID.String())
	case "policy_route_update", "policy_route_restore":
		id := op.ID.String()
		if op.Kind == "policy_route_restore" {
			id = op.Record.text("id")
		}
		row, err := d.row(ctx, "stream_ipport", id, false)
		if err != nil {
			return nil, err
		}
		var payload Row
		var timeout float64
		if op.Kind == "policy_route_update" {
			refs, useRefs, err := d.targetRefs(ctx, op, row["dst_addr"])
			if err != nil {
				return nil, err
			}
			payload = ikuaiPolicyPayload(row, ikuaiPolicyTarget(op.Gateway, op.Dst, op.Comment, refs, useRefs))
		} else {
			if err := sameObject("stream_ipport", row, op.Record); err != nil {
				return nil, err
			}
			record, err := d.rebind(ctx, op.Record)
			if err != nil {
				return nil, err
			}
			payload = ikuaiPolicyPayload(record, nil)
			timeout = ikuaiRestoreTimeout(payload)
		}
		undo := ikuaiRecord{Kind: "policy_route_restore", Record: row}
		if err := d.edit(ctx, "stream_ipport", payload, undo, timeout); err != nil {
			return nil, err
		}
		return undo, nil
	}
	return nil, routerError(fmt.Sprintf(messageUnsupportedOperation, op.Kind))
}

// --- IP groups (route_object) ---------------------------------------------------------

// groupRows is every IP group; in a batch one complete read serves until a
// group it names changes.
func (d *Ikuai) groupRows(ctx context.Context, names []string, fresh bool) ([]*IkuaiGroup, error) {
	b := d.batch
	if b != nil && !fresh && b.groups != nil &&
		!slices.ContainsFunc(names, func(name string) bool { return b.groupsStale[name] }) {
		return b.groups, nil
	}
	groups, err := d.readGroups(ctx)
	if err != nil {
		return nil, err
	}
	if b != nil {
		b.groups, b.groupsStale = groups, map[string]bool{}
	}
	return groups, nil
}

func (d *Ikuai) namedGroups(ctx context.Context, name string, fresh bool) ([]*IkuaiGroup, error) {
	groups, err := d.groupRows(ctx, []string{name}, fresh)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(groups), func(g *IkuaiGroup) bool { return g.Name != name }), nil
}

func groupRole(op ikuaiOp) (string, error) {
	role, _, ok := GroupNameRole(op.Name)
	if _, known := GroupMarkers[op.Role]; !known || !ok || role != op.Role {
		return "", routerError(messageIkuaiGroupOp)
	}
	return role, nil
}

// ownedGroup is our group the op names; with unmarked also one with our name
// that lost its marker.
func (d *Ikuai) ownedGroup(ctx context.Context, op ikuaiOp, fresh, unmarked bool) (*IkuaiGroup, error) {
	role, err := groupRole(op)
	if err != nil {
		return nil, err
	}
	found, err := d.namedGroups(ctx, op.Name, fresh)
	if err != nil {
		return nil, err
	}
	if len(found) != 1 || (found[0].Role != role && !(unmarked && found[0].Role == "")) {
		return nil, routerError(fmt.Sprintf(messageIkuaiGroupNotOurs, op.Name))
	}
	if found[0].ID == nil {
		return nil, routerError(fmt.Sprintf(messageIkuaiGroupNoID, op.Name))
	}
	return found[0], nil
}

func objectsOf(field any) []Row {
	var out []Row
	for _, item := range ikuaiAddressField(field)["object"].([]any) {
		if ref, ok := item.(map[string]any); ok {
			out = append(out, ref)
		}
	}
	return out
}

// groupUsers are the NAT and 端口分流 rules referencing the group, read now.
func (d *Ikuai) groupUsers(ctx context.Context, group *IkuaiGroup) ([]string, error) {
	var users []string
	for _, part := range [][2]string{{"nat_rule", labelIkuaiNATRule}, {"stream_ipport", labelIkuaiPolicy}} {
		rows, err := d.ListAll(ctx, part[0], "", true, "")
		if err != nil {
			return nil, err
		}
		for _, row := range decodeRows(rows) {
			for _, field := range []string{"src_addr", "dst_addr"} {
				for _, ref := range objectsOf(row[field]) {
					if ResolveGroup([]*IkuaiGroup{group}, ref) == group {
						users = append(users, fmt.Sprintf(messageIkuaiGroupUser, part[1],
							cmp.Or(nonEmptyText(row["comment"]), row.text("id"))))
					}
				}
			}
		}
	}
	return uniqueStrings(users), nil
}

func (d *Ikuai) groupWrite(ctx context.Context, action, name string, param any, undo any) error {
	_, err := d.Write(ctx, "route_object", action, param, 0)
	if d.batch != nil {
		d.batch.groupsStale[name] = true
	}
	if err != nil && !isRejected(err) && undo != nil {
		// The reply was lost, not refused: the change may have been made.
		return withUndo(err, undo)
	}
	return err
}

// targetRefs are the destination references an op writes (useRefs false: the
// op lists destinations literally): the user's groups it keeps, then ours.
func (d *Ikuai) targetRefs(ctx context.Context, op ikuaiOp, field any) ([]any, bool, error) {
	if op.Groups == nil {
		return nil, false, nil
	}
	refs, err := d.cfRefs(ctx, *op.Groups)
	if err != nil {
		return nil, true, err
	}
	if op.KeepForeign {
		groups, err := d.groupRows(ctx, nil, false)
		if err != nil {
			return nil, true, err
		}
		var foreign []any
		for _, ref := range ForeignRefs(groups, objectsOf(field)) {
			foreign = append(foreign, map[string]any(ref))
		}
		refs = append(foreign, refs...)
	}
	return refs, true, nil
}

// cfRefs references our groups by their current gid, proving each group
// written in this batch holds what was written.
func (d *Ikuai) cfRefs(ctx context.Context, names []string) ([]any, error) {
	groups, err := d.groupRows(ctx, names, false)
	if err != nil {
		return nil, err
	}
	written := map[string]ikuaiWrittenGroup{}
	if d.batch != nil {
		written = d.batch.groupsWritten
	}
	refs, missing := []any{}, []string{}
	for _, name := range names {
		var found []*IkuaiGroup
		for _, g := range groups {
			if g.Name == name {
				found = append(found, g)
			}
		}
		role, _, ok := GroupNameRole(name)
		w, wrote := written[name]
		if len(found) != 1 || !ok || found[0].Role != role || found[0].GID == "" ||
			(wrote && GroupDigest(found[0].Values) != w.expected) {
			missing = append(missing, name)
			if wrote && w.added && len(found) == 0 {
				// A complete read without the (unique) name: the add never happened.
				d.absent[absentKey("route_object", name)] = true
			}
			continue
		}
		refs = append(refs, map[string]any{"type": 0, "gid": found[0].GID, "gp_name": name})
	}
	if len(missing) > 0 {
		return nil, routerError(fmt.Sprintf(messageIkuaiGroupUnreadable, strings.Join(missing, "、")))
	}
	return refs, nil
}

// rebind is a saved rule whose references to our groups point at their
// current gid: a rollback may delete a group and create it again under a new
// gid. The user's own references are written back exactly as they were.
func (d *Ikuai) rebind(ctx context.Context, record Row) (Row, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("routers: iKuai record: %w", err)
	}
	var copied Row
	if err := devhttp.DecodeJSON(data, &copied); err != nil || copied == nil {
		return nil, routerError(messageIkuaiOperationFormat)
	}
	var names []string
	var refs []map[string]any
	for _, field := range []string{"src_addr", "dst_addr"} {
		value, _ := copied[field].(map[string]any)
		objects, _ := value["object"].([]any)
		for _, item := range objects {
			ref, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if name, isString := ref["gp_name"].(string); isString {
				if _, _, managed := GroupNameRole(name); managed {
					names = append(names, name)
					refs = append(refs, ref)
				}
			}
		}
	}
	if len(names) == 0 {
		return copied, nil
	}
	groups, err := d.groupRows(ctx, names, false)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		name := ref["gp_name"].(string)
		if current := ResolveGroup(groups, ref); current != nil && current.Name == name {
			continue
		}
		// Gone, or its gid now belongs to another group (iKuai may reuse a freed ID).
		var found []*IkuaiGroup
		for _, g := range groups {
			if g.Name == name && g.Role != "" {
				found = append(found, g)
			}
		}
		if len(found) != 1 {
			// Python kept a gid that now names another group when ours was
			// not found; the rule would have referenced the wrong group.
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupRebind, name))
		}
		ref["gid"] = found[0].GID
	}
	return copied, nil
}

func (d *Ikuai) applyGroup(ctx context.Context, op ikuaiOp) (any, error) {
	name := op.Name
	switch {
	case op.Kind == "ikuai_group_add":
		role, err := groupRole(op)
		if err != nil {
			return nil, err
		}
		found, err := d.namedGroups(ctx, name, false)
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupExists, name))
		}
		undo := ikuaiGroupUndo{Kind: "ikuai_group_undo", Name: name, Role: role, Expected: GroupDigest(op.Values), Confirmed: true}
		delete(d.absent, absentKey("route_object", name))
		lost := undo
		lost.Confirmed = false
		if err := d.groupWrite(ctx, "add", name, GroupPayload(name, role, op.Values, nil, op.Notes), lost); err != nil {
			return nil, err
		}
		if d.batch != nil {
			d.batch.groupsWritten[name] = ikuaiWrittenGroup{expected: undo.Expected, added: true}
		}
		return undo, nil
	case op.Kind == "ikuai_group_update" && op.Mark:
		// The marker back on the first entry; addresses and notes unchanged.
		// Not undone: a rollback leaves the marker, which only restores ownership.
		group, err := d.ownedGroup(ctx, op, false, true)
		if err != nil {
			return nil, err
		}
		if GroupDigest(group.Values) != op.digest() {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupDrift, name))
		}
		if err := d.groupWrite(ctx, "edit", name, GroupPayload(name, op.Role, op.Values, group.ID, op.Notes), nil); err != nil {
			return nil, err
		}
		return noUndo, nil
	case op.Kind == "ikuai_group_update":
		group, err := d.ownedGroup(ctx, op, false, false)
		if err != nil {
			return nil, err
		}
		if GroupDigest(group.Values) != op.digest() {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupDrift, name))
		}
		// The entries exactly as read, notes included: a rollback writes them back.
		undo := ikuaiGroupRestore{Kind: "ikuai_group_restore", Name: name, Role: group.Role, Values: group.Values,
			Entries: group.Entries, Previous: op.digest(), Expected: GroupDigest(op.Values)}
		if err := d.groupWrite(ctx, "edit", name, GroupPayload(name, group.Role, op.Values, group.ID, op.Notes), undo); err != nil {
			return nil, err
		}
		if d.batch != nil {
			d.batch.groupsWritten[name] = ikuaiWrittenGroup{expected: undo.Expected}
		}
		return undo, nil
	case op.Kind == "ikuai_group_delete":
		group, err := d.ownedGroup(ctx, op, true, false)
		if err != nil {
			return nil, err
		}
		if GroupDigest(group.Values) != op.digest() {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupDrift, name))
		}
		users, err := d.groupUsers(ctx, group)
		if err != nil {
			return nil, err
		}
		if len(users) > 0 {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupInUse, name, strings.Join(users, "、")))
		}
		undo := ikuaiGroupReadd{Kind: "ikuai_group_readd", Name: name, Role: group.Role, Values: group.Values, Entries: group.Entries}
		if err := d.groupWrite(ctx, "del", name, Row{"id": *group.ID}, undo); err != nil {
			return nil, err
		}
		return undo, nil
	case op.Kind == "ikuai_group_undo":
		role, err := groupRole(op)
		if err != nil {
			return nil, err
		}
		found, err := d.namedGroups(ctx, name, true)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			if op.Confirmed && !d.absent[absentKey("route_object", name)] {
				return nil, routerError(fmt.Sprintf(messageIkuaiGroupAddGone, name))
			}
			return noUndo, nil
		}
		group := found[0]
		if len(found) != 1 || group.Role != role || GroupDigest(group.Values) != op.digest() {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupChanged, name))
		}
		if group.ID == nil {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupUndoNoID, name))
		}
		users, err := d.groupUsers(ctx, group)
		if err != nil {
			return nil, err
		}
		if len(users) > 0 {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupUndoInUse, name, strings.Join(users, "、")))
		}
		return noUndo, d.groupWrite(ctx, "del", name, Row{"id": *group.ID}, nil)
	case op.Kind == "ikuai_group_restore":
		group, err := d.ownedGroup(ctx, op, true, false)
		if err != nil {
			return nil, err
		}
		current := GroupDigest(group.Values)
		if (op.Entries != nil && slices.Equal(group.Entries, *op.Entries)) || (op.Entries == nil && current == op.Previous) {
			return noUndo, nil
		}
		if current != op.digest() {
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupKept, name))
		}
		param := GroupPayload(name, group.Role, op.Values, group.ID, nil)
		if op.Entries != nil {
			param = EntryPayload(name, *op.Entries, group.ID)
		}
		return noUndo, d.groupWrite(ctx, "edit", name, param, nil)
	case op.Kind == "ikuai_group_readd":
		role, err := groupRole(op)
		if err != nil {
			return nil, err
		}
		found, err := d.namedGroups(ctx, name, true)
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			same := len(found) == 1 && found[0].Role == role
			if op.Entries != nil {
				same = same && slices.Equal(found[0].Entries, *op.Entries)
			} else {
				same = same && GroupDigest(found[0].Values) == GroupDigest(op.Values)
			}
			if same {
				return noUndo, nil
			}
			return nil, routerError(fmt.Sprintf(messageIkuaiGroupTaken, name))
		}
		undo := ikuaiGroupUndo{Kind: "ikuai_group_undo", Name: name, Role: role, Expected: GroupDigest(op.Values), Confirmed: true}
		param := GroupPayload(name, role, op.Values, nil, nil)
		if op.Entries != nil {
			param = EntryPayload(name, *op.Entries, nil)
		}
		delete(d.absent, absentKey("route_object", name))
		lost := undo
		lost.Confirmed = false
		if err := d.groupWrite(ctx, "add", name, param, lost); err != nil {
			return nil, err
		}
		return undo, nil
	}
	return nil, routerError(fmt.Sprintf(messageUnsupportedOperation, op.Kind))
}

// IkuaiOversizedRequests are blockers for planned writes whose single request
// iKuai would refuse as too large. Destinations go into IP groups split to
// fit one request, so this is a defence: it sizes the form the driver would
// send for each operation (an update counts only the destinations it adds:
// it may miss an oversize but never blocks one that fits).
func IkuaiOversizedRequests(ops []json.RawMessage) []string {
	blockers := []string{}
	for _, raw := range ops {
		var op ikuaiOp
		if devhttp.DecodeJSON(raw, &op) != nil {
			continue
		}
		var funcName, label, name string
		var count int
		var payload any
		placeholders := func() []any {
			refs := []any{}
			for _, group := range *op.Groups {
				refs = append(refs, map[string]any{"type": 0, "gid": "IPGP99999", "gp_name": group})
			}
			return refs
		}
		groupOp := op.Kind == "ikuai_group_add" || op.Kind == "ikuai_group_update"
		switch {
		case groupOp:
			funcName, label, name, count = "route_object", labelIkuaiGroup, op.Name, len(op.Values)
			form := GroupPayload(op.Name, op.Role, op.Values, nil, op.Notes)
			if op.Kind == "ikuai_group_update" {
				form.ID = new(int64)
			}
			payload = form
		case op.Kind == "policy_route_add" || op.Kind == "policy_route_update":
			funcName, label, name, count = "stream_ipport", labelIkuaiPolicy, op.Comment, len(op.Dst)
			var refs []any
			if op.Groups != nil {
				refs, count = placeholders(), len(*op.Groups)
			}
			form := ikuaiNewPolicy(op.Gateway, op.Dst, op.Comment, refs, op.Groups != nil)
			if op.Kind == "policy_route_update" {
				form["id"] = 0
			}
			payload = form
		case op.Kind == "nat_add" || op.Kind == "nat_update":
			funcName, label, name = "nat_rule", labelIkuaiNATBypass, op.Comment
			dst := op.Dst
			if op.Kind == "nat_update" {
				dst = op.AddDst
			}
			count = len(dst)
			var refs []any
			if op.Groups != nil {
				refs, count = placeholders(), len(*op.Groups)
			}
			form := ikuaiNewNAT(op.Src, dst, op.Comment, refs, op.Groups != nil)
			if op.Kind == "nat_update" {
				form["id"] = 0
			}
			payload = form
		default:
			continue
		}
		action := "edit"
		if strings.HasSuffix(op.Kind, "_add") {
			action = "add"
		}
		if size := IkuaiRequestBytes(funcName, action, payload); size > IkuaiMaxRequestBytes {
			unit := unitIkuaiNetworks
			if !groupOp && op.Groups != nil {
				unit = unitIkuaiGroupRefs
			}
			blockers = append(blockers, fmt.Sprintf(messageIkuaiOversized, label, name, count, unit,
				size/1024, IkuaiMaxRequestBytes/1024))
		}
	}
	return blockers
}
