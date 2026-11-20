package draft

// Repository/generator/router-split adapters are isolated here until N8-5
// exposes their native APIs. The draft algorithm uses native JSON and Nodes.
import (
	"configflow/internal/agents/diag"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/gateway/host"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Service owns the draft transaction and the forwarding preflight outside L3.
type Service struct {
	Repository *repo.Repository
	Host       *host.Bridge
}

func modelDocument(record json.Marshaler) (document, error) {
	raw, err := record.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return fields(raw), nil
}
func nativeConfig(config document) (*model.Config, error) { return model.DecodeConfig(encode(config)) }
func selection(config document, value json.RawMessage) (gateway.Settings, error) {
	raw, e := nativeConfig(config)
	if e != nil {
		return gateway.Settings{}, e
	}
	return gateway.NewConfig(raw).ValidateSelection(value)
}
func settings(config document) (gateway.Settings, error) {
	raw, e := nativeConfig(config)
	if e != nil {
		return gateway.Settings{}, e
	}
	return gateway.SettingsOf(raw)
}
func generate(ctx context.Context, config document, baseURL string) (*yamlx.Node, error) {
	raw, e := nativeConfig(config)
	if e != nil {
		return nil, e
	}
	text, e := convert.MihomoConfig(ctx, raw, convert.MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true})
	if e != nil {
		return nil, e
	}
	return yamlx.Parse([]byte(text))
}
func overlaps(ctx context.Context, config document) ([]string, error) {
	raw, e := nativeConfig(config)
	if e != nil {
		return nil, e
	}
	return gateway.NewConfig(raw).DirectOverlapWarnings(ctx, nil)
}
func safeValue(key string, value json.RawMessage, seeds redact.Secrets) (json.RawMessage, error) {
	if !json.Valid(value) {
		return nil, errors.New("gateway draft: display value is not JSON")
	}
	seeds = redact.CollectCredentials(map[string]any{key: value}, seeds)
	node, err := yamlx.FromJSON(value)
	if err != nil {
		return nil, err
	}
	var project func(*yamlx.Node, string) error
	project = func(n *yamlx.Node, key string) error {
		if slices.Contains([]string{"custom_config", "native_config", "template_base_config", "legacy_native_config", "additional_files"}, key) {
			b, err := yamlx.ToJSON(n)
			if err != nil {
				return err
			}
			*n = *yamlx.Str(msgDigest + digest(json.RawMessage(b))[:12])
			return nil
		}
		if hiddenKey(key) {
			*n = *yamlx.Str(msgHidden)
			return nil
		}
		if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" && strings.Contains(n.Value, "://") {
			u, err := url.Parse(n.Value)
			if err != nil {
				*n = *yamlx.Str(msgAddressHidden)
			} else {
				u.User = nil
				u.Fragment = ""
				u.RawFragment = ""
				if u.RawQuery != "" {
					u.RawQuery = "…"
				}
				n.Value = u.String()
			}
		}
		if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
			n.Value = sanitizedText(n.Value, seeds)
			return nil
		}
		if n.Kind == yaml.MappingNode {
			for k, v := range yamlx.Pairs(n) {
				if err := project(v, k); err != nil {
					return err
				}
			}
		} else if n.Kind == yaml.SequenceNode {
			for _, v := range n.Content {
				if err := project(v, key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err = project(node, key); err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	// Scan metadata keys too, after named fields have their Chinese mask.
	// This pass never discovers credentials from projected placeholders.
	return json.Marshal(redact.SanitizeWithSecrets(json.RawMessage(data), seeds, true))
}

// hiddenKeyWords are the field-name words the preview has always masked by
// substring ("secret_key", "api_tokens", "vmess-uuid"). The shared list only
// matches exact names and suffixes; the mask must never show more than before.
var hiddenKeyWords = []string{"password", "secret", "token", "authorization", "private-key", "private_key", "uuid"}

func hiddenKey(key string) bool {
	if diag.IsCredentialKey(key) {
		return true
	}
	lower := strings.ToLower(key)
	return slices.ContainsFunc(hiddenKeyWords, func(word string) bool { return strings.Contains(lower, word) })
}

func sourceCredentials(config, candidate document) (redact.Secrets, error) {
	var seeds redact.Secrets
	for _, input := range []document{config, candidate} {
		seeds = redact.CollectCredentials(json.RawMessage(encode(input)), seeds)
	}
	return seeds, nil
}
func generatedCredentials(before, after *yamlx.Node, seeds redact.Secrets) (redact.Secrets, error) {
	for _, node := range []*yamlx.Node{before, after} {
		if node == nil {
			continue
		}
		raw, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		if fields(raw) == nil {
			return nil, errors.New("gateway draft: generated configuration is not an object")
		}
		seeds = redact.CollectCredentials(json.RawMessage(raw), seeds)
	}
	return seeds, nil
}
func failureText(err error, seeds redact.Secrets) string {
	return sanitizedText(err.Error(), seeds)
}

// sanitizedText passes one display string through the shared credential
// sanitizer, which returns the redacted value as encoded JSON.
func sanitizedText(text string, seeds redact.Secrets) string {
	var out string
	if json.Unmarshal(redact.SanitizeCredentials(text, seeds), &out) != nil {
		return redact.Redacted
	}
	return out
}

func (s *Service) guard(legacyContext *routersplit.Context, current, candidate document, restore bool, forwarding func() (string, error)) error {
	raw, err := nativeConfig(current)
	if err != nil {
		return err
	}
	after, err := settings(candidate)
	if err != nil {
		return err
	}
	needs, err := needsContext(raw)
	if err != nil {
		return err
	}
	if legacyContext == nil && needs {
		return invalid(msgConcurrent)
	}
	contextValue := legacyContext
	if contextValue == nil {
		contextValue = &routersplit.Context{}
	}
	modeValue, err := routersplit.ProfileState(raw.Profile, contextValue)
	if err != nil {
		return err
	}
	mode := ""
	if modeValue.Mode != nil {
		mode = *modeValue.Mode
	}
	splitError := func(message, code string) error { return routersplit.NewRouterSplitError(message, code, 409) }
	if mode == "invalid" {
		return splitError(routersplit.Messages[routersplit.CodeInvalid], routersplit.CodeInvalid)
	}
	if mode == "full_tun" {
		if restore {
			message := routersplit.FullTunNoRestorePoint
			if p := point(current); p != nil {
				message = routersplit.Messages[routersplit.CodeRestoreBlocked]
				snapshot, _ := nativeConfig(document{"mihomo": encode(p.Mihomo), "oxidns": encode(p.OxiDNS)})
				full, err := baseFullTun(snapshot)
				if err != nil {
					return err
				}
				if full {
					message = routersplit.FullTunRestoreOverwrites
				}
			}
			return splitError(message, routersplit.CodeRestoreBlocked)
		}
		if after.Enabled {
			return splitError(routersplit.Messages[routersplit.CodeFullTun], routersplit.CodeFullTun)
		}
	}
	before, err := settings(current)
	if err != nil {
		return err
	}
	if legacyContext == nil || after.Enabled || !before.Enabled {
		return nil
	}
	source, err := routersplit.LegacyProfileSource(raw.Profile, legacyContext)
	if err != nil {
		return err
	}
	if source == "" {
		return nil
	}
	target, err := nativeConfig(candidate)
	if err != nil {
		return err
	}
	full, err := baseFullTun(target)
	if err != nil {
		return err
	}
	if !full {
		if restore {
			return splitError(routersplit.LegacyRestoreBlocked, routersplit.CodeRestoreBlocked)
		}
		return splitError(routersplit.Messages[routersplit.CodeLegacy], routersplit.CodeLegacy)
	}
	refusal, err := forwarding()
	if err != nil {
		return err
	}
	if refusal != "" {
		return splitError(refusal, "ROUTER_SPLIT_FORWARDING_NOT_PERSISTED")
	}
	return nil
}

// Execute reads, previews or atomically saves a draft. Every write entry detaches
// before preflight and refuses an expired Runtime generation before any I/O.
func (s *Service) Execute(ctx context.Context, op Operation) (json.RawMessage, error) {
	if op.Write && !op.Preview {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, revision, err := s.Repository.PublishConfig(ctx, op.ProfileID, true)
	if err != nil {
		return nil, err
	}
	config, err := modelDocument(raw)
	if err != nil {
		return nil, err
	}
	var legacyContext *routersplit.Context
	needs, err := needsContext(raw)
	if err != nil {
		return nil, err
	}
	if needs {
		legacyContext, err = routersplit.ReadContext(ctx, s.Repository, op.ProfileID)
		if err != nil {
			return nil, err
		}
	}
	checked := false
	refusal := ""
	forwarding := func(io bool) func() (string, error) {
		return func() (string, error) {
			if checked {
				return refusal, nil
			}
			if !io {
				return "", invalid(msgConcurrent)
			}
			ids, err := routersplit.LegacyAgentIDs(raw.Profile, legacyContext)
			if err != nil {
				return "", err
			}
			agentIDs := ids
			if s.Host == nil {
				return "", invalid(msgConcurrent)
			}
			refusal, err = s.Host.GatewayOffRefusal(ctx, op.ProfileID, agentIDs)
			if err != nil {
				return "", err
			}
			checked = true
			return refusal, nil
		}
	}
	makeCandidate := func(current document, io bool) (document, error) {
		if op.Restore {
			if err := s.guard(legacyContext, current, current, true, forwarding(io)); err != nil {
				return nil, err
			}
		}
		candidate, err := prepare(current, op.Body, op.Restore)
		if err != nil {
			return nil, err
		}
		if err = s.guard(legacyContext, current, candidate, op.Restore, forwarding(io)); err != nil {
			return nil, err
		}
		return candidate, nil
	}
	if op.Write {
		if op.Preview {
			if err = checkRevision(op.Body, revision); err != nil {
				return nil, err
			}
			candidate, err := makeCandidate(config, true)
			if err != nil {
				return nil, err
			}
			p, err := preview(ctx, config, candidate, revision, op.Restore, op.BaseURL)
			if err != nil {
				return nil, err
			}
			return json.Marshal(p)
		}
		// No device I/O while holding L3. A changed configuration must either have
		// its preflight result or refuse; it cannot silently skip the host check.
		_, _ = makeCandidate(config, true)
		var committed document
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, revision, err = s.Repository.CommitConfig(ctx, op.ProfileID, func(tx context.Context, current *model.Config) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			native, err := modelDocument(current)
			if err != nil {
				return err
			}
			rev, err := s.Repository.ConfigRevision(tx, op.ProfileID)
			if err != nil {
				return err
			}
			_, hasRevision := op.Body["expected_config_revision"]
			_, hasToken := op.Body["preview_token"]
			guarded := op.Restore || hasRevision || hasToken
			if guarded {
				if err = checkRevision(op.Body, rev); err != nil {
					return err
				}
			}
			candidate, err := makeCandidate(native, false)
			if err != nil {
				return err
			}
			if guarded {
				p, err := preview(tx, native, candidate, rev, op.Restore, op.BaseURL)
				if err != nil {
					return err
				}
				if err = checkPreview(op.Body, p); err != nil {
					return err
				}
			}
			if !op.Restore {
				if err = remember(native, candidate); err != nil {
					return err
				}
			}
			for _, key := range []string{"mihomo", "oxidns"} {
				if value, ok := candidate[key]; ok {
					err := current.Profile.SetJSON(key, value)
					if err != nil {
						return err
					}
				}
			}
			committed, err = modelDocument(current)
			if err == nil {
				err = ctx.Err()
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		repo.ResetConfigContext(ctx)
		config = committed
	}
	current, err := nativeConfig(config)
	if err != nil {
		return nil, err
	}
	editor, err := gateway.NewConfig(current).EditorState(false)
	if err != nil {
		return nil, err
	}
	out := fields(encode(editor))
	out["config_revision"] = encode(revision)
	out["restore_point"] = encode(summary(config))
	if legacyContext == nil {
		needs, err := needsContext(current)
		if err != nil {
			return nil, err
		}
		if needs {
			legacyContext, err = routersplit.ReadContext(ctx, s.Repository, op.ProfileID)
			if err != nil {
				return nil, err
			}
		}
	}
	contextValue := legacyContext
	if contextValue == nil {
		contextValue = &routersplit.Context{}
	}
	state, err := routersplit.ProfileState(current.Profile, contextValue)
	if err != nil {
		return nil, err
	}
	out["router_split"] = encode(struct {
		Mode    any `json:"mode"`
		Source  any `json:"source"`
		Issues  any `json:"issues"`
		RuleIDs any `json:"route_rule_set_ids"`
	}{state.Mode, state.Source, state.Issues, routersplit.ProfileRuleIDs(current.Profile)})
	if op.Write {
		out["success"] = encode(true)
		out["publish_guard"] = encode(struct {
			Profile  string `json:"expected_profile_id"`
			Service  string `json:"expected_service_type"`
			Revision int64  `json:"expected_config_revision"`
		}{op.ProfileID, "mihomo", revision})
	}
	return json.Marshal(out)
}

func needsContext(config *model.Config) (bool, error) {
	state, _ := routersplit.ReadMarkerProfile(config.Profile)
	return state == routersplit.StateAbsent && routersplit.GatewayEnabled(config.Profile), nil
}
func baseFullTun(config *model.Config) (bool, error) {
	return routersplit.BaseFullTun(config.Profile), nil
}
