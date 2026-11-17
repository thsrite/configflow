package routers

import (
	"configflow/internal/devhttp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func joinPolicyMessages(messages []string) string { return strings.Join(messages, "；") }

var policyVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)`)

func policyManifest(raw json.RawMessage) (*ROSPolicyManifest, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var manifest ROSPolicyManifest
	if devhttp.DecodeJSON(raw, &manifest) != nil {
		return nil, routerError(messageOperationFormat)
	}
	seen := map[[2]string]bool{}
	for _, item := range manifest.Resources {
		key := [2]string{item.Path, item.ID}
		if !slices.Contains(policyPaths, item.Path) || item.ID == "" || item.Config == nil || seen[key] {
			return nil, routerError(messagePolicyDuplicateID)
		}
		seen[key] = true
	}
	return &manifest, nil
}

// SnapshotROSPolicy reads the complete preview dependency set, including
// occupied names without downloading unrelated address-list members.
func SnapshotROSPolicy(ctx context.Context, d *RouterOS) (*ROSPolicySnapshot, error) {
	return d.snapshotPolicy(ctx, true)
}

func (d *RouterOS) snapshotPolicy(ctx context.Context, reserve bool) (*ROSPolicySnapshot, error) {
	result := ROSPolicySnapshot{}
	for _, path := range policyPaths {
		if path == rosAddresses {
			continue
		}
		rows, err := d.list(ctx, path)
		if err != nil {
			return nil, err
		}
		result[path] = rows
	}
	names := referencedAddressLists(result[rosNAT])
	for name := range d.knownAddresses() {
		names = append(names, name)
	}
	lists, err := d.ReadAddresses(ctx, names)
	if err != nil {
		return nil, err
	}
	result[rosAddresses] = []Row{}
	for _, name := range slices.Sorted(maps.Keys(lists)) {
		result[rosAddresses] = append(result[rosAddresses], lists[name]...)
	}
	manifest, err := policyManifest(d.policyManifest)
	if err != nil {
		return nil, err
	}
	present := rowIDs(result[rosAddresses])
	missing := []string{}
	if manifest != nil {
		for _, entry := range manifest.Resources {
			if entry.Path == rosAddresses && !present[entry.ID] {
				missing = append(missing, entry.ID)
			}
		}
	}
	located, err := d.LocateAddresses(ctx, missing)
	if err != nil {
		return nil, err
	}
	result[rosAddresses] = append(result[rosAddresses], located...)
	if reserve {
		occupied, err := d.OccupiedAddressNames(ctx, policyNamePatterns, names)
		if err != nil {
			return nil, err
		}
		result[rosListNames] = []Row{}
		for _, name := range occupied {
			result[rosListNames] = append(result[rosListNames], Row{".id": "name:" + name, "occupied-list": name})
		}
	}
	value, err := d.get(ctx, rosSettings, rosTimeout)
	if err != nil {
		return nil, err
	}
	settings := asRow(value)
	if settings == nil {
		return nil, routerError(messagePolicySettingsUnreadable)
	}
	settings[".id"] = "routing-settings"
	result[rosSettings] = []Row{settings}
	if !settings.has("policy-rules") {
		value, err := d.get(ctx, "/system/resource", rosTimeout)
		if err == nil {
			if fields := strings.Fields(asRow(value).text("version")); len(fields) > 0 {
				settings["routeros-version"] = fields[0]
			}
		}
	}
	return &result, nil
}

func stablePolicy(snapshot ROSPolicySnapshot) ROSPolicySnapshot {
	result := ROSPolicySnapshot{}
	for path, rows := range snapshot {
		result[path] = []Row{}
		for _, row := range rows {
			if path == rosAddresses && row.flag("dynamic") {
				continue
			}
			value := configROSRow(row)
			value[".id"] = row[".id"]
			result[path] = append(result[path], value)
		}
	}
	return result
}

// OwnedROSPolicyRows proves exact IDs and complete stable configurations;
// missing resources can be repaired, changed or reused IDs are never adopted.
func OwnedROSPolicyRows(raw json.RawMessage, snap *ROSPolicySnapshot) ([]ROSResource, error) {
	manifest, err := policyManifest(raw)
	if err != nil {
		return nil, err
	}
	owned := []ROSResource{}
	if manifest == nil {
		return owned, nil
	}
	if snap == nil {
		return nil, routerError(messagePolicyUnreadable)
	}
	indexed := map[string]map[string][]Row{}
	for path, rows := range *snap {
		indexed[path] = map[string][]Row{}
		for _, row := range rows {
			indexed[path][row.id()] = append(indexed[path][row.id()], row)
		}
	}
	for _, entry := range manifest.Resources {
		matches := indexed[entry.Path][entry.ID]
		if len(matches) > 1 {
			return nil, routerError(messagePolicyDuplicateID)
		}
		if len(matches) == 1 {
			if !sameRow(configROSRow(matches[0]), configROSRow(entry.Config)) {
				return nil, routerError(fmt.Sprintf(messagePolicyExternalEdit, entry.Path, entry.ID))
			}
			owned = append(owned, ROSResource{entry.Path, entry.ID, cloneAddressRow(entry.Config)})
		}
	}
	return owned, nil
}

func policyKeys(resources []ROSResource) map[[2]string]bool {
	keys := map[[2]string]bool{}
	for _, item := range resources {
		keys[[2]string{item.Path, item.ID}] = true
	}
	return keys
}
func checkPolicyReferences(owned []ROSResource, live ROSPolicySnapshot) error {
	keys := policyKeys(owned)
	names := map[string]bool{}
	for _, item := range owned {
		field := ""
		switch item.Path {
		case rosTables:
			field = "name"
		case rosAddresses:
			field = "list"
		case rosMangle:
			if !item.Config.is("chain", "prerouting") {
				field = "chain"
			}
		}
		if name := item.Config.text(field); name != "" {
			names[name] = true
		}
	}
	refs := stringSet("table", "routing-table", "new-routing-mark", "routing-mark", "src-address-list", "dst-address-list", "jump-target", "chain", "list", "name")
	for _, path := range slices.Sorted(maps.Keys(live)) {
		for _, row := range live[path] {
			if keys[[2]string{path, row.id()}] {
				continue
			}
			for _, key := range slices.Sorted(maps.Keys(row)) {
				value := row.text(key)
				if refs[key] && names[strings.TrimLeft(value, "!")] {
					return routerError(messagePolicyReference + value)
				}
				if key == "gateway" {
					for name := range names {
						if strings.Contains(value, "@"+name) {
							return routerError(messagePolicyTableReference)
						}
					}
				}
			}
		}
	}
	return nil
}

func checkPolicyOrder(live ROSPolicySnapshot) error {
	settings := live[rosSettings]
	if len(settings) == 1 && !settings[0].has("policy-rules") {
		version := settings[0].text("routeros-version")
		major, minor := 0, 0
		parts := policyVersionPattern.FindStringSubmatch(version)
		if len(parts) == 3 {
			major, _ = strconv.Atoi(parts[1])
			minor, _ = strconv.Atoi(parts[2])
		}
		if major > 7 || major == 7 && minor >= 22 {
			return routerError(fmt.Sprintf(messagePolicyOrderMissing, version))
		}
		return routerError(fmt.Sprintf(messagePolicyVersion, firstNonEmpty(version, messagePolicyUnknownVersion)))
	}
	order := []string{}
	if len(settings) != 1 {
		return routerError(messagePolicyOrderUnknown)
	}
	switch value := settings[0]["policy-rules"].(type) {
	case string:
		for _, part := range strings.Split(value, ",") {
			order = append(order, strings.TrimSpace(part))
		}
	case []any:
		for _, part := range value {
			s, ok := part.(string)
			if !ok {
				return routerError(messagePolicyOrderUnknown)
			}
			order = append(order, s)
		}
	case []string:
		order = slices.Clone(value)
	default:
		return routerError(messagePolicyOrderUnknown)
	}
	allowed := stringSet("mangle", "main", "user", "vrf-lookup", "vrf-unreach", "local")
	seen := map[string]bool{}
	for _, item := range order {
		if seen[item] || !allowed[item] {
			return routerError(messagePolicyOrderUnknown)
		}
		seen[item] = true
	}
	if !seen["mangle"] || !seen["main"] {
		return routerError(messagePolicyOrderUnknown)
	}
	for _, name := range []string{"main", "user"} {
		if at := slices.Index(order, name); at >= 0 && at < slices.Index(order, "mangle") {
			return routerError(messagePolicyOrderLower)
		}
	}
	return nil
}

func checkPolicyEgress(row Row, gateway, iface string) error {
	if row == nil || !row.enabled() || !rosTruthy(row["active"]) || row.text("immediate-gw") != gateway+"%"+iface || row.flag("invalid", "blackhole", "ecmp") {
		return routerError(messagePolicyEgressChanged)
	}
	return nil
}
func policyHealth(raw json.RawMessage, live ROSPolicySnapshot) error {
	manifest, err := policyManifest(raw)
	if err != nil || manifest == nil {
		return err
	}
	byPath := map[string]map[string]Row{}
	for path, rows := range live {
		byPath[path], _ = indexROSRows(rows)
	}
	for _, path := range []string{rosMangle, rosNAT} {
		expected, present := []string{}, []string{}
		keys := map[string]bool{}
		for _, item := range manifest.Resources {
			if item.Path == path {
				expected = append(expected, item.ID)
				keys[item.ID] = true
			}
		}
		for _, row := range live[path] {
			if keys[row.id()] {
				present = append(present, row.id())
			}
		}
		if len(present) == len(expected) && !slices.Equal(present, expected) {
			return routerError(messagePolicyRuleOrder)
		}
	}
	for _, item := range manifest.Resources {
		row := byPath[item.Path][item.ID]
		if row == nil {
			continue
		}
		if row.flag("invalid") {
			return routerError(fmt.Sprintf(messagePolicyNotActive, item.Path, item.ID))
		}
		if item.Path == rosRoutes {
			if !rosTruthy(row["active"]) {
				return routerError(messagePolicyRouteInactive)
			}
			if manifest.Spec != nil && manifest.Spec.EgressInterface != "" {
				if err := checkPolicyEgress(row, manifest.Spec.Gateway, manifest.Spec.EgressInterface); err != nil {
					return err
				}
			}
		}
		if item.Path == rosNAT {
			for _, other := range live[rosNAT][:indexOfRow(live[rosNAT], item.ID)] {
				if other.enabled() && other.is("chain", "srcnat") && !slices.Contains(nonTranslating, other.text("action")) {
					return routerError(messagePolicyNATPreceded)
				}
			}
		}
	}
	return nil
}

// ROSPolicyObject is the read-only public projection of a recorded resource.
type ROSPolicyObject struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	Editor            string `json:"editor"`
	ConfiguredAddress string `json:"configured_address"`
	Fields            Row    `json:"fields"`
}

var policyPublicFields = stringSet("list", "address", "name", "fib", "chain", "action", "src-address", "dst-address", "src-address-list", "dst-address-list", "dst-address-type", "routing-mark", "new-routing-mark", "jump-target", "passthrough", "routing-table", "gateway", "immediate-gw", "distance", "scope", "target-scope", "in-interface", "out-interface", "disabled", "active", "invalid", "comment", "protocol", "src-port", "dst-port", "packets", "bytes")

// ReadPolicyObjects resolves roles using saved references, never live names.
// It exposes only public fields and never changes the ownership baseline.
func ReadPolicyObjects(ctx context.Context, d *RouterOS, raw json.RawMessage) ([]ROSPolicyObject, error) {
	manifest, err := policyManifest(raw)
	if err != nil {
		return nil, err
	}
	result := []ROSPolicyObject{}
	if manifest == nil {
		return result, nil
	}
	kinds := map[string]string{rosAddresses: "addresses", rosTables: "tables", rosMangle: "mangle", rosRoutes: "routes", rosNAT: "nat"}
	roles := map[string]string{}
	for _, entry := range manifest.Resources {
		if entry.Path == rosMangle && entry.Config.is("chain", "prerouting") && entry.Config.is("action", "jump") {
			roles[entry.Config.text("src-address-list")] = "source"
			roles[entry.Config.text("dst-address-list")] = "destination"
		}
	}
	live := map[string][]Row{}
	for _, entry := range manifest.Resources {
		if kinds[entry.Path] != "" {
			if _, ok := live[entry.Path]; !ok {
				live[entry.Path], err = d.list(ctx, entry.Path)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	for _, entry := range manifest.Resources {
		kind := kinds[entry.Path]
		if kind == "" {
			continue
		}
		row := findRow(live[entry.Path], entry.ID)
		status := "missing"
		shown := entry.Config
		if row != nil {
			shown = row
			status = "matched"
			if !sameRow(configROSRow(row), configROSRow(entry.Config)) {
				status = "changed"
			}
		}
		if entry.Path == rosTables {
			shown = Row(ComparableValues(rosTables, shown))
		}
		editor := "settings"
		if entry.Path == rosAddresses {
			editor = firstNonEmpty(roles[entry.Config.text("list")], editor)
		}
		fields := selectFields([]Row{shown}, policyPublicFields)[0]
		result = append(result, ROSPolicyObject{entry.ID, kind, status, editor, entry.Config.text("address"), fields})
	}
	return result, nil
}
