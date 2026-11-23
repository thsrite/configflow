package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
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
	Kind     string      `json:"kind"`
	Steps    []NATStep   `json:"steps"`
	Keep     []string    `json:"keep"`
	Remapped [][3]string `json:"remapped,omitempty"`
}

func init() {
	RegisterRouterOSHandler("nat_list_sync", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op NATListSync
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		undo, err := d.ApplyNATLists(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	})
	RegisterRouterOSHandler("nat_list_restore", func(ctx context.Context, d *RouterOS, raw json.RawMessage) (json.RawMessage, error) {
		var op NATListUndo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, routerError(messageOperationFormat)
		}
		if err := d.RestoreNATLists(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage(`{}`), nil
	})
}

func (d *RouterOS) confirmResource(ctx context.Context, path string, values Row, id string, undo *NATStep, actual Row) (ROSResource, error) {
	if actual == nil || !valuesMatch(path, actual, values) {
		detail := messagePolicyMissingDetail
		if actual != nil {
			fields := []string{}
			for _, key := range slices.Sorted(maps.Keys(values)) {
				if !valuesMatch(path, actual, Row{key: values[key]}) {
					fields = append(fields, key)
				}
			}
			detail = fmt.Sprintf(messagePolicyFieldsDetail, strings.Join(fields, "、"))
		}
		return ROSResource{}, routerError(fmt.Sprintf(messagePolicyConfirm, path, detail))
	}
	if actual.flag("dynamic") {
		return ROSResource{}, routerError(messagePolicyDynamic)
	}
	undo.Config = configROSRow(actual)
	if path == rosRoutes && firstNonEmpty(values.text("routing-table"), "main") != "main" && !rosFlag(actual["active"]) {
		return ROSResource{}, routerError(messagePolicyRouteNotReady)
	}
	if path == rosNAT && values.set("place-before") {
		rows, err := d.list(ctx, path)
		if err != nil {
			return ROSResource{}, err
		}
		before, at := indexOfRow(rows, values.text("place-before")), indexOfRow(rows, id)
		if before < 0 || at < 0 || at >= before {
			return ROSResource{}, routerError(messagePolicyNATOrder)
		}
	}
	return resource(path, actual), nil
}
func (d *RouterOS) createResource(ctx context.Context, path string, values Row, journal *[]NATStep, ids ROSIDs, restoring bool) (ROSResource, error) {
	values = cloneRow(values)
	if path == rosNAT && !restoring {
		rows, err := d.list(ctx, path)
		if err != nil {
			return ROSResource{}, err
		}
		if len(rows) > 0 && !values.has("place-before") {
			values["place-before"] = rows[0][".id"]
		}
	}
	value, err := d.request(ctx, "PUT", path, values, rosTimeout)
	row := asRow(value)
	if err == nil && (row == nil || !rosIDPattern.MatchString(row.id()) || ids.table(path)[row.id()]) {
		err = rosError(KindUncertainWrite, messagePolicyMissingID)
	}
	if err != nil {
		if routerErrorKind(err) != KindUncertainWrite {
			return ROSResource{}, err
		}
		rows, readErr := d.list(ctx, path)
		if readErr != nil {
			return ROSResource{}, readErr
		}
		candidates := []Row{}
		for _, row := range rows {
			if !ids.table(path)[row.id()] && rosIDPattern.MatchString(row.id()) && !row.flag("dynamic") && valuesMatch(path, row, values) {
				candidates = append(candidates, row)
			}
		}
		if len(candidates) != 1 {
			return ROSResource{}, rollbackError(messagePolicyUnknownWrite)
		}
		*journal = append(*journal, stepResource("delete", resource(path, candidates[0])))
		return ROSResource{}, err
	}
	id := row.id()
	ids.table(path)[id] = true
	step := stepResource("delete", resource(path, row))
	*journal = append(*journal, step)
	index := len(*journal) - 1
	actual, err := d.resourceRow(ctx, path, id)
	if err != nil {
		return ROSResource{}, err
	}
	return d.confirmResource(ctx, path, values, id, &(*journal)[index], actual)
}
func (d *RouterOS) deleteResource(ctx context.Context, item ROSResource, journal *[]NATStep) error {
	row, err := d.guardResource(ctx, item)
	if err != nil {
		return err
	}
	values, err := writableResource(item.Path, row)
	if err != nil {
		return err
	}
	if item.Path == rosNAT || item.Path == rosMangle {
		rows, err := d.list(ctx, item.Path)
		if err != nil {
			return err
		}
		at := indexOfRow(rows, item.ID)
		if at < 0 {
			return routerError(fmt.Sprintf(messagePolicyChanged, item.Path, item.ID))
		}
		if at+1 < len(rows) {
			values["place-before"] = rows[at+1][".id"]
		}
	}
	step := stepResource("restore", item)
	step.Values = values
	*journal = append(*journal, step)
	if _, err = d.request(ctx, "DELETE", item.Path+"/"+escapeROSPathID(item.ID), nil, rosTimeout); err != nil {
		return err
	}
	current, err := d.resourceRow(ctx, item.Path, item.ID)
	if err != nil {
		return err
	}
	if current != nil {
		return routerError(messagePolicyStillPresent)
	}
	return nil
}

func indexROSRows(rows []Row) (map[string]Row, map[string]int) {
	byID := make(map[string]Row, len(rows))
	position := make(map[string]int, len(rows))
	for i, row := range rows {
		byID[row.id()] = row
		position[row.id()] = i
	}
	return byID, position
}

// DeleteNATRules guards a whole legacy run and records original positions before
// batched removal. A fallback rechecks every remaining snapshot before DELETE.
func (d *RouterOS) DeleteNATRules(ctx context.Context, items []ROSResource, journal *[]NATStep) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := d.list(ctx, rosNAT)
	if err != nil {
		return err
	}
	byID, position := indexROSRows(rows)
	deleting := map[string]bool{}
	for _, item := range items {
		if item.Path != rosNAT || item.Config == nil || item.ID == "" {
			return routerError(messageOperationFormat)
		}
		row := byID[item.ID]
		if row == nil || !sameRow(configROSRow(row), configROSRow(item.Config)) {
			return routerError(fmt.Sprintf(messageNATChanged, rosNAT, item.ID))
		}
		deleting[item.ID] = true
	}
	following := make([]int, len(rows))
	next := -1
	for i := len(rows) - 1; i >= 0; i-- {
		following[i] = next
		if !deleting[rows[i].id()] {
			next = i
		}
	}
	steps := []NATStep{}
	ids := []string{}
	for _, item := range items {
		row := byID[item.ID]
		values, err := writableResource(rosNAT, row)
		if err != nil {
			return err
		}
		at := position[item.ID]
		step := stepResource("restore", item)
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
		_, err = d.request(ctx, "POST", rosNAT+"/remove", Row{".id": strings.Join(ids[start:min(start+500, len(ids))], ",")}, rosTimeout)
		if err != nil {
			break
		}
	}
	if err != nil {
		remaining, err := d.list(ctx, rosNAT)
		if err != nil {
			return err
		}
		remainingByID, _ := indexROSRows(remaining)
		for _, item := range items {
			if row := remainingByID[item.ID]; row != nil {
				if !sameRow(configROSRow(row), configROSRow(item.Config)) || row.flag("dynamic") {
					return routerError(fmt.Sprintf(messageNATChanged, rosNAT, item.ID))
				}
				if _, err := d.request(ctx, "DELETE", rosNAT+"/"+escapeROSPathID(item.ID), nil, rosTimeout); err != nil {
					return err
				}
			}
		}
	}
	remaining, err := d.list(ctx, rosNAT)
	if err != nil {
		return err
	}
	for _, row := range remaining {
		if deleting[row.id()] {
			return routerError(messageNATLegacyPresent)
		}
	}
	return nil
}

func (d *RouterOS) restoreResource(ctx context.Context, s NATStep, ids ROSIDs, remapped map[[2]string]string, deleteSafe bool) error {
	row, err := d.resourceRow(ctx, s.Path, s.ID)
	if err != nil {
		return err
	}
	if s.Action == "delete" {
		if !deleteSafe || row == nil {
			return nil
		}
		if _, err := d.guardResource(ctx, ROSResource{s.Path, s.ID, s.Config}); err != nil {
			return err
		}
		if row.flag("dynamic") {
			return routerError(messagePolicyDynamic)
		}
		if s.Path == rosTables {
			live, err := d.snapshotPolicy(ctx, false)
			if err != nil {
				return err
			}
			if err = checkPolicyReferences([]ROSResource{{s.Path, s.ID, s.Config}}, *live); err != nil {
				return err
			}
		}
		if _, err := d.request(ctx, "DELETE", s.Path+"/"+escapeROSPathID(s.ID), nil, rosTimeout); err != nil {
			return err
		}
		row, err = d.resourceRow(ctx, s.Path, s.ID)
		if err != nil {
			return err
		}
		if row != nil {
			return routerError(messageRollbackDeletePresent)
		}
		return nil
	}
	if row != nil {
		if _, err := d.guardResource(ctx, ROSResource{s.Path, s.ID, s.Config}); err != nil {
			return err
		}
		return nil
	}
	values := cloneRow(s.Values)
	if before := values.text("place-before"); before != "" {
		if id, ok := remapped[[2]string{s.Path, before}]; ok {
			before = id
			values["place-before"] = id
		}
		anchor, err := d.resourceRow(ctx, s.Path, before)
		if err != nil {
			return err
		}
		if anchor == nil {
			return routerError(messagePolicyAnchor)
		}
	}
	restored, err := d.createResource(ctx, s.Path, values, &[]NATStep{}, ids, true)
	if err != nil {
		return err
	}
	remapped[[2]string{s.Path, s.ID}] = restored.ID
	return nil
}
func (d *RouterOS) restoreNATRules(ctx context.Context, steps []NATStep, ids ROSIDs, remapped map[[2]string]string, failures *[]string) {
	rows, err := d.list(ctx, rosNAT)
	if err != nil {
		*failures = append(*failures, err.Error())
		return
	}
	byID, _ := indexROSRows(rows)
	batched := []NATStep{}
	single := []NATStep{}
	for _, s := range steps {
		if s.Order != nil {
			batched = append(batched, s)
		} else {
			single = append(single, s)
		}
	}
	slices.SortFunc(batched, func(a, b NATStep) int { return *a.Order - *b.Order })
	present := []NATStep{}
	for _, s := range batched {
		if byID[s.ID] != nil {
			present = append(present, s)
		}
	}
	pending := []NATStep{}
	changes := []ROSChange{}
	for _, s := range batched {
		if row := byID[s.ID]; row != nil {
			if !sameRow(configROSRow(row), configROSRow(s.Config)) {
				*failures = append(*failures, fmt.Sprintf(messageNATChanged, rosNAT, s.ID))
			}
			continue
		}
		values := cloneRow(s.Values)
		before := values.text("place-before")
		if newID, ok := remapped[[2]string{rosNAT, before}]; ok {
			before = newID
			values["place-before"] = newID
		}
		var sibling *NATStep
		if i := sort.Search(len(present), func(i int) bool { return *present[i].Order > *s.Order }); i < len(present) {
			sibling = &present[i]
		}
		if sibling != nil && !(byID[before] != nil && s.FollowingOrder != nil && *s.FollowingOrder < *sibling.Order) {
			values["place-before"] = sibling.ID
		}
		if anchor := values.text("place-before"); anchor != "" && byID[anchor] == nil {
			*failures = append(*failures, messagePolicyAnchor)
			continue
		}
		s.Values = values
		pending = append(pending, s)
		changes = append(changes, ROSChange{Method: "PUT", Path: rosNAT, Values: values})
	}
	handled := 0
	undo := []ROSChange{}
	if len(pending) >= d.batchMin {
		handled, err = d.putNAT(ctx, changes, &undo)
	}
	for i := 0; i < min(handled, len(undo)); i++ {
		remapped[[2]string{rosNAT, pending[i].ID}] = undo[i].ID
		ids.table(rosNAT)[undo[i].ID] = true
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
func (d *RouterOS) runNATSteps(ctx context.Context, steps []NATStep, ids ROSIDs, remapped map[[2]string]string, failures *[]string, deleteSafe bool) {
	for start := 0; start < len(steps); {
		end := start + 1
		address := steps[start].Path == rosAddresses
		action := steps[start].Action
		for end < len(steps) && steps[end].Action == action && (steps[end].Path == rosAddresses) == address && (action != "restore" || steps[end].Path == steps[start].Path) {
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
				err = d.restoreAddresses(ctx, run, ids, remapped)
			case "delete":
				if deleteSafe {
					err = d.restoreDeleteAddresses(ctx, run)
				}
			case "sweep":
				if deleteSafe {
					for _, s := range run {
						if err = d.restoreAddressSweep(ctx, s); err != nil {
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
					var head OpHead
					_ = jsondecode.Unmarshal(raw, &head)
					if head.Kind == "nat_list_restore" {
						var undo NATListUndo
						if jsondecode.Unmarshal(raw, &undo) != nil {
							*failures = append(*failures, messageOperationFormat)
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
						raw = mustPolicyJSON(undo)
					}
					undos = append(undos, raw)
				}
				if len(*failures) == 0 {
					for _, failure := range d.RestoreMany(ctx, undos, true) {
						if failure != "" && failure != BatchSkipped {
							*failures = append(*failures, failure)
						}
					}
				}
			}
		} else if action == "restore" && run[0].Path == rosRoutes && len(run) >= d.batchMin {
			d.restorePolicyRoutes(ctx, run, ids, remapped, failures)
		} else if action == "restore" && len(run) >= d.batchMin && run[0].Path == rosNAT {
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

func natIDs(rows []Row, lists map[string][]Row) ROSIDs {
	ids := ROSIDs{rosNAT: rowIDs(rows), rosAddresses: map[string]bool{}}
	for _, name := range []string{NATSrcList, NATDstList} {
		for _, row := range lists[name] {
			ids[rosAddresses][row.id()] = true
		}
	}
	return ids
}
func (d *RouterOS) checkNATUnchanged(ctx context.Context, op NATListSync, full, rows []Row, lists map[string][]Row) error {
	keep := map[string]bool{}
	if op.Rule != nil {
		keep[op.Rule.ID] = true
	}
	foreign, err := d.foreignNATRefs(ctx, keep, full)
	if err != nil {
		return err
	}
	if len(foreign) > 0 {
		return routerError(fmt.Sprintf(messageNATForeignWrite, foreign[0].Path, foreign[0].Name))
	}
	found := InspectNATListRule(rows, nil)
	if found.Problem != "" {
		return routerError(found.Problem)
	}
	if op.Rule == nil {
		if found.Rule != nil {
			return routerError(messageNATChangedPreview)
		}
	} else if found.Rule == nil || found.Rule.id() != op.Rule.ID || !sameRow(configROSRow(found.Rule), configROSRow(op.Rule.Config)) {
		return routerError(messageNATChangedPreview)
	}
	byID, _ := indexROSRows(rows)
	for _, item := range append(slices.Clone(op.Legacy), op.Stale...) {
		row := byID[item.ID]
		if row == nil || !sameRow(configROSRow(row), configROSRow(item.Config)) {
			return routerError(messageNATChangedPreview)
		}
	}
	for i, name := range []string{NATSrcList, NATDstList} {
		_, foreign, problems := natMembers(name, lists[name])
		if len(problems) > 0 {
			return routerError(problems[0])
		}
		if len(foreign) > 0 && op.Rule == nil {
			return routerError(fmt.Sprintf(messageNATListForeign, name))
		}
		removing, adding := op.SrcRemove, op.SrcAdd
		if i == 1 {
			removing, adding = op.DstRemove, op.DstAdd
		}
		members, _ := indexROSRows(lists[name])
		presentKeys := map[string]bool{}
		for _, row := range lists[name] {
			presentKeys[natKey(row.text("address"))] = true
		}
		for _, item := range removing {
			row := members[item.ID]
			if row == nil || !sameRow(configROSRow(row), configROSRow(item.Config)) {
				return routerError(messageNATChangedPreview)
			}
		}
		for _, value := range adding {
			if presentKeys[natKey(value)] {
				return routerError(messageNATChangedPreview)
			}
		}
	}
	return nil
}
func (d *RouterOS) settleNATList(ctx context.Context, id string) error {
	for attempt := 0; attempt < 8; attempt++ {
		row, err := d.resourceRow(ctx, rosNAT, id)
		if err != nil {
			return err
		}
		if row == nil {
			return routerError(messageNATNewMissing)
		}
		if !row.flag("invalid") {
			return nil
		}
		if attempt < 7 {
			if err := waitContext(ctx, 250*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return routerError(messageNATNewInvalid)
}
func (d *RouterOS) proveNATList(ctx context.Context, op NATListSync, id string) error {
	legacy := map[string]bool{}
	for _, item := range append(slices.Clone(op.Legacy), op.Stale...) {
		legacy[item.ID] = true
	}
	rows, err := d.list(ctx, rosNAT)
	if err != nil {
		return err
	}
	rows = slices.DeleteFunc(slices.Clone(d.assessedNAT(rows)), func(row Row) bool { return legacy[row.id()] })
	found := InspectNATListRule(rows, nil)
	lists, err := d.readNATLists(ctx)
	if err != nil {
		return err
	}
	sets := map[string][]string{}
	for _, name := range []string{NATSrcList, NATDstList} {
		for _, row := range lists[name] {
			if row.enabled() {
				sets[name] = append(sets[name], row.text("address"))
			}
		}
	}
	contextRows := &NatContext{}
	if found.Problem != "" || found.Rule == nil || found.Rule.id() != id || !PreservesSources(rows, Networks(op.Sources), Networks(op.Destinations), op.OutInterface, contextRows, sets, nil, nil) {
		message := messageNATCoverage
		if found.Problem != "" {
			message += "：" + found.Problem
		}
		return routerError(message)
	}
	return nil
}

// ApplyNATLists runs one list delta to completion, compensating in reverse order
// on failure. Every mutation is guarded by the preview and recorded before I/O.
func (d *RouterOS) ApplyNATLists(ctx context.Context, op NATListSync) (*NATListUndo, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.InvalidateAddressReads()
	full, err := d.list(ctx, rosNAT)
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
	d.natListTouched = true
	ids := natIDs(full, lists)
	journal := []NATStep{}
	keep := []string{}
	if op.Rule != nil {
		keep = append(keep, op.Rule.ID)
	}
	createRule := func() (string, error) {
		values := Row{"chain": "srcnat", "action": "accept", "src-address-list": NATSrcList, "dst-address-list": NATDstList, "comment": NATListComment, "disabled": "false"}
		if op.OutInterface != "" {
			values["out-interface"] = op.OutInterface
		}
		item, err := d.createResource(ctx, rosNAT, values, &journal, ids, false)
		return item.ID, err
	}
	run := func() error {
		if op.Withdraw {
			if op.Rule == nil {
				return routerError(messageNATChangedPreview)
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
			if _, err := d.createAddresses(ctx, NATEntries(NATSrcList, op.SrcAdd), &journal, ids, nil); err != nil {
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
				return routerError(messageNATChangedPreview)
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
			if _, err := d.createAddresses(ctx, NATEntries(NATDstList, op.DstAdd), &journal, ids, nil); err != nil {
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
			slices.SortFunc(items, func(a, b ROSResource) int { return position[a.ID] - position[b.ID] })
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
			return nil, rollbackError(err.Error() + "；" + rollback.Error())
		}
		return nil, err
	}
	return undo, nil
}

// RestoreNATLists restores removed protection before deleting new objects. A
// failed restoration or new foreign reference preserves the new protection and
// reports an incomplete rollback; it never deletes by a destination guess.
func (d *RouterOS) RestoreNATLists(ctx context.Context, op NATListUndo) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, step := range op.Steps {
		if (step.Path != rosNAT && step.Path != rosAddresses) || (step.Action != "delete" && step.Action != "restore" && step.Action != "sweep") || (step.Action != "sweep" && (step.ID == "" || step.Config == nil)) {
			return routerError(messageOperationFormat)
		}
	}
	d.InvalidateAddressReads()
	d.natListTouched = true
	rows, err := d.list(ctx, rosNAT)
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
	ruleIDs := stringSet(op.Keep...)
	for _, step := range op.Steps {
		if step.Path == rosNAT {
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
		return rollbackError(messageNATRollbackPrefix + strings.Join(failures, "；"))
	}
	return nil
}
