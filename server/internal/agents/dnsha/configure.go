package dnsha

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"configflow/internal/agents/rulesync"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func (s *Service) haLease(ctx context.Context) (context.Context, func(), bool) {
	return haLocks.TryLock(ctx, s.m.Repository().DataDir())
}
func (s *Service) validated(ctx context.Context, id string, body []byte) (*agent, *agent, normalized, []string, error) {
	list, e := s.all(ctx)
	if e != nil {
		return nil, nil, normalized{}, nil, e
	}
	a := selected(list, id)
	b := selected(list, parseObject(body).text("peer_id"))
	n, e := normalize(body, a, b, list)
	if e != nil {
		return nil, nil, n, nil, e
	}
	blockers, warnings := s.upstreamFindings(ctx, a, b)
	if len(blockers) > 0 {
		return nil, nil, n, nil, &Error{Message: fmt.Sprintf(msgCannotEnable, strings.Join(blockers, "；")), Blockers: blockers}
	}
	if e = s.requireProtocols(ctx, []*agent{a, b}); e != nil {
		return nil, nil, n, nil, e
	}
	if e = s.preview(ctx, a, b, n); e != nil {
		return nil, nil, n, nil, e
	}
	return a, b, n, warnings, nil
}

// Configure validates both members then durably reserves a single shared source.
func (s *Service) Configure(ctx context.Context, id string, body []byte, baseURL string) (out ConfigureView, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-configure", func(ctx context.Context) error {
		held, release, ok := s.haLease(ctx)
		if !ok {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return refuse(msgBusy)
		}
		defer release()
		a, b, n, warnings, e := s.validated(held, id, body)
		if e != nil {
			return e
		}
		routers, e := s.routersUsing(held, []string{a.ID, b.ID})
		if e != nil {
			return e
		}
		if baseURL == "" && a.AppliedContent != nil {
			baseURL = a.AppliedContent.BaseURL
		}
		pub, e := s.startEnable(held, a, b, n.intent, baseURL, warnings, routers)
		if e != nil {
			return e
		}
		out = ConfigureView{true, a.OxiDNSHA, pub, warnings, routers}
		return nil
	})
	return
}
func (s *Service) startEnable(ctx context.Context, a, b *agent, intent model.HAEnable, base string, warnings []string, routers []model.HARouter) (*model.HAPublicationSummary, error) {
	if active(a) {
		j := a.OxiDNSHAPublication
		if j.Enable == nil || !equal(j.Enable, intent) || !slices.Equal(keys(j), sortedIDs(a.ID, b.ID)) {
			return nil, refuse(msgEnableExisting)
		}
		return summary(j), nil
	}
	src, rev, e := s.source(ctx, a, true)
	if e != nil {
		return nil, e
	}
	held, release, ok := s.lease(ctx, []string{a.ID, b.ID})
	if !ok {
		return nil, refuse(msgPairBusy)
	}
	defer release()
	id, e := randomID()
	if e != nil {
		return nil, e
	}
	j, e := s.reserve(held, []*agent{a, b}, a.ID, "ha-"+id, true, &intent)
	if e != nil {
		return nil, e
	}
	settings := rulesync.SettingsFor(a.target.Record)
	j.RuleSyncSettings = &model.RuleSyncSettings{Library: ptr(settings.Library), Rules: ptr(settings.Rules)}
	j.Warnings = warnings
	j.Routers = routers
	raw, e := src.Raw()
	if e == nil {
		e = s.writePayload(held, j, &payload{Source: raw, Resource: raw, Revision: revisionRaw(rev), BaseURL: base}, nil)
	}
	if e == nil {
		e = s.beginChildren(held, j)
	}
	if e == nil {
		e = s.save(held, j)
	}
	if e != nil {
		if held.Err() != nil {
			return nil, held.Err()
		}
		if failure := s.fail(held, j, msgEnablePrepareFailed, "failed"); failure != nil {
			return nil, failure
		}
		return nil, e
	}
	return summary(j), nil
}
func sortedIDs(ids ...string) []string { slices.Sort(ids); return ids }
func (s *Service) recordError(ctx context.Context, ids []string, msg string, failed bool) error {
	return s.update(ctx, func(list []*agent) error {
		for _, a := range list {
			if !slices.Contains(ids, a.ID) || (!haEnabled(a) && a.OxiDNSHAState == nil) {
				continue
			}
			st := a.OxiDNSHAState
			if st == nil {
				st = &model.HAState{}
			}
			if failed || st.Status == "" {
				st.Status = "failed"
			}
			st.Error = ptr(msg)
			st.UpdatedAt = model.Int(s.now().Unix())
			a.OxiDNSHAState = st
		}
		return nil
	})
}
func (s *Service) recordLeftover(ctx context.Context, ids []string, msg string) error {
	return s.update(ctx, func(list []*agent) error {
		for _, a := range list {
			if slices.Contains(ids, a.ID) && !haEnabled(a) {
				a.OxiDNSHAState = &model.HAState{Status: "failed", Error: ptr(msg), UpdatedAt: model.Int(s.now().Unix()), Members: slices.Clone(ids)}
			}
		}
		return nil
	})
}
func (s *Service) undo(ctx context.Context, attempted []*agent, previous map[string]*model.HASettings, members []*agent) []string {
	out := []string{}
	for i := len(attempted) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return out
		}
		a := attempted[i]
		var e error
		if previous != nil {
			old := previous[a.ID]
			if old == nil || selected(members, old.PeerID) == nil {
				out = append(out, msgInFlightIdentity)
				continue
			}
			_, e = s.call(ctx, a, "PUT", "", rawObject(spec(old.VIP, int(old.VRID), old.Role, a, selected(members, old.PeerID))), 90)
		} else {
			_, e = s.call(ctx, a, "DELETE", "", nil, 90)
		}
		if e != nil {
			out = append(out, s.safe(ctx, e.Error()))
		}
	}
	return out
}
func (s *Service) enableFailure(ctx context.Context, attempted []*agent, n normalized, members []*agent, cause string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	undo := s.undo(ctx, attempted, n.previous, members)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	msg := cause
	if len(undo) > 0 {
		msg += fmt.Sprintf(msgUndo, strings.Join(undo, "；"))
	}
	ids := []string{}
	for _, a := range members {
		ids = append(ids, a.ID)
	}
	var e error
	if n.previous != nil {
		e = s.recordError(ctx, ids, msg, len(undo) > 0)
	} else if len(undo) > 0 {
		ids = nil
		for _, a := range attempted {
			ids = append(ids, a.ID)
		}
		e = s.recordLeftover(ctx, ids, msg)
	}
	if e != nil && ctx.Err() == nil {
		logger.Error(msgFailureRecord)
	}
	return refuse(msg)
}
func (s *Service) configureKeepalived(ctx context.Context, id string, intent *model.HAEnable, settings *model.RuleSyncSettings) error {
	held, release, ok := s.haLease(ctx)
	if !ok {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errHALockBusy
	}
	defer release()
	a, b, n, _, e := s.validated(held, id, rawObject(intent))
	if e != nil {
		return e
	}
	members := []*agent{a, b}
	attempted := []*agent{}
	for i, m := range []*agent{b, a} {
		if held.Err() != nil {
			return held.Err()
		}
		other, role := a, "secondary"
		if i == 1 {
			other, role = b, "primary"
		}
		attempted = append(attempted, m)
		if _, e = s.call(held, m, "PUT", "", rawObject(spec(n.intent.VIP, int(n.intent.VRID), role, m, other)), 90); e != nil {
			if held.Err() != nil {
				return held.Err()
			}
			if IsUserError(e) {
				return s.enableFailure(held, attempted, n, members, e.Error())
			}
			return e
		}
	}
	group := ""
	if old := n.previous[a.ID]; old != nil {
		group = old.GroupID
	}
	if group == "" {
		group, e = randomID()
		if e != nil {
			return s.enableFailure(held, attempted, n, members, msgSaveFailed)
		}
	}
	e = s.update(held, func(list []*agent) error {
		for _, old := range members {
			cur := selected(list, old.ID)
			if cur == nil || !equal(cur.identity, old.identity) {
				return refuse(msgEnableChanged)
			}
			if haEnabled(cur) && (cur.OxiDNSHA.GroupID != group || n.previous == nil) {
				return refuse(fmt.Sprintf(msgEnableOtherGroup, name(cur)))
			}
		}
		for i, old := range members {
			cur := selected(list, old.ID)
			peer, role := b, "primary"
			if i == 1 {
				peer, role = a, "secondary"
			}
			cur.OxiDNSHA = &model.HASettings{Enabled: ptr(true), GroupID: group, PeerID: peer.ID, VIP: n.intent.VIP, VRID: n.intent.VRID, Role: role}
			cur.OxiDNSHAState = &model.HAState{Status: "succeeded", Error: ptr(""), UpdatedAt: model.Int(s.now().Unix())}
			if settings != nil {
				if cur.RuleSync == nil {
					cur.RuleSync = &model.AgentRuleSync{}
				}
				cur.RuleSync.Settings = *settings
			}
		}
		return nil
	})
	if e != nil {
		if held.Err() != nil {
			return held.Err()
		}
		msg := msgSaveFailed
		if IsUserError(e) {
			msg = e.Error()
		}
		return s.enableFailure(held, attempted, n, members, msg)
	}
	clock := lifecycle.ClockFrom(held)
	deadline := clock.Now().Add(SettleTime)
	for {
		v, e := s.status(held, id)
		if e != nil {
			return e
		}
		if v.Health != "split" && v.Health != "down" || !clock.Now().Before(deadline) {
			return nil
		}
		timer := clock.NewTimer(time.Second)
		select {
		case <-held.Done():
			timer.Stop()
			return held.Err()
		case <-lifecycle.Stopping(held):
			timer.Stop()
			return context.Canceled
		case <-timer.C():
		}
	}
}

// Disable removes keepalived secondary first, preserving history and refusing active work.
func (s *Service) Disable(ctx context.Context, id string, force bool) (out DisableView, err error) {
	err = deploy.RunDetached(ctx, "oxidns-ha-disable", func(ctx context.Context) error {
		a, e := s.get(ctx, id)
		if e != nil {
			return e
		}
		ids := []string{id}
		if a != nil {
			if a.OxiDNSHAPublication != nil {
				ids = keys(a.OxiDNSHAPublication)
			}
			if a.OxiDNSHA != nil && a.OxiDNSHA.PeerID != "" {
				ids = []string{id, a.OxiDNSHA.PeerID}
			}
		}
		held, release, ok := s.lease(ctx, ids)
		if !ok {
			return refuse(msgDisableBusy)
		}
		defer release()
		held, haRelease, ok := s.haLease(held)
		if !ok {
			return refuse(msgBusy)
		}
		defer haRelease()
		list, e := s.all(held)
		if e != nil {
			return e
		}
		a = selected(list, id)
		if active(a) {
			return refuse(msgDisableActive)
		}
		if a == nil {
			return refuse(msgMissing)
		}
		out = DisableView{Success: true, Warnings: []string{}, Routers: []model.HARouter{}}
		if a.OxiDNSHA == nil && !a.hasHAState {
			return nil
		}
		members := []*agent{a}
		cleaning := haEnabled(a)
		if cleaning {
			b := selected(list, a.OxiDNSHA.PeerID)
			if b != nil && b.ID != a.ID && b.OxiDNSHA != nil && (b.OxiDNSHA.GroupID == a.OxiDNSHA.GroupID || b.OxiDNSHA.PeerID == a.ID) {
				members = append(members, b)
			}
		} else if a.OxiDNSHAState != nil {
			for _, peerID := range a.OxiDNSHAState.Members {
				b := selected(list, peerID)
				if b != nil && b.ID != a.ID && !haEnabled(b) && !slices.ContainsFunc(members, func(m *agent) bool { return m.ID == b.ID }) {
					members = append(members, b)
				}
			}
		}
		slices.SortStableFunc(members, func(a, b *agent) int {
			secondary := func(a *agent) bool { return a.OxiDNSHA != nil && a.OxiDNSHA.Role == "secondary" }
			if secondary(a) && !secondary(b) {
				return -1
			}
			if secondary(b) && !secondary(a) {
				return 1
			}
			return 0
		})
		ids = nil
		for _, m := range members {
			ids = append(ids, m.ID)
			if _, e = s.call(held, m, "DELETE", "", nil, 90); e != nil {
				if held.Err() != nil {
					return held.Err()
				}
				if !IsUserError(e) {
					return e
				}
				out.Warnings = append(out.Warnings, s.safe(held, e.Error()))
			}
		}
		if len(out.Warnings) > 0 && !force {
			msg := fmt.Sprintf(msgDisableFailed, strings.Join(out.Warnings, "；"))
			if e = s.recordError(held, ids, msg, false); e != nil {
				return e
			}
			return &Error{Message: msg + msgForce, CanForce: true}
		}
		e = s.update(held, func(current []*agent) error {
			for _, m := range current {
				if slices.Contains(ids, m.ID) && (cleaning || !haEnabled(m)) {
					m.dropHA = true
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		out.Routers, e = s.routersUsing(held, ids)
		return e
	})
	return
}
