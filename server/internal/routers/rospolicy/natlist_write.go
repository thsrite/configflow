package rospolicy

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

// NATListUndo is the reverse-ordered journal returned after a successful update.
// Keep contains the original and replacement protection rules for ref checks.
type NATListUndo struct {
	Kind     string        `json:"kind"`
	Steps    []ros.NATStep `json:"steps"`
	Keep     []string      `json:"keep"`
	Remapped [][3]string   `json:"remapped,omitempty"`
}

func (d rosDevice) confirmResource(ctx context.Context, path string, values ros.Row, id string, undo *ros.NATStep, actual ros.Row) (ros.Resource, error) {
	if actual == nil || !ros.ValuesMatch(path, actual, values) {
		detail := MessageMissingDetail
		if actual != nil {
			fields := []string{}
			for _, key := range slices.Sorted(maps.Keys(values)) {
				if !ros.ValuesMatch(path, actual, ros.Row{key: values[key]}) {
					fields = append(fields, key)
				}
			}
			detail = fmt.Sprintf(messagePolicyFieldsDetail, strings.Join(fields, "、"))
		}
		return ros.Resource{}, device.Error(fmt.Sprintf(MessageConfirm, path, detail))
	}
	if actual.Flag("dynamic") {
		return ros.Resource{}, device.Error(device.MessagePolicyDynamic)
	}
	undo.Config = ros.ConfigROSRow(actual)
	if path == ros.PathRoutes && device.FirstNonEmpty(values.Text("routing-table"), "main") != "main" && !ros.Flag(actual["active"]) {
		return ros.Resource{}, device.Error(messagePolicyRouteNotReady)
	}
	if path == ros.PathNAT && values.Set("place-before") {
		rows, err := d.List(ctx, path)
		if err != nil {
			return ros.Resource{}, err
		}
		before, at := ros.IndexOfRow(rows, values.Text("place-before")), ros.IndexOfRow(rows, id)
		if before < 0 || at < 0 || at >= before {
			return ros.Resource{}, device.Error(messagePolicyNATOrder)
		}
	}
	return ros.NewResource(path, actual), nil
}
func (d rosDevice) createResource(ctx context.Context, path string, values ros.Row, journal *[]ros.NATStep, ids ros.ROSIDs, restoring bool) (ros.Resource, error) {
	values = ros.CloneRow(values)
	if path == ros.PathNAT && !restoring {
		rows, err := d.List(ctx, path)
		if err != nil {
			return ros.Resource{}, err
		}
		if len(rows) > 0 && !values.Has("place-before") {
			values["place-before"] = rows[0][".id"]
		}
	}
	value, err := d.Request(ctx, "PUT", path, values, ros.Timeout)
	row := ros.AsRow(value)
	if err == nil && (row == nil || !ros.IDPattern.MatchString(row.ID()) || ids.Table(path)[row.ID()]) {
		err = device.KindError(device.KindUncertainWrite, messagePolicyMissingID)
	}
	if err != nil {
		if device.RouterErrorKind(err) != device.KindUncertainWrite {
			return ros.Resource{}, err
		}
		rows, readErr := d.List(ctx, path)
		if readErr != nil {
			return ros.Resource{}, readErr
		}
		candidates := []ros.Row{}
		for _, row := range rows {
			if !ids.Table(path)[row.ID()] && ros.IDPattern.MatchString(row.ID()) && !row.Flag("dynamic") && ros.ValuesMatch(path, row, values) {
				candidates = append(candidates, row)
			}
		}
		if len(candidates) != 1 {
			return ros.Resource{}, device.RollbackError(messagePolicyUnknownWrite)
		}
		*journal = append(*journal, ros.StepResource("delete", ros.NewResource(path, candidates[0])))
		return ros.Resource{}, err
	}
	id := row.ID()
	ids.Table(path)[id] = true
	step := ros.StepResource("delete", ros.NewResource(path, row))
	*journal = append(*journal, step)
	index := len(*journal) - 1
	actual, err := d.ResourceRow(ctx, path, id)
	if err != nil {
		return ros.Resource{}, err
	}
	return d.confirmResource(ctx, path, values, id, &(*journal)[index], actual)
}
func (d rosDevice) deleteResource(ctx context.Context, item ros.Resource, journal *[]ros.NATStep) error {
	row, err := d.GuardResource(ctx, item)
	if err != nil {
		return err
	}
	values, err := ros.WritableResource(item.Path, row)
	if err != nil {
		return err
	}
	if item.Path == ros.PathNAT || item.Path == ros.PathMangle {
		rows, err := d.List(ctx, item.Path)
		if err != nil {
			return err
		}
		at := ros.IndexOfRow(rows, item.ID)
		if at < 0 {
			return device.Error(fmt.Sprintf(device.MessagePolicyChanged, item.Path, item.ID))
		}
		if at+1 < len(rows) {
			values["place-before"] = rows[at+1][".id"]
		}
	}
	step := ros.StepResource("restore", item)
	step.Values = values
	*journal = append(*journal, step)
	if _, err = d.Request(ctx, "DELETE", item.Path+"/"+ros.EscapePathID(item.ID), nil, ros.Timeout); err != nil {
		return err
	}
	current, err := d.ResourceRow(ctx, item.Path, item.ID)
	if err != nil {
		return err
	}
	if current != nil {
		return device.Error(device.MessagePolicyStillPresent)
	}
	return nil
}

func indexROSRows(rows []ros.Row) (map[string]ros.Row, map[string]int) {
	byID := make(map[string]ros.Row, len(rows))
	position := make(map[string]int, len(rows))
	for i, row := range rows {
		byID[row.ID()] = row
		position[row.ID()] = i
	}
	return byID, position
}

// DeleteNATRules guards a whole legacy run and records original positions before
// batched removal. A fallback rechecks every remaining snapshot before DELETE.
func (d rosDevice) DeleteNATRules(ctx context.Context, items []ros.Resource, journal *[]ros.NATStep) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return err
	}
	byID, position := indexROSRows(rows)
	deleting := map[string]bool{}
	for _, item := range items {
		if item.Path != ros.PathNAT || item.Config == nil || item.ID == "" {
			return device.Error(device.MessageOperationFormat)
		}
		row := byID[item.ID]
		if row == nil || !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(item.Config)) {
			return device.Error(fmt.Sprintf(messageNATChanged, ros.PathNAT, item.ID))
		}
		deleting[item.ID] = true
	}
	following := make([]int, len(rows))
	next := -1
	for i := len(rows) - 1; i >= 0; i-- {
		following[i] = next
		if !deleting[rows[i].ID()] {
			next = i
		}
	}
	steps := []ros.NATStep{}
	ids := []string{}
	for _, item := range items {
		row := byID[item.ID]
		values, err := ros.WritableResource(ros.PathNAT, row)
		if err != nil {
			return err
		}
		at := position[item.ID]
		step := ros.StepResource("restore", item)
		step.Values = values
		step.Order = new(at)
		if next := following[at]; next >= 0 {
			values["place-before"] = rows[next][".id"]
			step.FollowingOrder = new(next)
		}
		steps = append(steps, step)
		ids = append(ids, item.ID)
	}
	*journal = append(*journal, steps...)
	for start := 0; start < len(ids); start += 500 {
		_, err = d.Request(ctx, "POST", ros.PathNAT+"/remove", ros.Row{".id": strings.Join(ids[start:min(start+500, len(ids))], ",")}, ros.Timeout)
		if err != nil {
			break
		}
	}
	if err != nil {
		remaining, err := d.List(ctx, ros.PathNAT)
		if err != nil {
			return err
		}
		remainingByID, _ := indexROSRows(remaining)
		for _, item := range items {
			if row := remainingByID[item.ID]; row != nil {
				if !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(item.Config)) || row.Flag("dynamic") {
					return device.Error(fmt.Sprintf(messageNATChanged, ros.PathNAT, item.ID))
				}
				if _, err := d.Request(ctx, "DELETE", ros.PathNAT+"/"+ros.EscapePathID(item.ID), nil, ros.Timeout); err != nil {
					return err
				}
			}
		}
	}
	remaining, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return err
	}
	for _, row := range remaining {
		if deleting[row.ID()] {
			return device.Error(messageNATLegacyPresent)
		}
	}
	return nil
}

func (d rosDevice) restoreResource(ctx context.Context, s ros.NATStep, ids ros.ROSIDs, remapped map[[2]string]string, deleteSafe bool) error {
	row, err := d.ResourceRow(ctx, s.Path, s.ID)
	if err != nil {
		return err
	}
	if s.Action == "delete" {
		if !deleteSafe || row == nil {
			return nil
		}
		if _, err := d.GuardResource(ctx, ros.Resource{Path: s.Path, ID: s.ID, Config: s.Config}); err != nil {
			return err
		}
		if row.Flag("dynamic") {
			return device.Error(device.MessagePolicyDynamic)
		}
		if s.Path == ros.PathTables {
			live, err := d.snapshotPolicy(ctx, false)
			if err != nil {
				return err
			}
			if err = CheckReferences([]ros.Resource{{Path: s.Path, ID: s.ID, Config: s.Config}}, *live); err != nil {
				return err
			}
		}
		if _, err := d.Request(ctx, "DELETE", s.Path+"/"+ros.EscapePathID(s.ID), nil, ros.Timeout); err != nil {
			return err
		}
		row, err = d.ResourceRow(ctx, s.Path, s.ID)
		if err != nil {
			return err
		}
		if row != nil {
			return device.Error(device.MessageRollbackDeletePresent)
		}
		return nil
	}
	if row != nil {
		if _, err := d.GuardResource(ctx, ros.Resource{Path: s.Path, ID: s.ID, Config: s.Config}); err != nil {
			return err
		}
		return nil
	}
	values := ros.CloneRow(s.Values)
	if before := values.Text("place-before"); before != "" {
		if id, ok := remapped[[2]string{s.Path, before}]; ok {
			before = id
			values["place-before"] = id
		}
		anchor, err := d.ResourceRow(ctx, s.Path, before)
		if err != nil {
			return err
		}
		if anchor == nil {
			return device.Error(messagePolicyAnchor)
		}
	}
	restored, err := d.createResource(ctx, s.Path, values, &[]ros.NATStep{}, ids, true)
	if err != nil {
		return err
	}
	remapped[[2]string{s.Path, s.ID}] = restored.ID
	return nil
}
func (d rosDevice) restoreNATRules(ctx context.Context, steps []ros.NATStep, ids ros.ROSIDs, remapped map[[2]string]string, failures *[]string) {
	rows, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		*failures = append(*failures, err.Error())
		return
	}
	byID, _ := indexROSRows(rows)
	batched := []ros.NATStep{}
	single := []ros.NATStep{}
	for _, s := range steps {
		if s.Order != nil {
			batched = append(batched, s)
		} else {
			single = append(single, s)
		}
	}
	slices.SortFunc(batched, func(a, b ros.NATStep) int { return *a.Order - *b.Order })
	present := []ros.NATStep{}
	for _, s := range batched {
		if byID[s.ID] != nil {
			present = append(present, s)
		}
	}
	pending := []ros.NATStep{}
	changes := []ros.ROSChange{}
	for _, s := range batched {
		if row := byID[s.ID]; row != nil {
			if !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(s.Config)) {
				*failures = append(*failures, fmt.Sprintf(messageNATChanged, ros.PathNAT, s.ID))
			}
			continue
		}
		values := ros.CloneRow(s.Values)
		before := values.Text("place-before")
		if newID, ok := remapped[[2]string{ros.PathNAT, before}]; ok {
			before = newID
			values["place-before"] = newID
		}
		var sibling *ros.NATStep
		if i := sort.Search(len(present), func(i int) bool { return *present[i].Order > *s.Order }); i < len(present) {
			sibling = &present[i]
		}
		if sibling != nil && !(byID[before] != nil && s.FollowingOrder != nil && *s.FollowingOrder < *sibling.Order) {
			values["place-before"] = sibling.ID
		}
		if anchor := values.Text("place-before"); anchor != "" && byID[anchor] == nil {
			*failures = append(*failures, messagePolicyAnchor)
			continue
		}
		s.Values = values
		pending = append(pending, s)
		changes = append(changes, ros.ROSChange{Method: "PUT", Path: ros.PathNAT, Values: values})
	}
	handled := 0
	undo := []ros.ROSChange{}
	if len(pending) >= d.BatchMin() {
		handled, err = d.PutNAT(ctx, changes, &undo)
	}
	for i := 0; i < min(handled, len(undo)); i++ {
		remapped[[2]string{ros.PathNAT, pending[i].ID}] = undo[i].ID
		ids.Table(ros.PathNAT)[undo[i].ID] = true
	}
	if err != nil {
		*failures = append(*failures, err.Error())
		return
	}
	for _, s := range append(pending[handled:], single...) {
		if err := d.restoreResource(ctx, s, ids, remapped, true); err != nil {
			*failures = append(*failures, err.Error())
		}
	}
}
func (d rosDevice) runNATSteps(ctx context.Context, steps []ros.NATStep, ids ros.ROSIDs, remapped map[[2]string]string, failures *[]string, deleteSafe bool) {
	for start := 0; start < len(steps); {
		end := start + 1
		address := steps[start].Path == ros.PathAddresses
		action := steps[start].Action
		for end < len(steps) && steps[end].Action == action && (steps[end].Path == ros.PathAddresses) == address && (action != "restore" || steps[end].Path == steps[start].Path) {
			end++
		}
		run := steps[start:end]
		var err error
		if len(*failures) > 0 && (action == "delete" || action == "sweep") {
			deleteSafe = false
		}
		if address {
			switch action {
			case "restore":
				err = d.RestoreAddresses(ctx, run, ids, remapped)
			case "delete":
				if deleteSafe {
					err = d.RestoreDeleteAddresses(ctx, run)
				}
			case "sweep":
				if deleteSafe {
					for _, s := range run {
						if err = d.RestoreAddressSweep(ctx, s); err != nil {
							break
						}
					}
				}
			}
		} else if action == "operation" {
			if len(*failures) == 0 {
				undos := []json.RawMessage{}
				for _, s := range run {
					raw := s.Undo
					var head device.OpHead
					_ = jsondecode.Unmarshal(raw, &head)
					if head.Kind == "nat_list_restore" {
						var undo NATListUndo
						if jsondecode.Unmarshal(raw, &undo) != nil {
							*failures = append(*failures, device.MessageOperationFormat)
							break
						}
						for _, key := range slices.SortedFunc(maps.Keys(remapped), func(a, b [2]string) int {
							if c := strings.Compare(a[0], b[0]); c != 0 {
								return c
							}
							return strings.Compare(a[1], b[1])
						}) {
							undo.Remapped = append(undo.Remapped, [3]string{key[0], key[1], remapped[key]})
						}
						raw = device.JSON(undo)
					}
					undos = append(undos, raw)
				}
				if len(*failures) == 0 {
					for _, failure := range d.restoreMany(ctx, undos, true) {
						if failure != "" && failure != ros.BatchSkipped {
							*failures = append(*failures, failure)
						}
					}
				}
			}
		} else if action == "restore" && run[0].Path == ros.PathRoutes && len(run) >= d.BatchMin() {
			d.restorePolicyRoutes(ctx, run, ids, remapped, failures)
		} else if action == "restore" && len(run) >= d.BatchMin() && run[0].Path == ros.PathNAT {
			d.restoreNATRules(ctx, run, ids, remapped, failures)
		} else {
			for _, s := range run {
				if e := d.restoreResource(ctx, s, ids, remapped, deleteSafe && len(*failures) == 0); e != nil {
					*failures = append(*failures, e.Error())
				}
			}
		}
		if err != nil {
			*failures = append(*failures, err.Error())
		}
		start = end
	}
}

func natIDs(rows []ros.Row, lists map[string][]ros.Row) ros.ROSIDs {
	ids := ros.ROSIDs{ros.PathNAT: ros.RowIDs(rows), ros.PathAddresses: map[string]bool{}}
	for _, name := range []string{ros.NATSrcList, ros.NATDstList} {
		for _, row := range lists[name] {
			ids[ros.PathAddresses][row.ID()] = true
		}
	}
	return ids
}
func (d rosDevice) checkNATUnchanged(ctx context.Context, op NATListSync, full, rows []ros.Row, lists map[string][]ros.Row) error {
	keep := map[string]bool{}
	if op.Rule != nil {
		keep[op.Rule.ID] = true
	}
	foreign, err := d.foreignNATRefs(ctx, keep, full)
	if err != nil {
		return err
	}
	if len(foreign) > 0 {
		return device.Error(fmt.Sprintf(messageNATForeignWrite, foreign[0].Path, foreign[0].Name))
	}
	found := ros.InspectNATListRule(rows, nil)
	if found.Problem != "" {
		return device.Error(found.Problem)
	}
	if op.Rule == nil {
		if found.Rule != nil {
			return device.Error(messageNATChangedPreview)
		}
	} else if found.Rule == nil || found.Rule.ID() != op.Rule.ID || !ros.SameRow(ros.ConfigROSRow(found.Rule), ros.ConfigROSRow(op.Rule.Config)) {
		return device.Error(messageNATChangedPreview)
	}
	byID, _ := indexROSRows(rows)
	for _, item := range append(slices.Clone(op.Legacy), op.Stale...) {
		row := byID[item.ID]
		if row == nil || !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(item.Config)) {
			return device.Error(messageNATChangedPreview)
		}
	}
	for i, name := range []string{ros.NATSrcList, ros.NATDstList} {
		_, foreign, problems := natMembers(name, lists[name])
		if len(problems) > 0 {
			return device.Error(problems[0])
		}
		if len(foreign) > 0 && op.Rule == nil {
			return device.Error(fmt.Sprintf(messageNATListForeign, name))
		}
		removing, adding := op.SrcRemove, op.SrcAdd
		if i == 1 {
			removing, adding = op.DstRemove, op.DstAdd
		}
		members, _ := indexROSRows(lists[name])
		presentKeys := map[string]bool{}
		for _, row := range lists[name] {
			presentKeys[NATKey(row.Text("address"))] = true
		}
		for _, item := range removing {
			row := members[item.ID]
			if row == nil || !ros.SameRow(ros.ConfigROSRow(row), ros.ConfigROSRow(item.Config)) {
				return device.Error(messageNATChangedPreview)
			}
		}
		for _, value := range adding {
			if presentKeys[NATKey(value)] {
				return device.Error(messageNATChangedPreview)
			}
		}
	}
	return nil
}
func (d rosDevice) settleNATList(ctx context.Context, id string) error {
	for attempt := 0; attempt < 8; attempt++ {
		row, err := d.ResourceRow(ctx, ros.PathNAT, id)
		if err != nil {
			return err
		}
		if row == nil {
			return device.Error(messageNATNewMissing)
		}
		if !row.Flag("invalid") {
			return nil
		}
		if attempt < 7 {
			if err := ros.WaitContext(ctx, 250*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return device.Error(messageNATNewInvalid)
}
func (d rosDevice) proveNATList(ctx context.Context, op NATListSync, id string) error {
	legacy := map[string]bool{}
	for _, item := range append(slices.Clone(op.Legacy), op.Stale...) {
		legacy[item.ID] = true
	}
	rows, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return err
	}
	rows = slices.DeleteFunc(slices.Clone(d.assessedNAT(rows)), func(row ros.Row) bool { return legacy[row.ID()] })
	found := ros.InspectNATListRule(rows, nil)
	lists, err := d.readNATLists(ctx)
	if err != nil {
		return err
	}
	sets := map[string][]string{}
	for _, name := range []string{ros.NATSrcList, ros.NATDstList} {
		for _, row := range lists[name] {
			if row.Enabled() {
				sets[name] = append(sets[name], row.Text("address"))
			}
		}
	}
	contextRows := &ros.NatContext{}
	if found.Problem != "" || found.Rule == nil || found.Rule.ID() != id || !ros.PreservesSources(rows, device.Networks(op.Sources), device.Networks(op.Destinations), op.OutInterface, contextRows, sets, nil, nil) {
		message := messageNATCoverage
		if found.Problem != "" {
			message += "：" + found.Problem
		}
		return device.Error(message)
	}
	return nil
}

// ApplyNATLists runs one list delta to completion, compensating in reverse order
// on failure. Every mutation is guarded by the preview and recorded before I/O.
func (d rosDevice) ApplyNATLists(ctx context.Context, op NATListSync) (*NATListUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.InvalidateAddressReads()
	full, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return nil, err
	}
	rows := d.assessedNAT(full)
	lists, err := d.readNATLists(ctx)
	if err != nil {
		return nil, err
	}
	if err = d.checkNATUnchanged(ctx, op, full, rows, lists); err != nil {
		return nil, err
	}
	d.MarkNATListTouched()
	ids := natIDs(full, lists)
	journal := []ros.NATStep{}
	keep := []string{}
	if op.Rule != nil {
		keep = append(keep, op.Rule.ID)
	}
	createRule := func() (string, error) {
		values := ros.Row{"chain": "srcnat", "action": "accept", "src-address-list": ros.NATSrcList, "dst-address-list": ros.NATDstList, "comment": ros.NATListComment, "disabled": "false"}
		if op.OutInterface != "" {
			values["out-interface"] = op.OutInterface
		}
		item, err := d.createResource(ctx, ros.PathNAT, values, &journal, ids, false)
		return item.ID, err
	}
	run := func() error {
		if op.Withdraw {
			if op.Rule == nil {
				return device.Error(messageNATChangedPreview)
			}
			if err := d.deleteResource(ctx, *op.Rule, &journal); err != nil {
				return err
			}
			if len(op.DstRemove) > 0 {
				if err := d.DeleteAddresses(ctx, op.DstRemove, &journal); err != nil {
					return err
				}
			}
			if len(op.SrcRemove) > 0 {
				return d.DeleteAddresses(ctx, op.SrcRemove, &journal)
			}
			return nil
		}
		if len(op.SrcAdd) > 0 {
			if _, err := d.CreateAddressesWithProof(ctx, ros.NATEntries(ros.NATSrcList, op.SrcAdd), &journal, ids, nil); err != nil {
				return err
			}
		}
		if len(op.SrcRemove) > 0 {
			if err := d.DeleteAddresses(ctx, op.SrcRemove, &journal); err != nil {
				return err
			}
		}
		id := ""
		if op.Rule != nil {
			id = op.Rule.ID
		}
		if op.ReplaceRule {
			if op.Rule == nil {
				return device.Error(messageNATChangedPreview)
			}
			var err error
			id, err = createRule()
			if err != nil {
				return err
			}
			keep = append(keep, id)
			if err := d.deleteResource(ctx, *op.Rule, &journal); err != nil {
				return err
			}
		}
		if len(op.DstAdd) > 0 {
			if _, err := d.CreateAddressesWithProof(ctx, ros.NATEntries(ros.NATDstList, op.DstAdd), &journal, ids, nil); err != nil {
				return err
			}
		}
		if op.Rule == nil && len(op.Destinations) > 0 {
			var err error
			id, err = createRule()
			if err != nil {
				return err
			}
			keep = append(keep, id)
		}
		if id != "" {
			if err := d.settleNATList(ctx, id); err != nil {
				return err
			}
		}
		if len(op.Legacy)+len(op.Stale) > 0 {
			if id != "" {
				if err := d.proveNATList(ctx, op, id); err != nil {
					return err
				}
			}
			items := append(slices.Clone(op.Legacy), op.Stale...)
			_, position := indexROSRows(full)
			slices.SortFunc(items, func(a, b ros.Resource) int { return position[a.ID] - position[b.ID] })
			if err := d.DeleteNATRules(ctx, items, &journal); err != nil {
				return err
			}
		}
		if len(op.DstRemove) > 0 {
			return d.DeleteAddresses(ctx, op.DstRemove, &journal)
		}
		return nil
	}
	err = run()
	slices.Reverse(journal)
	undo := &NATListUndo{Kind: "nat_list_restore", Steps: journal, Keep: keep}
	if err != nil {
		if rollback := d.RestoreNATLists(ctx, *undo); rollback != nil {
			return nil, device.RollbackError(err.Error() + "；" + rollback.Error())
		}
		return nil, err
	}
	return undo, nil
}

// RestoreNATLists restores removed protection before deleting new objects. A
// failed restoration or new foreign reference preserves the new protection and
// reports an incomplete rollback; it never deletes by a destination guess.
func (d rosDevice) RestoreNATLists(ctx context.Context, op NATListUndo) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, step := range op.Steps {
		if (step.Path != ros.PathNAT && step.Path != ros.PathAddresses) || (step.Action != "delete" && step.Action != "restore" && step.Action != "sweep") || (step.Action != "sweep" && (step.ID == "" || step.Config == nil)) {
			return device.Error(device.MessageOperationFormat)
		}
	}
	d.InvalidateAddressReads()
	d.MarkNATListTouched()
	rows, err := d.List(ctx, ros.PathNAT)
	if err != nil {
		return err
	}
	lists, err := d.readNATLists(ctx)
	if err != nil {
		return err
	}
	ids := natIDs(rows, lists)
	remapped := map[[2]string]string{}
	for _, r := range op.Remapped {
		remapped[[2]string{r[0], r[1]}] = r[2]
	}
	ruleIDs := device.StringSet(op.Keep...)
	for _, step := range op.Steps {
		if step.Path == ros.PathNAT {
			ruleIDs[step.ID] = true
		}
	}
	failures := []string{}
	deleteSafe := true
	foreign, err := d.foreignNATRefs(ctx, ruleIDs, nil)
	if err != nil {
		deleteSafe = false
		failures = append(failures, err.Error())
	} else if len(foreign) > 0 {
		deleteSafe = false
		failures = append(failures, fmt.Sprintf(messageNATForeignRestore, foreign[0].Path, foreign[0].Name))
	}
	kept := false
	for start := 0; start < len(op.Steps); {
		deleting := op.Steps[start].Action == "delete"
		end := start + 1
		for end < len(op.Steps) && (op.Steps[end].Action == "delete") == deleting {
			end++
		}
		if deleting && len(failures) > 0 && deleteSafe {
			kept = true
		}
		d.runNATSteps(ctx, op.Steps[start:end], ids, remapped, &failures, deleteSafe && len(failures) == 0)
		start = end
	}
	if kept {
		failures = append(failures, messageNATKeptProtection)
	}
	if len(failures) > 0 {
		return device.RollbackError(messageNATRollbackPrefix + strings.Join(failures, "；"))
	}
	return nil
}
