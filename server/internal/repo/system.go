package repo

import (
	"configflow/internal/ctxx"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"configflow/internal/jsonx"
	"configflow/internal/store"
)

// Updater edits a deep copy of a section inside a write transaction. It may
// change current in place and return nil, or return a replacement object.
// ctx belongs to the transaction: repository reads made with it see the
// working copy, writes made with it fail with ErrInUse. A non-nil error
// aborts the transaction and is returned by the transaction method unchanged.
type Updater func(ctx context.Context, current *jsonx.Object) (replacement *jsonx.Object, err error)

// applyUpdater is _apply_updater.
func applyUpdater(ctx context.Context, current *jsonx.Object, updater Updater) (*jsonx.Object, error) {
	if updater == nil {
		return nil, validationError("Updater must be callable")
	}
	replacement, err := updater(ctx, current)
	if err != nil {
		return nil, err
	}
	if replacement != nil {
		return replacement.DeepCopy(), nil
	}
	return current, nil
}

// randomHex is uuid.uuid4().hex[:length]. It is a variable so tests can
// make the generated ids deterministic (Python: a patched uuid.uuid4).
var randomHex = func(length int) string {
	raw := make([]byte, (length+1)/2)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)[:length]
}

// tokenURLSafe is secrets.token_urlsafe(32). It is a variable so tests can
// fix the generated token (Python: monkeypatched secrets.token_urlsafe).
var tokenURLSafe = func() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// ensureRuleProxyToken is _ensure_rule_proxy_token: it normalises the retired
// token list and creates (or, when it equals config_token, rotates) the rule
// proxy token. It reports whether system changed.
func ensureRuleProxyToken(system *jsonx.Object) (bool, error) {
	settings, err := compatObject(system.SetDefault("system_config", jsonx.NewObject()))
	if err != nil {
		return false, err
	}
	retired := settings.Value("retired_rule_proxy_tokens")
	normalized := []any{}
	contains := func(token string) bool {
		for _, value := range normalized {
			if value == token {
				return true
			}
		}
		return false
	}
	if values, ok := jsonx.AsList(retired); ok {
		for _, value := range values {
			if text, ok := value.(string); ok && text != "" && !contains(text) {
				normalized = append(normalized, text)
			}
		}
	}
	changed := false
	if _, isList := jsonx.AsList(retired); !isList || !jsonx.Equal(retired, normalized) {
		settings.Set("retired_rule_proxy_tokens", normalized)
		changed = true
	}
	configToken := settings.Value("config_token")
	if token, ok := settings.Value("rule_proxy_token").(string); ok && token != "" {
		if !jsonx.Equal(token, configToken) {
			return changed, nil
		}
		if !contains(token) {
			normalized = append(normalized, token)
			settings.Set("retired_rule_proxy_tokens", normalized)
		}
	}
	for {
		token := tokenURLSafe()
		if !jsonx.Equal(token, configToken) {
			settings.Set("rule_proxy_token", token)
			return true, nil
		}
	}
}

// TokenSet is a read-only set of secrets; never modify one.
type TokenSet map[string]struct{}

// Has reports membership.
func (s TokenSet) Has(token string) bool { _, ok := s[token]; return ok }

type sanitizationTokens struct {
	head      store.Head
	ruleProxy TokenSet
	agents    TokenSet
}

// SanitizationTokensOf is _sanitization_tokens_of(system): the rule proxy
// tokens (current and retired) and the Agent tokens of a system section. It
// only reads.
func SanitizationTokensOf(system *jsonx.Object) (ruleProxy, agents TokenSet) {
	ruleProxy, agents = TokenSet{}, TokenSet{}
	if settings, ok := jsonx.AsObject(system.GetOr("system_config", jsonx.NewObject())); ok {
		if token, ok := settings.Value("rule_proxy_token").(string); ok && token != "" {
			ruleProxy[token] = struct{}{}
		}
		if retired, ok := jsonx.AsList(settings.GetOr("retired_rule_proxy_tokens", []any{})); ok {
			for _, value := range retired {
				if token, ok := value.(string); ok && token != "" {
					ruleProxy[token] = struct{}{}
				}
			}
		}
	}
	if items, ok := jsonx.AsList(system.GetOr("agents", []any{})); ok {
		for _, item := range items {
			if agent, ok := jsonx.AsObject(item); ok {
				if token, ok := agent.Value("token").(string); ok && token != "" {
					agents[token] = struct{}{}
				}
			}
		}
	}
	return ruleProxy, agents
}

// sanitizationTokensNow is _sanitization_tokens: the tokens of the current
// generation, remembered by store head. Inside a transaction the working
// copy is read and nothing is cached.
func (r *Repository) sanitizationTokensNow(ctx context.Context) (TokenSet, TokenSet, error) {
	if document := r.working(ctx); document != nil {
		ruleProxy, agents := SanitizationTokensOf(document.GetObject("system"))
		return ruleProxy, agents, nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if cached := r.tokensCache.Load(); cached != nil && cached.head == entry.Head {
		return cached.ruleProxy, cached.agents, nil
	}
	ruleProxy, agents := SanitizationTokensOf(entry.Document.GetObject("system"))
	r.tokensCache.Store(&sanitizationTokens{head: entry.Head, ruleProxy: ruleProxy, agents: agents})
	return ruleProxy, agents, nil
}

// RuleProxyTokensForSanitization is rule_proxy_tokens_for_sanitization: the
// persisted current and retired rule proxy tokens, for output sanitization
// only. The set is shared: do not modify it.
func (r *Repository) RuleProxyTokensForSanitization(ctx context.Context) (TokenSet, error) {
	ruleProxy, _, err := r.sanitizationTokensNow(ctx)
	return ruleProxy, err
}

// AgentTokensForSanitization is agent_tokens_for_sanitization: the persisted
// Agent credentials, for output sanitization only. Do not modify the set.
func (r *Repository) AgentTokensForSanitization(ctx context.Context) (TokenSet, error) {
	_, agents, err := r.sanitizationTokensNow(ctx)
	return agents, err
}

// GetSystem is get_system(): a copy of the system section (system_config,
// backup, agents, _revision).
func (r *Repository) GetSystem(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = document.GetObject("system").DeepCopy()
		return nil
	})
	return result, err
}

type agentHistoryNotRead struct{}

// AgentHistoryNotRead stands in for the AgentHistoryFields of an Agent
// record read with GetAgents(ctx, false) (Python: AGENT_HISTORY_NOT_READ).
// It is not a jsonx value: the encoders refuse it. Unlike Python, the typed
// jsonx getters cannot refuse it and return zero values, so code that may
// receive such records must read history fields through AgentHistory.
var AgentHistoryNotRead any = &agentHistoryNotRead{}

// AgentHistoryNotReadMessage is the RuntimeError of the Python sentinel.
const AgentHistoryNotReadMessage = "Agent 记录是不带发布历史读取的，需要历史的代码必须用 history=True 读取"

// AgentHistory returns a history field (one of AgentHistoryFields) of an
// Agent record, or an error when the record was read without its history.
func AgentHistory(agent *jsonx.Object, field string) (any, error) {
	value := agent.Value(field)
	if value == AgentHistoryNotRead {
		return nil, helperError("RuntimeError", AgentHistoryNotReadMessage)
	}
	return value, nil
}

// GetAgents is get_agents(history=...): copies of the Agent records. With
// history=false the AgentHistoryFields, which outweigh the rest of a record
// many times over, are not copied and hold AgentHistoryNotRead.
func (r *Repository) GetAgents(ctx context.Context, history bool) (result []any, err error) {
	return r.GetAgentsInspect(ctx, history, nil)
}

// GetAgentsInspect calls inspect on each raw record in the same read view,
// before omitting history or copying. inspect must neither mutate nor retain
// the record. This keeps credential snapshots aligned with light reads without
// copying their history. N8-5 must preserve this inspection capability.
func (r *Repository) GetAgentsInspect(ctx context.Context, history bool, inspect func(*jsonx.Object)) (result []any, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		agents := document.GetObject("system").GetList("agents")
		if inspect != nil {
			for _, item := range agents {
				agent, _ := jsonx.AsObject(item)
				inspect(agent)
			}
		}
		if history {
			result, _ = jsonx.DeepCopy(agents).([]any)
			if result == nil {
				result = []any{}
			}
			return nil
		}
		result = make([]any, 0, len(agents))
		for _, item := range agents {
			agent, _ := jsonx.AsObject(item)
			record := jsonx.NewObject()
			for key, value := range agent.All() {
				if key != "deployments" && key != "ipv6_verifications" {
					record.Set(key, jsonx.DeepCopy(value))
				}
			}
			for _, field := range AgentHistoryFields {
				record.Set(field, AgentHistoryNotRead)
			}
			result = append(result, record)
		}
		return nil
	})
	return result, err
}

// GetSystemSettings is get_system_settings(): system_config alone.
func (r *Repository) GetSystemSettings(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = document.GetObject("system").GetObject("system_config").DeepCopy()
		return nil
	})
	return result, err
}

// GetSystemWithSource is get_system_with_source(): a copy of the system
// section and the committed section it was read from (nil inside a
// transaction). The source is shared and must never be modified.
func (r *Repository) GetSystemWithSource(ctx context.Context) (result, source *jsonx.Object, err error) {
	document, committed, err := r.committedSource(ctx)
	if err != nil {
		return nil, nil, err
	}
	system := document.GetObject("system")
	if committed {
		source = system
	}
	return system.DeepCopy(), source, nil
}

// UpdateSystemTransaction is update_system_transaction: updater edits a
// copy of the system section; system_config, backup and agents are taken
// from its result and the revision advances by one. It returns a copy of
// the committed section.
func (r *Repository) UpdateSystemTransaction(ctx context.Context, updater Updater) (result *jsonx.Object, err error) {
	ctx = ctxx.Detach(ctx)
	err = r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		current := document.GetObject("system")
		updated, err := applyUpdater(ctx, current.DeepCopy(), updater)
		if err != nil {
			return err
		}
		system := jsonx.NewObject()
		for _, key := range systemSections {
			system.Set(key, jsonx.DeepCopy(updated.GetOr(key, current.Value(key))))
		}
		system.Set("_revision", jsonx.Int(revisionOf(current)+1))
		document.Set("system", system)
		if _, isObject := jsonx.AsObject(system.Value("system_config")); isObject {
			if _, err := ensureRuleProxyToken(system); err != nil {
				return err
			}
		}
		result = system.DeepCopy()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// revisionConflict is the optimistic check of the save_* methods: without a
// baseline a snapshot that carries a _revision must still be current.
func revisionConflict(snapshot, current, baseline *jsonx.Object) bool {
	if baseline != nil {
		return false
	}
	revision, ok := snapshot.Get("_revision")
	return ok && !jsonx.Equal(revision, current.Value("_revision"))
}

// SaveSystem is save_system(snapshot, baseline): with a baseline the
// caller's changes are merged three-way into the current section; without
// one objects are deep-merged and other values replaced.
func (r *Repository) SaveSystem(ctx context.Context, snapshot, baseline *jsonx.Object) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	if snapshot == nil {
		return nil, validationError("System data must be an object")
	}
	return r.UpdateSystemTransaction(ctx, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if revisionConflict(snapshot, current, baseline) {
			return nil, inUse("系统设置已更新，请刷新后重试")
		}
		for _, key := range systemSections {
			value, ok := snapshot.Get(key)
			if !ok {
				continue
			}
			currentObject, isCurrentObject := jsonx.AsObject(current.Value(key))
			valueObject, isValueObject := jsonx.AsObject(value)
			if base, ok := baseline.Get(key); ok {
				merged, err := threeWayMerge(current.Value(key), base, value)
				if err != nil {
					return nil, err
				}
				current.Set(key, merged)
			} else if isCurrentObject && isValueObject {
				current.Set(key, DeepMerge(currentObject, valueObject))
			} else {
				current.Set(key, jsonx.DeepCopy(value))
			}
		}
		return nil, nil
	})
}
