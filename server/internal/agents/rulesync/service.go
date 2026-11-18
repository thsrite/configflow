package rulesync

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/taskstatus"
)

// Options injects a wall clock and candidate preparation for deterministic workers.
type Options struct {
	Now     func() time.Time
	Prepare PrepareFunc
}

// Service owns lightweight comparison memory and at most two concurrent workers.
type Service struct {
	m           *agents.Manager
	receipts    *deploy.Receipts
	now         func() time.Time
	prepare     PrepareFunc
	mu          sync.Mutex
	comparisons map[string]comparison
	pending     map[string]bool
	cursor      int
}

// New builds a rule-sync owner for one manager.
func New(m *agents.Manager, r *deploy.Receipts, o Options) *Service {
	if r == nil {
		r = deploy.NewReceipts(m.Repository().DataDir())
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Prepare == nil {
		o.Prepare = PrepareCandidate
	}
	return &Service{m: m, receipts: r, now: o.Now, prepare: o.Prepare, comparisons: map[string]comparison{}, pending: map[string]bool{}}
}

// PublicState intentionally exposes no task identity, token, keys or snapshots.
type PublicState struct {
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// StatusView is the independent device rule-sync editing contract.
type StatusView struct {
	Success       bool        `json:"success"`
	Settings      Settings    `json:"settings"`
	State         PublicState `json:"state"`
	BaselineReady bool        `json:"baseline_ready"`
}

func (s *Service) leader(ctx context.Context, record *agents.Record) (*agents.Record, error) {
	a, e := decodeAgent(record)
	if e != nil {
		return nil, e
	}
	if a.ServiceType != "oxidns" || !agents.HAEnabled(record) {
		return record, nil
	}
	members, e := s.m.HAPair(ctx, a.ID)
	if errors.Is(e, agents.ErrPairIncomplete) {
		return record, nil
	}
	if e != nil {
		return nil, e
	}
	eligible := []*agents.Record{}
	for _, member := range members {
		if SettingsFor(member).Any() {
			eligible = append(eligible, member)
		}
	}
	slices.SortFunc(eligible, func(a, b *agents.Record) int { return compareLeader(a, b) })
	if len(eligible) > 0 {
		return eligible[0], nil
	}
	return record, nil
}

// Status resolves a device's own profile and never trusts the active-profile header.
func (s *Service) Status(ctx context.Context, id string) (StatusView, error) {
	record, e := s.m.GetAgentByID(ctx, id, false)
	if e != nil {
		return StatusView{}, e
	}
	if record == nil {
		return StatusView{}, ErrAgentNotFound
	}
	a, e := decodeAgent(record)
	if e != nil {
		return StatusView{}, e
	}
	if a.ServiceType != "mihomo" && a.ServiceType != "oxidns" {
		return StatusView{}, refuse(msgService)
	}
	settings := SettingsFor(record)
	leader, e := s.leader(ctx, record)
	if e != nil {
		return StatusView{}, e
	}
	la, e := decodeAgent(leader)
	if e != nil {
		return StatusView{}, e
	}
	view := PublicState{}
	if la.RuleSync != nil && la.RuleSync.State != nil {
		state := la.RuleSync.State
		view = PublicState{Status: state.Status, Message: safeText(ctx, s.m, leader, state.Message), UpdatedAt: state.UpdatedAt}
	}
	if !settings.Any() {
		view = PublicState{Status: "disabled", Message: msgDisabled}
	} else if view.Status == "" && view.Message == "" && view.UpdatedAt == "" {
		view = PublicState{Status: "pending", Message: msgWaiting}
	}
	baseline, e := s.receipts.Load(ctx, record, false)
	return StatusView{Success: true, Settings: settings, State: view, BaselineReady: baseline != nil}, e
}

// Configure atomically replaces selected scopes, on both members of a saved pair.
func (s *Service) Configure(ctx context.Context, id string, body json.RawMessage) (view StatusView, err error) {
	err = deploy.RunDetached(ctx, "rule-sync-configure-"+id, func(ctx context.Context) error {
		if _, e := s.Status(ctx, id); e != nil {
			return e
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || len(fields) != 2 {
			return refuse(msgSettings)
		}
		for _, key := range []string{"library", "rules"} {
			v, ok := fields[key]
			if !ok || string(v) != "true" && string(v) != "false" {
				return refuse(msgSettings)
			}
		}
		var settings Settings
		if e := json.Unmarshal(body, &settings); e != nil {
			return refuse(msgSettings)
		}
		record, e := s.m.GetAgentByID(ctx, id, false)
		if e != nil {
			return e
		}
		if pairEnabling(record) {
			return refuse(msgPairEnabling)
		}
		pair, e := s.m.HAPair(ctx, id)
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if len(pair) > 0 {
			e = configurePair(ctx, s.m, pair, settings)
		} else {
			e = persistSync(ctx, s.m, id, &settings, nil)
		}
		if e != nil {
			return e
		}
		view, e = s.Status(ctx, id)
		return e
	})
	return
}

var workerLocks = lockx.NewKeyed[string](lockx.LevelJob, "agent-rule-sync")

// Run executes one non-overlapping worker and contains failures at its owner boundary.
func (s *Service) Run(ctx context.Context, id string) error {
	return deploy.RunDetached(ctx, "agent-rule-sync-"+id, func(ctx context.Context) (err error) {
		root, e := filepath.EvalSymlinks(s.m.Repository().DataDir())
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		held, release, ok := workerLocks.TryLock(ctx, root+"\x00"+id)
		if !ok {
			return ctx.Err()
		}
		defer release()
		w := worker{s: s}
		defer func() {
			if p := recover(); p != nil {
				logger.Warning("Agent rule sync worker panicked")
				err = errors.New("rule sync worker panicked")
			}
			if err != nil && held.Err() == nil {
				logger.Warningf("Agent rule sync failed (%T)", err)
				stateError := w.failure(held, id, err)
				if stateError != nil {
					err = stateError
				}
			}
		}()
		return w.run(held, id)
	})
}

// Round observes presence and admits at most two tracked workers in round-robin order.
func (s *Service) Round(ctx context.Context) error {
	records, e := s.m.GetAllAgents(ctx, false)
	if e != nil {
		return e
	}
	observePresence(ctx, s.m, records)
	s.mu.Lock()
	for id, running := range s.pending {
		if !running {
			delete(s.pending, id)
		}
	}
	ids := map[string]bool{}
	for _, r := range records {
		ids[recordID(r)] = true
	}
	for id := range s.comparisons {
		if !ids[id] {
			delete(s.comparisons, id)
		}
	}
	n := len(records)
	if n == 0 {
		s.cursor = 0
		s.mu.Unlock()
		return nil
	}
	s.cursor %= n
	ordered := append(append([]*agents.Record{}, records[s.cursor:]...), records[:s.cursor]...)
	s.mu.Unlock()
	for _, r := range ordered {
		id := recordID(r)
		s.mu.Lock()
		if len(s.pending) >= 2 {
			s.mu.Unlock()
			break
		}
		s.cursor = (s.cursor + 1) % n
		eligible := !s.pending[id] && recordSupported(r) && SettingsFor(r).Any()
		if eligible {
			s.pending[id] = true
		}
		s.mu.Unlock()
		if !eligible {
			continue
		}
		if e = ctx.Err(); e != nil {
			s.mu.Lock()
			delete(s.pending, id)
			s.mu.Unlock()
			return e
		}
		e = lifecycle.Spawn(ctx, "agent-rule-sync-"+id, func(job context.Context) {
			defer func() { s.mu.Lock(); s.pending[id] = false; s.mu.Unlock() }()
			_ = s.Run(job, id)
		})
		if e != nil {
			s.mu.Lock()
			delete(s.pending, id)
			s.mu.Unlock()
			return e
		}
	}
	return nil
}
func stateOf(a *model.Agent) *model.RuleSyncState {
	if a != nil && a.RuleSync != nil && a.RuleSync.State != nil {
		return a.RuleSync.State
	}
	return &model.RuleSyncState{}
}
func ptr[T any](v T) *T { return &v }
func value[T any](p *T) T {
	if p != nil {
		return *p
	}
	var zero T
	return zero
}
func nowSeconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }
func (s *Service) saveState(ctx context.Context, id string, next model.RuleSyncState) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	record, e := s.m.GetAgentByID(ctx, id, false)
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
	previous := stateOf(a)
	// Compare the complete state excluding the timestamp, including unknown state
	// members. Replacement deliberately removes old per-run fields.
	before, e := stateWithoutTime(previous, false)
	if e != nil {
		return e
	}
	after, e := stateWithoutTime(&next, true)
	if e != nil {
		return e
	}
	if bytesEqualJSON(before, after) {
		return nil
	}
	next.Message = safeText(ctx, s.m, record, next.Message)
	next.UpdatedAt = s.now().UTC().Format("2006-01-02T15:04:05Z")
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = persistSync(ctx, s.m, id, nil, &next); e != nil {
		return e
	}
	if previous.Status != next.Status || previous.Message != next.Message {
		logState(ctx, s.m, record, next.Status, next.Message)
	}
	if (next.Status == "succeeded" || next.Status == "failed") && previous.Status != next.Status {
		if e = ctx.Err(); e != nil {
			return e
		}
		_, e = notifications.TaskStatus(ctx, s.m.Repository().DataDir()).RecordContext(ctx, "agent_rule_sync:"+id, taskstatus.Result{OK: next.Status == "succeeded", Trigger: "event", Error: next.Message})
	}
	return e
}
func stateWithoutTime(state *model.RuleSyncState, replacement bool) (json.RawMessage, error) {
	raw, e := json.Marshal(state)
	if replacement {
		raw, e = encodeState(state)
	}
	if e != nil {
		return nil, e
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(raw, &fields); e != nil {
		return nil, e
	}
	delete(fields, "updated_at")
	return json.Marshal(fields)
}
func bytesEqualJSON(a, b []byte) bool { return deploy.CanonicalDigest(a) == deploy.CanonicalDigest(b) }

func encodeState(state *model.RuleSyncState) ([]byte, error) {
	type wire struct {
		Status       string   `json:"status"`
		Message      string   `json:"message"`
		UpdatedAt    string   `json:"updated_at,omitempty"`
		DeploymentID *string  `json:"deployment_id,omitempty"`
		Key          *string  `json:"key,omitempty"`
		NoticedAt    *float64 `json:"noticed_at,omitempty"`
		RetryAt      *float64 `json:"retry_at,omitempty"`
		ForceRefresh *bool    `json:"force_refresh,omitempty"`
		NativeTask   *string  `json:"native_task,omitempty"`
	}
	return json.Marshal(wire{state.Status, state.Message, state.UpdatedAt, state.DeploymentID, state.Key, state.NoticedAt, state.RetryAt, state.ForceRefresh, state.NativeTask})
}
