package repo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"configflow/internal/ctxx"
	"configflow/internal/model"
	"configflow/internal/store"
)

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

// ensureRuleProxyToken is _ensure_rule_proxy_token on the typed settings: it
// normalises the retired token list (its non-empty strings, distinct; a
// retired token must never be forgotten, so the strings of a stored list
// that also holds other values are kept) and creates the rule proxy token,
// or rotates it when it equals config_token. retired is the stored list
// member. It touches no other member and reports whether it changed anything.
func ensureRuleProxyToken(settings *model.SystemConfig, retired json.RawMessage) (bool, error) {
	var stored []json.RawMessage
	isList := jsonKind(retired) == '[' && json.Unmarshal(retired, &stored) == nil
	normalized := []string{}
	clean := isList
	for _, item := range stored {
		token, ok := jsonString(item)
		if !ok || token == "" || slices.Contains(normalized, token) {
			clean = false
			continue
		}
		normalized = append(normalized, token)
	}
	// Members are set as stored JSON so new ones are appended in this order
	// (retired list, then token), as Python wrote them.
	set := func(member string, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return settings.SetJSON(member, data)
	}
	changed := false
	if !clean {
		if err := set("retired_rule_proxy_tokens", normalized); err != nil {
			return false, err
		}
		changed = true
	}
	if token := settings.RuleProxyToken; token != "" {
		if token != settings.ConfigToken {
			return changed, nil
		}
		if !slices.Contains(normalized, token) {
			if err := set("retired_rule_proxy_tokens", append(normalized, token)); err != nil {
				return false, err
			}
		}
	}
	for {
		if token := tokenURLSafe(); token != settings.ConfigToken {
			return true, set("rule_proxy_token", token)
		}
	}
}

// ensureSystemTokens runs ensureRuleProxyToken when system_config is stored
// as an object; a malformed one is left for write validation to refuse.
func ensureSystemTokens(system *model.System) (bool, error) {
	view, err := viewOf(system)
	if err != nil || (view.has("system_config") && view.kind("system_config") != '{') {
		return false, err
	}
	settings, err := viewOf(&system.SystemConfig)
	if err != nil {
		return false, err
	}
	retired, _ := settings.get("retired_rule_proxy_tokens")
	return ensureRuleProxyToken(&system.SystemConfig, retired)
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

// sanitizationTokensNow is _sanitization_tokens: the tokens of the current
// generation, remembered by store head. Inside a transaction the working
// copy is read and nothing is cached.
func (r *Repository) sanitizationTokensNow(ctx context.Context) (TokenSet, TokenSet, error) {
	if r.InTransaction(ctx) {
		document, _, err := r.workingModel(ctx)
		if err != nil {
			return nil, nil, err
		}
		ruleProxy, agents := sanitizationTokensModel(&document.System)
		return ruleProxy, agents, nil
	}
	if snapshot := r.lockedSnapshot(ctx); snapshot != nil {
		ruleProxy, agents := sanitizationTokensModel(&snapshot.entry.Model.System)
		return ruleProxy, agents, nil
	}
	var head store.Head
	if err := r.store.Reading(func(connection *store.Conn) error { var err error; head, err = connection.Head(); return err }); err != nil {
		if _, failure := r.readEntry(ctx, false); failure != nil {
			return nil, nil, failure
		}
		return nil, nil, err
	}
	if cached := r.tokensCache.Load(); cached != nil && cached.head == head {
		return cached.ruleProxy, cached.agents, nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	ruleProxy, agents := sanitizationTokensModel(&entry.Model.System)
	r.tokensCache.Store(&sanitizationTokens{head: entry.Head, ruleProxy: ruleProxy, agents: agents})
	return ruleProxy, agents, nil
}

type agentCredentials struct {
	head   store.Head
	values []string
}

// AgentRecordCredentials returns the credential values collect finds in each
// encoded stored Agent record (histories included) of the current generation,
// remembered by store head like the sanitization tokens. Inside a transaction
// or a held snapshot the working records are read and nothing is cached.
// collect must depend only on its input (one output redactor uses this); the
// returned slice is shared and must not be modified.
func (r *Repository) AgentRecordCredentials(ctx context.Context, collect func(json.RawMessage) []string) ([]string, error) {
	gather := func(records []model.Agent) ([]string, error) {
		var values []string
		for i := range records {
			data, err := model.Marshal(records[i])
			if err != nil {
				return nil, err
			}
			values = append(values, collect(data)...)
		}
		return values, nil
	}
	if r.InTransaction(ctx) {
		document, _, err := r.workingModel(ctx)
		if err != nil {
			return nil, err
		}
		return gather(document.System.Agents)
	}
	if snapshot := r.lockedSnapshot(ctx); snapshot != nil {
		return gather(snapshot.entry.Model.System.Agents)
	}
	var head store.Head
	if err := r.store.Reading(func(connection *store.Conn) error { var err error; head, err = connection.Head(); return err }); err != nil {
		if _, failure := r.readEntry(ctx, false); failure != nil {
			return nil, failure
		}
		return nil, err
	}
	if cached := r.agentCredentials.Load(); cached != nil && cached.head == head {
		return cached.values, nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, err
	}
	values, err := gather(entry.Model.System.Agents)
	if err != nil {
		return nil, err
	}
	r.agentCredentials.Store(&agentCredentials{head: entry.Head, values: values})
	return values, nil
}

// AgentRecordCredentialsOf is AgentRecordCredentials for a registry already
// borrowed with BorrowAgentsAt: records must be that generation's records and
// head/shared what it reported. The values are collected from those records,
// never from a later generation, so a projection of a borrowed record always
// masks its own generation's credentials even when a commit lands between the
// two reads. Shared generations reuse and refresh the per-head cache.
func (r *Repository) AgentRecordCredentialsOf(head store.Head, shared bool, records []model.Agent, collect func(json.RawMessage) []string) ([]string, error) {
	if shared {
		if cached := r.agentCredentials.Load(); cached != nil && cached.head == head {
			return cached.values, nil
		}
	}
	var values []string
	for i := range records {
		data, err := model.Marshal(records[i])
		if err != nil {
			return nil, err
		}
		values = append(values, collect(data)...)
	}
	if shared {
		r.agentCredentials.Store(&agentCredentials{head: head, values: values})
	}
	return values, nil
}

func sanitizationTokensModel(system *model.System) (TokenSet, TokenSet) {
	ruleProxy, agents := TokenSet{}, TokenSet{}
	if token := system.SystemConfig.RuleProxyToken; token != "" {
		ruleProxy[token] = struct{}{}
	}
	for _, token := range system.SystemConfig.RetiredRuleProxyTokens {
		if token != "" {
			ruleProxy[token] = struct{}{}
		}
	}
	for i := range system.Agents {
		if token := system.Agents[i].Token; token != "" {
			agents[token] = struct{}{}
		}
	}
	return ruleProxy, agents
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

// UpdateSystem runs fn on a copy of the system section (settings, backup and
// Agents) in one write transaction; the system revision advances by one and
// the rule proxy token is maintained.
func (r *Repository) UpdateSystem(ctx context.Context, fn func(ctx context.Context, s *model.System) error) error {
	ctx = ctxx.Detach(ctx)
	return r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		_, err := applySystemIn(document, func(s *model.System) error { return r.editSystemView(ctx, s, fn) })
		return err
	})
}

// applySystemIn edits a copy of the system section that then replaces it,
// with the revision advanced and the rule proxy token maintained.
func applySystemIn(document *model.Document, fn func(s *model.System) error) (*model.System, error) {
	proposed, err := copyRecord(&document.System)
	if err != nil {
		return nil, err
	}
	if err := fn(proposed); err != nil {
		return nil, err
	}
	proposed.Revision = document.System.Revision + 1
	if _, err := ensureSystemTokens(proposed); err != nil {
		return nil, err
	}
	document.System = *proposed
	return &document.System, nil
}

// SystemMemberJSON returns one effective non-Agent member of system (for
// example system_config or backup) exactly as the whole record would encode it,
// including original evidence, without encoding the Agent registry or
// registration sessions. Those members are cleared on a shallow copy only;
// Raw/Fields are copy-on-write, so system is never changed.
func SystemMemberJSON(system *model.System, member string) (json.RawMessage, bool, error) {
	if system == nil {
		return nil, false, nil
	}
	if member == "agents" || member == "agent_registration_sessions" {
		return nil, false, fmt.Errorf("%w: SystemMemberJSON does not read %s", model.ErrType, member)
	}
	view := *system
	for _, skipped := range []string{"agents", "agent_registration_sessions"} {
		if err := view.Clear(skipped); err != nil {
			return nil, false, err
		}
	}
	return recordMemberJSON(view, member)
}

// SanitizationTokens returns persisted rule proxy and Agent secrets without mutating system.
func SanitizationTokens(system model.System) (ruleProxy, agents TokenSet) {
	return sanitizationTokensModel(&system)
}

// AgentHistoryJSON returns an owned history member of a full typed Agent record.
func AgentHistoryJSON(agent model.Agent, field string) (json.RawMessage, error) {

	raw, _, err := recordMemberJSON(agent, field)
	return raw, err
}

// systemSettingsIssue is the load issue path of a stored system_config that
// is not an object at all.
const systemSettingsIssue = "system.system_config"

// settingsDamaged reports whether the stored settings record itself could not
// be read (it is not an object) and was replaced by empty settings on load.
func settingsDamaged(issues []model.FieldIssue) bool {
	for _, issue := range issues {
		if issue.Path == systemSettingsIssue || issue.Path == systemSettingsIssue+".config_token" {
			return true
		}
	}
	return false
}

// configTokenIssue is the load issue path of a stored config_token that is
// not text (read as no token; SystemSettingsDamaged).
const configTokenIssue = systemSettingsIssue + ".config_token"

// keepDamagedConfigToken keeps a stored config_token that is not text when a
// write rewrites system_config without setting a token (HR O18 T1c). The
// record reads such a token as empty, so writing the record as read (a server
// domain save, token maintenance with other changes) would drop it: the
// damage evidence would be gone and an empty token is anonymous access when
// login is off, where Python kept the stored value (which never matched a
// request). Only setting a token repairs it; clearing one (DELETE) leaves the
// damaged value, as it did before O18 T1c.
func keepDamagedConfigToken(next, base *store.Document, issues []model.FieldIssue, document *model.Document) error {
	if next == nil || base == nil || next.System == nil || base.System == nil || document.System.SystemConfig.ConfigToken != "" ||
		!slices.ContainsFunc(issues, func(issue model.FieldIssue) bool { return issue.Path == configTokenIssue }) {
		return nil
	}
	var stored json.RawMessage
	for _, field := range base.System.Fields {
		if field.Name == "system_config" {
			stored = field.Value
		}
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(stored, &members) != nil {
		return nil
	}
	damaged, ok := members["config_token"]
	if !ok {
		return nil
	}
	for i, field := range next.System.Fields {
		if field.Name != "system_config" || bytes.Equal(field.Value, stored) {
			continue
		}
		var written map[string]json.RawMessage
		if err := json.Unmarshal(field.Value, &written); err != nil {
			return fmt.Errorf("keep damaged config token: %w", err)
		}
		if _, present := written["config_token"]; present {
			continue
		}
		value := bytes.TrimSpace(field.Value)
		var out bytes.Buffer
		out.Write(value[:len(value)-1])
		if len(written) > 0 {
			out.WriteByte(',')
		}
		out.WriteString(`"config_token":`)
		out.Write(damaged)
		out.WriteByte('}')
		next.System.Fields[i].Value = out.Bytes()
	}
	return nil
}

// SystemSettingsDamaged reports whether the current stored system_config is
// not an object (O18 T1a reads it as empty settings with a load warning) or
// its config_token is not text (O18 T1c reads it as no token).
// Credential checks treat such settings as damaged, never as "no token": a
// damaged stored token must not become anonymous access. Inside a transaction
// the transaction's base generation is checked.
func (r *Repository) SystemSettingsDamaged(ctx context.Context) (bool, error) {
	if tx := r.transactionOf(ctx); tx != nil {
		return settingsDamaged(tx.base.Issues), nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return false, err
	}
	return settingsDamaged(entry.Issues), nil
}

// StoredIssues returns the load issues of the current stored generation
// (paths such as "shared.subscriptions[2]"; no values). Callers that would
// delete data on what a damaged row reads as (for example no subscriptions)
// check them and fail closed. Inside a transaction the base generation is used.
func (r *Repository) StoredIssues(ctx context.Context) ([]model.FieldIssue, error) {
	if tx := r.transactionOf(ctx); tx != nil {
		return slices.Clone(tx.base.Issues), nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, err
	}
	return slices.Clone(entry.Issues), nil
}

// keepDamagedSettings restores the base generation's settings in document
// when the base settings are damaged (not an object, read as empty) and the
// transaction changed nothing in them except what token maintenance adds. It
// reports whether it did: such a transaction must not run token maintenance,
// which would replace the damaged record with fresh settings whose config
// token is empty (anonymous access). Only an explicit settings change repairs
// damaged settings.
func keepDamagedSettings(base, document *model.Document) (bool, error) {
	withoutMaintenance := func(settings *model.SystemConfig) ([]byte, error) {
		copied, err := copyRecord(settings)
		if err != nil {
			return nil, err
		}
		for _, member := range []string{"rule_proxy_token", "retired_rule_proxy_tokens"} {
			if err := copied.Clear(member); err != nil {
				return nil, err
			}
		}
		return model.Marshal(copied)
	}
	before, err := withoutMaintenance(&base.System.SystemConfig)
	if err != nil {
		return false, err
	}
	after, err := withoutMaintenance(&document.System.SystemConfig)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(before, after) {
		return false, nil
	}
	restored, err := copyRecord(&base.System.SystemConfig)
	if err != nil {
		return false, err
	}
	document.System.SystemConfig = *restored
	return true, nil
}
