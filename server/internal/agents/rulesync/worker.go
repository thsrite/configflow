package rulesync

import (
	"configflow/internal/store"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/deploy"
	"configflow/internal/logx"
	"configflow/internal/model"
)

var logger = logx.Get("backend.agents.rule_sync")

type comparison struct {
	At       time.Time
	Inputs   string
	Outcome  Kind
	Unsafe   bool
	Withheld []string
}
type worker struct {
	s      *Service
	notice string
}
type nativeTask struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func finished(status string) bool {
	return status == "succeeded" || status == "failed" || status == "rolled_back"
}
func (w *worker) state(ctx context.Context, id, status, message string, fields model.RuleSyncState) error {
	fields.Status = status
	fields.Message = message
	if w.notice != "" {
		fields.Message += "；" + w.notice
	}
	return w.s.saveState(ctx, id, fields)
}
func (w *worker) failure(ctx context.Context, id string, cause error) error {
	record, e := w.s.m.GetAgentByID(ctx, id, false)
	if e != nil {
		return e
	}
	if record == nil {
		return nil
	}
	a, e := decodeAgent(record)
	if e != nil {
		return e
	}
	old := stateOf(a)
	message := msgFailed
	status := "failed"
	var monitorErr *monitor.Error
	if errors.As(cause, &monitorErr) {
		messages := monitor.PanelMessages
		if a.ServiceType == "oxidns" {
			messages = monitor.RuleUpdateMessages
		}
		if text := messages[monitorErr.Code]; text != "" {
			message = text
		}
		if slices.Contains([]string{"RULE_UPDATE_NOT_CONFIGURED", "RULE_UPDATE_AMBIGUOUS", "API_NOT_CONFIGURED", "CONFIG_UNSUPPORTED", "TLS_UNSUPPORTED", "CONTROLLER_UNAVAILABLE", "UPGRADE_REQUIRED", "CORE_UPDATE_UNSUPPORTED"}, monitorErr.Code) {
			status = "blocked"
		}
	}
	return w.state(ctx, id, status, message, model.RuleSyncState{RetryAt: ptr(nowSeconds(w.s.now().Add(RetryAfter))), ForceRefresh: ptr(true), NativeTask: old.NativeTask, Key: old.Key})
}
func (s *Service) comparison(ctx context.Context, record *agents.Record, settings Settings) (comparison, string, error) {
	head, e := s.m.Repository().Head(ctx)
	if e != nil {
		return comparison{}, "", e
	}
	a, e := decodeAgent(record)
	if e != nil {
		return comparison{}, "", e
	}
	stamp, _ := s.receipts.Stamp(record)
	type cachedFile struct {
		Path     string
		Size     int64
		Modified int64
	}
	files := []cachedFile{}
	root, e := s.m.Repository().SharedRulesDir()
	if e != nil {
		return comparison{}, "", e
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			files = append(files, cachedFile{path, info.Size(), info.ModTime().UnixNano()})
		}
		return nil
	})
	if e = ctx.Err(); e != nil {
		return comparison{}, "", e
	}
	raw, e := json.Marshal(struct {
		Head                      store.Head
		Stamp                     deploy.Stamp
		Applied                   *model.AppliedContent
		Profile, Service, Version string
		Files                     []cachedFile
		Settings                  Settings
	}{head, stamp, a.AppliedContent, a.ProfileID, a.ServiceType, a.ConfigVersion, files, settings})
	if e != nil {
		return comparison{}, "", e
	}
	inputs := deploy.CanonicalDigest(raw)
	s.mu.Lock()
	entry, ok := s.comparisons[a.ID]
	s.mu.Unlock()
	now := s.now()
	if !ok || now.Before(entry.At) || now.Sub(entry.At) >= RecheckAfter {
		entry = comparison{At: now}
	}
	return entry, inputs, nil
}
func (s *Service) remember(id string, entry comparison) {
	s.mu.Lock()
	s.comparisons[id] = entry
	s.mu.Unlock()
}
func identity(a *model.Agent) string {
	type targetIdentity struct {
		ID, ProfileID, Service, Host string
		Port                         model.Int
		Token, Version, Deployment   string
	}
	deployment := ""
	if a.AppliedContent != nil {
		deployment = a.AppliedContent.DeploymentID
	}
	raw, _ := json.Marshal(targetIdentity{a.ID, a.ProfileID, a.ServiceType, a.Host, a.Port, a.Token, a.ConfigVersion, deployment})
	return deploy.CanonicalDigest(raw)
}
func (s *Service) unchanged(ctx context.Context, record *agents.Record, settings Settings, deploymentID string) (bool, error) {
	old, e := decodeAgent(record)
	if e != nil {
		return false, e
	}
	latest, e := s.m.GetAgentByID(ctx, old.ID, false)
	if e != nil || latest == nil {
		return false, e
	}
	a, e := decodeAgent(latest)
	if e != nil {
		return false, e
	}
	if identity(a) != identity(old) || SettingsFor(latest) != settings {
		return false, nil
	}
	task := a.LatestDeployment
	return task == nil || finished(task.Status) || task.DeploymentID == deploymentID, nil
}
func (w *worker) run(ctx context.Context, id string) error {
	s := w.s
	record, e := workerAgent(ctx, s.m, id)
	if e != nil || record == nil {
		return e
	}
	a, e := decodeAgent(record)
	if e != nil {
		return e
	}
	if a.ServiceType != "mihomo" && a.ServiceType != "oxidns" {
		return nil
	}
	if agents.HAActive(record) {
		return advancePair(ctx, s.m, id)
	}
	if a.ServiceType == "oxidns" && agents.HAEnabled(record) {
		members, e := s.m.HAPair(ctx, id)
		if e != nil {
			return e
		}
		if len(members) > 0 {
			leader, e := s.leader(ctx, record)
			if e != nil {
				return e
			}
			if recordID(leader) != id {
				return nil
			}
		}
	}
	settings := SettingsFor(record)
	if !settings.Any() {
		return nil
	}
	old := stateOf(a)
	now := nowSeconds(s.now())
	if value(old.RetryAt) > now {
		return nil
	}
	if a.Enabled != nil && !*a.Enabled || a.Status != "online" {
		force := old.ForceRefresh
		if old.Status == "running" && force == nil {
			force = ptr(true)
		}
		return w.state(ctx, id, "pending", msgOffline, model.RuleSyncState{NativeTask: old.NativeTask, Key: old.Key, ForceRefresh: force})
	}
	supported := agents.CompareVersions(a.Version, MinAgentVersion)
	if supported < 0 {
		return w.state(ctx, id, "blocked", fmt.Sprintf(msgUpgrade, MinAgentVersion), model.RuleSyncState{})
	}
	task := a.LatestDeployment
	if task != nil && (!finished(task.Status) || task.HAPublication != nil) {
		task, e = pollDeployment(ctx, s.m, id, task.DeploymentID)
		if e != nil {
			return e
		}
		if !finished(task.Status) {
			status := "pending"
			if value(old.DeploymentID) == task.DeploymentID {
				status = "running"
			}
			return w.state(ctx, id, status, msgPublishWaiting, model.RuleSyncState{DeploymentID: old.DeploymentID})
		}
	}
	if task != nil && value(old.DeploymentID) == task.DeploymentID {
		if task.Status != "succeeded" {
			return w.state(ctx, id, "failed", msgReloadFailed, model.RuleSyncState{RetryAt: ptr(nowSeconds(s.now().Add(RetryAfter)))})
		}
		if e = w.state(ctx, id, "succeeded", msgReloadSuccess, model.RuleSyncState{}); e != nil {
			return e
		}
		record, e = s.m.GetAgentByID(ctx, id, false)
		if e != nil {
			return e
		}
		a, e = decodeAgent(record)
		if e != nil {
			return e
		}
		old = stateOf(a)
	}
	resuming := old.NativeTask != nil && *old.NativeTask != "" || settings.Library && (value(old.ForceRefresh) || old.Status == "running")
	remembered, inputs, e := s.comparison(ctx, record, settings)
	if e != nil {
		return e
	}
	if remembered.Inputs == inputs && remembered.Unsafe {
		return w.state(ctx, id, "blocked", msgUnsafe, model.RuleSyncState{})
	}
	var kind Kind
	var source *deploy.Source
	var withheld []string
	if remembered.Inputs == inputs && remembered.Outcome == None && !resuming {
		kind = None
		withheld = remembered.Withheld
	} else {
		baseline, e := s.receipts.Load(ctx, record, false)
		if e != nil {
			return e
		}
		if baseline == nil {
			return w.state(ctx, id, "blocked", msgBaseline, model.RuleSyncState{})
		}
		base, e := deploy.SourceFromJSON(baseline.Source, nil)
		if e != nil {
			return e
		}
		current, e := publishSource(ctx, s.m, a.ProfileID)
		if e != nil {
			return e
		}
		kind, source, withheld, e = RuleChanges(base, current, a.ServiceType, settings)
		remembered.Inputs = inputs
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			remembered.Unsafe = true
			s.remember(id, remembered)
			return w.state(ctx, id, "blocked", msgUnsafe, model.RuleSyncState{})
		}
		remembered.Unsafe = false
		remembered.Outcome = ""
		if kind == None {
			remembered.Outcome = None
		}
		remembered.Withheld = withheld
		s.remember(id, remembered)
	}
	if len(withheld) > 0 && a.ServiceType == "oxidns" {
		w.notice = PinnedWithheldNotice
	}
	if kind == None && !resuming {
		if old.Status != "succeeded" || w.notice != "" && !stringsContains(old.Message, w.notice) {
			return w.state(ctx, id, "idle", msgIdle, model.RuleSyncState{})
		}
		return nil
	}
	if kind == None {
		kind = Content
	}
	if source == nil {
		return refuse(msgSnapshotVerify)
	}
	sourceRaw, e := source.Raw()
	if e != nil {
		return e
	}
	deployment := ""
	if a.AppliedContent != nil {
		deployment = a.AppliedContent.DeploymentID
	}
	keyRaw, e := json.Marshal([]json.RawMessage{sourceRaw, mustSettings(settings), mustString(deployment)})
	if e != nil {
		return e
	}
	key := deploy.CanonicalDigest(keyRaw)
	if value(old.NativeTask) != "" && a.ServiceType == "oxidns" {
		return w.resumeNative(ctx, record, a, settings, old, source, key)
	}
	if value(old.Key) != key {
		return w.state(ctx, id, "pending", msgDebounce, model.RuleSyncState{Key: &key, NoticedAt: &now, ForceRefresh: ptr(value(old.ForceRefresh) || old.Status == "running")})
	}
	if now-value(old.NoticedAt) < PollInterval.Seconds() {
		return nil
	}
	receipt, e := s.receipts.Load(ctx, record, true)
	if e != nil {
		return e
	}
	if receipt == nil {
		return refuse(msgSnapshotVerify)
	}
	baseline := Baseline{Receipt: receipt, Bundle: receipt.Bundle}
	if kind == Content {
		return w.refresh(ctx, record, a, settings, baseline, source, key)
	}
	return w.publish(ctx, record, a, settings, baseline, source, key)
}
func mustSettings(s Settings) json.RawMessage { raw, _ := json.Marshal(s); return raw }
func mustString(s string) json.RawMessage     { raw, _ := json.Marshal(s); return raw }
func stringsContains(text, part string) bool  { return strings.Contains(text, part) }
func (w *worker) resumeNative(ctx context.Context, record *agents.Record, a *model.Agent, settings Settings, old *model.RuleSyncState, source *deploy.Source, key string) error {
	s := w.s
	task, e := nativeCall(ctx, s.m, a.ID, "GET", nil)
	if e != nil {
		return e
	}
	if task.ID == value(old.NativeTask) && task.Status == "running" {
		return nil
	}
	receipt, e := s.receipts.Load(ctx, record, true)
	if e != nil {
		return e
	}
	if receipt == nil {
		return refuse(msgSnapshotVerify)
	}
	baseline := Baseline{receipt, receipt.Bundle}
	if !settings.Library {
		snapshot, e := FetchSnapshot(ctx, s.m, record, baseline)
		if e != nil {
			return e
		}
		same, e := s.unchanged(ctx, record, settings, "")
		if e != nil || !same {
			return e
		}
		previous, e := deploy.SourceFromJSON(receipt.Source, nil)
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if _, e = s.receipts.AdvanceContent(ctx, s.m, record, previous, snapshot); e != nil {
			return e
		}
		return w.state(ctx, a.ID, "idle", msgNativeEnded, model.RuleSyncState{})
	}
	same, e := s.unchanged(ctx, record, settings, "")
	if e != nil {
		return e
	}
	if task.ID == value(old.NativeTask) && task.Status == "succeeded" && key == value(old.Key) && same {
		snapshot, e := FetchSnapshot(ctx, s.m, record, baseline)
		if e != nil {
			return e
		}
		same, e = s.unchanged(ctx, record, settings, "")
		if e != nil || !same {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		advanced, e := s.receipts.AdvanceContent(ctx, s.m, record, source, snapshot)
		if e != nil {
			return e
		}
		if !advanced {
			return w.state(ctx, a.ID, "pending", msgDeviceChanged, model.RuleSyncState{})
		}
		return w.state(ctx, a.ID, "succeeded", msgDownloaded, model.RuleSyncState{})
	}
	if task.ID == value(old.NativeTask) && task.Status == "failed" {
		message := task.Message
		if message == "" {
			message = msgNativeFailed
		}
		return w.state(ctx, a.ID, "failed", message, model.RuleSyncState{ForceRefresh: ptr(true), RetryAt: ptr(nowSeconds(s.now().Add(RetryAfter)))})
	}
	return w.state(ctx, a.ID, "pending", msgContinue, model.RuleSyncState{Key: &key, NoticedAt: ptr(nowSeconds(s.now())), ForceRefresh: ptr(true)})
}
func newDeploymentID() (string, error) {
	var raw [16]byte
	if _, e := rand.Read(raw[:]); e != nil {
		return "", fmt.Errorf("generate rule deployment ID: %w", e)
	}
	return hex.EncodeToString(raw[:]), nil
}
func (w *worker) refresh(ctx context.Context, record *agents.Record, a *model.Agent, settings Settings, baseline Baseline, source *deploy.Source, key string) error {
	s := w.s
	if a.ServiceType == "oxidns" && agents.HAEnabled(record) {
		return w.publish(ctx, record, a, settings, baseline, source, key)
	}
	same, e := s.unchanged(ctx, record, settings, "")
	if e != nil || !same {
		return e
	}
	if e = w.state(ctx, a.ID, "running", msgDownloading, model.RuleSyncState{Key: &key}); e != nil {
		return e
	}
	if a.ServiceType == "mihomo" {
		files, e := ReadBundleFiles(baseline.Bundle)
		if e != nil {
			return e
		}
		main, e := parseMapping(files[baseline.Bundle.Manifest().ConfigPath].Data)
		if e != nil {
			return e
		}
		providers := get(main, "rule-providers")
		names := keys(providers)
		if len(names) == 0 {
			return w.publish(ctx, record, a, settings, baseline, source, key)
		}
		for _, name := range names {
			if text(get(get(providers, name), "type")) != "http" {
				return w.publish(ctx, record, a, settings, baseline, source, key)
			}
		}
		base, e := deploy.SourceFromJSON(baseline.Receipt.Source, nil)
		if e != nil {
			return e
		}
		entries := MihomoRuleEntries(main)
		unchanged := UnchangedRulePaths(entries, entries, base, source, baseline.Receipt.BaseURL)
		changed := []string{}
		for _, entry := range entries {
			path, e := deploy.RelativePath(entry.LocalPath)
			if e != nil {
				return e
			}
			if !unchanged[path] {
				changed = append(changed, entry.Name)
			}
		}
		if len(changed) == 0 {
			changed = names
		}
		for _, name := range changed {
			same, e = s.unchanged(ctx, record, settings, "")
			if e != nil || !same {
				return e
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			if e = refreshProvider(ctx, s.m, a.ID, name); e != nil {
				return e
			}
		}
	} else {
		existing, e := nativeCall(ctx, s.m, a.ID, "GET", nil)
		if e != nil {
			return e
		}
		if existing.Status == "running" {
			return w.state(ctx, a.ID, "pending", msgExisting, model.RuleSyncState{Key: &key, NoticedAt: ptr(nowSeconds(s.now().Add(-PollInterval)))})
		}
		var paths []string
		if monitor.SupportsPaths(record) {
			base, e := deploy.SourceFromJSON(baseline.Receipt.Source, nil)
			if e != nil {
				return e
			}
			oldEntries, nextEntries, e := candidateEntries("oxidns", nil, nil, base, source, baseline.Receipt.BaseURL)
			if e == nil {
				unchanged := UnchangedRulePaths(oldEntries, nextEntries, base, source, baseline.Receipt.BaseURL)
				selected := map[string]bool{}
				for _, entry := range nextEntries {
					path, e := deploy.RelativePath(entry.LocalPath)
					if e != nil {
						continue
					}
					if !unchanged[path] {
						selected[path] = true
					}
				}
				for path := range selected {
					paths = append(paths, path)
				}
				slices.Sort(paths)
			} else {
				logger.Infof("Rule sync file selection unavailable (%T)", e)
			}
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		same, e = s.unchanged(ctx, record, settings, "")
		if e != nil || !same {
			return e
		}
		task, e := nativeCall(ctx, s.m, a.ID, "POST", paths)
		var nativeErr *monitor.Error
		if len(paths) > 0 && errors.As(e, &nativeErr) && nativeErr.Code == "RULE_UPDATE_TARGET_UNKNOWN" {
			paths = nil
			if e = ctx.Err(); e != nil {
				return e
			}
			same, e = s.unchanged(ctx, record, settings, "")
			if e != nil || !same {
				return e
			}
			task, e = nativeCall(ctx, s.m, a.ID, "POST", nil)
		}
		if e != nil {
			return e
		}
		if task.Status == "running" {
			message := msgDownloading
			if len(paths) > 0 {
				message = fmt.Sprintf(msgChangedFiles, len(paths))
			}
			return w.state(ctx, a.ID, "running", message, model.RuleSyncState{Key: &key, NativeTask: &task.ID, NoticedAt: ptr(nowSeconds(s.now().Add(-PollInterval)))})
		}
		if task.Status != "succeeded" {
			message := task.Message
			if message == "" {
				message = msgUnconfirmed
			}
			return w.state(ctx, a.ID, "failed", message, model.RuleSyncState{RetryAt: ptr(nowSeconds(s.now().Add(RetryAfter)))})
		}
	}
	same, e = s.unchanged(ctx, record, settings, "")
	if e != nil || !same {
		return e
	}
	current, e := publishSource(ctx, s.m, a.ProfileID)
	if e != nil {
		return e
	}
	base, e := deploy.SourceFromJSON(baseline.Receipt.Source, nil)
	if e != nil {
		return e
	}
	projected, e := ProjectRuleChanges(base, current, settings, a.ServiceType, nil)
	if e != nil {
		return e
	}
	before, e := source.Raw()
	if e != nil {
		return e
	}
	after, e := projected.Raw()
	if e != nil {
		return e
	}
	if !bytesEqualJSON(before, after) {
		return w.state(ctx, a.ID, "pending", msgChangedDuring, model.RuleSyncState{})
	}
	snapshot, e := FetchSnapshot(ctx, s.m, record, baseline)
	if e != nil {
		return e
	}
	same, e = s.unchanged(ctx, record, settings, "")
	if e != nil || !same {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	advanced, e := s.receipts.AdvanceContent(ctx, s.m, record, source, snapshot)
	if e != nil {
		return e
	}
	if !advanced {
		return w.state(ctx, a.ID, "pending", msgDeviceChanged, model.RuleSyncState{})
	}
	return w.state(ctx, a.ID, "succeeded", msgDownloaded, model.RuleSyncState{})
}
func (w *worker) publish(ctx context.Context, record *agents.Record, a *model.Agent, settings Settings, baseline Baseline, source *deploy.Source, key string) (err error) {
	s := w.s
	id, e := newDeploymentID()
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	accepted, e := beginPublication(ctx, s.m, a.ID, id)
	if e != nil {
		return e
	}
	if !accepted {
		return w.state(ctx, a.ID, "pending", msgBusy, model.RuleSyncState{})
	}
	defer s.m.FinishDeploymentPreparation(a.ID, id)
	defer func() {
		if err != nil && ctx.Err() == nil {
			if e := failUnsubmitted(ctx, s.m, a.ID, id); e != nil {
				logger.Warningf("Rule sync preparation failure record unavailable (%T)", e)
			}
		}
	}()
	if e = w.state(ctx, a.ID, "running", msgPreparing, model.RuleSyncState{DeploymentID: &id, Key: &key}); e != nil {
		return e
	}
	baseline.Bundle, e = FetchSnapshot(ctx, s.m, record, baseline)
	if e != nil {
		return e
	}
	old := stateOf(a)
	bundle, e := s.prepare(ctx, deploy.NewTarget(record), source, baseline, settings, id, value(old.ForceRefresh))
	if e != nil {
		return e
	}
	same, e := s.unchanged(ctx, record, settings, id)
	if e != nil {
		return e
	}
	if !same {
		return refuse(msgCandidateFailed)
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	result, e := deploy.Publish(ctx, s.m, a.ID, bundle, deploy.PublishOptions{Activate: true, BaseURL: baseline.Receipt.BaseURL, ResourceConfig: source, SourceSnapshot: source})
	if e != nil {
		return e
	}
	switch resultStatus(result) {
	case "succeeded":
		return w.state(ctx, a.ID, "succeeded", msgReloadSuccess, model.RuleSyncState{})
	case "failed", "rolled_back", "rollback_failed":
		return w.state(ctx, a.ID, "failed", msgReloadFailed, model.RuleSyncState{RetryAt: ptr(nowSeconds(s.now().Add(RetryAfter)))})
	}
	return nil
}
