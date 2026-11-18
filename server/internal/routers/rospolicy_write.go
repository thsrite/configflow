package routers

import (
	"configflow/internal/devhttp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/netipx"
)

// ROSPolicyUndo is a reverse-ordered device journal. Retained resources are
// used by destination-only refreshes to exclude their own references safely.
type ROSPolicyUndo struct {
	Kind     string          `json:"kind"`
	Steps    []NATStep       `json:"steps"`
	Previous json.RawMessage `json:"previous"`
	Retained []ROSResource   `json:"retained,omitempty"`
}

// policyWrite receives the driver's internal JSON contract, not a client plan.
// Raw sub-operations go straight to ApplyMany; no polymorphic plan decoder.
type policyWrite struct {
	Bundle          []ROSReplacement  `json:"bundle"`
	Previous        json.RawMessage   `json:"previous"`
	Owned           []ROSResource     `json:"owned"`
	Migrations      []ROSResource     `json:"migrations"`
	Replacements    []ROSReplacement  `json:"replacements"`
	Spec            *ROSPolicySpec    `json:"spec"`
	Evidence        ROSPolicySnapshot `json:"evidence"`
	StaticChecks    []CIDRGateway     `json:"static_checks"`
	PrepareStatic   []json.RawMessage `json:"prepare_static"`
	StaticNATChecks *NATChecks        `json:"static_nat_checks"`
}

func init() {
	RegisterRouterOSHandler("ros_policy_replace", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op policyWrite
		if devhttp.DecodeJSON(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		undo, err := d.applyPolicy(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	})
	RegisterRouterOSHandler("ros_policy_restore", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var undo ROSPolicyUndo
		if devhttp.DecodeJSON(raw, &undo) != nil {
			return nil, routerError(messageOperationFormat)
		}
		if err := d.RestoreROSPolicy(ctx, undo); err != nil {
			return nil, err
		}
		return json.RawMessage(`{}`), nil
	})
}

// ApplyROSPolicy applies a server-built typed plan and compensates any failure.
func (d *RouterOS) ApplyROSPolicy(ctx context.Context, op *ROSPolicyReplace) (*ROSPolicyUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return nil, routerError(messageOperationFormat)
	}
	var write policyWrite
	if devhttp.DecodeJSON(raw, &write) != nil {
		return nil, routerError(messageOperationFormat)
	}
	return d.applyPolicy(ctx, write)
}

func (d *RouterOS) applyPolicy(ctx context.Context, op policyWrite) (*ROSPolicyUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if op.Evidence == nil || op.Bundle == nil || op.Migrations == nil || op.Replacements == nil {
		return nil, routerError(messageOperationFormat)
	}
	if len(op.Bundle) > 0 && (op.Spec == nil || len(op.Spec.Destinations) == 0 || len(op.Spec.Sources) == 0 || op.Spec.Gateway == "") {
		return nil, routerError(messageOperationFormat)
	}
	d.InvalidateAddressReads()
	live, err := d.snapshotPolicy(ctx, true)
	if err != nil {
		return nil, err
	}
	expected, actual := maps.Clone(op.Evidence), stablePolicy(*live)
	delete(expected, rosRoutes)
	delete(actual, rosRoutes)
	if !jsonValuesEqual(mustPolicyJSON(expected), mustPolicyJSON(actual)) {
		return nil, routerError(messagePolicyPreviewChanged)
	}
	owned, err := OwnedROSPolicyRows(op.Previous, live)
	if err != nil {
		return nil, err
	}
	if err = checkPolicyReferences(owned, *live); err != nil {
		return nil, err
	}
	for _, migration := range op.Migrations {
		if err = guardPolicyIn(*live, migration); err != nil {
			return nil, err
		}
	}
	journal := []NATStep{}
	resources := []ROSResource{}
	ids := policyIDs(*live)
	run := func() error {
		record := func(_ json.RawMessage, undo json.RawMessage) {
			journal = append(journal, NATStep{Action: "operation", Undo: slices.Clone(undo)})
		}
		if err := d.ApplyMany(ctx, op.PrepareStatic, record); err != nil {
			if undo := UndoOf(err); len(undo) > 0 {
				record(nil, undo)
			}
			return err
		}
		if len(op.StaticChecks) > 0 {
			state, err := d.ReadState(ctx)
			if err != nil {
				return err
			}
			index := NewStaticRouteIndex(state.Routes)
			for _, check := range op.StaticChecks {
				selected, _ := index.Select(check.CIDR, check.Gateway)
				if len(selected.Candidates) == 0 || slices.ContainsFunc(selected.Candidates, func(row *RouteEntry) bool { return !IsManaged(row.Comment) }) {
					return routerError(messagePolicyStaticInactive + check.CIDR)
				}
			}
		}
		if op.StaticNATChecks != nil {
			state, err := d.ReadState(ctx)
			if err != nil {
				return err
			}
			if err = VerifyNAT(ctx, *op.StaticNATChecks, state); err != nil {
				return err
			}
			for _, path := range []string{rosNAT, rosRoutes} {
				rows, err := d.list(ctx, path)
				if err != nil {
					return err
				}
				maps.Copy(ids.table(path), rowIDs(rows))
			}
		}
		for start := 0; start < len(op.Bundle); {
			entry := op.Bundle[start]
			if entry.Path == rosAddresses {
				end := start + 1
				for end < len(op.Bundle) && op.Bundle[end].Path == rosAddresses {
					end++
				}
				values := []Row{}
				for _, item := range op.Bundle[start:end] {
					values = append(values, item.Values)
				}
				created, err := d.createAddresses(ctx, values, &journal, ids, nil)
				if err != nil {
					return err
				}
				resources = append(resources, created...)
				start = end
				continue
			}
			item, err := d.createResource(ctx, entry.Path, entry.Values, &journal, ids, false)
			if err != nil {
				return err
			}
			resources = append(resources, item)
			if entry.Path == rosRoutes {
				row, err := d.resourceRow(ctx, rosRoutes, item.ID)
				if err != nil {
					return err
				}
				if err = checkPolicyEgress(row, op.Spec.Gateway, op.Spec.EgressInterface); err != nil {
					return err
				}
			}
			start++
		}
		for _, replacement := range op.Replacements {
			if _, err := d.createResource(ctx, replacement.Path, replacement.Values, &journal, ids, false); err != nil {
				return err
			}
		}
		if err := d.verifyReplacementPolicyNAT(ctx, op, resources); err != nil {
			return err
		}
		members, rest := []ROSResource{}, []ROSResource{}
		for _, item := range op.Migrations {
			if item.Path == rosAddresses {
				members = append(members, item)
			} else {
				rest = append(rest, item)
			}
		}
		if len(members) > 0 {
			if err := d.DeleteAddresses(ctx, members, &journal); err != nil {
				return err
			}
		}
		if err := d.deletePolicyResources(ctx, rest, &journal); err != nil {
			return err
		}
		fresh, err := d.snapshotPolicy(ctx, false)
		if err != nil {
			return err
		}
		if err = checkPolicyReferences(owned, *fresh); err != nil {
			return err
		}
		retiring := slices.Clone(owned)
		slices.Reverse(retiring)
		return d.deletePolicyResources(ctx, retiring, &journal)
	}
	err = run()
	slices.Reverse(journal)
	undo := &ROSPolicyUndo{Kind: "ros_policy_restore", Steps: journal, Previous: slices.Clone(op.Previous)}
	if err != nil {
		if rollback := d.RestoreROSPolicy(ctx, *undo); rollback != nil {
			return nil, rollbackError(err.Error() + "；" + rollback.Error())
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		d.SetPolicyManifest(json.RawMessage(`null`))
	} else {
		d.SetPolicyManifest(mustPolicyJSON(ROSPolicyManifest{op.Spec, resources}))
	}
	return undo, nil
}

func guardPolicyIn(live ROSPolicySnapshot, item ROSResource) error {
	if item.ID == "" || item.Config == nil {
		return routerError(messageOperationFormat)
	}
	count := 0
	for _, row := range live[item.Path] {
		if row.id() == item.ID {
			count++
			if !sameRow(configROSRow(row), configROSRow(item.Config)) {
				return routerError(fmt.Sprintf(messagePolicyChanged, item.Path, item.ID))
			}
		}
	}
	if count != 1 {
		return routerError(fmt.Sprintf(messagePolicyChanged, item.Path, item.ID))
	}
	return nil
}

func policyIDs(live ROSPolicySnapshot) ROSIDs {
	ids := ROSIDs{}
	for path, rows := range live {
		ids[path] = rowIDs(rows)
	}
	return ids
}

func (d *RouterOS) deletePolicyResources(ctx context.Context, items []ROSResource, journal *[]NATStep) error {
	for start := 0; start < len(items); {
		path := items[start].Path
		end := start + 1
		for end < len(items) && items[end].Path == path {
			end++
		}
		switch {
		case path == rosAddresses:
			if err := d.DeleteAddresses(ctx, items[start:end], journal); err != nil {
				return err
			}
		case path == rosRoutes && end-start >= d.batchMin:
			if err := d.deletePolicyRoutes(ctx, items[start:end], journal); err != nil {
				return err
			}
		default:
			for _, item := range items[start:end] {
				if err := d.deleteResource(ctx, item, journal); err != nil {
					return err
				}
			}
		}
		start = end
	}
	return nil
}

func (d *RouterOS) deletePolicyRoutes(ctx context.Context, items []ROSResource, journal *[]NATStep) error {
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	rows, err := d.query(ctx, ".id", ids, rosRoutes)
	if err != nil {
		return err
	}
	live := ROSPolicySnapshot{rosRoutes: rows}
	for _, item := range items {
		if err = guardPolicyIn(live, item); err != nil {
			return err
		}
	}
	for _, item := range items {
		values, err := writableResource(rosRoutes, item.Config)
		if err != nil {
			return err
		}
		step := stepResource("restore", item)
		step.Values = values
		*journal = append(*journal, step)
	}
	for start := 0; start < len(ids); start += 500 {
		if _, err = d.request(ctx, "POST", rosRoutes+"/remove", Row{".id": strings.Join(ids[start:min(start+500, len(ids))], ",")}, rosTimeout); err != nil {
			break
		}
	}
	if err != nil {
		// Re-prove every remaining ID before the single-request fallback.
		for _, item := range items {
			row, err := d.resourceRow(ctx, rosRoutes, item.ID)
			if err != nil {
				return err
			}
			if row == nil {
				continue
			}
			if _, err = d.guardResource(ctx, item); err != nil {
				return err
			}
			if _, err = d.request(ctx, "DELETE", rosRoutes+"/"+escapeROSPathID(item.ID), nil, rosTimeout); err != nil {
				return err
			}
		}
	}
	rows, err = d.query(ctx, ".id", ids, rosRoutes)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return routerError(messagePolicyStillPresent)
	}
	return nil
}

// RestoreROSPolicy replays the reverse journal. Any failed restoration keeps
// replacement protection; changed IDs and foreign references remain untouched.
func (d *RouterOS) RestoreROSPolicy(ctx context.Context, undo ROSPolicyUndo) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	d.InvalidateAddressReads()
	for _, step := range undo.Steps {
		if step.Action == "operation" {
			if len(step.Undo) == 0 {
				return routerError(messageOperationFormat)
			}
			continue
		}
		if !slices.Contains(policyPaths, step.Path) || !slices.Contains([]string{"delete", "restore", "sweep"}, step.Action) || step.Action != "sweep" && (step.ID == "" || step.Config == nil) {
			return routerError(messageOperationFormat)
		}
	}
	live, err := d.snapshotPolicy(ctx, false)
	if err != nil {
		return err
	}
	failures := []string{}
	deleting := []ROSResource{}
	for _, step := range undo.Steps {
		if step.Action == "delete" {
			deleting = append(deleting, ROSResource{step.Path, step.ID, step.Config})
		}
	}
	// The journal can contain fresh IDs not yet present in any saved manifest.
	// Locate missing members globally: moving an ID is an edit, not a deletion.
	present := rowIDs((*live)[rosAddresses])
	missing := []string{}
	for _, item := range deleting {
		if item.Path == rosAddresses && !present[item.ID] {
			missing = append(missing, item.ID)
		}
	}
	located, err := d.LocateAddresses(ctx, missing)
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		(*live)[rosAddresses] = append((*live)[rosAddresses], located...)
	}
	if _, err := OwnedROSPolicyRows(mustPolicyJSON(ROSPolicyManifest{Resources: deleting}), live); err != nil {
		failures = append(failures, err.Error())
	}
	if len(undo.Retained) > 0 {
		present, e := OwnedROSPolicyRows(mustPolicyJSON(ROSPolicyManifest{Resources: undo.Retained}), live)
		if e != nil {
			failures = append(failures, e.Error())
		} else {
			allowed := policyKeys(present)
			for _, step := range undo.Steps {
				if step.Action == "restore" {
					allowed[[2]string{step.Path, step.ID}] = true
				}
			}
			for _, item := range undo.Retained {
				if !allowed[[2]string{item.Path, item.ID}] {
					failures = append(failures, messagePolicyRetainedMissing)
					break
				}
			}
		}
	}
	if e := checkPolicyReferences(append(deleting, undo.Retained...), *live); e != nil {
		failures = append(failures, e.Error())
	}
	remapped := map[[2]string]string{}
	d.runNATSteps(ctx, undo.Steps, policyIDs(*live), remapped, &failures, len(failures) == 0)
	// Keep all unknown manifest fields while updating identities recreated by undo.
	previous := slices.Clone(undo.Previous)
	if manifest, e := policyManifest(previous); e != nil {
		failures = append(failures, e.Error())
	} else if manifest != nil {
		// One scoped read per list, not a GET for every restored member.
		names := []string{}
		for _, item := range manifest.Resources {
			if _, ok := remapped[[2]string{item.Path, item.ID}]; ok && item.Path == rosAddresses {
				names = append(names, item.Config.text("list"))
			}
		}
		lists, e := d.ReadAddresses(ctx, names)
		addresses := map[string]Row{}
		if e != nil {
			failures = append(failures, e.Error())
		}
		for _, rows := range lists {
			for _, row := range rows {
				addresses[row.id()] = row
			}
		}
		for i, item := range manifest.Resources {
			if id := remapped[[2]string{item.Path, item.ID}]; id != "" {
				manifest.Resources[i].ID = id
				row := addresses[id]
				if item.Path != rosAddresses {
					row, e = d.resourceRow(ctx, item.Path, id)
					if e != nil {
						failures = append(failures, e.Error())
					}
				}
				if row != nil {
					manifest.Resources[i].Config = configROSRow(row)
				}
			}
		}
		var fields map[string]json.RawMessage
		_ = devhttp.DecodeJSON(previous, &fields)
		fields["resources"] = mustPolicyJSON(manifest.Resources)
		previous = mustPolicyJSON(fields)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.SetPolicyManifest(previous)
	if len(failures) > 0 {
		return rollbackError(messagePolicyRollbackPrefix + joinPolicyMessages(failures))
	}
	return nil
}

func (d *RouterOS) verifyReplacementPolicyNAT(ctx context.Context, op policyWrite, resources []ROSResource) error {
	previous, err := policyManifest(op.Previous)
	if err != nil {
		return err
	}
	if previous == nil || previous.Spec == nil || len(previous.Spec.Sources) == 0 || len(op.StaticChecks) == 0 {
		return nil
	}
	state, err := d.ReadState(ctx)
	if err != nil {
		return err
	}
	live, err := d.snapshotPolicy(ctx, false)
	if err != nil {
		return err
	}
	removing := policyKeys(append(slices.Clone(op.Owned), op.Migrations...))
	policy := maps.Clone(removing)
	maps.Copy(policy, policyKeys(resources))
	context := state.NatContext
	if context == nil {
		context = &NatContext{}
	}
	copyContext := *context
	context = &copyContext
	context.Mangle = []Row{}
	rows := []Row{}
	for _, row := range (*live)[rosMangle] {
		if !policy[[2]string{rosMangle, row.id()}] {
			context.Mangle = append(context.Mangle, row)
		}
	}
	for _, row := range (*live)[rosNAT] {
		if !removing[[2]string{rosNAT, row.id()}] {
			rows = append(rows, row)
		}
	}
	index := NewRouteEgressIndex(context)
	sources := Networks(previous.Spec.Sources)
	for _, check := range op.StaticChecks {
		destinations := []netip.Prefix{}
		target, ok := netipx.ParseNetwork4(check.CIDR, true)
		if !ok {
			return routerError(messageOperationFormat)
		}
		for _, old := range Networks(previous.Spec.Destinations) {
			if old.Overlaps(target) {
				if subnetOf(old, target) {
					destinations = append(destinations, old)
				} else {
					destinations = append(destinations, target)
				}
			}
		}
		if len(destinations) == 0 {
			continue
		}
		iface, reason := Egress(check.CIDR, check.Gateway, context, false, index)
		if iface == "" || !PreservesSources(rows, sources, destinations, iface, context, state.AddressSets, nil, nil) {
			if reason != "" {
				reason = "；" + reason
			}
			return routerError(messagePolicyStaticNATUnproven + check.CIDR + reason)
		}
	}
	return nil
}

// VerifyNAT proves the same NAT prerequisites before withdrawal and on final
// readback. R12 shares this implementation rather than reimplementing policy NAT.
func VerifyNAT(ctx context.Context, checks NATChecks, state *RouterState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	index := NewRouteEgressIndex(state.NatContext)
	uncovered := []string{}
	managed := []netip.Prefix{}
	if state.NatContext == nil {
		for _, rule := range state.NatBypass {
			if rule.Enabled && IsManaged(rule.Comment) {
				managed = append(managed, Networks(rule.Dst)...)
			}
		}
	}
	coverage := NewSupernetIndex(managed)
	for _, cidr := range checks.NATBypass {
		ok := false
		if state.NatContext != nil {
			status := AssessBypass(cidr, checks.Gateway, state, index).Status
			ok = status == "synced" || status == "not_required"
		} else if prefix, valid := netipx.ParseNetwork4(cidr, true); valid {
			ok = coverage.Covers(prefix)
		}
		if !ok {
			uncovered = append(uncovered, cidr)
		}
	}
	if len(uncovered) > 0 {
		return routerError(fmt.Sprintf(messageNATVerifyUncovered, strings.Join(uncovered, ", ")))
	}
	for _, check := range checks.NATNotRequired {
		assessment := AssessBypass(check.CIDR, check.Gateway, state, index)
		if assessment.Status != "not_required" {
			return routerError(fmt.Sprintf(messageNATVerifyRequired, check.CIDR, assessment.Detail))
		}
	}
	return nil
}

// VerifyROSPolicy validates ownership, runtime activation, lookup order, actual
// next hop and NAT evidence. Only transient invalid flags are waited for.
func VerifyROSPolicy(ctx context.Context, d *RouterOS, check *ROSPolicyCheck) error {
	if check == nil {
		return routerError(messageOperationFormat)
	}
	raw := check.Previous
	if d.policyManifestSet {
		raw = d.policyManifest
	}
	live, err := d.snapshotPolicy(ctx, false)
	if err != nil {
		return err
	}
	owned, err := OwnedROSPolicyRows(raw, live)
	if err != nil {
		return err
	}
	if check.Spec == nil {
		previous, e := OwnedROSPolicyRows(check.Previous, live)
		if e != nil {
			return e
		}
		if len(owned)+len(previous) > 0 {
			return routerError(messagePolicyWithdrawalIncomplete)
		}
		return nil
	}
	if len(check.Spec.Destinations) == 0 {
		return routerError(messagePolicyIncomplete)
	}
	keys := policyKeys(owned)
	for range 8 {
		invalid := false
		for path, rows := range *live {
			for _, row := range rows {
				if keys[[2]string{path, row.id()}] && row.flag("invalid") {
					invalid = true
				}
			}
		}
		if !invalid {
			break
		}
		if err = waitContext(ctx, 250*time.Millisecond); err != nil {
			return err
		}
		live, err = d.snapshotPolicy(ctx, false)
		if err != nil {
			return err
		}
		owned, err = OwnedROSPolicyRows(raw, live)
		if err != nil {
			return err
		}
	}
	manifest, err := policyManifest(raw)
	if err != nil {
		return err
	}
	if manifest == nil || !jsonValuesEqual(mustPolicyJSON(manifest.Spec), mustPolicyJSON(check.Spec)) || len(owned) != len(manifest.Resources) {
		return routerError(messagePolicyIncomplete)
	}
	if err = checkPolicyReferences(owned, *live); err != nil {
		return err
	}
	if err = checkPolicyOrder(*live); err != nil {
		return err
	}
	if err = policyHealth(raw, *live); err != nil {
		return err
	}
	keys = policyKeys(owned)
	for _, row := range (*live)[rosFilter] {
		if row.enabled() && row.is("action", "fasttrack-connection") {
			return routerError(messagePolicyNewFastTrack)
		}
	}
	state, err := d.ReadState(ctx)
	if err != nil {
		return err
	}
	if state.NatContext == nil {
		return routerError(messagePolicyUnreadable)
	}
	context := *state.NatContext
	state.NatContext = &context
	context.Mangle = []Row{}
	for _, row := range (*live)[rosMangle] {
		if !keys[[2]string{rosMangle, row.id()}] {
			context.Mangle = append(context.Mangle, row)
		}
	}
	context.RoutingRules = (*live)["/routing/rule"]
	context.VRFs = (*live)["/ip/vrf"]
	context.Routes = (*live)[rosRoutes]
	if slices.ContainsFunc(context.Mangle, conflictingPolicyMangle) {
		return routerError(messagePolicyNewMangle)
	}
	index := NewRouteEgressIndex(&context)
	iface, reason := Egress(check.Spec.Destinations[0], check.Spec.Gateway, &context, true, index)
	if iface == "" {
		return routerError(messagePolicyReadEgress + reason)
	}
	if iface != check.Spec.EgressInterface || check.Spec.NATInterface != "" && iface != check.Spec.NATInterface {
		return routerError(messagePolicyInterfaceChanged)
	}
	if check.Spec.NATInterface == "" {
		for _, cidr := range check.Spec.Destinations {
			assessment := AssessNAT(cidr, check.Spec.Gateway, state, true, index)
			if assessment.Status != "not_required" {
				return routerError(messagePolicyNATNowRequired + assessment.Detail)
			}
		}
	}
	for _, entry := range owned {
		if entry.Path == rosRoutes {
			row, err := d.resourceRow(ctx, rosRoutes, entry.ID)
			if err != nil {
				return err
			}
			if err = checkPolicyEgress(row, check.Spec.Gateway, iface); err != nil {
				return err
			}
		}
	}
	present := map[[2]string]bool{}
	for path, rows := range *live {
		for _, row := range rows {
			present[[2]string{path, row.id()}] = true
		}
	}
	for _, entry := range check.Migrations {
		if present[[2]string{entry.Path, entry.ID}] {
			return routerError(messagePolicyMigrationPresent)
		}
	}
	for _, entry := range check.Replacements {
		values := cloneRow(entry.Values)
		delete(values, "place-before")
		if !slices.ContainsFunc((*live)[entry.Path], func(row Row) bool { return valuesMatch(entry.Path, row, values) }) {
			return routerError(messagePolicyReplacementMissing)
		}
	}
	return nil
}
