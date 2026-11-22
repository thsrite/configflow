package dnsha

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// Automatic completion of a pair publication stopped by a lost member (lab
// O-1 follow-up). The stopped journal keeps the intent: its payload is the
// interrupted content, AutoCompleteWatch the recovery evidence. A manual
// publication or disabling HA replaces or drops the journal and with it the
// intent; the completing publication is an ordinary pair publication.
const (
	autoWaiting = "waiting"
	autoRunning = "running"
	autoBlocked = "blocked"
	// AutoCompleteHeartbeats is how many consecutive healthy heartbeats each
	// lost member must report before the publication is completed.
	AutoCompleteHeartbeats = 2
	// AutoCompleteMaxAge bounds how long a stopped publication stays eligible.
	AutoCompleteMaxAge = 24 * time.Hour
)

// lostStop reports whether j already stopped (or is stopping) for a lost member.
func lostStop(j *model.HAJournal) bool {
	for _, msg := range []string{msgMemberLost, msgMemberLostManual} {
		if strings.HasPrefix(j.Message, msg) || (j.HandoffFailure != nil && j.HandoffFailure.Message == msg) {
			return true
		}
	}
	return false
}

// markLost records, once, which members were unreachable when the
// publication stopped and returns the stop message. Enabling HA is not
// completed automatically.
func (s *Service) markLost(ctx context.Context, j *model.HAJournal) (string, error) {
	if j.AutoComplete != nil && j.AutoComplete.Status == autoWaiting {
		return msgMemberLost, nil
	}
	if j.Enable != nil {
		return msgMemberLostManual, nil
	}
	members, e := s.members(ctx, j)
	if e != nil {
		return msgMemberLostManual, nil
	}
	lost := []string{}
	for _, a := range members {
		if _, e := s.runtime(ctx, a); e != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			lost = append(lost, a.ID)
		}
	}
	if len(lost) == 0 {
		// Unreachable again only between the round and this check: require
		// both members to prove recovery.
		for _, a := range members {
			lost = append(lost, a.ID)
		}
	}
	slices.Sort(lost)
	j.AutoComplete = &model.HAAutoComplete{Status: autoWaiting, Message: msgAutoWaiting}
	j.AutoCompleteWatch = &model.HAAutoCompleteWatch{Lost: lost, StoppedAt: float64(s.now().UnixNano()) / 1e9}
	return msgMemberLost, nil
}

// awaitingCompletion reports a stopped pair whose automatic completion is pending.
func awaitingCompletion(j *model.HAJournal) bool {
	return j != nil && j.Status == "failed" && j.AutoComplete != nil && !j.AutoComplete.Attempt &&
		(j.AutoComplete.Status == autoWaiting || j.AutoComplete.Status == autoRunning)
}

// publishInput is the interrupted content, re-identified for a new publication.
type publishInput struct {
	owner   string
	journal string
	run     func(context.Context) (*model.Deployment, error)
}

// AutoComplete evaluates one stopped pair: it records heartbeat evidence and,
// when every lost member is back and healthy, publishes the interrupted
// content once through the ordinary pair publication.
func (s *Service) AutoComplete(ctx context.Context, id string) error {
	a, e := s.get(ctx, id)
	if e != nil || a == nil || !awaitingCompletion(a.OxiDNSHAPublication) {
		return e
	}
	held, release, ok := s.lease(ctx, keys(a.OxiDNSHAPublication))
	if !ok {
		// Another publication or disable owns the pair; try next round.
		return nil
	}
	next, e := func() (*publishInput, error) {
		defer release()
		return s.evaluate(held, id)
	}()
	if e != nil || next == nil {
		return e
	}
	out, e := next.run(ctx)
	if ctx.Err() != nil {
		// Interrupted before the publication was reserved: the journal still
		// says running and the next owner retries.
		return ctx.Err()
	}
	reason := ""
	switch {
	case e != nil && IsUserError(e):
		reason = fmt.Sprintf(msgAutoRefused, s.safe(ctx, e.Error()))
	case e != nil:
		logger.Warningf("OxiDNS pair auto-completion failed (%T)", e)
		reason = fmt.Sprintf(msgAutoRefused, msgUnexpected)
	case out != nil && out.HTTPStatus != nil && *out.HTTPStatus >= 400:
		reason = fmt.Sprintf(msgAutoRefused, s.safe(ctx, message(*out, msgUnexpected)))
	}
	if reason == "" {
		logger.Info("OxiDNS pair publication auto-completion started")
		return nil
	}
	// Only a refusal before reservation leaves the stopped journal in place.
	return s.blockIfCurrent(ctx, id, next.journal, reason)
}

// evaluate runs under the pair lease. It returns the publication to start, or
// nil after recording progress or a blocking reason.
func (s *Service) evaluate(ctx context.Context, id string) (*publishInput, error) {
	a, e := s.get(ctx, id)
	if e != nil || a == nil || !awaitingCompletion(a.OxiDNSHAPublication) {
		return nil, e
	}
	j, e := ownJournal(a.OxiDNSHAPublication)
	if e != nil {
		return nil, e
	}
	block := func(reason string) (*publishInput, error) {
		j.AutoComplete = &model.HAAutoComplete{Status: autoBlocked, Message: fmt.Sprintf(msgAutoBlocked, reason)}
		logger.Warningf("OxiDNS pair auto-completion skipped: %s", reason)
		return nil, s.save(ctx, j)
	}
	w := j.AutoCompleteWatch
	if w == nil || len(w.Lost) == 0 {
		return block(msgAutoPayload)
	}
	if float64(s.now().UnixNano())/1e9-w.StoppedAt >= AutoCompleteMaxAge.Seconds() {
		return block(msgAutoExpired)
	}
	members, e := s.members(ctx, j)
	if e != nil {
		if IsUserError(e) {
			return block(msgAutoPairChanged)
		}
		return nil, e
	}
	if len(members) != 2 || !haEnabled(members[0]) || !haEnabled(members[1]) || !agents.HAPairMatchesModel(members[0].Agent, members[1].Agent) {
		return block(msgAutoPairChanged)
	}
	changed := false
	ready := true
	for _, lost := range w.Lost {
		m := selected(members, lost)
		if m == nil {
			return block(msgAutoPairChanged)
		}
		count, _ := w.Healthy.Get(lost)
		previous, _ := w.Heartbeats.Get(lost)
		if m.LastHeartbeat != "" && m.LastHeartbeat != previous {
			w.Heartbeats.Set(lost, m.LastHeartbeat)
			healthy := m.Status == "online" && m.ServiceStatus != nil && *m.ServiceStatus == "running"
			if healthy {
				if _, e := s.runtime(ctx, m); e != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					healthy = false
				}
			}
			count++
			if !healthy {
				count = 0
			}
			w.Healthy.Set(lost, count)
			changed = true
		}
		ready = ready && count >= AutoCompleteHeartbeats
	}
	for _, m := range members {
		ready = ready && m.Status == "online"
	}
	if ready {
		for _, m := range members {
			if slices.Contains(w.Lost, m.ID) {
				continue
			}
			if _, e := s.runtime(ctx, m); e != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				ready = false
			}
		}
	}
	if !ready {
		if changed {
			return nil, s.save(ctx, j)
		}
		return nil, nil
	}
	p, b, e := s.readPayload(ctx, j)
	if e != nil || b == nil {
		if e != nil && !IsUserError(e) && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return block(msgAutoPayload)
	}
	for _, m := range members {
		raw, e := s.capabilities(ctx, m)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return block(fmt.Sprintf(msgAutoUnsupported, name(m)))
		}
		caps := parseObject(raw)
		if !hasProtocol(caps, "oxidns_ha_protocols") || (!b.Manifest().ReloadOnly && !hasProtocol(caps, "oxidns_ha_handoff_protocols")) {
			return block(fmt.Sprintf(msgAutoUnsupported, name(m)))
		}
	}
	o, e := publicationOptions(p)
	if e != nil {
		return block(msgAutoPayload)
	}
	o.Activate = yes(j.Activate)
	did, e := randomID()
	if e != nil {
		return nil, e
	}
	// The interrupted content with a new identity: deployment IDs are single use.
	next, e := childBundle(b, j.OwnerAgentID, did)
	if e != nil {
		return block(msgAutoPayload)
	}
	j.AutoComplete = &model.HAAutoComplete{Status: autoRunning, Message: msgAutoRunning}
	if e = s.save(ctx, j); e != nil {
		return nil, e
	}
	owner, stopped := j.OwnerAgentID, j.ID
	return &publishInput{owner: owner, journal: stopped, run: func(ctx context.Context) (*model.Deployment, error) {
		out, _, e := s.publishPair(ctx, owner, next, o, stopped)
		return out, e
	}}, nil
}

// blockIfCurrent records a refusal on the stopped journal unless a new
// publication has replaced it meanwhile.
func (s *Service) blockIfCurrent(ctx context.Context, id, journal, reason string) error {
	a, e := s.get(ctx, id)
	if e != nil || a == nil || a.OxiDNSHAPublication == nil || a.OxiDNSHAPublication.ID != journal {
		return e
	}
	held, release, ok := s.lease(ctx, keys(a.OxiDNSHAPublication))
	if !ok {
		return nil
	}
	defer release()
	a, e = s.get(held, id)
	if e != nil || a == nil || a.OxiDNSHAPublication == nil || a.OxiDNSHAPublication.ID != journal {
		return e
	}
	j, e := ownJournal(a.OxiDNSHAPublication)
	if e != nil {
		return e
	}
	j.AutoComplete = &model.HAAutoComplete{Status: autoBlocked, Message: fmt.Sprintf(msgAutoBlocked, reason)}
	logger.Warningf("OxiDNS pair auto-completion stopped: %s", reason)
	return s.save(held, j)
}
