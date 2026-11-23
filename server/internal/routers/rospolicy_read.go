package routers

import (
	"configflow/internal/devhttp"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
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
		return nil, device.Error(device.MessageOperationFormat)
	}
	seen := map[[2]string]bool{}
	for _, item := range manifest.Resources {
		key := [2]string{item.Path, item.ID}
		if !slices.Contains(policyPaths, item.Path) || item.ID == "" || item.Config == nil || seen[key] {
			return nil, device.Error(messagePolicyDuplicateID)
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
		if path == ros.PathAddresses {
			continue
		}
		rows, err := d.List(ctx, path)
		if err != nil {
			return nil, err
		}
		result[path] = rows
	}
	names := ros.ReferencedAddressLists(result[ros.PathNAT])
	for name := range d.KnownAddresses() {
		names = append(names, name)
	}
	lists, err := d.ReadAddresses(ctx, names)
	if err != nil {
		return nil, err
	}
	result[ros.PathAddresses] = []Row{}
	for _, name := range slices.Sorted(maps.Keys(lists)) {
		result[ros.PathAddresses] = append(result[ros.PathAddresses], lists[name]...)
	}
	manifest, err := policyManifest(d.PolicyManifest())
	if err != nil {
		return nil, err
	}
	present := ros.RowIDs(result[ros.PathAddresses])
	missing := []string{}
	if manifest != nil {
		for _, entry := range manifest.Resources {
			if entry.Path == ros.PathAddresses && !present[entry.ID] {
				missing = append(missing, entry.ID)
			}
		}
	}
	located, err := d.LocateAddresses(ctx, missing)
	if err != nil {
		return nil, err
	}
	result[ros.PathAddresses] = append(result[ros.PathAddresses], located...)
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
	value, err := d.Get(ctx, rosSettings, ros.Timeout)
	if err != nil {
		return nil, err
	}
	settings := ros.AsRow(value)
	if settings == nil {
		return nil, device.Error(messagePolicySettingsUnreadable)
	}
	// A null lookup order is unknown, not missing.
	ros.MarkNulls(settings, value, []string{"policy-rules"})
	settings[".id"] = "routing-settings"
	result[rosSettings] = []Row{settings}
	if !settings.Has("policy-rules") {
		value, err := d.Get(ctx, "/system/resource", ros.Timeout)
		if err == nil {
			if fields := strings.Fields(ros.AsRow(value).Text("version")); len(fields) > 0 {
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
			if path == ros.PathAddresses && row.Flag("dynamic") {
				continue
			}
			value := ros.ConfigROSRow(row)
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
		return nil, device.Error(messagePolicyUnreadable)
	}
	indexed := map[string]map[string][]Row{}
	for path, rows := range *snap {
		indexed[path] = map[string][]Row{}
		for _, row := range rows {
			indexed[path][row.ID()] = append(indexed[path][row.ID()], row)
		}
	}
	for _, entry := range manifest.Resources {
		matches := indexed[entry.Path][entry.ID]
		if len(matches) > 1 {
			return nil, device.Error(messagePolicyDuplicateID)
		}
		if len(matches) == 1 {
			if !ros.SameRow(ros.ConfigROSRow(matches[0]), ros.ConfigROSRow(entry.Config)) {
				return nil, device.Error(fmt.Sprintf(messagePolicyExternalEdit, entry.Path, entry.ID))
			}
			owned = append(owned, ROSResource{Path: entry.Path, ID: entry.ID, Config: ros.CloneRow(entry.Config)})
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
		case ros.PathTables:
			field = "name"
		case ros.PathAddresses:
			field = "list"
		case ros.PathMangle:
			if !item.Config.Is("chain", "prerouting") {
				field = "chain"
			}
		}
		if name := item.Config.Text(field); name != "" {
			names[name] = true
		}
	}
	refs := device.StringSet("table", "routing-table", "new-routing-mark", "routing-mark", "src-address-list", "dst-address-list", "jump-target", "chain", "list", "name")
	for _, path := range slices.Sorted(maps.Keys(live)) {
		for _, row := range live[path] {
			if keys[[2]string{path, row.ID()}] {
				continue
			}
			for _, key := range slices.Sorted(maps.Keys(row)) {
				value := row.Text(key)
				if refs[key] && names[strings.TrimLeft(value, "!")] {
					return device.Error(messagePolicyReference + value)
				}
				if key == "gateway" {
					for name := range names {
						if strings.Contains(value, "@"+name) {
							return device.Error(messagePolicyTableReference)
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
	if len(settings) == 1 && !settings[0].Has("policy-rules") {
		version := settings[0].Text("routeros-version")
		major, minor := 0, 0
		parts := policyVersionPattern.FindStringSubmatch(version)
		if len(parts) == 3 {
			major, _ = strconv.Atoi(parts[1])
			minor, _ = strconv.Atoi(parts[2])
		}
		if major > 7 || major == 7 && minor >= 22 {
			return device.Error(fmt.Sprintf(messagePolicyOrderMissing, version))
		}
		return device.Error(fmt.Sprintf(messagePolicyVersion, device.FirstNonEmpty(version, messagePolicyUnknownVersion)))
	}
	if len(settings) != 1 {
		return device.Error(messagePolicyOrderUnknown)
	}
	value, present := settings[0]["policy-rules"]
	order, ok := ros.TextList(value)
	if !present || !ok {
		return device.Error(messagePolicyOrderUnknown)
	}
	allowed := device.StringSet("mangle", "main", "user", "vrf-lookup", "vrf-unreach", "local")
	seen := map[string]bool{}
	for _, item := range order {
		if seen[item] || !allowed[item] {
			return device.Error(messagePolicyOrderUnknown)
		}
		seen[item] = true
	}
	if !seen["mangle"] || !seen["main"] {
		return device.Error(messagePolicyOrderUnknown)
	}
	for _, name := range []string{"main", "user"} {
		if at := slices.Index(order, name); at >= 0 && at < slices.Index(order, "mangle") {
			return device.Error(messagePolicyOrderLower)
		}
	}
	return nil
}

func checkPolicyEgress(row Row, gateway, iface string) error {
	if row == nil || !row.Enabled() || !ros.Flag(row["active"]) || row.Text("immediate-gw") != gateway+"%"+iface || row.Flag("invalid", "blackhole", "ecmp") {
		return device.Error(messagePolicyEgressChanged)
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
	for _, path := range []string{ros.PathMangle, ros.PathNAT} {
		expected, present := []string{}, []string{}
		keys := map[string]bool{}
		for _, item := range manifest.Resources {
			if item.Path == path {
				expected = append(expected, item.ID)
				keys[item.ID] = true
			}
		}
		for _, row := range live[path] {
			if keys[row.ID()] {
				present = append(present, row.ID())
			}
		}
		if len(present) == len(expected) && !slices.Equal(present, expected) {
			return device.Error(messagePolicyRuleOrder)
		}
	}
	for _, item := range manifest.Resources {
		row := byPath[item.Path][item.ID]
		if row == nil {
			continue
		}
		if row.Flag("invalid") {
			return device.Error(fmt.Sprintf(messagePolicyNotActive, item.Path, item.ID))
		}
		if item.Path == ros.PathRoutes {
			if !ros.Flag(row["active"]) {
				return device.Error(messagePolicyRouteInactive)
			}
			if manifest.Spec != nil && manifest.Spec.EgressInterface != "" {
				if err := checkPolicyEgress(row, manifest.Spec.Gateway, manifest.Spec.EgressInterface); err != nil {
					return err
				}
			}
		}
		if item.Path == ros.PathNAT {
			for _, other := range live[ros.PathNAT][:ros.IndexOfRow(live[ros.PathNAT], item.ID)] {
				if other.Enabled() && other.Is("chain", "srcnat") && !slices.Contains(ros.NonTranslating, other.Text("action")) {
					return device.Error(messagePolicyNATPreceded)
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

var policyPublicFields = device.StringSet("list", "address", "name", "fib", "chain", "action", "src-address", "dst-address", "src-address-list", "dst-address-list", "dst-address-type", "routing-mark", "new-routing-mark", "jump-target", "passthrough", "routing-table", "gateway", "immediate-gw", "distance", "scope", "target-scope", "in-interface", "out-interface", "disabled", "active", "invalid", "comment", "protocol", "src-port", "dst-port", "packets", "bytes")

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
	kinds := map[string]string{ros.PathAddresses: "addresses", ros.PathTables: "tables", ros.PathMangle: "mangle", ros.PathRoutes: "routes", ros.PathNAT: "nat"}
	roles := map[string]string{}
	for _, entry := range manifest.Resources {
		if entry.Path == ros.PathMangle && entry.Config.Is("chain", "prerouting") && entry.Config.Is("action", "jump") {
			for _, field := range [][2]string{{"src-address-list", "source"}, {"dst-address-list", "destination"}} {
				if name := entry.Config.Text(field[0]); name != "" {
					roles[name] = field[1]
				}
			}
		}
	}
	live := map[string][]Row{}
	for _, entry := range manifest.Resources {
		if kinds[entry.Path] != "" {
			if _, ok := live[entry.Path]; !ok {
				live[entry.Path], err = d.List(ctx, entry.Path)
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
		row := ros.FindRow(live[entry.Path], entry.ID)
		status := "missing"
		shown := entry.Config
		if row != nil {
			shown = row
			status = "matched"
			if !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(entry.Config)) {
				status = "changed"
			}
		}
		if entry.Path == ros.PathTables {
			shown = ros.ComparableValues(ros.PathTables, shown)
		}
		editor := "settings"
		if entry.Path == ros.PathAddresses {
			editor = device.FirstNonEmpty(roles[entry.Config.Text("list")], editor)
		}
		fields := ros.SelectFields([]Row{shown}, policyPublicFields)[0]
		result = append(result, ROSPolicyObject{entry.ID, kind, status, editor, entry.Config.Text("address"), fields})
	}
	return result, nil
}
