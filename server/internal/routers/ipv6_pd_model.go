package routers

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	pdScripts       = "/system/script"
	pdSchedulers    = "/system/scheduler"
	pdClients       = "/ipv6/dhcp-client"
	pdLists         = "/ipv6/firewall/address-list"
	pdPrefixes      = "/ipv6/nd/prefix"
	pdWatch         = "cf-pd-watch"
	pdPoll          = "cf-pd-poll"
	pdLast          = "cf-pd-last"
	pdRollback      = "cf-pd-rollback"
	pdHold          = 3600
	pdPolicy        = "read,write,test"
	pdEvent         = "/system script run cf-pd-watch"
	pdHook          = `:if ($"pd-valid" = 1) do={:for i from=1 to=10 do={:delay 1s; /system script run cf-pd-watch}}`
	pdScriptComment = "cf-pd: IPv6 prefix change guard (ConfigFlow)"
)

//go:embed ipv6_pd_watch.rsc
var pdWatchTemplate string
var pdDeprecated = regexp.MustCompile(`^cf-pd: deprecated until [0-9]+$`)
var pdParameter = regexp.MustCompile(`^:local (lan|pool) "([^"\\]*)"$|^:local (hold) ([0-9]+)$`)

func pdName(name string) bool {
	if len([]rune(name)) < 1 || len([]rune(name)) > 64 {
		return false
	}
	return !strings.ContainsAny(name, "\"\\$;{}[]") && !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// IPv6PDWatchSource returns the current cf-pd script for safe literal parameters.
func IPv6PDWatchSource(lan, pool string) (string, error) {
	if !pdName(lan) || !pdName(pool) {
		return "", routerError(messagePDScriptParameters)
	}
	return strings.NewReplacer("@@LAN@@", lan, "@@POOL@@", pool).Replace(pdWatchTemplate), nil
}
func pdNormalize(text string) string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
func pdParameters(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(pdNormalize(text), "\n") {
		m := pdParameter.FindStringSubmatch(line)
		if len(m) > 0 {
			key, value := m[1], m[2]
			if key == "" {
				key, value = m[3], m[4]
			}
			if _, ok := out[key]; !ok {
				out[key] = value
			}
		}
	}
	return out
}
func pdIsWatch(text string) bool {
	s := pdNormalize(text)
	return strings.Contains(s, pdLast) && strings.Contains(s, ":local lan") && strings.Contains(s, ":local pool")
}
func pdPolicies(text string) []string {
	out := []string{}
	for _, p := range strings.Split(text, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	out = uniqueStrings(out)
	slices.Sort(out)
	return out
}

func pdSettings(r *Router) PDGuard {
	if r != nil && r.IPv6PDGuard != nil {
		return *r.IPv6PDGuard
	}
	return PDGuard{}
}

// IPv6PDGuardRelevant avoids device reads while disabled and unowned.
func IPv6PDGuardRelevant(r *Router) bool {
	return r != nil && r.Type == "routeros" && (pdSettings(r).Enabled || hasStoredState(r.Extra["ipv6_pd_guard_state"]))
}

// IPv6PDSpec identifies the single pool, LAN and DHCPv6 client to watch.
type IPv6PDSpec struct {
	LAN             string `json:"lan"`
	Pool            string `json:"pool"`
	Client          string `json:"client"`
	ClientInterface string `json:"client_interface"`
	Hold            int    `json:"hold"`
}

// IPv6PDResource records a table-qualified ID and its stable proof. Config is
// optional only when reading a legacy Python ID-only manifest. For the hook
// role it only records the dhcp-client at write time: the client is the user's
// object, so later ownership checks identify it by ID alone.
type IPv6PDResource struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	ID     string `json:"id"`
	Config Row    `json:"config,omitempty"`
}

// IPv6PDManifest is compatible with legacy version-one publication records.
type IPv6PDManifest struct {
	Version   int              `json:"version"`
	Spec      *IPv6PDSpec      `json:"spec"`
	Resources []IPv6PDResource `json:"resources"`
}

// IPv6PDItem is a role's installation or withdrawal checklist item.
type IPv6PDItem struct {
	Role   string `json:"role"`
	Object string `json:"object"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// IPv6PDGuardReport is the public prefix-change guard checklist.
type IPv6PDGuardReport struct {
	Enabled        bool         `json:"enabled"`
	Status         string       `json:"status"`
	Message        string       `json:"message"`
	LAN            string       `json:"lan"`
	Pool           string       `json:"pool"`
	Client         string       `json:"client"`
	Hold           int          `json:"hold"`
	Managed        bool         `json:"managed"`
	RollbackScript bool         `json:"rollback_script"`
	Items          []IPv6PDItem `json:"items"`
	Blockers       []string     `json:"blockers"`
	Warnings       []string     `json:"warnings"`
}

// IPv6PDChange attaches a preview role to a shared transactional change.
type IPv6PDChange struct {
	Role string `json:"role"`
	ROSChange
}

// IPv6PDGuardSync is a preview-bound installation or withdrawal.
type IPv6PDGuardSync struct {
	OpHead
	Enabled  bool            `json:"enabled"`
	Gateway  string          `json:"gateway"`
	Spec     *IPv6PDSpec     `json:"spec"`
	Previous json.RawMessage `json:"previous"`
	Changes  []IPv6PDChange  `json:"changes"`
	Owned    []string        `json:"owned"`
}

// IPv6PDGuardUndo compensates in order using only stable per-write proofs.
type IPv6PDGuardUndo struct {
	Kind     string          `json:"kind"`
	Changes  []ROSChange     `json:"changes"`
	Previous json.RawMessage `json:"previous"`
}

// IPv6PDGuardCheck verifies the template and ownership or table-qualified removal.
type IPv6PDGuardCheck struct {
	Enabled  bool        `json:"enabled"`
	Spec     *IPv6PDSpec `json:"spec,omitempty"`
	Gateway  string      `json:"gateway,omitempty"`
	Owned    []string    `json:"owned"`
	Withdraw bool        `json:"withdraw"`
	Removed  [][2]string `json:"removed,omitempty"`
}

// IPv6PDEvidence binds a plan to named objects and legacy owned IDs.
type IPv6PDEvidence struct {
	Spec       *IPv6PDSpec       `json:"spec"`
	Owned      map[string]string `json:"owned"`
	Scripts    []string          `json:"scripts"`
	Schedulers []string          `json:"schedulers"`
}

// IPv6PDGuardPlan supplies report data, operations and readback proof to R12.
type IPv6PDGuardPlan struct {
	IPv6PDGuardReport
	proofs   map[string]IPv6PDResource
	Ops      []Op              `json:"ops"`
	Check    *IPv6PDGuardCheck `json:"check"`
	Evidence *IPv6PDEvidence   `json:"evidence"`
}
type pdLive struct{ clients, addresses, scripts, schedulers, ipv4, last, prefixes []Row }

func pdRows(ctx context.Context, d *RouterOS, path string) ([]Row, error) {
	rows, e := d.list(ctx, path)
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if id, ok := r.str(".id"); !ok || !rosIDPattern.MatchString(id) || seen[id] {
			return nil, routerError(fmt.Sprintf(messagePDRowID, path))
		}
		seen[r.id()] = true
	}
	return rows, nil
}
func pdSnapshot(ctx context.Context, d *RouterOS, withdraw bool) (pdLive, error) {
	live := pdLive{}
	for _, p := range []struct {
		path string
		to   *[]Row
	}{{pdClients, &live.clients}, {rosIPv6Addresses, &live.addresses}, {pdScripts, &live.scripts}, {pdSchedulers, &live.schedulers}, {pdLists + "?list=" + pdLast, &live.last}} {
		rows, e := pdRows(ctx, d, p.path)
		if e != nil {
			return live, e
		}
		*p.to = rows
	}
	if withdraw {
		var e error
		live.prefixes, e = pdRows(ctx, d, pdPrefixes)
		if e != nil {
			return live, e
		}
	}
	return live, nil
}
func pdDerive(ctx context.Context, d *RouterOS, live *pdLive, gateway string) (*IPv6PDSpec, Row, []string, error) {
	fail := func(s string) (*IPv6PDSpec, Row, []string, error) { return nil, nil, nil, routerError(s) }
	clients := []Row{}
	for _, r := range live.clients {
		if ipv6Enabled(r) && slices.Contains(pdPolicies(r.text("request")), "prefix") {
			clients = append(clients, r)
		}
	}
	if len(clients) == 0 {
		return fail(messagePDNoClient)
	}
	if len(clients) > 1 {
		names := []string{}
		for _, r := range clients {
			names = append(names, rosTextDefault(r["interface"], r.id()))
		}
		return fail(fmt.Sprintf(messagePDManyClients, strings.Join(names, "、")))
	}
	client := clients[0]
	pool := client.text("pool-name")
	if pool == "" {
		return fail(fmt.Sprintf(messagePDNoPool, client.text("interface")))
	}
	if !pdName(pool) {
		return fail(fmt.Sprintf(messagePDPoolUnsafe, pool))
	}
	rows := []Row{}
	names := []string{}
	for _, r := range live.addresses {
		if r.is("from-pool", pool) && ipv6Enabled(r) {
			rows = append(rows, r)
			names = append(names, r.text("interface"))
		}
	}
	names = uniqueStrings(names)
	slices.Sort(names)
	if len(rows) == 0 {
		return fail(fmt.Sprintf(messagePDNoLAN, pool, pool))
	}
	if slices.Contains(names, "") {
		return fail(fmt.Sprintf(messagePDLANMissing, pool))
	}
	lan := names[0]
	warnings := []string{}
	if len(names) > 1 {
		if live.ipv4 == nil && d != nil {
			var e error
			live.ipv4, e = pdRows(ctx, d, rosIPv4Addresses)
			if e != nil {
				return nil, nil, nil, e
			}
		}
		lan = IPv6LAN(IPv6Snapshot{"ipv4_addresses": live.ipv4}, gateway)
		if !slices.Contains(names, lan) {
			return fail(fmt.Sprintf(messagePDManyLAN, pool, strings.Join(names, "、")))
		}
		others := slices.DeleteFunc(slices.Clone(names), func(s string) bool { return s == lan })
		warnings = append(warnings, fmt.Sprintf(messagePDSharedPool, pool, strings.Join(others, "、"), lan))
	}
	if !pdName(lan) {
		return fail(fmt.Sprintf(messagePDLANUnsafe, lan))
	}
	selected := []Row{}
	for _, r := range rows {
		if r.is("interface", lan) {
			selected = append(selected, r)
		}
	}
	if len(selected) != 1 {
		return fail(fmt.Sprintf(messagePDManyAddresses, lan, pool))
	}
	// ParsePrefix retains host bits and rejects scoped addresses, which cannot be a delegated LAN prefix.
	address, e := netip.ParsePrefix(selected[0].text("address"))
	if e != nil || !address.Addr().Is6() {
		return fail(fmt.Sprintf(messagePDAddress, lan))
	}
	if address.Bits() != 64 {
		return fail(fmt.Sprintf(messagePDLength, lan, address.Bits()))
	}
	if !policyFlag(selected[0]["advertise"]) {
		return fail(fmt.Sprintf(messagePDAdvertise, lan))
	}
	return &IPv6PDSpec{lan, pool, client.id(), client.text("interface"), pdHold}, client, warnings, nil
}
func pdNamed(rows []Row, name string) Row {
	for _, r := range rows {
		if r.is("name", name) {
			return r
		}
	}
	return nil
}

type pdState struct{ state, detail string }

func pdScriptState(row Row, spec *IPv6PDSpec) pdState {
	if row == nil {
		return pdState{"missing", messagePDNew}
	}
	source := row.text("source")
	if !pdIsWatch(source) {
		return pdState{"conflict", messagePDWatchConflict}
	}
	template, _ := IPv6PDWatchSource(spec.LAN, spec.Pool)
	same := pdNormalize(source) == pdNormalize(template)
	policy := slices.Equal(pdPolicies(row.text("policy")), pdPolicies(pdPolicy))
	if same && policy {
		return pdState{"ok", fmt.Sprintf(messagePDWatchOK, spec.LAN, spec.Pool, pdHold)}
	}
	found := pdParameters(source)
	diffs := []string{}
	for _, v := range [][3]string{{"lan", messagePDLANLabel, spec.LAN}, {"pool", messagePDPoolLabel, spec.Pool}} {
		if found[v[0]] != v[2] {
			x := found[v[0]]
			if x == "" {
				x = messagePDUnknown
			}
			diffs = append(diffs, fmt.Sprintf(messagePDParameterDiff, v[1], x, v[2]))
		}
	}
	if found["hold"] != strconv.Itoa(pdHold) {
		x := found["hold"]
		if x == "" {
			x = messagePDUnknown
		}
		diffs = append(diffs, fmt.Sprintf(messagePDHoldDiff, x, pdHold))
	}
	if len(diffs) == 0 && !same {
		diffs = append(diffs, messagePDTemplateDiff)
	}
	if !policy {
		v := row.text("policy")
		if v == "" {
			v = messagePDEmpty
		}
		diffs = append(diffs, fmt.Sprintf(messagePDPolicyDiff, v, pdPolicy))
	}
	return pdState{"update", strings.Join(diffs, "；")}
}
func pdSchedulerState(row Row) pdState {
	if row == nil {
		return pdState{"missing", messagePDPollNew}
	}
	if !strings.Contains(row.text("on-event"), pdWatch) {
		return pdState{"conflict", messagePDPollConflict}
	}
	diffs := []string{}
	if pdNormalize(row.text("on-event")) != pdEvent {
		diffs = append(diffs, messagePDEventDiff)
	}
	if !slices.Contains([]string{"30s", "00:00:30"}, row.text("interval")) {
		x := row.text("interval")
		if x == "" {
			x = messagePDEmpty
		}
		diffs = append(diffs, fmt.Sprintf(messagePDIntervalDiff, x))
	}
	if !row.is("start-time", "startup") {
		diffs = append(diffs, messagePDStartupDiff)
	}
	if !slices.Equal(pdPolicies(row.text("policy")), pdPolicies(pdPolicy)) {
		x := row.text("policy")
		if x == "" {
			x = messagePDEmpty
		}
		diffs = append(diffs, fmt.Sprintf(messagePDPolicyDiff, x, pdPolicy))
	}
	if policyFlag(row["disabled"]) {
		diffs = append(diffs, messagePDDisabled)
	}
	if len(diffs) > 0 {
		return pdState{"update", strings.Join(diffs, "；")}
	}
	return pdState{"ok", messagePDPollOK}
}
func pdHookState(row Row) pdState {
	s := pdNormalize(row.text("script"))
	switch s {
	case "":
		return pdState{"missing", messagePDHookNew}
	case pdNormalize(pdHook):
		return pdState{"ok", messagePDHookOK}
	case pdEvent:
		return pdState{"update", messagePDHookLegacy}
	}
	if slices.Contains(strings.Split(s, "\n"), pdNormalize(pdHook)) {
		return pdState{"shared", messagePDHookShared}
	}
	return pdState{"conflict", fmt.Sprintf(messagePDHookConflict, row.text("interface"), pdHook)}
}
func pdRolePath(role string) string {
	switch role {
	case "script":
		return pdScripts
	case "scheduler":
		return pdSchedulers
	case "hook":
		return pdClients
	}
	return ""
}
func pdRoleName(role string) string {
	switch role {
	case "script":
		return messagePDWatchName
	case "scheduler":
		return messagePDPollName
	case "hook":
		return messagePDHookName
	default:
		return messagePDStateName
	}
}
func pdOwned(d *RouterOS, raw json.RawMessage, live pdLive) (map[string]Row, error) {
	out := map[string]Row{}
	if !hasStoredState(raw) {
		return out, nil
	}
	var m IPv6PDManifest
	if json.Unmarshal(raw, &m) != nil || m.Version != 1 || m.Resources == nil {
		return nil, routerError(messagePDManifest)
	}
	if m.Spec == nil || !pdName(m.Spec.LAN) || !pdName(m.Spec.Pool) || !rosIDPattern.MatchString(m.Spec.Client) || m.Spec.Hold != pdHold {
		return nil, routerError(messagePDManifest)
	}
	// Presence matters: null/empty config is damaged proof, not a legacy ID-only
	// receipt. Validate it even if the referenced ID is currently absent.
	var fields struct {
		Resources []struct {
			Config json.RawMessage `json:"config"`
		} `json:"resources"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return nil, routerError(messagePDManifest)
	}
	seen := map[string]bool{}
	for i, entry := range m.Resources {
		if len(fields.Resources[i].Config) > 0 && len(entry.Config) == 0 {
			return nil, routerError(messagePDManifestObject)
		}
		path := pdRolePath(entry.Role)
		if path == "" || entry.Path != path || !rosIDPattern.MatchString(entry.ID) || seen[entry.Role] {
			return nil, routerError(messagePDManifestObject)
		}
		seen[entry.Role] = true
		rows := live.clients
		if entry.Role == "script" {
			rows = live.scripts
		} else if entry.Role == "scheduler" {
			rows = live.schedulers
		}
		row := findRow(rows, entry.ID)
		if row == nil {
			continue
		}
		if entry.Role == "hook" {
			// The dhcp-client is the user's object; ConfigFlow owns only its hook
			// line. pdHookState gates every write of that line and each write
			// still proves the preview snapshot. A recorded client config is no
			// ownership proof: use-peer-dns, a pool rename or disabling the client
			// when the prefix moves must not block both update and withdrawal.
			if payload(path, row) == nil {
				return nil, payloadError(path, row)
			}
			out[entry.Role] = row
			continue
		}
		if entry.Config == nil {
			// Legacy IDs have no byte-for-byte proof. Accept only a recognised template,
			// never a merely same-named script or scheduler that could execute foreign code.
			valid := false
			switch entry.Role {
			case "script":
				valid = m.Spec != nil && pdScriptState(row, m.Spec).state == "ok"
			case "scheduler":
				valid = pdSchedulerState(row).state == "ok"
			}
			if !valid {
				return nil, routerError(messagePDOwnership)
			}
		}
		if entry.Config != nil && !sameRow(d.ownershipSnapshot(path, row), entry.Config) {
			return nil, routerError(messagePDOwnership)
		}
		if entry.Role == "script" && !row.is("name", pdWatch) || entry.Role == "scheduler" && !row.is("name", pdPoll) {
			return nil, routerError(messagePDOwnership)
		}
		if payload(path, row) == nil {
			return nil, payloadError(path, row)
		}
		out[entry.Role] = row
	}
	return out, nil
}
