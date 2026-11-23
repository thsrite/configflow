package routers

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ikuai"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
)

type stageBatcher interface {
	Batch(context.Context, bool, func(context.Context) error) error
}
type releaseDone struct {
	stage, summary string
	op, undo       json.RawMessage
}

func inStageBatch(ctx context.Context, d Driver, rollback bool, fn func(context.Context) error) error {
	if b, ok := d.(stageBatcher); ok {
		return b.Batch(ctx, rollback, fn)
	}
	return fn(ctx)
}
func invalidateRelease(d Driver) {
	if r, ok := d.(*RouterOS); ok {
		r.InvalidateAddressReads()
	}
}
func releaseErrorText(err error) string {
	if err == nil || err.Error() == "" {
		return messageReleaseUnknown
	}
	return err.Error()
}
func rawOp(op Op) (json.RawMessage, error) { return json.Marshal(op) }
func effectful(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && !bytes.Equal(b, []byte("null")) && !bytes.Equal(b, []byte("{}"))
}

// Verify reads back one stage; an unchanged state may be reused between routes
// and NAT. Feature verifiers keep their own proof and external-edit checks.
func Verify(ctx context.Context, stage string, checks *Checks, d Driver, a ReturnAgent, state *RouterState) (*RouterState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if checks == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	switch stage {
	case StageIPv6FakeIP, StageIPv6Routes, StageIPv6DNS, StageIPv6PDGuard, StageDNSGuard:
		ros, ok := d.(*RouterOS)
		if !ok {
			return nil, device.Error(fmt.Sprintf(messageStageNeedsRouterOS, stage))
		}
		var err error
		switch stage {
		case StageIPv6FakeIP:
			err = VerifyIPv6FakeIP(ctx, ros, checks.IPv6FakeIP)
		case StageIPv6Routes:
			err = VerifyIPv6Routes(ctx, ros, checks.IPv6Routes)
		case StageIPv6DNS:
			err = VerifyIPv6DNS(ctx, ros, checks.IPv6DNS)
		case StageIPv6PDGuard:
			var c IPv6PDGuardCheck
			if jsondecode.Unmarshal(checks.IPv6PDGuard, &c) != nil {
				return nil, device.Error(device.MessageOperationFormat)
			}
			err = VerifyIPv6PDGuard(ctx, ros, &c)
		case StageDNSGuard:
			err = dnsguard.Verify(ctx, ros.Client, checks.DNSGuard)
		}
		return nil, err
	case StageDHCPDNS:
		if checks.DHCPDNS == nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		live, err := d.ReadDHCPDNS(ctx)
		if err != nil {
			return nil, err
		}
		return nil, device.VerifyDNS(*checks.DHCPDNS, live)
	case StageReturnRouting, StageReturnCleanup:
		if a == nil {
			return nil, device.Error(messageNoReturnAgent)
		}
		live, err := a.Status(ctx)
		if err != nil {
			return nil, err
		}
		if live == nil {
			return nil, device.Error(messageReturnIncomplete)
		}
		if stage == StageReturnCleanup {
			if live.Managed != nil && (live.Managed.Active || live.Managed.TablePresent) {
				return nil, device.Error(messageReturnStillPresent)
			}
			return nil, nil
		}
		want := checks.ReturnRouting
		if want == nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err = checkReturnAllocation(live, want); err != nil {
			return nil, err
		}
		if live.Managed == nil || !live.Managed.TablePresent {
			return nil, device.Error(messageReturnNoTable)
		}
		var actual ReturnSpec
		if jsondecode.Unmarshal(live.Managed.Spec, &actual) != nil {
			return nil, device.Error(messageReturnSpecMismatch)
		}
		if !sameStrings(returnDest(&actual), returnDest(want)) {
			return nil, device.Error(messageReturnDestMismatch)
		}
		if !sameReturnSpec(live.Managed.Spec, want) {
			return nil, device.Error(messageReturnSpecMismatch)
		}
		if !slices.ContainsFunc(live.FWMarkRules, func(r json.RawMessage) bool { return returnRuleMatches(r, want) }) {
			return nil, device.Error(messageReturnRuleMissing)
		}
		return nil, nil
	}
	if state == nil {
		var err error
		state, err = d.ReadState(ctx)
		if err != nil {
			return nil, err
		}
	}
	if stage == StageRoutes {
		if checks.ROSPolicy != nil {
			ros, ok := d.(*RouterOS)
			if !ok {
				return nil, device.Error(fmt.Sprintf(messageStageNeedsRouterOS, stage))
			}
			if err := VerifyROSPolicy(ctx, ros, checks.ROSPolicy); err != nil {
				return nil, err
			}
		}
		ix := device.NewStaticRouteIndex(state.Routes)
		for _, c := range checks.Routes {
			sel, _ := ix.Select(c.CIDR, c.Gateway)
			candidates := sel.Candidates
			if len(candidates) == 0 || slices.ContainsFunc(candidates, func(r *RouteEntry) bool { return !device.IsManaged(r.Comment) }) {
				return nil, device.Error(fmt.Sprintf(messageRouteVerify, c.CIDR, c.Gateway))
			}
		}
		if checks.PolicyRoutes != nil {
			for _, c := range *checks.PolicyRoutes {
				r := device.PolicyRoute(state.PolicyRoutes, c.Comment)
				if r == nil || !device.PolicyRouteOK(r, c.Gateway) || !sameCanonical(r.Dst, c.Dst) {
					return nil, device.Error(fmt.Sprintf(messagePolicyVerify, c.Comment, c.Gateway, c.Gateway))
				}
			}
		}
		for _, r := range state.Routes {
			if slices.Contains(checks.RemovedRoutes, r.ID) {
				return nil, device.Error(messageDeletedRoutePresent)
			}
		}
		if checks.RestoredRoutes != nil {
			for _, c := range *checks.RestoredRoutes {
				if !slices.ContainsFunc(state.Routes, func(r *RouteEntry) bool { return r.Dst == c.CIDR && r.Gateway == c.Gateway && r.Enabled }) {
					return nil, device.Error(fmt.Sprintf(messageRestoredRouteMissing, c.CIDR, c.Gateway))
				}
			}
		}
		if checks.RemovedPolicyRoutes != nil {
			for _, r := range state.PolicyRoutes {
				if slices.Contains(*checks.RemovedPolicyRoutes, r.ID) {
					return nil, device.Error(messageDeletedPolicyPresent)
				}
			}
		}
	} else if stage == StageNATBypass {
		if err := VerifyNAT(ctx, checks.NATChecks, &state.State, state.NatContext); err != nil {
			return nil, err
		}
		if checks.NATList != nil {
			if err := VerifyNATLists(ctx, checks.NATList, state.NatContext, &state.NATLists, &state.NATListsReader); err != nil {
				return nil, err
			}
		}
	}
	if c, ok := checks.IkuaiGroups[stage]; ok {
		if err := ikuai.VerifyGroups(c, state.IkuaiGroups); err != nil {
			return nil, err
		}
	}
	return state, nil
}
func sameCanonical(a, b []string) bool {
	normalize := func(values []string) []string {
		out := make([]string, len(values))
		for i, v := range values {
			out[i] = ikuai.CanonAddress(v)
		}
		return out
	}
	return sameStrings(normalize(a), normalize(b))
}

func applyRelease(ctx context.Context, raw json.RawMessage, d Driver, a ReturnAgent, compensating bool) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	invalidateRelease(d)
	var op struct {
		Kind          string          `json:"kind"`
		Spec          json.RawMessage `json:"spec"`
		Previous      json.RawMessage `json:"previous"`
		RestoreLegacy bool            `json:"restore_legacy"`
	}
	if err := jsondecode.Unmarshal(raw, &op); err != nil {
		return nil, err
	}
	if op.Kind != "return_routing_apply" && op.Kind != "return_routing_remove" {
		return d.Apply(ctx, raw)
	}
	if a == nil {
		return nil, device.Error(messageNoReturnAgent)
	}
	status, err := a.Status(ctx)
	if err != nil {
		return nil, err
	}
	var undo json.RawMessage
	if effectful(op.Previous) {
		undo, _ = json.Marshal(struct {
			Kind     string          `json:"kind"`
			Spec     json.RawMessage `json:"spec"`
			Previous any             `json:"previous"`
		}{"return_routing_apply", op.Previous, nil})
	} else if op.Kind == "return_routing_apply" {
		undo = json.RawMessage(`{"kind":"return_routing_remove","restore_legacy":false}`)
	}
	if op.Kind == "return_routing_apply" {
		var s ReturnSpec
		var extra struct {
			AdoptLegacy bool `json:"adopt_legacy"`
		}
		if jsondecode.Unmarshal(op.Spec, &s) != nil || jsondecode.Unmarshal(op.Spec, &extra) != nil || (s.SafetyVersion != 1 && s.SafetyVersion != 2) || extra.AdoptLegacy {
			return nil, device.Error(messageReturnHandshake)
		}
		if err = checkReturnAllocation(status, &s); err != nil {
			return nil, err
		}
		if !compensating && !returnPreviousHolds(status, op.Previous) {
			return nil, device.Error(messageReturnChanged)
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		err = a.Apply(ctx, op.Spec)
	} else {
		if blockers := ReturnSafetyBlockers(status, false); len(blockers) > 0 {
			return nil, device.Error(strings.Join(blockers, "；"))
		}
		if !compensating && !returnPreviousHolds(status, op.Previous) {
			return nil, device.Error(messageReturnChanged)
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		err = a.Remove(ctx, op.RestoreLegacy)
	}
	if err == nil {
		return undo, nil
	}
	// A definite remote rejection has no side effect. A lost transport reply may
	// have committed, so retain its compensation before surfacing the failure.
	var remote *agents.RemoteError
	if errors.As(err, &remote) {
		return nil, err
	}
	if compensating && ctx.Err() == nil {
		live, readErr := a.Status(ctx)
		if readErr == nil && live != nil {
			if op.Kind == "return_routing_remove" && (live.Managed == nil || (!live.Managed.Active && !live.Managed.TablePresent)) {
				return undo, nil
			}
			if op.Kind == "return_routing_apply" && live.Managed != nil && live.Managed.TablePresent {
				var s ReturnSpec
				if jsondecode.Unmarshal(op.Spec, &s) == nil && sameReturnSpec(live.Managed.Spec, &s) && returnSpecProof(live.Managed.Spec, op.Spec) {
					return undo, nil
				}
			}
		}
	}
	return nil, &RouterError{Kind: KindUncertainWrite, Message: releaseErrorText(err), Undo: undo}
}

// Execute runs a rebuilt plan in order, retaining proven partial writes before
// failure. Request disconnects detach; Runtime cancellation stops all device I/O.
func Execute(ctx context.Context, plan *Plan, d Driver, a ReturnAgent) *Result {
	work := lifecycle.Detach(ctx)
	result := &Result{Log: []ReleaseEntry{}, Rollback: []ReleaseEntry{}}
	if ros, ok := d.(*RouterOS); ok {
		defer ros.ReuseAddressReads()()
	}
	current := ""
	done := []releaseDone{}
	record := func(summary, status string) { result.Log = append(result.Log, ReleaseEntry{current, summary, status}) }
	fail := func(err error) *Result {
		result.FailedStage = &current
		if work.Err() != nil {
			result.Aborted = true
			record(messageReleaseAborted, "failed")
			return result
		}
		record(releaseErrorText(err), "failed")
		var re *RouterError
		if errors.As(err, &re) && re.Kind == KindRollback {
			result.Rollback = append(result.Rollback, ReleaseEntry{current, re.Error(), "failed"})
		}
		result.Rollback = append(result.Rollback, rollbackRelease(work, done, d, a)...)
		if work.Err() != nil {
			result.Aborted = true
		}
		return result
	}
	if err := work.Err(); err != nil {
		return fail(err)
	}
	if plan == nil {
		return fail(device.Error(device.MessageOperationFormat))
	}
	if len(plan.Blockers) > 0 {
		return fail(device.Error(fmt.Sprintf(messageReleaseBlockers, strings.Join(plan.Blockers, "；"))))
	}
	var unchanged *RouterState
	for _, stage := range plan.Stages {
		current = stage.Key
		if err := work.Err(); err != nil {
			return fail(err)
		}
		if stage.Enabled != nil && !*stage.Enabled {
			if len(stage.Ops) > 0 {
				return fail(device.Error(messageDisabledWrites))
			}
			continue
		}
		if len(stage.Ops) == 0 {
			summary, status, verify := emptyStageReadback(stage, plan.Checks)
			if verify {
				var err error
				var readback *RouterState
				readback, err = Verify(work, current, plan.Checks, d, a, func() *RouterState {
					if current == StageNATBypass {
						return unchanged
					}
					return nil
				}())
				if err != nil {
					return fail(err)
				}
				if current == StageRoutes || current == StageNATBypass {
					unchanged = readback
				}
			}
			if summary != "" {
				record(summary, status)
			}
			continue
		}
		if stage.Manual {
			record(messageReleaseManual, "manual")
			continue
		}
		if plan.Checks == nil {
			return fail(device.Error(device.MessageOperationFormat))
		}
		var checked *DhcpDnsState
		if current == StageDHCPDNS || (current == StageDNSGuard && plan.Checks.DNSGuard != nil && len(plan.Checks.DNSGuard.Roles) > 0) {
			state, err := Verify(work, StageRoutes, plan.Checks, d, a, nil)
			if err != nil {
				return fail(err)
			}
			if plan.Checks.IPv6FakeIP != nil {
				if _, err = Verify(work, StageIPv6FakeIP, plan.Checks, d, a, nil); err != nil {
					return fail(err)
				}
			}
			if _, err = Verify(work, StageNATBypass, plan.Checks, d, a, state); err != nil {
				return fail(err)
			}
			if current == StageDHCPDNS {
				if plan.Checks.DHCPDNS == nil {
					return fail(device.Error(device.MessageOperationFormat))
				}
				checked, err = d.ReadDHCPDNS(work)
				if err != nil {
					return fail(err)
				}
				if _, err = device.CheckedDNSTarget(*plan.Checks.DHCPDNS, checked); err != nil {
					return fail(err)
				}
			}
		}
		if current == StageIPv6DNS {
			if plan.Checks.DHCPDNS == nil {
				return fail(device.Error(messageIPv6DNSNeedsDHCP))
			}
			if _, err := Verify(work, StageDHCPDNS, plan.Checks, d, a, nil); err != nil {
				return fail(err)
			}
		}
		ops := make([]json.RawMessage, len(stage.Ops))
		for i, op := range stage.Ops {
			var err error
			ops[i], err = rawOp(op)
			if err != nil {
				return fail(err)
			}
		}
		// This transient proof belongs only to the execution copy, never the plan.
		if checked != nil && d.Vendor() == "routeros" && len(ops) > 0 && stage.Ops[0].OpKind() == "dhcp_dns_update" {
			var first map[string]json.RawMessage
			if err := json.Unmarshal(ops[0], &first); err != nil {
				return fail(err)
			}
			first["checked_dhcp_dns"], _ = json.Marshal(checked)
			ops[0], _ = json.Marshal(first)
		}
		unchanged = nil
		err := inStageBatch(work, d, false, func(held context.Context) error {
			batch, batched := d.(BatchDriver)
			for i := 0; i < len(ops); {
				if err := held.Err(); err != nil {
					return err
				}
				end := i + 1
				if batched && slices.Contains(batch.BatchKinds(), stage.Ops[i].OpKind()) {
					for end < len(ops) && stage.Ops[end].OpKind() == stage.Ops[i].OpKind() {
						end++
					}
				}
				seen := make([]bool, end-i)
				finished := func(raw, undo json.RawMessage, ok bool) {
					for n := i; n < end; n++ {
						if !seen[n-i] && bytes.Equal(raw, ops[n]) {
							seen[n-i] = true
							done = append(done, releaseDone{current, stage.Ops[n].OpSummary(), raw, undo})
							result.Wrote = true
							if ok {
								result.Applied++
								record(stage.Ops[n].OpSummary(), "ok")
							}
							return
						}
					}
				}
				// From here a device or Agent write may land even if this attempt
				// never proves it: shutdown must still record it (D-R12-3).
				result.Wrote = true
				if end-i > 1 {
					invalidateRelease(d)
					err := batch.ApplyMany(held, ops[i:end], func(op, undo json.RawMessage) { finished(op, undo, true) })
					if err != nil {
						if u := device.UndoOf(err); effectful(u) {
							for n := i; n < end; n++ {
								if !seen[n-i] {
									finished(ops[n], u, false)
									break
								}
							}
						}
						return err
					}
				} else {
					undo, err := applyRelease(held, ops[i], d, a, false)
					if err != nil {
						if u := device.UndoOf(err); effectful(u) {
							finished(ops[i], u, false)
						}
						return err
					}
					finished(ops[i], undo, true)
				}
				i = end
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
		if err = work.Err(); err != nil {
			return fail(err)
		}
		unchanged, err = Verify(work, current, plan.Checks, d, a, nil)
		if err != nil {
			return fail(err)
		}
		record(messageReleaseVerified, "verified")
		if current == StageRoutes {
			if stale := StaleRoutes(unchanged, plan.Checks.Gateway); len(stale) > 0 {
				record(StaleRouteWarning(stale, plan.Checks.Gateway), "warning")
			}
		}
	}
	if err := work.Err(); err != nil {
		return fail(err)
	}
	result.OK = true
	return result
}

func rollbackRelease(ctx context.Context, done []releaseDone, d Driver, a ReturnAgent) []ReleaseEntry {
	out := []ReleaseEntry{}
	entries := []releaseDone{}
	for _, v := range slices.Backward(done) {
		if effectful(v.undo) {
			entries = append(entries, v)
		}
	}
	appendResult := func(v releaseDone, problem string) {
		if problem != "" {
			out = append(out, ReleaseEntry{v.stage, fmt.Sprintf(messageRollbackFailed, v.summary, problem), "failed"})
		} else {
			out = append(out, ReleaseEntry{v.stage, fmt.Sprintf(messageRollbackOK, v.summary), "ok"})
		}
	}
	err := inStageBatch(ctx, d, true, func(held context.Context) error {
		b, batched := d.(BatchDriver)
		for i := 0; i < len(entries); {
			if err := held.Err(); err != nil {
				return err
			}
			end := i
			if batched {
				for end < len(entries) && b.BatchRestorable(entries[end].undo) {
					end++
				}
			}
			if end-i > 1 {
				raw := make([]json.RawMessage, end-i)
				for n := i; n < end; n++ {
					raw[n-i] = entries[n].undo
				}
				invalidateRelease(d)
				failures := b.RestoreMany(held, raw, false)
				for n := i; n < end; n++ {
					problem := messageReleaseUnknown
					if n-i < len(failures) {
						problem = failures[n-i]
					}
					appendResult(entries[n], problem)
				}
				i = end
			} else {
				_, err := applyRelease(held, entries[i].undo, d, a, true)
				problem := ""
				if err != nil {
					problem = releaseErrorText(err)
				}
				appendResult(entries[i], problem)
				i++
			}
		}
		return nil
	})
	if err != nil && len(done) > 0 {
		out = append(out, ReleaseEntry{done[0].stage, fmt.Sprintf(messageRollbackBatchFailed, releaseErrorText(err)), "failed"})
	}
	return out
}

func emptyStageReadback(stage Stage, c *Checks) (string, string, bool) {
	if stage.Skipped != nil && *stage.Skipped {
		if m := releaseSkippedMessages[stage.Key]; m != "" {
			return m, "warning", false
		}
	}
	if c == nil {
		return "", "", false
	}
	verify := false
	switch stage.Key {
	case StageIPv6FakeIP, StageIPv6DNS:
		verify = true
	case StageIPv6Routes:
		verify = c.IPv6Routes != nil
	case StageIPv6PDGuard:
		var v struct {
			Enabled bool `json:"enabled"`
		}
		_ = jsondecode.Unmarshal(c.IPv6PDGuard, &v)
		verify = v.Enabled
	case StageRoutes:
		verify = c.ROSPolicy != nil
	case StageReturnRouting:
		verify = c.ReturnRouting != nil
	case StageNATBypass:
		if len(c.NATNotRequired) > 0 {
			return messageNATNotRequiredVerified, "verified", true
		}
		verify = c.NATList != nil
	case StageDHCPDNS:
		verify = c.DHCPDNS != nil
	case StageDNSGuard:
		verify = c.DNSGuard != nil && len(c.DNSGuard.Roles) > 0
	}
	if verify {
		return releaseEmptyMessages[stage.Key], "verified", true
	}
	return "", "", false
}

// returnPreviousHolds proves that the Agent still holds the managed spec the
// plan was built from. Another router sharing the Agent may have applied or
// removed one since; a forward write must not remove or overwrite it, and its
// undo must not restore a stale spec.
func returnPreviousHolds(live *ReturnStatus, previous json.RawMessage) bool {
	var current json.RawMessage
	if live != nil && live.Managed != nil && live.Managed.TablePresent {
		current = live.Managed.Spec
	}
	if !effectful(previous) || !effectful(current) {
		return effectful(previous) == effectful(current)
	}
	var have, want struct {
		ProxyDst []string `json:"proxy_dst"`
	}
	return returnSpecProof(current, previous) && jsondecode.Unmarshal(current, &have) == nil &&
		jsondecode.Unmarshal(previous, &want) == nil && sameStrings(have.ProxyDst, want.ProxyDst)
}

func returnSpecProof(live, expected json.RawMessage) bool {
	var have, want map[string]json.RawMessage
	if json.Unmarshal(live, &have) != nil || json.Unmarshal(expected, &want) != nil {
		return false
	}
	delete(have, "proxy_dst")
	delete(want, "proxy_dst")
	return device.JSONEqual(have, want)
}
