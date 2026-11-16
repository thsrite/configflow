package routers

// ConfigFlow-owned iKuai IP groups (route_object) for proxy destinations.
//
// The 端口分流 rules cf-mihomo / cf-mihomo-manual and the NAT bypass rule
// reference these groups instead of listing every CIDR (dst_addr.custom stays
// empty): a rule listing ~5000 CIDRs exceeds iKuai's ~96 KiB request limit and
// each change rewrote the whole rule; a group edit takes effect at once.
//
//	role  rule                       group name  marker
//	pa    端口分流 cf-mihomo          cf_pa_<n>   cf-mihomo
//	pm    端口分流 cf-mihomo-manual   cf_pm_<n>   cf-mihomo-manual
//	nat   the NAT bypass target rule cf_nat_<n>  cf-mihomo-nat
//
// iKuai 4.0.314 keeps no comment on a group but keeps each entry's comment and
// the entry order, so the marker is the first entry's comment. A group is ours
// only when both its name and its marker match; anything else is the user's,
// never edited, deleted or referenced (a matching name only reserves its
// number). Exception: a group with our name that lost its marker while that
// role's managed rule still references it is still ours ("adopted") and the
// marker is written back. Notes the user gives other entries stay with their
// entries. Groups hold at most 2000 entries and are split by request bytes,
// notes included, so a group write always fits in one request.
//
// iKuai counts the rules referencing a group (ref_count, one per rule, enabled
// or not), including features ConfigFlow does not read (ACL, 流控…), and
// refuses to delete a referenced group: such a group is never deleted or edited.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/netipx"
)

const (
	GroupMaxEntries = 2000
	// iKuai refuses request bodies from about 98 300 bytes (4.0.314); keep a margin.
	GroupMaxBytes = 92 * 1024
	// Provisional until a rule referencing more groups is verified on a device.
	MaxRuleGroups = 16
	maxGroupIndex = 9999
)

// GroupMarkers are the marker (first entry comment) of each role.
var GroupMarkers = map[string]string{"pa": "cf-mihomo", "pm": "cf-mihomo-manual", "nat": "cf-mihomo-nat"}

var groupRoleRules = map[string]string{"pa": "端口分流「cf-mihomo」", "pm": "端口分流「cf-mihomo-manual」", "nat": "NAT 免转换"}

var groupNameRE = regexp.MustCompile(`^cf_(pa|pm|nat)_([1-9][0-9]{0,3})$`)

// GroupName is cf_<role>_<index>.
func GroupName(role string, index int) string { return "cf_" + role + "_" + strconv.Itoa(index) }

// GroupNameRole parses a managed group name (ok false for any other name).
func GroupNameRole(name string) (role string, index int, ok bool) {
	m := groupNameRE.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	index, _ = strconv.Atoi(m[2])
	return m[1], index, true
}

// CanonAddress compares entries as addresses: iKuai may read 1.1.1.1/32 back as 1.1.1.1.
func CanonAddress(value string) string {
	text := strings.TrimSpace(value)
	if first, last, isRange := strings.Cut(text, "-"); isRange {
		a, ok1 := netipx.ParseAddr4(strings.TrimSpace(first))
		b, ok2 := netipx.ParseAddr4(strings.TrimSpace(last))
		if ok1 && ok2 {
			return a.String() + "-" + b.String()
		}
		return text
	}
	if n, ok := netipx.ParseNetwork(text, false); ok {
		return n.String()
	}
	return text
}

// GroupDigest identifies a group's content (order and address spelling ignored).
func GroupDigest(values []string) string {
	keys := map[string]bool{}
	for _, v := range values {
		keys[CanonAddress(v)] = true
	}
	sum := sha1.Sum([]byte(strings.Join(sortedKeys(keys), "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// GroupEntry is one entry of a group.
type GroupEntry struct {
	IP      string `json:"ip"`
	Comment string `json:"comment"`
}

// GroupForm is the route_object add/edit form (ID nil for add).
type GroupForm struct {
	ID         *int64       `json:"id,omitempty"`
	GroupName  string       `json:"group_name"`
	Type       int          `json:"type"`
	GroupValue []GroupEntry `json:"group_value"`
}

// GroupPayload is the form writing values for role: the first entry carries
// the marker. notes (by CanonAddress) are the user's entry comments; the
// marker goes on an entry without one, moved to the front, so none is lost.
func GroupPayload(name, role string, values []string, id *int64, notes map[string]string) GroupForm {
	entries := make([]GroupEntry, len(values))
	for i, v := range values {
		entries[i] = GroupEntry{IP: v, Comment: notes[CanonAddress(v)]}
	}
	if len(entries) > 0 {
		spare := max(0, slices.IndexFunc(entries, func(e GroupEntry) bool { return e.Comment == "" }))
		first := entries[spare]
		entries = slices.Insert(slices.Delete(entries, spare, spare+1), 0, first)
		entries[0].Comment = GroupMarkers[role]
	}
	return EntryPayload(name, entries, id)
}

// EntryPayload is the form writing exactly these entries (a rollback restores what it read).
func EntryPayload(name string, entries []GroupEntry, id *int64) GroupForm {
	return GroupForm{ID: id, GroupName: name, GroupValue: slices.Clone(entries)}
}

// ikuaiEncode encodes a form string like the iKuai web UI before add/edit
// calls: '%' first, line breaks of comments to %0A, whitespace to %20, then
// the characters its backend treats specially. Comments are trimmed.
func ikuaiEncode(text string, comment bool) string {
	if comment {
		text = strings.TrimSpace(text)
	}
	var b strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '%':
			b.WriteString("%25")
		case comment && (r == '\n' || r == '\r'):
			if r == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			b.WriteString("%0A")
		case unicode.IsSpace(r):
			b.WriteString("%20")
		case strings.ContainsRune("\"`$&;\\|'", r):
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(r), 16)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wireLen bounds the bytes of s inside the JSON request: quotes, backslashes
// and short escapes take 2, other controls 6, non-ASCII is counted as
// \uXXXX escapes (6, or 12 outside the BMP) — never less than the driver sends.
func wireLen(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case strings.ContainsRune("\"\\\b\f\n\r\t", r):
			n += 2
		case r < 0x20 || r == 0x7f:
			n += 6
		case r < 0x80:
			n++
		case r <= 0xffff:
			n += 6
		default:
			n += 12
		}
	}
	return n
}

// Request bytes of a route_object edit of an empty group named "" with a
// 10-digit ID, and of one entry (`{"ip": "", "comment": ""}` plus its ", "
// separator), with spaced JSON separators.
const (
	groupRequestBytes = 124
	groupEntryBytes   = 27
)

func entryBytes(ip, note string) int {
	return groupEntryBytes + wireLen(ikuaiEncode(ip, false)) + wireLen(ikuaiEncode(note, true))
}

// groupBaseBytes is the worst case of every variable part of a group write:
// a 10-digit ID, the longest name and marker.
var groupBaseBytes = groupRequestBytes + len("cf_nat_9999") + len(GroupMarkers["pm"])

// GroupBytes is the request size of writing values to a group (10-digit ID).
func GroupBytes(name, role string, values []string, notes map[string]string) int {
	form := GroupPayload(name, role, values, nil, notes)
	n := groupRequestBytes + wireLen(ikuaiEncode(name, false))
	for _, e := range form.GroupValue {
		n += entryBytes(e.IP, e.Comment)
	}
	if len(form.GroupValue) > 0 {
		n -= 2
	}
	return n
}

// GroupUser is one rule address field referencing a group.
type GroupUser struct {
	Rule    string `json:"rule"`
	Field   string `json:"field"`
	Comment string `json:"comment"`
	Label   string `json:"label"`
}

// IkuaiGroup is a route_object row as planner and driver see it. Role and
// Index are set for a group ConfigFlow owns ("" and 0 otherwise).
type IkuaiGroup struct {
	ID      *int64
	GID     string
	Name    string
	Comment string // the marker: first entry's comment
	Values  []string
	Entries []GroupEntry
	Type    any
	Refs    int // iKuai's ref_count
	Role    string
	Index   int
	Users   []GroupUser
	Dup     bool // the name is used twice: neither group is ours
	Adopted bool // ours by name and a managed rule's reference; marker to write back
}

// intValue is a decoded JSON integer.
func intValue(v any) (int64, bool) {
	switch v := v.(type) {
	case float64:
		if v == float64(int64(v)) {
			return int64(v), true
		}
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	}
	return 0, false
}

// GroupFromRow reads a decoded route_object row.
func GroupFromRow(row Row) *IkuaiGroup {
	g := &IkuaiGroup{Name: row.text("group_name"), GID: row.text("group_id"), Values: []string{},
		Entries: []GroupEntry{}, Type: row["type"], Users: []GroupUser{}}
	if !row.has("type") {
		g.Type = 0.0
	}
	if id, ok := intValue(row["id"]); ok {
		g.ID = &id
	}
	if refs, ok := intValue(row["ref_count"]); ok && refs > 0 {
		g.Refs = int(refs)
	}
	items, _ := row["group_value"].([]any)
	var entries []Row
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			entries = append(entries, m)
		}
	}
	if len(entries) > 0 {
		g.Comment = entries[0].text("comment")
	}
	for _, e := range entries {
		if present(e["ip"]) {
			g.Entries = append(g.Entries, GroupEntry{IP: e.text("ip"), Comment: e.text("comment")})
			g.Values = append(g.Values, e.text("ip"))
		}
	}
	if role, index, ok := GroupNameRole(g.Name); ok && g.Comment == GroupMarkers[role] {
		g.Role, g.Index = role, index
	}
	return g
}

// GroupsFromRows reads route_object rows; a name used twice identifies neither group.
func GroupsFromRows(rows []Row) []*IkuaiGroup {
	groups := make([]*IkuaiGroup, len(rows))
	counts := map[string]int{}
	for i, row := range rows {
		groups[i] = GroupFromRow(row)
		counts[groups[i].Name]++
	}
	for _, g := range groups {
		if counts[g.Name] > 1 {
			g.Role, g.Index, g.Dup = "", 0, true
		}
	}
	return groups
}

// HiddenRefs are references iKuai counts that no NAT or 端口分流 rule accounts
// for (ACL, 流控…): iKuai counts each rule once, disabled ones too.
func (g *IkuaiGroup) HiddenRefs() int {
	rules := map[string]bool{}
	for _, u := range g.Users {
		rules[u.Rule] = true
	}
	return max(0, g.Refs-len(rules))
}

// entryNotes are the user's own entry comments: all but our marker on the first entry.
func (g *IkuaiGroup) entryNotes(notes map[string]string) {
	for position, e := range g.Entries {
		_, isMarker := markerRole(e.Comment)
		if e.Comment != "" && !isMarker && (position > 0 || g.Role == "" || g.Adopted) {
			if key := CanonAddress(e.IP); notes[key] == "" {
				notes[key] = e.Comment
			}
		}
	}
}

func markerRole(comment string) (string, bool) {
	for role, marker := range GroupMarkers {
		if marker == comment {
			return role, true
		}
	}
	return "", false
}

// GroupRule is a NAT or 端口分流 rule: its literal destinations and group references.
type GroupRule struct {
	Key        string // nat:<id> or policy:<id>
	Func       string // nat_rule or stream_ipport
	ID         string
	Comment    string
	Enabled    bool
	Action     string
	Custom     []string // literal destinations
	Objects    []Row    // destination group references
	SrcObjects []Row
}

// GroupState is the iKuai group readback: groups with ownership and the
// rules in read order (NAT rules, then 端口分流).
type GroupState struct {
	Groups []*IkuaiGroup
	Rules  []*GroupRule
	byKey  map[string]*GroupRule
}

// Rule is the rule with key (nil when there is none).
func (s *GroupState) Rule(key string) *GroupRule { return s.byKey[key] }

// ResolveGroup is the group a rule reference points at: by gid; by name only
// when the reference has none.
func ResolveGroup(groups []*IkuaiGroup, ref Row) *IkuaiGroup {
	if gid := ref.text("gid"); gid != "" {
		if i := slices.IndexFunc(groups, func(g *IkuaiGroup) bool { return g.GID == gid }); i >= 0 {
			return groups[i]
		}
		return nil
	}
	name := ref.text("gp_name")
	var named []*IkuaiGroup
	for _, g := range groups {
		if g.Name == name {
			named = append(named, g)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return nil
}

// Reference kinds.
const (
	RefOwned   = "owned"   // one of ours
	RefStale   = "stale"   // ours by name, gone
	RefForeign = "foreign" // the user's
)

// ClassifyRef tells what a rule reference points at.
func ClassifyRef(groups []*IkuaiGroup, ref Row) (string, *IkuaiGroup) {
	if g := ResolveGroup(groups, ref); g != nil {
		if g.Role != "" {
			return RefOwned, g
		}
		return RefForeign, g
	}
	if _, _, ok := GroupNameRole(ref.text("gp_name")); ok {
		return RefStale, nil
	}
	return RefForeign, nil
}

// ForeignRefs are the references to the user's groups among objects.
func ForeignRefs(groups []*IkuaiGroup, objects []Row) []Row {
	out := []Row{}
	for _, ref := range objects {
		if kind, _ := ClassifyRef(groups, ref); ref != nil && kind == RefForeign {
			out = append(out, ref)
		}
	}
	return out
}

func addressField(row Row, field string) (custom []string, objects []Row) {
	value, _ := row[field].(map[string]any)
	list, _ := value["custom"].([]any)
	custom = []string{}
	for _, item := range list {
		if s, ok := item.(string); ok {
			custom = append(custom, s)
		}
	}
	list, _ = value["object"].([]any)
	objects = []Row{}
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			objects = append(objects, m)
		}
	}
	return custom, objects
}

// ReadGroupRules records each rule's destinations and group references and
// who uses each group, then adopts our groups whose marker was removed.
func ReadGroupRules(groups []*IkuaiGroup, natRows, policyRows []Row) *GroupState {
	s := &GroupState{Groups: groups, byKey: map[string]*GroupRule{}}
	for _, part := range []struct {
		fn, prefix, label string
		rows              []Row
	}{{"nat_rule", "nat", "NAT 规则", natRows}, {"stream_ipport", "policy", "端口分流", policyRows}} {
		for _, row := range part.rows {
			key := part.prefix + ":" + row.text("id")
			rule := &GroupRule{Key: key, Func: part.fn, ID: row.text("id"), Comment: row.text("comment"),
				Enabled: row.is("enabled", "yes"), Action: row.text("action")}
			for _, field := range []string{"src_addr", "dst_addr"} {
				custom, objects := addressField(row, field)
				for _, ref := range objects {
					if g := ResolveGroup(groups, ref); g != nil {
						g.Users = append(g.Users, GroupUser{Rule: key, Field: field, Comment: rule.Comment, Label: part.label})
					}
				}
				if field == "dst_addr" {
					rule.Custom, rule.Objects = custom, objects
				} else {
					rule.SrcObjects = objects
				}
			}
			if old := s.byKey[key]; old != nil {
				*old = *rule
			} else {
				s.byKey[key] = rule
				s.Rules = append(s.Rules, rule)
			}
		}
	}
	// Our groups whose marker the user removed: our name, and that role's
	// managed rule references them; the next release writes the marker back.
	for _, g := range groups {
		role, index, ok := GroupNameRole(g.Name)
		if g.Role != "" || g.Dup || !ok {
			continue
		}
		if slices.ContainsFunc(g.Users, func(u GroupUser) bool { return u.Field == "dst_addr" && roleRule(role, s.byKey[u.Rule]) }) {
			g.Role, g.Index, g.Adopted = role, index, true
		}
	}
	return s
}

// roleRule: rule is the managed rule a role family belongs to.
func roleRule(role string, rule *GroupRule) bool {
	if role == "nat" {
		return rule.Func == "nat_rule" && rule.Action == "filter" && IsManaged(rule.Comment)
	}
	return rule.Func == "stream_ipport" && rule.Comment == GroupMarkers[role]
}

// --- layout ---

type groupSlot struct {
	name   string
	index  int
	before []string
	isNew  bool
	values []string
	size   int
}

type layoutGroup struct {
	name   string
	index  int
	values []string
	frozen bool // another rule uses it too
}

type namedValues struct {
	name   string
	values []string
}

type groupLayout struct {
	final   []namedValues
	adds    []namedValues
	updates []struct {
		name           string
		values, before []string
	}
	err string
}

func hasKey[V any](m map[string]V, key string) bool { _, ok := m[key]; return ok }

// pack fills each slot in order from queue[position:] and returns the new position.
func pack(slots []*groupSlot, queue []string, position int, sizeOf func(string) int) int {
	for _, slot := range slots {
		for position < len(queue) {
			size := sizeOf(queue[position])
			if len(slot.values) >= GroupMaxEntries || slot.size+size > GroupMaxBytes {
				break
			}
			slot.values = append(slot.values, queue[position])
			slot.size += size
			position++
		}
	}
	return position
}

// minimumGroups is how many groups values need at least (an entry too long
// for any group ends the count; layout reports it).
func minimumGroups(values []string, sizeOf func(string) int) int {
	count, position := 0, 0
	for position < len(values) {
		count++
		next := pack([]*groupSlot{{size: groupBaseBytes}}, values, position, sizeOf)
		if next == position {
			break
		}
		position = next
	}
	return count
}

// layout places want into this role's groups, writing as little as possible:
// entries stay where they are, new ones fill free space from the lowest
// number, then new groups; emptied groups are deleted. Only a badly
// fragmented family is repacked, every entry moving to a lower or equal number.
func layout(role string, existing []layoutGroup, want []string, taken map[int]bool, notes map[string]string) groupLayout {
	sizeOf := func(v string) int { return entryBytes(v, notes[CanonAddress(v)]) }
	var wantedKeys []string
	wanted := map[string]string{}
	for _, v := range want {
		if key := CanonAddress(v); !hasKey(wanted, key) {
			wantedKeys = append(wantedKeys, key)
			wanted[key] = v
		}
	}
	placed := map[string]bool{}
	var slots, fixed []*groupSlot
	sorted := slices.Clone(existing)
	slices.SortStableFunc(sorted, func(a, b layoutGroup) int { return a.index - b.index })
	for _, g := range sorted {
		if g.frozen {
			// Never edited or deleted here; referenced as it is when every entry is wanted.
			keys := make([]string, len(g.values))
			unique := map[string]bool{}
			usable := len(g.values) > 0
			for i, v := range g.values {
				keys[i] = CanonAddress(v)
				_, isWanted := wanted[keys[i]]
				usable = usable && isWanted && !placed[keys[i]] && !unique[keys[i]]
				unique[keys[i]] = true
			}
			if usable {
				for _, k := range keys {
					placed[k] = true
				}
				fixed = append(fixed, &groupSlot{name: g.name, index: g.index, before: g.values, values: slices.Clone(g.values)})
			}
			continue
		}
		slot := &groupSlot{name: g.name, index: g.index, before: g.values, values: []string{}, size: groupBaseBytes}
		for _, v := range g.values {
			key := CanonAddress(v)
			if _, isWanted := wanted[key]; isWanted && !placed[key] {
				placed[key] = true
				slot.values = append(slot.values, v)
				slot.size += sizeOf(v)
			}
		}
		slots = append(slots, slot)
	}
	var pending []string
	for _, key := range wantedKeys {
		if !placed[key] {
			pending = append(pending, wanted[key])
		}
	}
	used := maps.Clone(taken)
	if used == nil {
		used = map[int]bool{}
	}
	for _, g := range existing {
		used[g.index] = true
	}
	var result groupLayout
	newSlots := func(queue []string, position int) []*groupSlot {
		var created []*groupSlot
		for position < len(queue) {
			index := 0
			for n := 1; n <= maxGroupIndex; n++ {
				if !used[n] {
					index = n
					break
				}
			}
			if index == 0 {
				result.err = "iKuai IP 分组编号 cf_" + role + "_1…cf_" + role + "_" + strconv.Itoa(maxGroupIndex) + " 已用尽"
				break
			}
			used[index] = true
			slot := &groupSlot{name: GroupName(role, index), index: index, isNew: true, values: []string{}, size: groupBaseBytes}
			created = append(created, slot)
			position = pack([]*groupSlot{slot}, queue, position, sizeOf)
			if len(slot.values) == 0 {
				entry := []rune(queue[position])
				result.err = "iKuai IP 分组条目「" + string(entry[:min(64, len(entry))]) + "」过长，无法写入"
				break
			}
		}
		return created
	}
	position := pack(slots, pending, 0, sizeOf)
	created := newSlots(pending, position)
	groups := 0
	for _, s := range slices.Concat(slots, created) {
		if len(s.values) > 0 {
			groups++
		}
	}
	fixedKeys := map[string]bool{}
	for _, s := range fixed {
		for _, v := range s.values {
			fixedKeys[CanonAddress(v)] = true
		}
	}
	var rest []string
	for _, key := range wantedKeys {
		if !fixedKeys[key] {
			rest = append(rest, wanted[key])
		}
	}
	if groups > minimumGroups(rest, sizeOf)+2 {
		// Repack in number order, existing groups first: entries only move down.
		var sequence []string
		for _, s := range slots {
			sequence = append(sequence, s.values...)
		}
		sequence = append(sequence, pending...)
		for _, s := range slots {
			s.values, s.size = []string{}, groupBaseBytes
		}
		for _, s := range created {
			delete(used, s.index)
		}
		position = pack(slots, sequence, 0, sizeOf)
		created = newSlots(sequence, position)
	}
	final := slices.DeleteFunc(slices.Concat(slots, created, fixed), func(s *groupSlot) bool { return len(s.values) == 0 })
	slices.SortStableFunc(final, func(a, b *groupSlot) int { return a.index - b.index })
	for _, s := range final {
		result.final = append(result.final, namedValues{s.name, s.values})
		if s.isNew {
			result.adds = append(result.adds, namedValues{s.name, s.values})
		} else if !slices.Equal(s.values, s.before) {
			result.updates = append(result.updates, struct {
				name           string
				values, before []string
			}{s.name, s.values, s.before})
		}
	}
	if result.err == "" && len(final) > MaxRuleGroups {
		result.err = "iKuai IP 分组需要 " + strconv.Itoa(len(final)) + " 个，超过单条规则可引用的 " + strconv.Itoa(MaxRuleGroups) +
			" 个（每组最多 " + strconv.Itoa(GroupMaxEntries) + " 个网段），请减少代理网段（例如少选规则集）后重试"
	}
	return result
}

// --- planning ---

// GroupSpan is "cf_pa_1…cf_pa_3" for consecutive groups, otherwise each name.
func GroupSpan(names []string) string {
	switch len(names) {
	case 0:
		return "（无）"
	case 1:
		return names[0]
	}
	consecutive := true
	firstRole, first, _ := GroupNameRole(names[0])
	for i, name := range names {
		role, index, ok := GroupNameRole(name)
		consecutive = consecutive && ok && role == firstRole && index == first+i
	}
	if consecutive {
		return names[0] + "…" + names[len(names)-1]
	}
	return strings.Join(names, "、")
}

func samples(values []string) string {
	shown := strings.Join(values[:min(5, len(values))], "、")
	if len(values) > 5 {
		shown += "…"
	}
	return shown
}

// Group operation kinds.
const (
	OpGroupAdd    = "ikuai_group_add"
	OpGroupUpdate = "ikuai_group_update"
	OpGroupDelete = "ikuai_group_delete"
)

// GroupOp is one group write of a release plan.
type GroupOp struct {
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Role     string            `json:"role"`
	Comment  string            `json:"comment"`  // add, update
	Values   []string          `json:"values"`   // add, update
	Expected string            `json:"expected"` // update, delete: digest the device must still have
	Added    []string          `json:"added"`    // update
	Removed  []string          `json:"removed"`  // update
	Mark     bool              `json:"mark"`     // update writing the marker back
	Notes    map[string]string `json:"notes"`    // add, update: user entry comments kept (when any)
	Count    int               `json:"count"`    // delete
	Summary  string            `json:"summary"`
}

// MarshalJSON writes the fields of the op's kind.
func (op GroupOp) MarshalJSON() ([]byte, error) {
	out := map[string]any{"kind": op.Kind, "name": op.Name, "role": op.Role, "summary": op.Summary}
	switch op.Kind {
	case OpGroupDelete:
		out["expected"], out["count"] = op.Expected, op.Count
	case OpGroupUpdate:
		out["expected"], out["added"], out["removed"] = op.Expected, op.Added, op.Removed
		if op.Mark {
			out["mark"] = true
		}
		fallthrough
	default:
		out["comment"], out["values"] = op.Comment, op.Values
		if len(op.Notes) > 0 {
			out["notes"] = op.Notes
		}
	}
	return json.Marshal(out)
}

// GroupRuleCheck is a rule the read-back must find referencing exactly Groups.
type GroupRuleCheck struct {
	Func        string   `json:"func"`
	ID          string   `json:"id,omitempty"`
	Comment     string   `json:"comment"`
	Groups      []string `json:"groups"`
	KeepForeign bool     `json:"keep_foreign"`
}

// GroupStageCheck is the group structure one release stage planned.
type GroupStageCheck struct {
	Groups  map[string]string `json:"groups"` // name -> digest
	Rules   []GroupRuleCheck  `json:"rules"`
	Removed []string          `json:"removed"`
}

// GroupMigration is a rule still listing destinations itself.
type GroupMigration struct {
	Rule  string `json:"rule"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// GroupPlanner is the group side of one release plan, shared by the routes
// and NAT stages. Warnings and Blockers are the plan's lists.
type GroupPlanner struct {
	State    *GroupState
	Warnings *[]string
	Blockers *[]string
	Finals   map[string][]string            // role -> final group names
	Contents map[string]map[string][]string // role -> name -> values
	// Rule key -> the managed group names its destination references after this plan.
	Rewritten    map[string][]string
	DeletedRules map[string]bool
	Migrations   []GroupMigration
	RouteRules   []GroupRuleCheck
	StageChecks  map[string]GroupStageCheck
	marked       map[string]bool
}

func NewGroupPlanner(state *GroupState, warnings, blockers *[]string) *GroupPlanner {
	return &GroupPlanner{State: state, Warnings: warnings, Blockers: blockers, Finals: map[string][]string{},
		Contents: map[string]map[string][]string{}, Rewritten: map[string][]string{}, DeletedRules: map[string]bool{},
		StageChecks: map[string]GroupStageCheck{}, marked: map[string]bool{}}
}

// Owned are our groups of role by number.
func (p *GroupPlanner) Owned(role string) []*IkuaiGroup {
	var out []*IkuaiGroup
	for _, g := range p.State.Groups {
		if g.Role == role {
			out = append(out, g)
		}
	}
	slices.SortStableFunc(out, func(a, b *IkuaiGroup) int { return a.Index - b.Index })
	return out
}

// notes are the user's entry comments in this role's groups; they follow their entries.
func (p *GroupPlanner) notes(role string) map[string]string {
	notes := map[string]string{}
	for _, g := range p.Owned(role) {
		g.entryNotes(notes)
	}
	return notes
}

func notesFor(notes map[string]string, values []string) map[string]string {
	picked := map[string]string{}
	for _, v := range values {
		if note, ok := notes[CanonAddress(v)]; ok {
			picked[CanonAddress(v)] = note
		}
	}
	return picked
}

// markOps write the marker back on adopted groups first; later ops then find them ours.
func (p *GroupPlanner) markOps(role string) []GroupOp {
	var ops []GroupOp
	notes := p.notes(role)
	for _, g := range p.Owned(role) {
		if !g.Adopted || p.marked[g.Name] {
			continue
		}
		p.marked[g.Name] = true
		ops = append(ops, GroupOp{Kind: OpGroupUpdate, Name: g.Name, Role: role, Comment: GroupMarkers[role],
			Values: g.Values, Expected: GroupDigest(g.Values), Added: []string{}, Removed: []string{}, Mark: true,
			Notes: notesFor(notes, g.Values),
			Summary: "恢复 IP 分组「" + g.Name + "」的 ConfigFlow 归属标记：第一个条目的备注不是「" + GroupMarkers[role] +
				"」（可能在 iKuai 中被改过），" + groupRoleRules[role] + "仍在引用它，改回标记，网段和其他备注不变"})
	}
	return ops
}

func (p *GroupPlanner) taken(role string) map[int]bool {
	taken := map[int]bool{}
	for _, g := range p.State.Groups {
		if r, index, ok := GroupNameRole(g.Name); ok && r == role {
			taken[index] = true
		}
	}
	return taken
}

// Refs is the rule with key (an empty one when key is "" or unknown).
func (p *GroupPlanner) Refs(key string) *GroupRule {
	if rule := p.State.Rule(key); key != "" && rule != nil {
		return rule
	}
	return &GroupRule{}
}

// ReferencedValues are the entries of every managed group the rule's destination references.
func (p *GroupPlanner) ReferencedValues(key string) []string {
	var values []string
	for _, ref := range p.Refs(key).Objects {
		if kind, g := ClassifyRef(p.State.Groups, ref); kind == RefOwned {
			values = append(values, g.Values...)
		}
	}
	return values
}

// shared: anything but subject's destination uses the group (and keeps it).
func (p *GroupPlanner) shared(g *IkuaiGroup, subject string) bool {
	return g.HiddenRefs() > 0 || slices.ContainsFunc(g.Users, func(u GroupUser) bool {
		return !p.DeletedRules[u.Rule] && (u.Rule != subject || u.Field != "dst_addr")
	})
}

// Family plans the group adds and updates of one role and returns the final
// names. subject is the rule that will reference the family ("" for a new
// rule); a group some other rule uses as well is frozen.
func (p *GroupPlanner) Family(role string, want []string, subject string) ([]string, []GroupOp) {
	var existing []layoutGroup
	for _, g := range p.Owned(role) {
		existing = append(existing, layoutGroup{g.Name, g.Index, g.Values, p.shared(g, subject)})
	}
	notes := p.notes(role)
	result := layout(role, existing, want, p.taken(role), notes)
	if result.err != "" {
		*p.Blockers = append(*p.Blockers, result.err)
	}
	p.Finals[role] = []string{}
	p.Contents[role] = map[string][]string{}
	for _, f := range result.final {
		p.Finals[role] = append(p.Finals[role], f.name)
		p.Contents[role][f.name] = f.values
	}
	user := groupRoleRules[role]
	ops := p.markOps(role)
	for _, a := range result.adds {
		ops = append(ops, GroupOp{Kind: OpGroupAdd, Name: a.name, Role: role, Comment: GroupMarkers[role], Values: a.values,
			Notes:   notesFor(notes, a.values),
			Summary: "新建 IP 分组「" + a.name + "」（" + user + "使用，" + strconv.Itoa(len(a.values)) + " 个网段）"})
	}
	for _, u := range result.updates {
		old, current := map[string]bool{}, map[string]bool{}
		for _, v := range u.before {
			old[CanonAddress(v)] = true
		}
		for _, v := range u.values {
			current[CanonAddress(v)] = true
		}
		added := slices.DeleteFunc(slices.Clone(u.values), func(v string) bool { return old[CanonAddress(v)] })
		removed := slices.DeleteFunc(slices.Clone(u.before), func(v string) bool { return current[CanonAddress(v)] })
		var parts, examples []string
		if len(added) > 0 {
			parts = append(parts, "加入 "+strconv.Itoa(len(added))+" 个")
			examples = append(examples, "加入 "+samples(added))
		}
		if len(removed) > 0 {
			parts = append(parts, "移除 "+strconv.Itoa(len(removed))+" 个")
			examples = append(examples, "移除 "+samples(removed))
		}
		detail := strings.Join(parts, "、")
		if detail == "" {
			detail = "整理重复条目"
		}
		summary := "更新 IP 分组「" + u.name + "」：" + detail + "网段（现 " + strconv.Itoa(len(u.values)) + " 个）"
		if len(examples) > 0 {
			summary += "：" + strings.Join(examples, "；")
		}
		ops = append(ops, GroupOp{Kind: OpGroupUpdate, Name: u.name, Role: role, Comment: GroupMarkers[role],
			Values: u.values, Expected: GroupDigest(u.before), Added: nonNil(added), Removed: nonNil(removed),
			Notes: notesFor(notes, u.values), Summary: summary})
	}
	return p.Finals[role], ops
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// NeedsRewrite: the rule must be written to reference exactly the role's final groups.
func (p *GroupPlanner) NeedsRewrite(key, role string, keepForeign bool) bool {
	rule := p.State.Rule(key)
	if key == "" || rule == nil || len(rule.Custom) > 0 {
		return true
	}
	var names []string
	for _, ref := range rule.Objects {
		kind, g := ClassifyRef(p.State.Groups, ref)
		if kind == RefStale || kind == RefForeign && !keepForeign {
			return true
		}
		if kind == RefOwned {
			if g.Role != role || !ref.is("gp_name", g.Name) {
				return true
			}
			names = append(names, g.Name)
		}
	}
	return !sameNames(names, p.Finals[role]) || len(slices.Compact(slices.Sorted(slices.Values(names)))) != len(names)
}

func sameNames(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// DroppedForeign names the user's groups the rule references.
func (p *GroupPlanner) DroppedForeign(key string) []string {
	out := []string{}
	for _, ref := range p.Refs(key).Objects {
		if kind, _ := ClassifyRef(p.State.Groups, ref); kind == RefForeign {
			name := ref.text("gp_name")
			if name == "" {
				name = ref.text("gid")
			}
			out = append(out, name)
		}
	}
	return out
}

// Migration records a rule whose literal destinations move into groups; it
// returns how many there are.
func (p *GroupPlanner) Migration(key, kind, rule string) int {
	count := len(p.Refs(key).Custom)
	if count > 0 {
		p.Migrations = append(p.Migrations, GroupMigration{Rule: rule, Kind: kind, Count: count})
	}
	return count
}

// Cleanup deletes our groups of these roles nothing will reference and warns
// about the rest; it returns marker write-backs (before their deletes) and deletes.
func (p *GroupPlanner) Cleanup(roles ...string) []GroupOp {
	ops := []GroupOp{}
	for _, role := range roles {
		p.unmarkedHints(role)
		// Any adopted group no family wrote back yet (e.g. only a disabled rule uses it).
		ops = append(ops, p.markOps(role)...)
		for _, g := range p.Owned(role) {
			if slices.Contains(p.Finals[role], g.Name) {
				continue
			}
			var users []GroupUser
			for _, u := range g.Users {
				names, rewritten := p.Rewritten[u.Rule]
				if !p.DeletedRules[u.Rule] && !(u.Field == "dst_addr" && rewritten && !slices.Contains(names, g.Name)) {
					users = append(users, u)
				}
			}
			hidden := g.HiddenRefs()
			if len(users) > 0 && hidden == 0 && role == "nat" && !slices.ContainsFunc(users, func(u GroupUser) bool {
				return u.Field != "dst_addr" || !roleRule("nat", p.State.Rule(u.Rule))
			}) {
				// Another managed NAT bypass rule's destinations: ConfigFlow's own, kept.
				continue
			}
			if len(users) > 0 || hidden > 0 {
				p.keptWarning(g, users, hidden)
				continue
			}
			ops = append(ops, GroupOp{Kind: OpGroupDelete, Name: g.Name, Role: role, Expected: GroupDigest(g.Values),
				Count: len(g.Values), Summary: "删除不再使用的 IP 分组「" + g.Name + "」（原 " + strconv.Itoa(len(g.Values)) + " 个网段）"})
		}
	}
	return ops
}

func (p *GroupPlanner) keptWarning(g *IkuaiGroup, users []GroupUser, hidden int) {
	var names []string
	owner := "规则"
	for _, u := range users {
		who := u.Comment
		if who == "" {
			who = u.Rule
		}
		if name := u.Label + "「" + who + "」"; !slices.Contains(names, name) {
			names = append(names, name)
		}
		if !IsManaged(u.Comment) {
			owner = "手工规则"
		}
	}
	unseen := "iKuai 中 ConfigFlow 读不到的配置（如 ACL、流控）引用 " + strconv.Itoa(hidden) + " 处"
	by := "还被 " + unseen
	switch {
	case hidden == 0:
		by = "被" + owner + strings.Join(names, "、") + "引用"
	case len(users) > 0:
		by = "被" + owner + strings.Join(names, "、") + "引用，还被 " + unseen
	}
	*p.Warnings = append(*p.Warnings, "ConfigFlow 的 IP 分组 "+g.Name+" "+by+"，已保留，请先在 iKuai 中解除引用")
}

// unmarkedHints: a group with our name but no marker that nothing uses is
// the user's and left alone; say how to get it back if ConfigFlow made it.
func (p *GroupPlanner) unmarkedHints(role string) {
	marker := GroupMarkers[role]
	for _, g := range p.State.Groups {
		if r, _, ok := GroupNameRole(g.Name); ok && r == role && g.Role == "" && !g.Dup && len(g.Users) == 0 && g.HiddenRefs() == 0 {
			*p.Warnings = append(*p.Warnings, "IP 分组「"+g.Name+"」名称是 ConfigFlow 的格式，但第一个条目的备注不是「"+marker+
				"」，按用户分组处理（不修改、不删除，只占用这个序号）。如果它是 ConfigFlow 建的，把第一个条目的备注改回「"+marker+
				"」后重新预览即可清理；如果是你自己的分组，建议改名")
		}
	}
}

// Check is the structure a stage's read-back verifies.
func (p *GroupPlanner) Check(roles []string, rules []GroupRuleCheck, removed []string) GroupStageCheck {
	groups := map[string]string{}
	for _, role := range roles {
		for name, values := range p.Contents[role] {
			groups[name] = GroupDigest(values)
		}
	}
	return GroupStageCheck{Groups: groups, Rules: nonNilChecks(rules), Removed: nonNil(removed)}
}

func nonNilChecks(rules []GroupRuleCheck) []GroupRuleCheck {
	if rules == nil {
		return []GroupRuleCheck{}
	}
	return rules
}

// MigrationWarning explains a migration of literal destinations into groups ("" when none).
func (p *GroupPlanner) MigrationWarning() string {
	if len(p.Migrations) == 0 {
		return ""
	}
	var rules, families []string
	total := 0
	for _, m := range p.Migrations {
		rules = append(rules, m.Kind+"「"+m.Rule+"」")
		total += m.Count
		family := "cf_pa_*"
		if m.Kind == "NAT 免转换" {
			family = "cf_nat_*"
		} else if m.Rule == GroupMarkers["pm"] {
			family = "cf_pm_*"
		}
		if !slices.Contains(families, family) {
			families = append(families, family)
		}
	}
	return "将把 iKuai " + strings.Join(rules, "和") + "中直接填写的 " + strconv.Itoa(total) +
		" 个目的网段移入 ConfigFlow 管理的 IP 分组（" + strings.Join(families, "、") + "），规则改为引用分组。" +
		"原因：单条规则直接填写约 5000 个网段就会超过 iKuai 单次请求上限，大规则写入还容易超时；" +
		"改用分组后，以后增删网段只改分组内容，不再重写整条规则。本次发布失败会自动恢复原规则并删除新建的分组。"
}

// NATTarget is the managed NAT bypass rule whose destinations live in the
// cf_nat groups: the rule already referencing them; before any does, the one
// listing the most CIDRs (the first on ties). Other managed rules keep theirs.
func NATTarget(s *GroupState, managed []*NatBypassEntry) *NatBypassEntry {
	for _, rule := range managed {
		if r := s.Rule("nat:" + rule.ID); r != nil && slices.ContainsFunc(r.Objects, func(ref Row) bool {
			kind, g := ClassifyRef(s.Groups, ref)
			return kind == RefOwned && g.Role == "nat"
		}) {
			return rule
		}
	}
	var best *NatBypassEntry
	most := -1
	for _, rule := range managed {
		count := 0
		if r := s.Rule("nat:" + rule.ID); r != nil {
			count = len(r.Custom)
		}
		if count > most {
			best, most = rule, count
		}
	}
	return best
}

// GroupReport is the iKuai group part of the drift report; any entry keeps
// the router out of sync.
type GroupReport struct {
	Migrations []GroupMigration `json:"migrations"`
	Orphans    []string         `json:"orphans"` // our groups nothing references
	Marks      []string         `json:"marks"`   // markers a release writes back
}

// GroupReportSection lists pending migrations, unreferenced groups and markers to restore.
func GroupReportSection(state *RouterState) GroupReport {
	s := state.IkuaiGroups
	report := GroupReport{Migrations: []GroupMigration{}, Orphans: []string{}, Marks: []string{}}
	custom := func(key string) []string {
		if r := s.Rule(key); r != nil {
			return r.Custom
		}
		return nil
	}
	if state.PolicyRoutes != nil {
		for _, comment := range []string{PolicyAutoComment, PolicyManualComment} {
			if rule := PolicyRoute(state, comment); rule != nil && len(custom("policy:"+rule.ID)) > 0 {
				report.Migrations = append(report.Migrations, GroupMigration{Rule: comment, Kind: "端口分流", Count: len(custom("policy:" + rule.ID))})
			}
		}
	}
	var managed []*NatBypassEntry
	for _, rule := range state.NatBypass {
		if rule.Enabled && IsManaged(rule.Comment) {
			managed = append(managed, rule)
		}
	}
	if target := NATTarget(s, managed); target != nil && len(custom("nat:"+target.ID)) > 0 {
		report.Migrations = append(report.Migrations, GroupMigration{Rule: target.Comment, Kind: "NAT 免转换", Count: len(custom("nat:" + target.ID))})
	}
	for _, g := range s.Groups {
		if g.Role != "" && len(g.Users) == 0 && g.HiddenRefs() == 0 {
			report.Orphans = append(report.Orphans, g.Name)
		}
		if g.Adopted {
			report.Marks = append(report.Marks, g.Name)
		}
	}
	return report
}

// VerifyGroups reads back the group structure one stage planned: contents,
// references, removals.
func VerifyGroups(check GroupStageCheck, state *RouterState) error {
	s := state.IkuaiGroups
	if s == nil {
		return routerError("回读校验失败：无法读取 iKuai IP 分组")
	}
	byName := map[string][]*IkuaiGroup{}
	for _, g := range s.Groups {
		byName[g.Name] = append(byName[g.Name], g)
	}
	// In plan order: role (pa, pm, nat), then group number.
	names := sortedKeys(check.Groups)
	slices.SortStableFunc(names, func(a, b string) int {
		ra, ia, _ := GroupNameRole(a)
		rb, ib, _ := GroupNameRole(b)
		if c := strings.Index("pa pm nat", ra) - strings.Index("pa pm nat", rb); c != 0 {
			return c
		}
		return ia - ib
	})
	for _, name := range names {
		found := byName[name]
		// Marked, not just adopted: every group the stage planned carries the marker.
		if len(found) != 1 || found[0].Role == "" || found[0].Adopted || GroupDigest(found[0].Values) != check.Groups[name] {
			return routerError("回读校验失败：IP 分组「" + name + "」内容与计划不一致")
		}
	}
	for _, c := range check.Rules {
		var candidates []*GroupRule
		for _, r := range s.Rules {
			if r.Func == c.Func && r.Enabled && (c.ID != "" && r.ID == c.ID || c.ID == "" && r.Comment == c.Comment) &&
				(c.Func != "nat_rule" || r.Action == "filter") {
				candidates = append(candidates, r)
			}
		}
		if c.Func == "stream_ipport" && len(candidates) > 1 {
			candidates = candidates[:1]
		}
		if !slices.ContainsFunc(candidates, func(r *GroupRule) bool { return ruleMatches(s.Groups, r, c) }) {
			label := "NAT 免转换规则"
			if c.Func == "stream_ipport" {
				label = "策略路由"
			}
			return routerError("回读校验失败：" + label + "「" + c.Comment + "」没有按计划只引用 ConfigFlow 的 IP 分组")
		}
	}
	for _, name := range check.Removed {
		if slices.ContainsFunc(byName[name], func(g *IkuaiGroup) bool { return g.Role != "" }) {
			return routerError("回读校验失败：应删除的 IP 分组「" + name + "」仍然存在")
		}
	}
	return nil
}

func ruleMatches(groups []*IkuaiGroup, rule *GroupRule, c GroupRuleCheck) bool {
	if len(rule.Custom) > 0 {
		return false
	}
	var names []string
	for _, ref := range rule.Objects {
		switch kind, g := ClassifyRef(groups, ref); {
		case kind == RefOwned:
			if !ref.is("gp_name", g.Name) {
				return false
			}
			names = append(names, g.Name)
		case kind == RefStale || !c.KeepForeign:
			return false
		}
	}
	return sameNames(names, c.Groups)
}
