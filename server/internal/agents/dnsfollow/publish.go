package dnsfollow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

func equalJSON(a, b any) bool {
	left, e := json.Marshal(a)
	if e != nil {
		return false
	}
	right, e := json.Marshal(b)
	return e == nil && deploy.CanonicalDigest(left) == deploy.CanonicalDigest(right)
}
func hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func (s *Service) sameNamespace(ctx context.Context, target, source *agent) error {
	r, e := s.request(ctx, target, "dns/peer", "")
	if e != nil {
		return e
	}
	if r.PeerAgentID == "" || r.PeerAgentID != source.ID || r.ServiceType != "mihomo" {
		return refusal(msgPeer)
	}
	return nil
}
func (s *Service) endpoint(ctx context.Context, target, source *agent, b *model.DNSFollowBinding, id string, entry *yamlx.Node) (string, error) {
	r, e := s.request(ctx, source, "dns/listener", id)
	if e != nil {
		return "", e
	}
	if r.AgentID == "" || r.AgentID != source.ID || r.DeploymentID == "" || r.DeploymentID != id || !r.Enabled {
		return "", refusal(msgListenerDisabled)
	}
	var port int
	if json.Unmarshal(r.Port, &port) != nil || port < 1 || port > 65535 || !r.HostIsText {
		return "", refusal(msgListenerInvalid)
	}
	ip, e := netip.ParseAddr(r.Host)
	if e != nil {
		return "", refusal(msgListenerIP)
	}
	host := r.Host
	if b.Connection == "auto" {
		if e = s.sameNamespace(ctx, target, source); e != nil {
			return "", e
		}
		if ip.IsUnspecified() {
			host = "127.0.0.1"
			if ip.Is6() {
				host = "::1"
			}
		}
	} else {
		if ip.IsLoopback() {
			return "", refusal(msgListenerLoopback)
		}
		host, e = networkHost(value(b.Address))
		if e != nil {
			return "", e
		}
	}
	protocol, e := scheme(text(yamlx.Get(entry, "addr")))
	if e != nil {
		return "", e
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return protocol + "://" + host + ":" + strconv.Itoa(port), nil
}

func generatedMain(src *deploy.Source, base string) (*yamlx.Node, error) {
	raw, e := src.Raw()
	if e != nil {
		return nil, e
	}
	p, e := oxidns.ProfileFromJSON(raw)
	if e != nil {
		return nil, e
	}
	text, e := oxidns.Generate(p, base)
	if e != nil {
		return nil, e
	}
	return yamlx.Parse([]byte(text))
}

var fullVersion = regexp.MustCompile(`^[0-9a-f]{64}$`)

func matchesAppliedConfig(target *agent, src *deploy.Source, base *model.AppliedContent) (bool, error) {
	if !fullVersion.MatchString(base.ConfigVersion) || target.ConfigVersion != base.ConfigVersion {
		return false, nil
	}
	main, e := generatedMain(src, base.BaseURL)
	if e != nil {
		return false, e
	}
	for _, watchdog := range []bool{false, true} {
		raw, e := deploy.RenderConfig(main, "oxidns", watchdog)
		if e != nil {
			return false, e
		}
		if hash(raw) == base.ConfigVersion {
			return true, nil
		}
	}
	return false, nil
}
func legacyMain(src *deploy.Source, base *model.AppliedContent) ([]byte, error) {
	main, e := generatedMain(src, base.BaseURL)
	if e != nil {
		return nil, e
	}
	return deploy.RenderConfig(main, "oxidns", false)
}
func matchesAppliedBundle(b *deploy.Bundle, src *deploy.Source, base *model.AppliedContent) (bool, error) {
	m := b.Manifest()
	fp, e := deploy.Fingerprint(m)
	if e != nil {
		return false, e
	}
	if fp == base.Fingerprint {
		return true, nil
	}
	raw, e := legacyMain(src, base)
	if e != nil {
		return false, e
	}
	for i := range m.Files {
		if m.Files[i].Path == m.ConfigPath {
			m.Files[i].SHA256 = hash(raw)
			m.Files[i].Size = int64(len(raw))
		}
	}
	fp, e = deploy.Fingerprint(m)
	return fp == base.Fingerprint, e
}
func driftMessage(b *deploy.Bundle, src *deploy.Source, base *model.AppliedContent) (string, error) {
	if base.Files == nil || value(base.SettingsDigest) == "" || value(base.SettingsDigest) != deploy.SettingsDigest(src.Member("oxidns")) {
		return UnpublishedChanges, nil
	}
	current, e := deploy.Digests(b.Manifest())
	if e != nil {
		return "", e
	}
	raw, e := legacyMain(src, base)
	if e != nil {
		return "", e
	}
	oldMain, _ := base.Files.Get("config.yaml")
	mainChanged := oldMain != current["config.yaml"] && oldMain != hash(raw)
	paths := map[string]bool{}
	for key := range base.Files.All() {
		paths[key] = true
	}
	for key := range current {
		paths[key] = true
	}
	var changed []string
	for path := range paths {
		previous, _ := base.Files.Get(path)
		if path != "config.yaml" && previous != current[path] {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	if !mainChanged {
		example := ""
		if len(changed) > 0 {
			example = fmt.Sprintf(msgRuleExample, changed[0])
		}
		return fmt.Sprintf(msgRuleDrift, len(changed), example), nil
	}
	extra := ""
	if len(changed) > 0 {
		extra = fmt.Sprintf(msgRuleExtra, len(changed))
	}
	return fmt.Sprintf(msgMainDrift, extra), nil
}
func resources(b *deploy.Bundle) map[string]string {
	m := b.Manifest()
	out := map[string]string{}
	for _, f := range m.Files {
		if f.Path != m.ConfigPath {
			out[f.Path] = f.SHA256
		}
	}
	return out
}

func ownership(list []*agent, target, source *agent, b *model.DNSFollowBinding, sourceID string, base *model.AppliedContent) error {
	currentSource, e := listed(list, source.ID, "mihomo")
	if e != nil {
		return e
	}
	currentTarget, e := listed(list, target.ID, "oxidns")
	if e != nil {
		return e
	}
	for _, pair := range [][2]*agent{{currentSource, source}, {currentTarget, target}} {
		if pair[0].Host != pair[1].Host || pair[0].Port != pair[1].Port || pair[0].Token != pair[1].Token {
			return refusal(msgConnectionChanged)
		}
	}
	if currentSource.ProfileID != b.SourceProfileID || currentTarget.ProfileID != b.TargetProfileID || !equalJSON(currentTarget.DNSFollow, b) {
		return refusal(msgBindingChanged)
	}
	ls := latest(currentSource)
	if ls.DeploymentID == "" || ls.DeploymentID != sourceID || ls.Status != "succeeded" || value(ls.ManualRollback) {
		return refusal(msgSourceChanged)
	}
	lt, prior := latest(currentTarget), latest(target)
	if !equalJSON(currentTarget.AppliedContent, base) || currentTarget.ConfigVersion != target.ConfigVersion || lt.DeploymentID != prior.DeploymentID || lt.Status != prior.Status || value(lt.ManualRollback) != value(prior.ManualRollback) || (lt.DeploymentID != "" && !terminal(lt.Status)) {
		return refusal(msgBaselineChanged)
	}
	return nil
}
func (s *Service) finishUnchanged(ctx context.Context, target, source *agent, b *model.DNSFollowBinding, sourceID string, base *model.AppliedContent, address string) error {
	_, _, e := s.saveState(ctx, target.ID, "succeeded", msgUnchanged, statePatch{clearTarget: true, address: &address}, func(_ context.Context, list []*agent) error {
		return ownership(list, target, source, b, sourceID, base)
	})
	return e
}
func (s *Service) run(ctx context.Context, target, source *agent, b *model.DNSFollowBinding, sourceState model.Deployment) error {
	if target == nil {
		return refusal("oxidns" + msgMissingAgent)
	}
	sourceID := sourceState.DeploymentID
	previous := state(target)
	attempt := model.Int(0)
	if value(previous.SourceDeploymentID) == sourceID {
		attempt = value(previous.Attempt)
	}
	raw, e := json.Marshal([]any{b.ID, sourceID, int64(attempt)})
	if e != nil {
		return e
	}
	childID := "dns-" + deploy.CanonicalDigest(raw)[:48]
	if _, _, e = s.saveState(ctx, target.ID, "preparing", msgReading, statePatch{sourceID: &sourceID, targetID: &childID, attempt: &attempt, retry: ptr(false)}, nil); e != nil {
		return e
	}
	base := target.AppliedContent
	if base == nil || base.Fingerprint == "" || base.ProfileID != b.TargetProfileID || base.ServiceType != "oxidns" {
		return refusal(msgBaseline)
	}
	src, revision, e := s.snapshot(ctx, b.TargetProfileID)
	if e != nil {
		return e
	}
	settings, doc, e := document(src, base.BaseURL)
	if e != nil {
		return e
	}
	entry, e := boundEntry(doc, b)
	if e != nil {
		return e
	}
	sig, e := signature(entry)
	if e != nil {
		return e
	}
	if sig != b.UpstreamSignature {
		return refusal(msgEdited)
	}
	address, e := s.endpoint(ctx, target, source, b, sourceID, entry)
	if e != nil {
		return e
	}
	active, e := s.request(ctx, target, "dns/active", base.DeploymentID)
	if e != nil {
		return e
	}
	if active.AgentID == "" || active.AgentID != target.ID || active.DeploymentID == "" || active.DeploymentID != base.DeploymentID {
		return refusal(msgActiveMismatch)
	}
	if text(yamlx.Get(entry, "addr")) == address {
		same, e := matchesAppliedConfig(target, src, base)
		if e != nil {
			return e
		}
		if same {
			return s.finishUnchanged(ctx, target, source, b, sourceID, base, address)
		}
	}
	if _, _, e = s.saveState(ctx, target.ID, "preparing", msgChecking, statePatch{}, nil); e != nil {
		return e
	}
	prepared, e := s.prepare(ctx, s.m, target.target, src, deploy.PrepareOptions{BaseURL: base.BaseURL, DeploymentID: childID})
	if e != nil {
		return e
	}
	matched, e := matchesAppliedBundle(prepared.Bundle, src, base)
	if e != nil {
		return e
	}
	if !matched {
		message, e := driftMessage(prepared.Bundle, src, base)
		if e != nil {
			return e
		}
		return refusal(message)
	}
	protocol, endpoint, _ := strings.Cut(address, "://")
	if _, _, e = s.saveState(ctx, target.ID, "preparing", fmt.Sprintf(msgSynchronizing, endpoint, strings.ToUpper(protocol)), statePatch{address: &address}, nil); e != nil {
		return e
	}
	if text(yamlx.Get(entry, "addr")) == address {
		return s.finishUnchanged(ctx, target, source, b, sourceID, base, address)
	}
	originalDigest := deploy.SettingsDigest(src.Member("oxidns"))
	yamlx.Set(entry, "addr", yamlx.Str(address))
	encoded, e := yamlx.Marshal(doc)
	if e != nil {
		return e
	}
	yamlx.Set(settings, "native_config", yamlx.Str(string(encoded)))
	settingsJSON, e := yamlx.ToJSON(settings)
	if e != nil {
		return e
	}
	candidateSource, e := src.WithMember("oxidns", settingsJSON)
	if e != nil {
		return e
	}
	candidate, e := s.prepare(ctx, s.m, target.target, candidateSource, deploy.PrepareOptions{BaseURL: base.BaseURL, DeploymentID: childID})
	if e != nil {
		return e
	}
	if !equalJSON(resources(prepared.Bundle), resources(candidate.Bundle)) {
		return refusal(msgResourcesChanged)
	}
	list, e := s.all(ctx)
	if e != nil {
		return e
	}
	if e = ownership(list, target, source, b, sourceID, base); e != nil {
		return e
	}
	if e = s.reserve(ctx, target, childID); e != nil {
		return e
	}
	defer s.m.FinishDeploymentPreparation(target.ID, childID)
	candidateSignature, e := signature(entry)
	if e != nil {
		return e
	}
	job := &model.DNSFollowJob{BindingID: b.ID, ProfileID: b.TargetProfileID, OriginalSettingsDigest: originalDigest, CandidateSignature: candidateSignature}
	if e = job.CandidateSettings.UnmarshalJSON(settingsJSON); e != nil {
		return e
	}
	if e = s.update(ctx, func(_ context.Context, list []*agent) error {
		a := selected(list, target.ID)
		if a == nil {
			return refusal("oxidns" + msgMissingAgent)
		}
		a.DNSFollowJob = job
		return nil
	}); e != nil {
		return e
	}
	if _, _, e = s.saveState(ctx, target.ID, "publishing", msgPublishing, statePatch{}, nil); e != nil {
		return e
	}
	if _, e = deploy.Publish(ctx, s.m, target.ID, candidate.Bundle, deploy.PublishOptions{Activate: true, ConfigRevision: revision, BaseURL: base.BaseURL, ResourceConfig: candidateSource, SourceSnapshot: candidateSource}); e != nil {
		return e
	}
	current, e := s.get(ctx, target.ID)
	if e != nil {
		return e
	}
	return s.resume(ctx, current)
}

func (s *Service) finalOwnership(ctx context.Context, list []*agent, targetID string, st model.DNSFollowState, job *model.DNSFollowJob) (*agent, error) {
	target, e := listed(list, targetID, "oxidns")
	if e != nil {
		return nil, e
	}
	id := value(st.TargetDeploymentID)
	if id == "" || job.BindingID == "" || job.ProfileID == "" || job.OriginalSettingsDigest == "" || job.CandidateSignature == "" || latest(target).DeploymentID != id || target.AppliedContent == nil || target.AppliedContent.DeploymentID != id || target.ProfileID != job.ProfileID || target.DNSFollow == nil || target.DNSFollow.ID != job.BindingID {
		return nil, refusal(msgFinalOwnership)
	}
	source, e := listed(list, target.DNSFollow.SourceAgentID, "mihomo")
	if e != nil {
		return nil, e
	}
	ls := latest(source)
	if source.ProfileID != target.DNSFollow.SourceProfileID || value(st.SourceDeploymentID) == "" || ls.DeploymentID != value(st.SourceDeploymentID) || ls.Status != "succeeded" || value(ls.ManualRollback) {
		return nil, refusal(msgFinalSource)
	}
	return target, nil
}
func (s *Service) finalize(ctx context.Context, target *agent, st model.DNSFollowState, job *model.DNSFollowJob) error {
	list, e := s.all(ctx)
	if e != nil {
		return e
	}
	if _, e = s.finalOwnership(ctx, list, target.ID, st, job); e != nil {
		return e
	}
	encoded, e := job.CandidateSettings.MarshalJSON()
	if e != nil {
		return e
	}
	// RawMessage, not []byte: json.Marshal turns a []byte into base64 text,
	// and the repeat check below must compare the settings themselves.
	candidate := json.RawMessage(encoded)
	warning := ""
	e = s.saveDraft(ctx, job.ProfileID, func(tx context.Context, src *deploy.Source) (*deploy.Source, error) {
		// Recheck ownership in the same L3 transaction that authorizes the CAS.
		list, e := s.all(tx)
		if e != nil {
			return nil, e
		}
		if _, e = s.finalOwnership(tx, list, target.ID, st, job); e != nil {
			return nil, e
		}
		if equalJSON(src.Member("oxidns"), candidate) {
			return nil, nil
		}
		if deploy.SettingsDigest(src.Member("oxidns")) != job.OriginalSettingsDigest {
			return nil, refusal(msgDraftEdited)
		}
		return src.WithMember("oxidns", candidate)
	})
	if e != nil {
		if !IsUserError(e) {
			return e
		}
		if e.Error() != msgDraftEdited && !repo.IsValueError(e) {
			return e
		}
		warning = e.Error()
	}
	if warning == "" {
		// The draft now holds the candidate entry. Its signature belongs to the
		// binding that authorized the job, whatever the source does next; a
		// newer Mihomo publication is followed by a later round.
		if e = s.update(ctx, func(_ context.Context, list []*agent) error {
			if a := selected(list, target.ID); a != nil && a.DNSFollow != nil && a.DNSFollow.ID == job.BindingID {
				a.DNSFollow.UpstreamSignature = job.CandidateSignature
			}
			return nil
		}); e != nil {
			return e
		}
	}
	message := msgSucceeded
	if warning != "" {
		message += "；" + warning
	}
	_, _, e = s.saveState(ctx, target.ID, "succeeded", message, statePatch{draftSynced: ptr(warning == "")}, nil)
	return e
}
func (s *Service) resume(ctx context.Context, target *agent) error {
	if target == nil {
		return refusal("oxidns" + msgMissingAgent)
	}
	st := state(target)
	id := value(st.TargetDeploymentID)
	job := target.DNSFollowJob
	if id == "" || job == nil {
		return refusal(msgInterrupted)
	}
	child, e := s.poll(ctx, target, id)
	if e != nil {
		return e
	}
	if child.Status == "succeeded" && !value(child.ManualRollback) {
		return s.finalize(ctx, target, st, job)
	}
	if child.Status == "rollback_failed" {
		_, _, e = s.saveState(ctx, target.ID, "publishing", msgRollbackFailed, statePatch{}, nil)
		return e
	}
	if child.Status == "failed" || child.Status == "rolled_back" || value(child.HTTPStatus) == 404 {
		message := value(child.ErrorDetails)
		if message == "" {
			message = value(child.Error)
		}
		if message == "" {
			message = value(child.Message)
		}
		if message == "" {
			message = msgPublishFailed
		}
		return refusal(message)
	}
	message := value(child.Message)
	if message == "" {
		message = msgConfirmingDefault
	}
	_, _, e = s.saveState(ctx, target.ID, "publishing", msgConfirming+message, statePatch{}, nil)
	return e
}
