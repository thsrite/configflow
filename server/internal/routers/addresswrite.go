package routers

import (
	"configflow/internal/lifecycle"
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

// Address batch limits follow the measured RouterOS console limits.
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

func stepResource(action string, item ROSResource) NATStep {
	return NATStep{Action: action, Path: item.Path, ID: item.ID, Config: cloneRow(item.Config)}
}

// ROSIDs tracks the IDs already known on each device table.
type ROSIDs map[string]map[string]bool

func (ids ROSIDs) table(path string) map[string]bool {
	if ids[path] == nil {
		ids[path] = map[string]bool{}
	}
	return ids[path]
}
func valuesMatch(path string, row, values Row) bool {
	return matches(Row(ComparableValues(path, row)), Row(ComparableValues(path, values)))
}

func (d *RouterOS) resourceRow(ctx context.Context, path, id string) (Row, error) {
	value, err := d.get(ctx, path+"/"+escapeROSPathID(id), rosTimeout)
	if routerErrorKind(err) == KindMissingResource {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row := asRow(value)
	if row == nil || row.id() != id {
		return nil, routerError(fmt.Sprintf(messagePolicyReadFormat, path))
	}
	return row, nil
}
func (d *RouterOS) guardResource(ctx context.Context, item ROSResource) (Row, error) {
	row, err := d.resourceRow(ctx, item.Path, item.ID)
	if err != nil {
		return nil, err
	}
	if row == nil || !sameRow(configROSRow(row), configROSRow(item.Config)) {
		return nil, routerError(fmt.Sprintf(messagePolicyChanged, item.Path, item.ID))
	}
	return row, nil
}
func writableResource(path string, row Row) (Row, error) {
	if row.flag("dynamic") || path == rosAddresses && row.set("timeout") {
		return nil, routerError(messagePolicyDynamic)
	}
	if path == rosAddresses || path == rosNAT {
		values := configROSRow(row)
		delete(values, "dynamic")
		delete(values, "invalid")
		return values, nil
	}
	if path == rosTables {
		return selectFields([]Row{row}, stringSet("name", "fib", "disabled"))[0], nil
	}
	if path == rosMangle {
		values := configROSRow(row)
		delete(values, "dynamic")
		return values, nil
	}
	values := payload(path, row)
	if values == nil {
		return nil, routerError(messagePolicyDynamic)
	}
	return values, nil
}

var consoleAddressField = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validateAddressEntry(values Row) error {
	for key, value := range values {
		if !consoleAddressField.MatchString(key) {
			return routerError(messageAddressInvalidField)
		}
		switch value.(type) {
		case string, bool, json.Number, int, int64:
		default:
			return routerError(messageAddressInvalidField)
		}
	}
	if values.text("address") == "" {
		return routerError(messageAddressInvalidField)
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
		value := ScriptString(rosText(shared[key]))
		if key == "disabled" && (comparable(key, shared[key]) == "true" || comparable(key, shared[key]) == "false") {
			if comparable(key, shared[key]) == "true" {
				value = "yes"
			} else {
				value = "no"
			}
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}
func (d *RouterOS) runAddressBatches(ctx context.Context, shared Row, addresses []string) (int, string, error) {
	head := ":local n 0\n:local m \"\"\n:foreach a in={"
	tail := `} do={:onerror e in={/ip/firewall/address-list/add ` + addressParams(shared) + ` address=$a} do={:set n ($n+1); :if ($n=1) do={:set m ($a . " " . $e)}}}` + "\n" + `:put ("cf-done:" . $n . ":" . $m)`
	budget := AddressScriptBytes - len(head) - len(tail)
	batches := [][]string{}
	current := []string{}
	size := 0
	for _, address := range addresses {
		item := ScriptString(address)
		if len(item) > budget {
			return 0, "", routerError(messageAddressOversize)
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
		value, err := d.request(ctx, "POST", "/execute", Row{"script": head + strings.Join(batch, ";") + tail, "as-string": ""}, rosTimeout)
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
			return failed, first, routerError(fmt.Sprintf(messageAddressIncomplete, d.safeDeviceText(shortConsoleOutput(output))))
		}
		failed += n
		if first == "" {
			first = d.safeDeviceText(strings.TrimSpace(reason))
		}
	}
	return failed, first, nil
}
func (d *RouterOS) addressRows(ctx context.Context, name string) ([]Row, error) {
	return d.retryRead(ctx, func() ([]Row, error) { return d.FetchAddresses(ctx, name) })
}
func (d *RouterOS) settledAddressRows(ctx context.Context, name string) ([]Row, error) {
	rows, err := d.addressRows(ctx, name)
	if err != nil {
		return nil, err
	}
	for range 10 {
		if err := waitContext(ctx, time.Second); err != nil {
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
	return nil, rollbackError(fmt.Sprintf(messageAddressUncertain, name))
}

// CreateAddresses creates members in bounded console batches and journals only
// fresh, unique IDs with matching submitted values. It detaches cancellation.
func (d *RouterOS) CreateAddresses(ctx context.Context, entries []Row, journal *[]NATStep, ids ROSIDs) ([]ROSResource, error) {
	ctx = lifecycle.Detach(ctx)
	return d.createAddresses(ctx, entries, journal, ids, nil)
}
func (d *RouterOS) createAddresses(ctx context.Context, entries []Row, journal *[]NATStep, ids ROSIDs, created map[int]ROSResource) ([]ROSResource, error) {
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
		shared := cloneRow(values)
		delete(shared, "address")
		at := slices.IndexFunc(groups, func(g group) bool { return sameRow(g.shared, shared) })
		if at < 0 {
			groups = append(groups, group{shared: shared})
			at = len(groups) - 1
		}
		groups[at].members = append(groups[at].members, member{i, values})
	}
	resources := make([]ROSResource, len(entries))
	for _, g := range groups {
		name := g.shared.text("list")
		if name == "" {
			return resources, routerError(messageAddressNoList)
		}
		live, err := d.ReadAddresses(ctx, []string{name})
		if err != nil {
			return resources, err
		}
		before := rowIDs(live[name])
		addresses := []string{}
		for _, m := range g.members {
			addresses = append(addresses, m.values.text("address"))
		}
		sweepIndex := len(*journal)
		*journal = append(*journal, NATStep{Action: "sweep", Path: rosAddresses, List: name, Shared: cloneRow(g.shared), Addresses: addresses, Before: slices.Sorted(maps.Keys(before))})
		failed, reason, failure := d.runAddressBatches(ctx, g.shared, addresses)
		var rows []Row
		if routerErrorKind(failure) == KindUncertainWrite {
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
			key := comparable("address", m.values["address"])
			if _, ok := wanted[key]; !ok {
				keys = append(keys, key)
			}
			wanted[key] = append(wanted[key], m)
		}
		unknown := []Row{}
		counts := map[string]int{}
		fresh := map[string]int{}
		for _, row := range rows {
			counts[row.id()]++
			if !before[row.id()] && !ids.table(rosAddresses)[row.id()] {
				fresh[comparable("address", row["address"])]++
			}
		}
		limits := map[string]int{}
		for key, members := range wanted {
			limits[key] = len(members)
		}
		for _, row := range rows {
			if before[row.id()] || ids.table(rosAddresses)[row.id()] {
				continue
			}
			key := comparable("address", row["address"])
			candidates := wanted[key]
			if counts[row.id()] != 1 || fresh[key] > limits[key] || row.flag("dynamic") || row.set("timeout") || len(candidates) == 0 || !rosIDPattern.MatchString(row.id()) || !valuesMatch(rosAddresses, row, candidates[0].values) {
				unknown = append(unknown, row)
				continue
			}
			m := candidates[0]
			wanted[key] = candidates[1:]
			ids.table(rosAddresses)[row.id()] = true
			item := resource(rosAddresses, row)
			resources[m.index] = item
			*journal = append(*journal, stepResource("delete", item))
			if created != nil {
				created[m.index] = item
			}
		}
		if failure != nil {
			return resources, failure
		}
		if len(unknown) > 0 {
			return resources, routerError(fmt.Sprintf(messageAddressUnknown, name))
		}
		missing := []string{}
		for _, key := range keys {
			for _, m := range wanted[key] {
				missing = append(missing, m.values.text("address"))
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
			return resources, routerError(fmt.Sprintf(messageAddressFailed, name, max(failed, len(missing)), strings.Join(missing[:min(5, len(missing))], "、"), detail))
		}
	}
	return resources, nil
}
func (d *RouterOS) removeAddresses(ctx context.Context, ids []string) error {
	d.InvalidateAddressReads()
	for start := 0; start < len(ids); start += AddressRemoveBatch {
		if _, err := d.request(ctx, "POST", rosAddresses+"/remove", Row{".id": strings.Join(ids[start:min(start+AddressRemoveBatch, len(ids))], ",")}, rosTimeout); err != nil {
			return err
		}
	}
	return nil
}
func (d *RouterOS) liveAddresses(ctx context.Context, items []ROSResource) (map[string]Row, error) {
	names := []string{}
	for _, item := range items {
		names = append(names, item.Config.text("list"))
	}
	lists, err := d.ReadAddresses(ctx, names)
	if err != nil {
		return nil, err
	}
	live := map[string]Row{}
	for _, rows := range lists {
		for _, row := range rows {
			live[row.id()] = row
		}
	}
	return live, nil
}

// forgetAddresses drops removed members from the lists ConfigFlow knows (or
// only expects), so the next read of those lists is again a digest proof.
func (d *RouterOS) forgetAddresses(items []ROSResource, removed map[string]bool) {
	names := map[string]bool{}
	for _, item := range items {
		names[item.Config.text("list")] = true
	}
	kept := func(rows []Row) []Row {
		return slices.DeleteFunc(slices.Clone(rows), func(row Row) bool { return removed[row.id()] })
	}
	known := d.knownAddresses()
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
func (d *RouterOS) DeleteAddresses(ctx context.Context, items []ROSResource, journal *[]NATStep) error {
	ctx = lifecycle.Detach(ctx)
	d.InvalidateAddressReads()
	live, err := d.liveAddresses(ctx, items)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Path != rosAddresses || item.Config == nil || item.ID == "" {
			return routerError(messageOperationFormat)
		}
		row := live[item.ID]
		if row == nil || !sameRow(configROSRow(row), configROSRow(item.Config)) {
			return routerError(fmt.Sprintf(messagePolicyChanged, item.Path, item.ID))
		}
		if _, err := writableResource(rosAddresses, row); err != nil {
			return err
		}
	}
	removing := []string{}
	for _, item := range items {
		values, _ := writableResource(rosAddresses, live[item.ID])
		step := stepResource("restore", item)
		step.Values = values
		*journal = append(*journal, step)
		removing = append(removing, item.ID)
	}
	if err := d.removeAddresses(ctx, removing); err != nil {
		return err
	}
	d.forgetAddresses(items, stringSet(removing...))
	live, err = d.liveAddresses(ctx, items)
	if err != nil {
		return err
	}
	for _, id := range removing {
		if live[id] != nil {
			return routerError(messagePolicyStillPresent)
		}
	}
	return nil
}

func stepsResources(steps []NATStep) []ROSResource {
	items := make([]ROSResource, len(steps))
	for i, s := range steps {
		items[i] = ROSResource{s.Path, s.ID, s.Config}
	}
	return items
}
func (d *RouterOS) restoreDeleteAddresses(ctx context.Context, steps []NATStep) error {
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
	moved := rowIDs(located)
	removing, changed := []string{}, ""
	for _, s := range steps {
		row := live[s.ID]
		if row == nil {
			if moved[s.ID] && changed == "" {
				changed = s.ID
			}
			continue
		}
		if !sameRow(configROSRow(row), configROSRow(s.Config)) || row.flag("dynamic") || row.set("timeout") {
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
			return routerError(messageRollbackDeletePresent)
		}
	}
	if changed != "" {
		return routerError(fmt.Sprintf(messagePolicyChanged, rosAddresses, changed))
	}
	return nil
}
func (d *RouterOS) restoreAddressSweep(ctx context.Context, s NATStep) error {
	rows, err := d.addressRows(ctx, s.List)
	if err != nil {
		return err
	}
	before := stringSet(s.Before...)
	wanted := map[string][]Row{}
	for _, address := range s.Addresses {
		values := cloneRow(s.Shared)
		values["address"] = address
		key := comparable("address", address)
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
		counts[row.id()]++
		if !before[row.id()] {
			fresh[comparable("address", row["address"])]++
		}
	}
	for _, row := range rows {
		key := comparable("address", row["address"])
		candidates := wanted[key]
		if counts[row.id()] != 1 || fresh[key] > limits[key] || before[row.id()] || !rosIDPattern.MatchString(row.id()) || row.flag("dynamic") || row.set("timeout") || len(candidates) == 0 || !valuesMatch(rosAddresses, row, candidates[0]) {
			continue
		}
		wanted[key] = candidates[1:]
		removing = append(removing, row.id())
	}
	if err := d.removeAddresses(ctx, removing); err != nil {
		return err
	}
	latest, err := d.addressRows(ctx, s.List)
	if err != nil {
		return err
	}
	gone := stringSet(removing...)
	for _, row := range latest {
		if gone[row.id()] {
			return routerError(messageRollbackDeletePresent)
		}
	}
	return nil
}
func (d *RouterOS) restoreAddresses(ctx context.Context, steps []NATStep, ids ROSIDs, remapped map[[2]string]string) error {
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
		} else if row.flag("dynamic") || row.set("timeout") || !sameRow(configROSRow(row), configROSRow(s.Config)) {
			if changed == "" {
				changed = s.ID
			}
		}
	}
	created := map[int]ROSResource{}
	if len(entries) > 0 {
		_, err = d.createAddresses(ctx, entries, &[]NATStep{}, ids, created)
	}
	for index, item := range created {
		remapped[[2]string{rosAddresses, absent[index].ID}] = item.ID
	}
	if err != nil {
		return err
	}
	if changed != "" {
		return routerError(fmt.Sprintf(messagePolicyChanged, rosAddresses, changed))
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
