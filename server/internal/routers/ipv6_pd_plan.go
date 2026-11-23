package routers

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func pdChange(d *RouterOS, role, method, path string, row, values Row) IPv6PDChange {
	c := IPv6PDChange{Role: role, ROSChange: ROSChange{Method: method, Path: path, Values: values}}
	if values != nil {
		c.Values = ros.CloneRow(values)
	}
	if row != nil {
		c.ID = row.ID()
		c.Expected = ros.MarshalRow(d.OwnershipSnapshot(path, row))
	}
	return c
}
func pdEvidence(live pdLive, spec *IPv6PDSpec, owned map[string]Row) *IPv6PDEvidence {
	e := &IPv6PDEvidence{Spec: spec, Owned: map[string]string{}, Scripts: []string{}, Schedulers: []string{}}
	for role, r := range owned {
		e.Owned[role] = r.ID()
	}
	for _, r := range live.scripts {
		if r.Is("name", pdWatch) || r.Is("name", pdRollback) {
			e.Scripts = append(e.Scripts, r.ID())
		}
	}
	for _, r := range live.schedulers {
		if r.Is("name", pdPoll) {
			e.Schedulers = append(e.Schedulers, r.ID())
		}
	}
	slices.Sort(e.Scripts)
	slices.Sort(e.Schedulers)
	return e
}

// InspectIPv6PDGuard reads one guard installation. Ambiguity blocks this stage
// alone; the release planner decides how to skip it and report its warnings.
func InspectIPv6PDGuard(ctx context.Context, r *Router, gateway string, d *RouterOS) (*IPv6PDGuardPlan, error) {
	if err := r.ValidateForDevice(); err != nil {
		return nil, err
	}
	p := &IPv6PDGuardPlan{IPv6PDGuardReport: IPv6PDGuardReport{Enabled: pdSettings(r).Enabled, Status: "disabled", Hold: pdHold, Items: []IPv6PDItem{}, Blockers: []string{}, Warnings: []string{}}, Ops: []Op{}}
	var previous json.RawMessage
	if r != nil {
		previous = ros.GuardRaw(r.Extra["ipv6_pd_guard_state"])
	}
	d.SetIPv6PDGuardManifest(previous)
	p.Managed = device.HasStoredState(previous)
	err := func() error {
		live, e := pdSnapshot(ctx, d, !p.Enabled)
		if e != nil {
			return e
		}
		owned, e := pdOwned(d, previous, live)
		if e != nil {
			return e
		}
		// Duplicate names are ambiguous even when the first match looks correct.
		for _, v := range []struct {
			rows []Row
			name string
		}{{live.scripts, pdWatch}, {live.schedulers, pdPoll}} {
			count := 0
			for _, row := range v.rows {
				if row.Is("name", v.name) {
					count++
				}
			}
			if count > 1 {
				return device.Error(messagePDDuplicate)
			}
		}
		p.RollbackScript = pdNamed(live.scripts, pdRollback) != nil
		if !p.Enabled {
			return pdWithdrawPlan(d, p, live, owned, previous)
		}
		spec, client, warnings, e := pdDerive(ctx, d, &live, gateway)
		if e != nil {
			return e
		}
		p.LAN, p.Pool, p.Client = spec.LAN, spec.Pool, spec.ClientInterface
		p.Warnings = append(p.Warnings, warnings...)
		script, scheduler := pdNamed(live.scripts, pdWatch), pdNamed(live.schedulers, pdPoll)
		states := map[string]pdState{"script": pdScriptState(script, spec), "scheduler": pdSchedulerState(scheduler), "hook": pdHookState(client)}
		current := map[string]Row{"script": script, "scheduler": scheduler, "hook": client}
		p.proofs = map[string]IPv6PDResource{}
		for role, row := range current {
			if row != nil {
				p.proofs[role] = IPv6PDResource{role, pdRolePath(role), row.ID(), d.OwnershipSnapshot(pdRolePath(role), row)}
			}
		}
		written, stillOwned, updating := false, false, false
		for _, role := range []string{"script", "scheduler", "hook"} {
			s := states[role]
			written = written || s.state == "missing" || s.state == "update"
			updating = updating || s.state == "update"
			if row := owned[role]; row != nil && current[role] != nil && row.ID() == current[role].ID() {
				stillOwned = true
			}
		}
		after := []string{}
		if written || stillOwned {
			for _, role := range []string{"hook", "scheduler", "script"} {
				if role != "hook" || states[role].state != "shared" {
					after = append(after, role)
				}
			}
		}
		conflicts := []string{}
		for _, role := range []string{"script", "scheduler", "hook"} {
			s := states[role]
			status, detail := s.state, s.detail
			if !slices.Contains([]string{"missing", "update", "conflict"}, status) {
				if slices.Contains(after, role) {
					if owned[role] != nil && current[role] != nil && owned[role].ID() == current[role].ID() {
						status = "synced"
					} else {
						status = "adopt"
						detail += messagePDAdopt
					}
				} else {
					status = "borrowed"
				}
			}
			p.Items = append(p.Items, IPv6PDItem{role, pdRoleName(role), status, detail})
			if s.state == "conflict" {
				conflicts = append(conflicts, s.detail)
			}
		}
		if len(conflicts) > 0 {
			return device.Error(strings.Join(conflicts, "；"))
		}
		changes := []IPv6PDChange{}
		source, _ := IPv6PDWatchSource(spec.LAN, spec.Pool)
		if states["script"].state == "missing" {
			changes = append(changes, pdChange(d, "script", "PUT", pdScripts, nil, Row{"name": pdWatch, "source": source, "policy": pdPolicy, "comment": pdScriptComment}))
		} else if states["script"].state == "update" {
			changes = append(changes, pdChange(d, "script", "PATCH", pdScripts, script, Row{"source": source, "policy": pdPolicy}))
			found := pdParameters(script.Text("source"))
			if found["lan"] != spec.LAN || found["pool"] != spec.Pool {
				for _, row := range live.last {
					changes = append(changes, pdChange(d, "state", "DELETE", ros.PathIPv6AddressLists, row, nil))
				}
			}
		}
		if states["scheduler"].state == "missing" {
			changes = append(changes, pdChange(d, "scheduler", "PUT", pdSchedulers, nil, Row{"name": pdPoll, "interval": "30s", "start-time": "startup", "policy": pdPolicy, "on-event": pdEvent, "comment": "cf-pd", "disabled": "false"}))
		} else if states["scheduler"].state == "update" {
			changes = append(changes, pdChange(d, "scheduler", "PATCH", pdSchedulers, scheduler, Row{"on-event": pdEvent, "interval": "30s", "start-time": "startup", "policy": pdPolicy, "disabled": "false"}))
		}
		if old := owned["hook"]; old != nil && old.ID() != client.ID() && slices.Contains([]string{"ok", "update"}, pdHookState(old).state) {
			changes = append(changes, pdChange(d, "hook", "PATCH", pdClients, old, Row{"script": ""}))
		}
		if slices.Contains([]string{"missing", "update"}, states["hook"].state) {
			changes = append(changes, pdChange(d, "hook", "PATCH", pdClients, client, Row{"script": pdHook}))
		}
		var old IPv6PDManifest
		_ = jsondecode.Unmarshal(previous, &old)
		before := []string{}
		for _, entry := range old.Resources {
			before = append(before, entry.Role)
		}
		slices.Sort(before)
		record := !slices.Equal(before, after) || len(after) > 0 && !reflect.DeepEqual(old.Spec, spec)
		borrowed := []string{}
		for _, role := range []string{"script", "scheduler", "hook"} {
			if !slices.Contains(after, role) && slices.Contains([]string{"ok", "shared"}, states[role].state) {
				borrowed = append(borrowed, pdRoleName(role))
			}
		}
		switch {
		case len(changes) > 0:
			p.Status, p.Message = "pending", messagePDInstall
			if updating {
				p.Message = messagePDUpdate
			}
		case record:
			p.Status, p.Message = "pending", messagePDRecord
		case len(after) > 0:
			p.Status, p.Message = "synced", messagePDSynced
		default:
			p.Status, p.Message = "borrowed", messagePDBorrowed
		}
		if len(after) > 0 && len(borrowed) > 0 {
			p.Message += fmt.Sprintf(messagePDSharedMessage, strings.Join(borrowed, "、"))
		}
		if states["hook"].state == "shared" {
			p.Warnings = append(p.Warnings, messagePDSharedWarning)
		}
		p.Managed = len(after) > 0
		p.Check = &IPv6PDGuardCheck{Enabled: true, Spec: spec, Gateway: gateway, Owned: after}
		p.Evidence = pdEvidence(live, spec, owned)
		if p.Status == "pending" {
			summary := messagePDRecordSummary
			if len(changes) > 0 {
				verb := messagePDInstallVerb
				if updating {
					verb = messagePDUpdateVerb
				}
				summary = fmt.Sprintf(messagePDSummary, verb, spec.LAN, spec.Pool)
			}
			p.Ops = append(p.Ops, &IPv6PDGuardSync{OpHead: OpHead{Kind: "ipv6_pd_guard_sync", Summary: summary}, Enabled: true, Gateway: gateway, Spec: spec, Previous: previous, Changes: changes, Owned: after})
		}
		return nil
	}()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p.Status, p.Message, p.Blockers, p.Ops, p.Check = "blocked", err.Error(), []string{err.Error()}, []Op{}, nil
		for i := range p.Items {
			item := &p.Items[i]
			if !slices.Contains([]string{"borrowed", "synced", "conflict"}, item.Status) {
				item.Status = "blocked"
				item.Detail = strings.TrimLeft(strings.ReplaceAll(item.Detail, messagePDWhenPublish, "")+messagePDSkipped, "；，")
			}
		}
	}
	return p, nil
}
func pdWithdrawPlan(d *RouterOS, p *IPv6PDGuardPlan, live pdLive, owned map[string]Row, previous json.RawMessage) error {
	if !device.HasStoredState(previous) {
		return nil
	}
	changes := []IPv6PDChange{}
	blockers := []string{}
	if hook := owned["hook"]; hook != nil && slices.Contains([]string{"ok", "update"}, pdHookState(hook).state) {
		changes = append(changes, pdChange(d, "hook", "PATCH", pdClients, hook, Row{"script": ""}))
	}
	if scheduler := owned["scheduler"]; scheduler != nil {
		if !strings.Contains(scheduler.Text("on-event"), pdWatch) {
			blockers = append(blockers, messagePDPollChanged)
		}
		changes = append(changes, pdChange(d, "scheduler", "DELETE", pdSchedulers, scheduler, nil))
	}
	if script := owned["script"]; script != nil {
		if !pdIsWatch(script.Text("source")) {
			blockers = append(blockers, messagePDWatchChanged)
		}
		changes = append(changes, pdChange(d, "script", "DELETE", pdScripts, script, nil))
		for _, row := range live.last {
			changes = append(changes, pdChange(d, "state", "DELETE", ros.PathIPv6AddressLists, row, nil))
		}
		for _, row := range live.prefixes {
			if pdDeprecated.MatchString(row.Text("comment")) && !ros.PolicyFlag(row["dynamic"]) {
				changes = append(changes, pdChange(d, "state", "DELETE", pdPrefixes, row, nil))
			}
		}
	}
	var old IPv6PDManifest
	_ = jsondecode.Unmarshal(previous, &old)
	if old.Spec != nil {
		p.LAN, p.Pool, p.Client = old.Spec.LAN, old.Spec.Pool, old.Spec.ClientInterface
	}
	for _, role := range []string{"hook", "scheduler", "script"} {
		for _, c := range changes {
			if c.Role == role {
				detail := messagePDDelete
				if role == "hook" {
					detail = messagePDHookClear
				}
				p.Items = append(p.Items, IPv6PDItem{role, pdRoleName(role), "remove", detail})
				break
			}
		}
	}
	lists, prefixes := 0, 0
	removed := [][2]string{}
	for _, c := range changes {
		if c.Role == "state" {
			if c.Path == ros.PathIPv6AddressLists {
				lists++
			} else {
				prefixes++
			}
		}
		if c.Method == "DELETE" {
			removed = append(removed, [2]string{c.Path, c.ID})
		}
	}
	if lists+prefixes > 0 {
		p.Items = append(p.Items, IPv6PDItem{"state", pdRoleName("state"), "remove", fmt.Sprintf(messagePDStateDelete, lists, prefixes)})
	}
	if len(blockers) > 0 {
		return device.Error(strings.Join(blockers, "；"))
	}
	slices.SortFunc(removed, func(a, b [2]string) int {
		if c := strings.Compare(a[0], b[0]); c != 0 {
			return c
		}
		return strings.Compare(a[1], b[1])
	})
	p.Status, p.Message = "pending", messagePDWithdraw
	if len(changes) == 0 {
		p.Message = messagePDForget
	}
	p.Check = &IPv6PDGuardCheck{Withdraw: true, Removed: removed}
	p.Evidence = pdEvidence(live, nil, owned)
	summary := messagePDWithdrawSummary
	if len(changes) == 0 {
		summary = messagePDForgetSummary
	}
	p.Ops = append(p.Ops, &IPv6PDGuardSync{OpHead: OpHead{Kind: "ipv6_pd_guard_sync", Summary: summary}, Previous: previous, Changes: changes, Owned: []string{}})
	return nil
}
