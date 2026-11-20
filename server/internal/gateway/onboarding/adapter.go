package onboarding

// Existing repo/generator/router-split boundaries still use jsonx. Keep that
// compatibility here; preparation, inputs, reports and stored edits are native.
import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/netip"
	"os"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/rules"
	"configflow/internal/setupflow"
	"configflow/internal/yamlx"
)

func modelDocument(record json.Marshaler) (Document, error) {
	raw, err := record.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return object(raw), nil
}
func nativeConfig(config Document) (*model.Config, error) { return model.DecodeConfig(encode(config)) }
func gatewayConfig(config Document) (*gateway.Config, error) {
	v, err := nativeConfig(config)
	if err != nil {
		return nil, err
	}
	return gateway.NewConfig(v), nil
}
func generate(ctx context.Context, config Document, baseURL string) (*yamlx.Node, error) {
	raw, err := nativeConfig(config)
	if err != nil {
		return nil, err
	}
	data, err := convert.MihomoConfig(ctx, raw, convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true})
	if err != nil {
		return nil, err
	}
	return yamlx.Parse([]byte(data))
}
func defaultMihomo() (*yamlx.Node, error) { return convert.MihomoTemplate(), nil }
func fullTun(config, candidate Document, ids []string) error {
	raw, err := nativeConfig(config)
	if err != nil {
		return err
	}
	gw := gateway.NewConfig(raw)
	previous := routersplit.ProfileRuleIDs(raw.Profile)
	if err = gw.ValidateRuleSetIDs(ids, previous); err != nil {
		return err
	}
	_, marker := routersplit.ReadMarkerProfile(raw.Profile)
	native := Document{"version": encode(1), "takeover": encode("full_tun"), "origin": encode("setup")}
	if marker != nil {
		data, present := raw.Profile.Mihomo.Fields.Get(routersplit.MarkerKey)
		if present {
			native = object(data)
		}
	}
	native["route_rule_set_ids"] = encode(ids)
	mihomo := object(candidate["mihomo"])
	if mihomo == nil {
		mihomo = Document{}
	}
	mihomo["linux_gateway"] = encode(gateway.Settings{Enabled: false, RuleSetIDs: []string{}})
	mihomo[routersplit.MarkerKey] = encode(native)
	source, err := routersplit.FullTunYAML(stringValue(mihomo["custom_config"]))
	if err != nil {
		return err
	}
	mihomo["custom_config"] = encode(source)
	candidate["mihomo"] = encode(mihomo)
	return nil
}
func markerState(config Document) (string, error) {
	v, err := nativeConfig(config)
	if err != nil {
		return "", err
	}
	state, _ := routersplit.ReadMarkerProfile(v.Profile)
	return state, nil
}
func fullTunDocument(document *yamlx.Node) bool { return routersplit.FullTunShape(document) }
func takeover(ctx context.Context, r *repo.Repository, id string, config Document) (string, error) {
	raw, err := nativeConfig(config)
	if err != nil {
		return "", err
	}
	state, _ := routersplit.ReadMarkerProfile(raw.Profile)
	switch state {
	case routersplit.StateValid:
		return "full_tun", nil
	case routersplit.StateInvalid:
		return "", invalid(routersplit.Messages[routersplit.CodeInvalid])
	}
	settings, err := gateway.SettingsOf(raw)
	if err != nil {
		return "", err
	}
	if settings.Enabled {
		state, err := readSplitState(ctx, r, id, raw.Profile)
		if err != nil {
			return "", err
		}
		if state.Mode != nil && *state.Mode == "legacy" {
			return "legacy", nil
		}
		return "selective", nil
	}
	evidence, err := routersplit.ReadContext(ctx, r, id)
	if err != nil {
		return "", err
	}
	if evidence.SessionGoal != nil && *evidence.SessionGoal == "router" && !evidence.SessionCompleted {
		return "full_tun", nil
	}
	return "selective", nil
}
func overlapWarnings(ctx context.Context, config Document) ([]string, error) {
	gw, err := gatewayConfig(config)
	if err != nil {
		return nil, err
	}
	return gw.DirectOverlapWarnings(ctx, nil)
}

// diagnosticFields are the free-text parts of a state/report that may echo
// user configuration (generator, YAML and OxiDNS validation messages).
var diagnosticFields = []string{"blockers", "warnings", "message", "validation", "field_warnings"}

// sanitize masks system secrets everywhere and configuration credentials in
// diagnostics. Credentials discovered in the profile (node passwords, UUIDs,
// short IDs) are user-chosen and may be short common words; masking every key
// and value containing them would corrupt the wizard's own fields (review A21
// F4), which Python returns verbatim. A bare JSON string is a diagnostic.
func sanitize(raw json.RawMessage, config Document) (json.RawMessage, error) {
	var system model.System
	if err := json.Unmarshal(encode(config), &system); err != nil {
		return nil, err
	}
	tokens := append([]string{system.SystemConfig.RuleProxyToken, system.SystemConfig.ConfigToken, system.SystemConfig.RuleFetchProxy, os.Getenv("AGENT_REGISTRATION_KEY")}, system.SystemConfig.RetiredRuleProxyTokens...)
	for _, agent := range system.Agents {
		tokens = append(tokens, agent.Token)
	}
	secrets := redact.NewSecrets(tokens)
	credentials := redact.CollectCredentials(map[string]any{"config": json.RawMessage(encode(config)), "proxy": system.SystemConfig.RuleFetchProxy}, secrets)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return json.Marshal(redact.SanitizeWithSecrets(raw, credentials, true))
	}
	for _, key := range diagnosticFields {
		if value, ok := fields[key]; ok {
			masked, err := json.Marshal(redact.SanitizeWithSecrets(value, credentials, true))
			if err != nil {
				return nil, err
			}
			fields[key] = masked
		}
	}
	return json.Marshal(redact.SanitizeWithSecrets(encode(fields), secrets, true))
}
func safeError(err error, config Document) error {
	var user *gateway.Error
	if !errors.As(err, &user) {
		return err
	}
	raw, e := sanitize(encode(user.Error()), config)
	if e != nil {
		return e
	}
	return invalid(stringValue(raw))
}

// DNSLoopChecker is supplied by the shared C34 gateway DNS validator.
type DNSLoopChecker func(*yamlx.Node, []netip.Addr) (bool, error)

// AdoptOwnedWrite lets C36 adopt the already committed write using its session
// locks and ownership proof. before/after are complete native profile JSON.
type AdoptOwnedWrite func(context.Context, *repo.Repository, string, json.RawMessage, json.RawMessage) error

var adoptHook struct {
	sync.RWMutex
	fn AdoptOwnedWrite
}

// SetAdoptOwnedWrite registers C36's owner. It returns a restoration function.
func SetAdoptOwnedWrite(fn AdoptOwnedWrite) func() {
	adoptHook.Lock()
	previous := adoptHook.fn
	adoptHook.fn = fn
	adoptHook.Unlock()
	return func() { adoptHook.Lock(); adoptHook.fn = previous; adoptHook.Unlock() }
}
func currentAdopter() AdoptOwnedWrite {
	adoptHook.RLock()
	defer adoptHook.RUnlock()
	return adoptHook.fn
}
func pendingSetup(ctx context.Context, r *repo.Repository, id string) bool {
	sessions, err := setupflow.ProfileSessions(ctx, r.DataDir(), id)
	if err != nil {
		return true
	}
	for _, session := range sessions {
		obj := object(session.Raw)
		if string(obj["completed"]) != "true" && string(obj["abandoned"]) != "true" {
			return true
		}
	}
	return false
}
func routeIDs(config Document) ([]string, error) {
	raw, err := nativeConfig(config)
	if err != nil {
		return nil, err
	}
	ids := routersplit.ProfileRuleIDs(raw.Profile)
	return ids, nil
}
func errorsYAML(err error) bool {
	return errors.Is(err, yamlx.ErrSyntax) || errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrAliasRecursion) || errors.Is(err, yamlx.ErrAliasBudget) || errors.Is(err, yamlx.ErrType) || errors.Is(err, yamlx.ErrMultipleDocuments)
}

// revisionMatches is a numeric comparison at the repository guard boundary.
// Schema validation still rejects floats/bools in ParseRequest after the guard.
func revisionMatches(raw json.RawMessage, revision int64) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	if string(raw) == "true" {
		return revision == 1
	}
	if string(raw) == "false" {
		return revision == 0
	}
	number, ok := new(big.Rat).SetString(string(raw))
	return ok && number.Cmp(new(big.Rat).SetInt64(revision)) == 0
}

func validationMessage(err error) (string, bool) {
	if msg, ok := oxidns.IsSettingsError(err); ok {
		return msg, true
	}
	var gw *gateway.Error
	var strategy *rules.StrategyReferenceError
	var base *convert.BaseSettingsError
	var tier *convert.EasyTierSettingsError
	if errors.As(err, &gw) || errors.As(err, &strategy) || errors.As(err, &base) || errors.As(err, &tier) || errorsYAML(err) {
		return err.Error(), true
	}

	return "", false
}

func readSplitState(ctx context.Context, r *repo.Repository, id string, p *model.Profile) (routersplit.SplitState, error) {
	evidence, err := routersplit.ReadContext(ctx, r, id)
	if err != nil {
		return routersplit.SplitState{}, err
	}
	return routersplit.ProfileState(p, evidence)
}
