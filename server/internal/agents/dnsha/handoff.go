package dnsha

import (
	"configflow/internal/jsonresp"
	"context"
	"fmt"

	"configflow/internal/deploy"
	"configflow/internal/model"
)

func (s *Service) prepareHandoff(ctx context.Context, j *model.HAJournal, members []*agent, b *deploy.Bundle) error {
	if raw, present := parseObject(jsonresp.Value(j))["handoff_required"]; present {
		if !booleanProof(raw) {
			return refuse(msgJournalChanged)
		}
		if j.Handoff != nil && !handoffFlags(j.Handoff) {
			return refuse(msgHandoffChanged)
		}
		return nil
	}
	if j.HandoffProtocol == nil || *j.HandoffProtocol != 1 {
		j.HandoffRequired = new(false)
		return s.save(ctx, j)
	}
	required := !b.Manifest().ReloadOnly
	isPair := true
	for _, a := range members {
		isPair = isPair && haEnabled(a)
	}
	if required {
		caps := []object{}
		for _, a := range members {
			raw, e := s.capabilities(ctx, a)
			if e != nil {
				return e
			}
			o := parseObject(raw)
			if len(o) == 0 && string(raw) != "{}" {
				return refuse(msgHandoffCaps)
			}
			caps = append(caps, o)
			if !hasProtocol(o, "oxidns_ha_handoff_protocols") {
				return refuse(fmt.Sprintf(msgHandoffUpgrade, name(a)))
			}
			if isPair {
				r, e := s.handoffRequest(ctx, a, "GET", j.ID)
				if e != nil {
					return e
				}
				record := r.obj("handoff")
				if len(record) > 0 && record.text("operation_id") != j.ID {
					return refuse(msgOtherHandoff)
				}
			}
		}
		if e := s.validateEnvironments(ctx, members, caps); e != nil {
			return e
		}
	}
	j.HandoffRequired = new(required && isPair)
	return s.save(ctx, j)
}
func (s *Service) handoffViews(ctx context.Context, members []*agent) (map[string]*Runtime, error) {
	out := map[string]*Runtime{}
	for _, a := range members {
		r, e := s.call(ctx, a, "GET", "", nil, 15)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &HandoffPending{msgHandoffViews, true}
		}
		managed := r.obj("managed")
		rt := runtimeOf(r)
		if r.truthy("error") || rt == nil || !managed.isTrue("active") || managed.truthy("pending") {
			return nil, &HandoffPending{msgHandoffManaged, true}
		}
		out[a.ID] = rt
	}
	return out, nil
}
func healthyHolder(views map[string]*Runtime, holder string) bool {
	count := 0
	for id, r := range views {
		if r.VIPPresent {
			count++
			if id != holder {
				return false
			}
		}
		if !r.Running || !r.Healthy || !r.VIPHealthy {
			return false
		}
	}
	return count == 1
}
func (s *Service) ensureHandoff(ctx context.Context, j *model.HAJournal, members []*agent) error {
	if !yes(j.HandoffRequired) {
		return nil
	}
	if len(j.Order) != 2 {
		return refuse(msgJournalChanged)
	}
	source, target := j.Order[1], j.Order[0]
	if j.Handoff == nil {
		j.Handoff = &model.HAHandoff{OperationID: j.ID, SourceAgentID: source, TargetAgentID: target, RequestedAt: float64(s.now().UnixNano()) / 1e9, Confirmed: new(false), Released: new(false)}
	}
	h := j.Handoff
	if !handoffFlags(h) {
		return refuse(msgHandoffChanged)
	}
	if h.OperationID == "" || h.OperationID != j.ID || h.SourceAgentID != source || h.TargetAgentID != target {
		return refuse(msgHandoffChanged)
	}
	if yes(h.Released) {
		return refuse(msgHandoffReleased)
	}
	j.Phase = "handoff"
	j.Status = "checking"
	j.ActiveAgentID = new(target)
	j.Message = msgHandoffChecking
	if e := s.save(ctx, j); e != nil {
		return e
	}
	if !yes(h.Confirmed) && float64(s.now().UnixNano())/1e9-h.RequestedAt >= HandoffTimeout.Seconds() {
		return refuse(msgHandoffTimeout)
	}
	a := selected(members, source)
	if a == nil {
		return refuse(msgInFlightIdentity)
	}
	r, e := s.handoffRequest(ctx, a, "GET", h.OperationID)
	if e != nil {
		return e
	}
	record := r.obj("handoff")
	if len(record) == 0 {
		r, e = s.handoffRequest(ctx, a, "POST", h.OperationID)
		if e != nil {
			return e
		}
		record = r.obj("handoff")
	}
	if len(record) == 0 || record.text("operation_id") != h.OperationID || !record.isTrue("active") {
		return &HandoffPending{msgHandoffYield, true}
	}
	views, e := s.handoffViews(ctx, members)
	if e != nil {
		return e
	}
	if !healthyHolder(views, target) {
		h.Confirmed = new(false)
		h.Confirmations = 0
		if e = s.save(ctx, j); e != nil {
			return e
		}
		return &HandoffPending{Message: msgHandoffWait}
	}
	h.Confirmations++
	h.Confirmed = new(h.Confirmations >= 2)
	if e = s.save(ctx, j); e != nil {
		return e
	}
	if !yes(h.Confirmed) {
		return &HandoffPending{Message: msgHandoffAgain}
	}
	return nil
}
func (s *Service) releaseHandoff(ctx context.Context, j *model.HAJournal) error {
	h := j.Handoff
	if h == nil {
		return nil
	}
	if !handoffFlags(h) {
		return &HandoffPending{msgHandoffChanged, true}
	}
	if yes(h.Released) {
		return nil
	}
	if h.OperationID == "" || h.OperationID != j.ID {
		return &HandoffPending{msgHandoffChanged, true}
	}
	a, e := s.get(ctx, h.SourceAgentID)
	if e != nil {
		return e
	}
	if a == nil {
		return &HandoffPending{msgHandoffSource, true}
	}
	j.Phase = "handoff"
	j.Status = "checking"
	j.Message = msgHandoffRelease
	if e = s.save(ctx, j); e != nil {
		return e
	}
	r, e := s.handoffRequest(ctx, a, "GET", h.OperationID)
	if e != nil {
		return e
	}
	record := r.obj("handoff")
	if len(record) > 0 {
		if record.text("operation_id") != h.OperationID {
			return &HandoffPending{msgHandoffChanged, true}
		}
		if _, e = s.handoffRequest(ctx, a, "DELETE", h.OperationID); e != nil {
			return e
		}
		r, e = s.handoffRequest(ctx, a, "GET", h.OperationID)
		if e != nil {
			return e
		}
		if len(r.obj("handoff")) > 0 {
			return &HandoffPending{msgHandoffCleanup, true}
		}
	}
	h.Released = new(true)
	h.ReleasedAt = new(float64(s.now().UnixNano()) / 1e9)
	return s.save(ctx, j)
}
func preferred(members []*agent) string {
	for _, a := range members {
		if haEnabled(a) && a.OxiDNSHA.Role == "primary" {
			return a.ID
		}
	}
	return ""
}
func (s *Service) finishHandoff(ctx context.Context, j *model.HAJournal, members []*agent) error {
	if j.Handoff == nil {
		return nil
	}
	if e := s.releaseHandoff(ctx, j); e != nil {
		return e
	}
	views, e := s.handoffViews(ctx, members)
	if e != nil {
		return e
	}
	p := preferred(members)
	if p == "" || !healthyHolder(views, p) {
		if j.Handoff.ReleasedAt != nil && float64(s.now().UnixNano())/1e9-*j.Handoff.ReleasedAt >= HandoffTimeout.Seconds() {
			return refuse(msgElectionFailed)
		}
		return &HandoffPending{Message: msgElectionWait}
	}
	return nil
}
func (s *Service) settleFailure(ctx context.Context, j *model.HAJournal) error {
	h := j.Handoff
	if h == nil || !yes(h.Released) || h.ReleasedAt == nil || float64(s.now().UnixNano())/1e9-*h.ReleasedAt >= HandoffTimeout.Seconds() {
		return nil
	}
	list, e := s.all(ctx)
	if e != nil {
		return e
	}
	members := []*agent{}
	for id := range j.Children.All() {
		a := selected(list, id)
		if a == nil {
			return nil
		}
		members = append(members, a)
	}
	views, e := s.handoffViews(ctx, members)
	if e != nil {
		return e
	}
	for _, v := range views {
		if !v.Healthy || !v.Running {
			return nil
		}
	}
	p := preferred(members)
	if p != "" && !healthyHolder(views, p) {
		return &HandoffPending{Message: msgFailureSettle}
	}
	return nil
}

// Corrupt saved proof flags must never turn a required/released handoff into
// false through the stored model's tolerant decoder. Null remains Python's
// false value; unknown journal fields remain preserved.
func booleanProof(raw []byte) bool {
	v := string(raw)
	return v == "true" || v == "false" || v == "null"
}
func handoffFlags(h *model.HAHandoff) bool {
	o := parseObject(jsonresp.Value(h))
	for _, key := range []string{"confirmed", "released"} {
		if raw, ok := o[key]; ok && !booleanProof(raw) {
			return false
		}
	}
	return true
}
