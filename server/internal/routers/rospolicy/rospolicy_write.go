package rospolicy

import (
	"configflow/internal/devhttp"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
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
	Steps    []ros.NATStep   `json:"steps"`
	Previous json.RawMessage `json:"previous"`
	Retained []ros.Resource  `json:"retained,omitempty"`
}

// policyWrite receives the driver's internal JSON contract, not a client plan.
// Raw sub-operations go straight to ApplyMany; no polymorphic plan decoder.
type PolicyWrite struct {
	Bundle          []ROSReplacement     `json:"bundle"`
	Previous        json.RawMessage      `json:"previous"`
	Owned           []ros.Resource       `json:"owned"`
	Migrations      []ros.Resource       `json:"migrations"`
	Replacements    []ROSReplacement     `json:"replacements"`
	Spec            *ROSPolicySpec       `json:"spec"`
	Evidence        ROSPolicySnapshot    `json:"evidence"`
	StaticChecks    []device.CIDRGateway `json:"static_checks"`
	PrepareStatic   []json.RawMessage    `json:"prepare_static"`
	StaticNATChecks *device.NATChecks    `json:"static_nat_checks"`
}

// ApplyROSPolicy applies a server-built typed plan and compensates any failure.
func (d rosDevice) ApplyROSPolicy(ctx context.Context, op *ROSPolicyReplace) (*ROSPolicyUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	var write PolicyWrite
	if devhttp.DecodeJSON(raw, &write) != nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	return d.applyPolicy(ctx, write)
}

func (d rosDevice) applyPolicy(ctx context.Context, op PolicyWrite) (*ROSPolicyUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if op.Evidence == nil || op.Bundle == nil || op.Migrations == nil || op.Replacements == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	if len(op.Bundle) > 0 && (op.Spec == nil || len(op.Spec.Destinations) == 0 || len(op.Spec.Sources) == 0 || op.Spec.Gateway == "") {
		return nil, device.Error(device.MessageOperationFormat)
	}
	d.InvalidateAddressReads()
	live, err := d.snapshotPolicy(ctx, true)
	if err != nil {
		return nil, err
	}
	expected, actual := maps.Clone(op.Evidence), stablePolicy(*live)
	delete(expected, ros.PathRoutes)
	delete(actual, ros.PathRoutes)
	if !ros.JSONValuesEqual(device.JSON(expected), device.JSON(actual)) {
		return nil, device.Error(messagePolicyPreviewChanged)
	}
	owned, err := OwnedROSPolicyRows(op.Previous, live)
	if err != nil {
		return nil, err
	}
	if err = CheckReferences(owned, *live); err != nil {
		return nil, err
	}
	for _, migration := range op.Migrations {
		if err = GuardIn(*live, migration); err != nil {
			return nil, err
		}
	}
	journal := []ros.NATStep{}
	resources := []ros.Resource{}
	ids := PolicyIDs(*live)
	run := func() error {
		record := func(_ json.RawMessage, undo json.RawMessage) {
			journal = append(journal, ros.NATStep{Action: "operation", Undo: slices.Clone(undo)})
		}
		if err := d.applyMany(ctx, op.PrepareStatic, record); err != nil {
			if undo := device.UndoOf(err); len(undo) > 0 {
				record(nil, undo)
			}
			return err
		}
		if len(op.StaticChecks) > 0 {
			facts, _, err := d.readState(ctx)
			if err != nil {
				return err
			}
			index := device.NewStaticRouteIndex(facts.Routes)
			for _, check := range op.StaticChecks {
				selected, _ := index.Select(check.CIDR, check.Gateway)
				if len(selected.Candidates) == 0 || slices.ContainsFunc(selected.Candidates, func(row *device.RouteEntry) bool { return !device.IsManaged(row.Comment) }) {
					return device.Error(messagePolicyStaticInactive + check.CIDR)
				}
			}
		}
		if op.StaticNATChecks != nil {
			facts, nat, err := d.readState(ctx)
			if err != nil {
				return err
			}
			if err = VerifyNAT(ctx, *op.StaticNATChecks, facts, nat); err != nil {
				return err
			}
			for _, path := range []string{ros.PathNAT, ros.PathRoutes} {
				rows, err := d.List(ctx, path)
				if err != nil {
					return err
				}
				maps.Copy(ids.Table(path), ros.RowIDs(rows))
			}
		}
		for start := 0; start < len(op.Bundle); {
			entry := op.Bundle[start]
			if entry.Path == ros.PathAddresses {
				end := start + 1
				for end < len(op.Bundle) && op.Bundle[end].Path == ros.PathAddresses {
					end++
				}
				values := []ros.Row{}
				for _, item := range op.Bundle[start:end] {
					values = append(values, item.Values)
				}
				created, err := d.CreateAddressesWithProof(ctx, values, &journal, ids, nil)
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
			if entry.Path == ros.PathRoutes {
				row, err := d.ResourceRow(ctx, ros.PathRoutes, item.ID)
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
		members, rest := []ros.Resource{}, []ros.Resource{}
		for _, item := range op.Migrations {
			if item.Path == ros.PathAddresses {
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
		if err = CheckReferences(owned, *fresh); err != nil {
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
			return nil, device.RollbackError(err.Error() + "；" + rollback.Error())
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		d.SetPolicyManifest(json.RawMessage(`null`))
	} else {
		d.SetPolicyManifest(device.JSON(ROSPolicyManifest{op.Spec, resources}))
	}
	return undo, nil
}

func GuardIn(live ROSPolicySnapshot, item ros.Resource) error {
	if item.ID == "" || item.Config == nil {
		return device.Error(device.MessageOperationFormat)
	}
	count := 0
	for _, row := range live[item.Path] {
		if row.ID() == item.ID {
			count++
			if !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(item.Config)) {
				return device.Error(fmt.Sprintf(device.MessagePolicyChanged, item.Path, item.ID))
			}
		}
	}
	if count != 1 {
		return device.Error(fmt.Sprintf(device.MessagePolicyChanged, item.Path, item.ID))
	}
	return nil
}

func PolicyIDs(live ROSPolicySnapshot) ros.ROSIDs {
	ids := ros.ROSIDs{}
	for path, rows := range live {
		ids[path] = ros.RowIDs(rows)
	}
	return ids
}

func (d rosDevice) deletePolicyResources(ctx context.Context, items []ros.Resource, journal *[]ros.NATStep) error {
	for start := 0; start < len(items); {
		path := items[start].Path
		end := start + 1
		for end < len(items) && items[end].Path == path {
			end++
		}
		switch {
		case path == ros.PathAddresses:
			if err := d.DeleteAddresses(ctx, items[start:end], journal); err != nil {
				return err
			}
		case path == ros.PathRoutes && end-start >= d.BatchMin():
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

func (d rosDevice) deletePolicyRoutes(ctx context.Context, items []ros.Resource, journal *[]ros.NATStep) error {
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	rows, err := d.Query(ctx, ".id", ids, ros.PathRoutes)
	if err != nil {
		return err
	}
	live := ROSPolicySnapshot{ros.PathRoutes: rows}
	for _, item := range items {
		if err = GuardIn(live, item); err != nil {
			return err
		}
	}
	for _, item := range items {
		values, err := ros.WritableResource(ros.PathRoutes, item.Config)
		if err != nil {
			return err
		}
		step := ros.StepResource("restore", item)
		step.Values = values
		*journal = append(*journal, step)
	}
	for start := 0; start < len(ids); start += 500 {
		if _, err = d.Request(ctx, "POST", ros.PathRoutes+"/remove", ros.Row{".id": strings.Join(ids[start:min(start+500, len(ids))], ",")}, ros.Timeout); err != nil {
			break
		}
	}
	if err != nil {
		// Re-prove every remaining ID before the single-request fallback.
		for _, item := range items {
			row, err := d.ResourceRow(ctx, ros.PathRoutes, item.ID)
			if err != nil {
				return err
			}
			if row == nil {
				continue
			}
			if _, err = d.GuardResource(ctx, item); err != nil {
				return err
			}
			if _, err = d.Request(ctx, "DELETE", ros.PathRoutes+"/"+ros.EscapePathID(item.ID), nil, ros.Timeout); err != nil {
				return err
			}
		}
	}
	rows, err = d.Query(ctx, ".id", ids, ros.PathRoutes)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return device.Error(device.MessagePolicyStillPresent)
	}
	return nil
}

// RestoreROSPolicy replays the reverse journal. Any failed restoration keeps
// replacement protection; changed IDs and foreign references remain untouched.
func (d rosDevice) RestoreROSPolicy(ctx context.Context, undo ROSPolicyUndo) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	d.InvalidateAddressReads()
	for _, step := range undo.Steps {
		if step.Action == "operation" {
			if len(step.Undo) == 0 {
				return device.Error(device.MessageOperationFormat)
			}
			continue
		}
		if !slices.Contains(policyPaths, step.Path) || !slices.Contains([]string{"delete", "restore", "sweep"}, step.Action) || step.Action != "sweep" && (step.ID == "" || step.Config == nil) {
			return device.Error(device.MessageOperationFormat)
		}
	}
	live, err := d.snapshotPolicy(ctx, false)
	if err != nil {
		return err
	}
	failures := []string{}
	deleting := []ros.Resource{}
	for _, step := range undo.Steps {
		if step.Action == "delete" {
			deleting = append(deleting, ros.Resource{Path: step.Path, ID: step.ID, Config: step.Config})
		}
	}
	// The journal can contain fresh IDs not yet present in any saved manifest.
	// Locate missing members globally: moving an ID is an edit, not a deletion.
	present := ros.RowIDs((*live)[ros.PathAddresses])
	missing := []string{}
	for _, item := range deleting {
		if item.Path == ros.PathAddresses && !present[item.ID] {
			missing = append(missing, item.ID)
		}
	}
	located, err := d.LocateAddresses(ctx, missing)
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		(*live)[ros.PathAddresses] = append((*live)[ros.PathAddresses], located...)
	}
	if _, err := OwnedROSPolicyRows(device.JSON(ROSPolicyManifest{Resources: deleting}), live); err != nil {
		failures = append(failures, err.Error())
	}
	if len(undo.Retained) > 0 {
		present, e := OwnedROSPolicyRows(device.JSON(ROSPolicyManifest{Resources: undo.Retained}), live)
		if e != nil {
			failures = append(failures, e.Error())
		} else {
			allowed := PolicyKeys(present)
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
	if e := CheckReferences(append(deleting, undo.Retained...), *live); e != nil {
		failures = append(failures, e.Error())
	}
	remapped := map[[2]string]string{}
	d.runNATSteps(ctx, undo.Steps, PolicyIDs(*live), remapped, &failures, len(failures) == 0)
	// Keep all unknown manifest fields while updating identities recreated by undo.
	previous := slices.Clone(undo.Previous)
	if manifest, e := ParseManifest(previous); e != nil {
		failures = append(failures, e.Error())
	} else if manifest != nil {
		// One scoped read per list, not a GET for every restored member.
		names := []string{}
		for _, item := range manifest.Resources {
			if _, ok := remapped[[2]string{item.Path, item.ID}]; ok && item.Path == ros.PathAddresses {
				names = append(names, item.Config.Text("list"))
			}
		}
		lists, e := d.ReadAddresses(ctx, names)
		addresses := map[string]ros.Row{}
		if e != nil {
			failures = append(failures, e.Error())
		}
		for _, rows := range lists {
			for _, row := range rows {
				addresses[row.ID()] = row
			}
		}
		for i, item := range manifest.Resources {
			if id := remapped[[2]string{item.Path, item.ID}]; id != "" {
				manifest.Resources[i].ID = id
				row := addresses[id]
				if item.Path != ros.PathAddresses {
					row, e = d.ResourceRow(ctx, item.Path, id)
					if e != nil {
						failures = append(failures, e.Error())
					}
				}
				if row != nil {
					manifest.Resources[i].Config = ros.ConfigROSRow(row)
				}
			}
		}
		var fields map[string]json.RawMessage
		_ = devhttp.DecodeJSON(previous, &fields)
		fields["resources"] = device.JSON(manifest.Resources)
		previous = device.JSON(fields)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.SetPolicyManifest(previous)
	if len(failures) > 0 {
		return device.RollbackError(MessageRollbackPrefix + joinPolicyMessages(failures))
	}
	return nil
}

func (d rosDevice) verifyReplacementPolicyNAT(ctx context.Context, op PolicyWrite, resources []ros.Resource) error {
	previous, err := ParseManifest(op.Previous)
	if err != nil {
		return err
	}
	if previous == nil || previous.Spec == nil || len(previous.Spec.Sources) == 0 || len(op.StaticChecks) == 0 {
		return nil
	}
	facts, nat, err := d.readState(ctx)
	if err != nil {
		return err
	}
	live, err := d.snapshotPolicy(ctx, false)
	if err != nil {
		return err
	}
	removing := PolicyKeys(append(slices.Clone(op.Owned), op.Migrations...))
	policy := maps.Clone(removing)
	maps.Copy(policy, PolicyKeys(resources))
	context := nat
	if context == nil {
		context = &ros.NatContext{}
	}
	copyContext := *context
	context = &copyContext
	context.Mangle = []ros.Row{}
	rows := []ros.Row{}
	for _, row := range (*live)[ros.PathMangle] {
		if !policy[[2]string{ros.PathMangle, row.ID()}] {
			context.Mangle = append(context.Mangle, row)
		}
	}
	for _, row := range (*live)[ros.PathNAT] {
		if !removing[[2]string{ros.PathNAT, row.ID()}] {
			rows = append(rows, row)
		}
	}
	index := ros.NewRouteEgressIndex(context)
	sources := device.Networks(previous.Spec.Sources)
	for _, check := range op.StaticChecks {
		destinations := []netip.Prefix{}
		target, ok := netipx.ParseNetwork4(check.CIDR, true)
		if !ok {
			return device.Error(device.MessageOperationFormat)
		}
		for _, old := range device.Networks(previous.Spec.Destinations) {
			if old.Overlaps(target) {
				if ros.SubnetOf(old, target) {
					destinations = append(destinations, old)
				} else {
					destinations = append(destinations, target)
				}
			}
		}
		if len(destinations) == 0 {
			continue
		}
		iface, reason := ros.Egress(check.CIDR, check.Gateway, context, false, index)
		if iface == "" || !ros.PreservesSources(rows, sources, destinations, iface, context, facts.AddressSets, nil, nil) {
			if reason != "" {
				reason = "；" + reason
			}
			return device.Error(messagePolicyStaticNATUnproven + check.CIDR + reason)
		}
	}
	return nil
}

// VerifyNAT proves the same NAT prerequisites before withdrawal and on final
// readback. R12 shares this implementation rather than reimplementing policy NAT.
func VerifyNAT(ctx context.Context, checks device.NATChecks, facts *device.State, nat *ros.NatContext) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	index := ros.NewRouteEgressIndex(nat)
	uncovered := []string{}
	managed := []netip.Prefix{}
	if nat == nil && facts != nil {
		for _, rule := range facts.NatBypass {
			if rule.Enabled && device.IsManaged(rule.Comment) {
				managed = append(managed, device.Networks(rule.Dst)...)
			}
		}
	}
	coverage := device.NewSupernetIndex(managed)
	for _, cidr := range checks.NATBypass {
		ok := false
		if nat != nil {
			status := ros.AssessBypass(cidr, checks.Gateway, facts, nat, index).Status
			ok = status == "synced" || status == "not_required"
		} else if prefix, valid := netipx.ParseNetwork4(cidr, true); valid {
			ok = coverage.Covers(prefix)
		}
		if !ok {
			uncovered = append(uncovered, cidr)
		}
	}
	if len(uncovered) > 0 {
		return device.Error(fmt.Sprintf(messageNATVerifyUncovered, strings.Join(uncovered, ", ")))
	}
	for _, check := range checks.NATNotRequired {
		assessment := ros.AssessBypass(check.CIDR, check.Gateway, facts, nat, index)
		if assessment.Status != "not_required" {
			return device.Error(fmt.Sprintf(messageNATVerifyRequired, check.CIDR, assessment.Detail))
		}
	}
	return nil
}

// VerifyROSPolicy validates ownership, runtime activation, lookup order, actual
// next hop and NAT evidence. Only transient invalid flags are waited for.
func VerifyROSPolicy(ctx context.Context, c *ros.Client, readState StateReader, check *ROSPolicyCheck) error {
	d := rosDevice{Client: c, readState: readState}
	if check == nil {
		return device.Error(device.MessageOperationFormat)
	}
	raw := check.Previous
	if d.PolicyManifestSet() {
		raw = d.PolicyManifest()
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
			return device.Error(messagePolicyWithdrawalIncomplete)
		}
		return nil
	}
	if len(check.Spec.Destinations) == 0 {
		return device.Error(messagePolicyIncomplete)
	}
	keys := PolicyKeys(owned)
	for range 8 {
		invalid := false
		for path, rows := range *live {
			for _, row := range rows {
				if keys[[2]string{path, row.ID()}] && row.Flag("invalid") {
					invalid = true
				}
			}
		}
		if !invalid {
			break
		}
		if err = ros.WaitContext(ctx, 250*time.Millisecond); err != nil {
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
	manifest, err := ParseManifest(raw)
	if err != nil {
		return err
	}
	if manifest == nil || !ros.JSONValuesEqual(device.JSON(manifest.Spec), device.JSON(check.Spec)) || len(owned) != len(manifest.Resources) {
		return device.Error(messagePolicyIncomplete)
	}
	if err = CheckReferences(owned, *live); err != nil {
		return err
	}
	if err = CheckOrder(*live); err != nil {
		return err
	}
	if err = policyHealth(raw, *live); err != nil {
		return err
	}
	keys = PolicyKeys(owned)
	for _, row := range (*live)[ros.PathFilter] {
		if row.Enabled() && row.Is("action", "fasttrack-connection") {
			return device.Error(messagePolicyNewFastTrack)
		}
	}
	_, nat, err := d.readState(ctx)
	if err != nil {
		return err
	}
	if nat == nil {
		return device.Error(messagePolicyUnreadable)
	}
	context := *nat
	context.Mangle = []ros.Row{}
	for _, row := range (*live)[ros.PathMangle] {
		if !keys[[2]string{ros.PathMangle, row.ID()}] {
			context.Mangle = append(context.Mangle, row)
		}
	}
	context.RoutingRules = (*live)["/routing/rule"]
	context.VRFs = (*live)["/ip/vrf"]
	context.Routes = (*live)[ros.PathRoutes]
	if slices.ContainsFunc(context.Mangle, conflictingPolicyMangle) {
		return device.Error(messagePolicyNewMangle)
	}
	index := ros.NewRouteEgressIndex(&context)
	iface, reason := ros.Egress(check.Spec.Destinations[0], check.Spec.Gateway, &context, true, index)
	if iface == "" {
		return device.Error(messagePolicyReadEgress + reason)
	}
	if iface != check.Spec.EgressInterface || check.Spec.NATInterface != "" && iface != check.Spec.NATInterface {
		return device.Error(messagePolicyInterfaceChanged)
	}
	if check.Spec.NATInterface == "" {
		for _, cidr := range check.Spec.Destinations {
			assessment := ros.AssessNAT(cidr, check.Spec.Gateway, &context, true, index)
			if assessment.Status != "not_required" {
				return device.Error(messagePolicyNATNowRequired + assessment.Detail)
			}
		}
	}
	for _, entry := range owned {
		if entry.Path == ros.PathRoutes {
			row, err := d.ResourceRow(ctx, ros.PathRoutes, entry.ID)
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
			present[[2]string{path, row.ID()}] = true
		}
	}
	for _, entry := range check.Migrations {
		if present[[2]string{entry.Path, entry.ID}] {
			return device.Error(messagePolicyMigrationPresent)
		}
	}
	for _, entry := range check.Replacements {
		values := ros.CloneRow(entry.Values)
		delete(values, "place-before")
		if !slices.ContainsFunc((*live)[entry.Path], func(row ros.Row) bool { return ros.ValuesMatch(entry.Path, row, values) }) {
			return device.Error(messagePolicyReplacementMissing)
		}
	}
	return nil
}
