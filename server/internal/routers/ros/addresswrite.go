package ros

import (
	"configflow/internal/lifecycle"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Address batch limits follow the measured Client console limits.
const (
	// AddressBatchMembers caps one console batch.
	AddressBatchMembers = 200
	// AddressScriptBytes is the maximum UTF-8 script size.
	AddressScriptBytes = 16 * 1024
	// AddressRemoveBatch caps one comma-ID remove request.
	AddressRemoveBatch = 500
)

// NATStep is one guarded compensation record. A sweep temporarily stands in
// for fresh address IDs while the post-write read is unavailable.
type NATStep struct {
	Action         string          `json:"action"`
	Path           string          `json:"path,omitempty"`
	ID             string          `json:"id,omitempty"`
	Config         Row             `json:"config,omitempty"`
	Values         Row             `json:"values,omitempty"`
	Order          *int            `json:"order,omitempty"`
	FollowingOrder *int            `json:"following_order,omitempty"`
	List           string          `json:"list,omitempty"`
	Shared         Row             `json:"shared,omitempty"`
	Addresses      []string        `json:"addresses,omitempty"`
	Before         []string        `json:"before,omitempty"`
	Undo           json.RawMessage `json:"undo,omitempty"`
}

// MarshalJSON keeps the required empty before-ID list on sweep records.
func (s NATStep) MarshalJSON() ([]byte, error) {
	type plain NATStep
	if s.Action == "sweep" {
		before := append([]string{}, s.Before...)
		return json.Marshal(struct {
			plain
			Before []string `json:"before"`
		}{plain(s), before})
	}
	return json.Marshal(plain(s))
}

func StepResource(action string, item Resource) NATStep {
	return NATStep{Action: action, Path: item.Path, ID: item.ID, Config: CloneRow(item.Config)}
}

// ROSIDs tracks the IDs already known on each device table.
type ROSIDs map[string]map[string]bool

func (ids ROSIDs) Table(path string) map[string]bool {
	if ids[path] == nil {
		ids[path] = map[string]bool{}
	}
	return ids[path]
}
func ValuesMatch(path string, row, values Row) bool {
	return matches(ComparableValues(path, row), ComparableValues(path, values))
}

func (d *Client) ResourceRow(ctx context.Context, path, id string) (Row, error) {
	value, err := d.Get(ctx, path+"/"+EscapePathID(id), Timeout)
	if device.RouterErrorKind(err) == device.KindMissingResource {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row := AsRow(value)
	if row == nil || row.ID() != id {
		return nil, device.Error(fmt.Sprintf(messagePolicyReadFormat, path))
	}
	return row, nil
}
func (d *Client) GuardResource(ctx context.Context, item Resource) (Row, error) {
	row, err := d.ResourceRow(ctx, item.Path, item.ID)
	if err != nil {
		return nil, err
	}
	if row == nil || !SameRow(ConfigROSRow(row), ConfigROSRow(item.Config)) {
		return nil, device.Error(fmt.Sprintf(device.MessagePolicyChanged, item.Path, item.ID))
	}
	return row, nil
}
func WritableResource(path string, row Row) (Row, error) {
	if row.Flag("dynamic") || path == PathAddresses && row.Set("timeout") {
		return nil, device.Error(device.MessagePolicyDynamic)
	}
	if path == PathAddresses || path == PathNAT {
		values := ConfigROSRow(row)
		delete(values, "dynamic")
		delete(values, "invalid")
		return values, nil
	}
	if path == PathTables {
		return SelectFields([]Row{row}, device.StringSet("name", "fib", "disabled"))[0], nil
	}
	if path == PathMangle {
		values := ConfigROSRow(row)
		delete(values, "dynamic")
		return values, nil
	}
	values := Payload(path, row)
	if values == nil {
		return nil, device.Error(device.MessagePolicyDynamic)
	}
	return values, nil
}

var consoleAddressField = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validateAddressEntry(values Row) error {
	for key := range values {
		if !consoleAddressField.MatchString(key) {
			return device.Error(messageAddressInvalidField)
		}
	}
	if values.Text("address") == "" {
		return device.Error(messageAddressInvalidField)
	}
	return nil
}

func addressParams(shared Row) string {
	keys := []string{}
	for _, key := range []string{"list", "comment", "disabled"} {
		if _, ok := shared[key]; ok {
			keys = append(keys, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(shared)) {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	parts := []string{}
	for _, key := range keys {
		value := ScriptString(shared[key])
		if key == "disabled" && (Comparable(key, shared[key]) == "true" || Comparable(key, shared[key]) == "false") {
			if Comparable(key, shared[key]) == "true" {
				value = "yes"
			} else {
				value = "no"
			}
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}
func (d *Client) runAddressBatches(ctx context.Context, shared Row, addresses []string) (int, string, error) {
	head := ":local n 0\n:local m \"\"\n:foreach a in={"
	tail := `} do={:onerror e in={/ip/firewall/address-list/add ` + addressParams(shared) + ` address=$a} do={:set n ($n+1); :if ($n=1) do={:set m ($a . " " . $e)}}}` + "\n" + `:put ("cf-done:" . $n . ":" . $m)`
	budget := AddressScriptBytes - len(head) - len(tail)
	batches := [][]string{}
	current := []string{}
	size := 0
	for _, address := range addresses {
		item := ScriptString(address)
		if len(item) > budget {
			return 0, "", device.Error(messageAddressOversize)
		}
		if len(current) > 0 && (size+len(item)+1 > budget || len(current) == AddressBatchMembers) {
			batches = append(batches, current)
			current = []string{}
			size = 0
		}
		current = append(current, item)
		size += len(item) + 1
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	failed, first := 0, ""
	d.InvalidateAddressReads()
	for _, batch := range batches {
		value, err := d.Request(ctx, "POST", "/execute", Row{"script": head + strings.Join(batch, ";") + tail, "as-string": ""}, Timeout)
		if err != nil {
			return failed, first, err
		}
		output := executeOutput(value)
		at := strings.LastIndex(output, "cf-done:")
		count, reason := "", ""
		if at >= 0 {
			count, reason, _ = strings.Cut(output[at+len("cf-done:"):], ":")
		}
		n, e := strconv.Atoi(strings.TrimSpace(count))
		if e != nil || n < 0 {
			return failed, first, device.Error(fmt.Sprintf(messageAddressIncomplete, d.SafeDeviceText(shortConsoleOutput(output))))
		}
		failed += n
		if first == "" {
			first = d.SafeDeviceText(strings.TrimSpace(reason))
		}
	}
	return failed, first, nil
}
func (d *Client) addressRows(ctx context.Context, name string) ([]Row, error) {
	return d.retryRead(ctx, func() ([]Row, error) { return d.FetchAddresses(ctx, name) })
}
func (d *Client) settledAddressRows(ctx context.Context, name string) ([]Row, error) {
	rows, err := d.addressRows(ctx, name)
	if err != nil {
		return nil, err
	}
	for range 10 {
		if err := WaitContext(ctx, time.Second); err != nil {
			return nil, err
		}
		latest, err := d.addressRows(ctx, name)
		if err != nil {
			return nil, err
		}
		if len(latest) == len(rows) {
			return latest, nil
		}
		rows = latest
	}
	return nil, device.RollbackError(fmt.Sprintf(messageAddressUncertain, name))
}

// CreateAddresses creates members in bounded console batches and journals only
// fresh, unique IDs with matching submitted values. It detaches cancellation.
func (d *Client) CreateAddresses(ctx context.Context, entries []Row, journal *[]NATStep, ids ROSIDs) ([]Resource, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.CreateAddressesWithProof(ctx, entries, journal, ids, nil)
}

// CreateAddressesWithProof creates address list entries and records created proofs in created.
func (d *Client) CreateAddressesWithProof(ctx context.Context, entries []Row, journal *[]NATStep, ids ROSIDs, created map[int]Resource) ([]Resource, error) {
	d.InvalidateAddressReads()
	type member struct {
		index  int
		values Row
	}
	type group struct {
		shared  Row
		members []member
	}
	groups := []group{}
	for i, values := range entries {
		if err := validateAddressEntry(values); err != nil {
			return nil, err
		}
		shared := CloneRow(values)
		delete(shared, "address")
		at := slices.IndexFunc(groups, func(g group) bool { return SameRow(g.shared, shared) })
		if at < 0 {
			groups = append(groups, group{shared: shared})
			at = len(groups) - 1
		}
		groups[at].members = append(groups[at].members, member{i, values})
	}
	resources := make([]Resource, len(entries))
	for _, g := range groups {
		name := g.shared.Text("list")
		if name == "" {
			return resources, device.Error(messageAddressNoList)
		}
		live, err := d.ReadAddresses(ctx, []string{name})
		if err != nil {
			return resources, err
		}
		before := RowIDs(live[name])
		addresses := []string{}
		for _, m := range g.members {
			addresses = append(addresses, m.values.Text("address"))
		}
		sweepIndex := len(*journal)
		*journal = append(*journal, NATStep{Action: "sweep", Path: PathAddresses, List: name, Shared: CloneRow(g.shared), Addresses: addresses, Before: slices.Sorted(maps.Keys(before))})
		failed, reason, failure := d.runAddressBatches(ctx, g.shared, addresses)
		var rows []Row
		if device.RouterErrorKind(failure) == device.KindUncertainWrite {
			rows, err = d.settledAddressRows(ctx, name)
		} else {
			rows, err = d.addressRows(ctx, name)
		}
		if err != nil {
			return resources, err
		}
		*journal = slices.Delete(*journal, sweepIndex, sweepIndex+1)
		wanted := map[string][]member{}
		keys := []string{}
		for _, m := range g.members {
			key := Comparable("address", m.values["address"])
			if _, ok := wanted[key]; !ok {
				keys = append(keys, key)
			}
			wanted[key] = append(wanted[key], m)
		}
		unknown := []Row{}
		counts := map[string]int{}
		fresh := map[string]int{}
		for _, row := range rows {
			counts[row.ID()]++
			if !before[row.ID()] && !ids.Table(PathAddresses)[row.ID()] {
				fresh[Comparable("address", row["address"])]++
			}
		}
		limits := map[string]int{}
		for key, members := range wanted {
			limits[key] = len(members)
		}
		for _, row := range rows {
			if before[row.ID()] || ids.Table(PathAddresses)[row.ID()] {
				continue
			}
			key := Comparable("address", row["address"])
			candidates := wanted[key]
			if counts[row.ID()] != 1 || fresh[key] > limits[key] || row.Flag("dynamic") || row.Set("timeout") || len(candidates) == 0 || !IDPattern.MatchString(row.ID()) || !ValuesMatch(PathAddresses, row, candidates[0].values) {
				unknown = append(unknown, row)
				continue
			}
			m := candidates[0]
			wanted[key] = candidates[1:]
			ids.Table(PathAddresses)[row.ID()] = true
			item := NewResource(PathAddresses, row)
			resources[m.index] = item
			*journal = append(*journal, StepResource("delete", item))
			if created != nil {
				created[m.index] = item
			}
		}
		if failure != nil {
			return resources, failure
		}
		if len(unknown) > 0 {
			return resources, device.Error(fmt.Sprintf(messageAddressUnknown, name))
		}
		missing := []string{}
		for _, key := range keys {
			for _, m := range wanted[key] {
				missing = append(missing, m.values.Text("address"))
			}
		}
		if len(missing) == 0 && failed == 0 {
			d.RememberAddresses(name, rows)
		}
		if failed > 0 || len(missing) > 0 {
			detail := ""
			if reason != "" {
				detail = fmt.Sprintf(messageAddressFirstError, reason)
			}
			return resources, device.Error(fmt.Sprintf(messageAddressFailed, name, max(failed, len(missing)), strings.Join(missing[:min(5, len(missing))], "、"), detail))
		}
	}
	return resources, nil
}
func (d *Client) removeAddresses(ctx context.Context, ids []string) error {
	d.InvalidateAddressReads()
	for start := 0; start < len(ids); start += AddressRemoveBatch {
		if _, err := d.Request(ctx, "POST", PathAddresses+"/remove", Row{".id": strings.Join(ids[start:min(start+AddressRemoveBatch, len(ids))], ",")}, Timeout); err != nil {
			return err
		}
	}
	return nil
}
func (d *Client) liveAddresses(ctx context.Context, items []Resource) (map[string]Row, error) {
	names := []string{}
	for _, item := range items {
		names = append(names, item.Config.Text("list"))
	}
	lists, err := d.ReadAddresses(ctx, names)
	if err != nil {
		return nil, err
	}
	live := map[string]Row{}
	for _, rows := range lists {
		for _, row := range rows {
			live[row.ID()] = row
		}
	}
	return live, nil
}

// forgetAddresses drops removed members from the lists ConfigFlow knows (or
// only expects), so the next read of those lists is again a digest proof.
func (d *Client) forgetAddresses(items []Resource, removed map[string]bool) {
	names := map[string]bool{}
	for _, item := range items {
		names[item.Config.Text("list")] = true
	}
	kept := func(rows []Row) []Row {
		return slices.DeleteFunc(slices.Clone(rows), func(row Row) bool { return removed[row.ID()] })
	}
	known := d.KnownAddresses()
	for name := range names {
		if rows, ok := known[name]; ok {
			d.RememberAddresses(name, kept(rows))
		} else if rows, ok := d.addressExpected[name]; ok {
			d.ExpectAddresses(name, kept(rows))
		}
	}
}

// DeleteAddresses proves every member against a fresh scoped read before
// journaling and removing any of them. Request cancellation cannot interrupt
// it; only the shutdown drain deadline can (lifecycle.Detach).
func (d *Client) DeleteAddresses(ctx context.Context, items []Resource, journal *[]NATStep) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	d.InvalidateAddressReads()
	live, err := d.liveAddresses(ctx, items)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Path != PathAddresses || item.Config == nil || item.ID == "" {
			return device.Error(device.MessageOperationFormat)
		}
		row := live[item.ID]
		if row == nil || !SameRow(ConfigROSRow(row), ConfigROSRow(item.Config)) {
			return device.Error(fmt.Sprintf(device.MessagePolicyChanged, item.Path, item.ID))
		}
		if _, err := WritableResource(PathAddresses, row); err != nil {
			return err
		}
	}
	removing := []string{}
	for _, item := range items {
		values, _ := WritableResource(PathAddresses, live[item.ID])
		step := StepResource("restore", item)
		step.Values = values
		*journal = append(*journal, step)
		removing = append(removing, item.ID)
	}
	if err := d.removeAddresses(ctx, removing); err != nil {
		return err
	}
	d.forgetAddresses(items, device.StringSet(removing...))
	live, err = d.liveAddresses(ctx, items)
	if err != nil {
		return err
	}
	for _, id := range removing {
		if live[id] != nil {
			return device.Error(device.MessagePolicyStillPresent)
		}
	}
	return nil
}

func stepsResources(steps []NATStep) []Resource {
	items := make([]Resource, len(steps))
	for i, s := range steps {
		items[i] = Resource{s.Path, s.ID, s.Config}
	}
	return items
}
func (d *Client) RestoreDeleteAddresses(ctx context.Context, steps []NATStep) error {
	d.InvalidateAddressReads()
	live, err := d.liveAddresses(ctx, stepsResources(steps))
	if err != nil {
		return err
	}
	// A fresh member missing from its own list may have been moved into another
	// one: that is an external edit to keep and report, never "already deleted".
	missing := []string{}
	for _, s := range steps {
		if live[s.ID] == nil {
			missing = append(missing, s.ID)
		}
	}
	located, err := d.LocateAddresses(ctx, missing)
	if err != nil {
		return err
	}
	moved := RowIDs(located)
	removing, changed := []string{}, ""
	for _, s := range steps {
		row := live[s.ID]
		if row == nil {
			if moved[s.ID] && changed == "" {
				changed = s.ID
			}
			continue
		}
		if !SameRow(ConfigROSRow(row), ConfigROSRow(s.Config)) || row.Flag("dynamic") || row.Set("timeout") {
			if changed == "" {
				changed = s.ID
			}
			continue
		}
		removing = append(removing, s.ID)
	}
	if err := d.removeAddresses(ctx, removing); err != nil {
		return err
	}
	live, err = d.liveAddresses(ctx, stepsResources(steps))
	if err != nil {
		return err
	}
	for _, id := range removing {
		if live[id] != nil {
			return device.Error(device.MessageRollbackDeletePresent)
		}
	}
	if changed != "" {
		return device.Error(fmt.Sprintf(device.MessagePolicyChanged, PathAddresses, changed))
	}
	return nil
}
func (d *Client) RestoreAddressSweep(ctx context.Context, s NATStep) error {
	rows, err := d.addressRows(ctx, s.List)
	if err != nil {
		return err
	}
	before := device.StringSet(s.Before...)
	wanted := map[string][]Row{}
	for _, address := range s.Addresses {
		values := CloneRow(s.Shared)
		values["address"] = address
		key := Comparable("address", address)
		wanted[key] = append(wanted[key], values)
	}
	removing := []string{}
	counts := map[string]int{}
	fresh := map[string]int{}
	limits := map[string]int{}
	for key, values := range wanted {
		limits[key] = len(values)
	}
	for _, row := range rows {
		counts[row.ID()]++
		if !before[row.ID()] {
			fresh[Comparable("address", row["address"])]++
		}
	}
	for _, row := range rows {
		key := Comparable("address", row["address"])
		candidates := wanted[key]
		if counts[row.ID()] != 1 || fresh[key] > limits[key] || before[row.ID()] || !IDPattern.MatchString(row.ID()) || row.Flag("dynamic") || row.Set("timeout") || len(candidates) == 0 || !ValuesMatch(PathAddresses, row, candidates[0]) {
			continue
		}
		wanted[key] = candidates[1:]
		removing = append(removing, row.ID())
	}
	if err := d.removeAddresses(ctx, removing); err != nil {
		return err
	}
	latest, err := d.addressRows(ctx, s.List)
	if err != nil {
		return err
	}
	gone := device.StringSet(removing...)
	for _, row := range latest {
		if gone[row.ID()] {
			return device.Error(device.MessageRollbackDeletePresent)
		}
	}
	return nil
}
func (d *Client) RestoreAddresses(ctx context.Context, steps []NATStep, ids ROSIDs, remapped map[[2]string]string) error {
	d.InvalidateAddressReads()
	live, err := d.liveAddresses(ctx, stepsResources(steps))
	if err != nil {
		return err
	}
	changed := ""
	absent := []NATStep{}
	entries := []Row{}
	for _, s := range steps {
		if row := live[s.ID]; row == nil {
			absent = append(absent, s)
			entries = append(entries, s.Values)
		} else if row.Flag("dynamic") || row.Set("timeout") || !SameRow(ConfigROSRow(row), ConfigROSRow(s.Config)) {
			if changed == "" {
				changed = s.ID
			}
		}
	}
	created := map[int]Resource{}
	if len(entries) > 0 {
		_, err = d.CreateAddressesWithProof(ctx, entries, &[]NATStep{}, ids, created)
	}
	for index, item := range created {
		remapped[[2]string{PathAddresses, absent[index].ID}] = item.ID
	}
	if err != nil {
		return err
	}
	if changed != "" {
		return device.Error(fmt.Sprintf(device.MessagePolicyChanged, PathAddresses, changed))
	}
	return nil
}

// NATEntries builds static ConfigFlow list members.
func NATEntries(name string, addresses []string) []Row {
	entries := make([]Row, len(addresses))
	for i, address := range addresses {
		entries[i] = Row{"list": name, "address": address, "comment": NATListComment, "disabled": "false"}
	}
	return entries
}
