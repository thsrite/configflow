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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
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

func fromLegacy(raw *jsonx.Object) (document, error) {
	b, e := jsonx.Compact(raw)
	if e != nil {
		return nil, e
	}
	return fields(b), nil
}
func legacy(config document) (*jsonx.Object, error) {
	v, e := jsonx.Parse(encode(config))
	if e != nil {
		return nil, e
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("gateway draft: expected object")
	}
	return o, nil
}
func selection(config document, value json.RawMessage) (gateway.Settings, error) {
	raw, e := legacy(config)
	if e != nil {
		return gateway.Settings{}, e
	}
	return gateway.NewConfig(raw).ValidateSelection(value)
}
func settings(config document) (gateway.Settings, error) {
	raw, e := legacy(config)
	if e != nil {
		return gateway.Settings{}, e
	}
	return gateway.SettingsOf(raw)
}
func generate(ctx context.Context, config document, baseURL string) (*yamlx.Node, error) {
	raw, e := legacy(config)
	if e != nil {
		return nil, e
	}
	text, e := convert.GenerateMihomoConfig(ctx, raw, convert.GenerateOptions{BaseURL: baseURL, SkipProviderPreflight: true})
	if e != nil {
		return nil, e
	}
	return yamlx.Parse([]byte(text))
}
func overlaps(ctx context.Context, config document) ([]string, error) {
	raw, e := legacy(config)
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

func (s *Service) guard(legacyContext *jsonx.Object, current, candidate document, restore bool, forwarding func() (string, error)) error {
	raw, err := legacy(current)
	if err != nil {
		return err
	}
	after, err := settings(candidate)
	if err != nil {
		return err
	}
	needs, err := routersplit.NeedsContext(raw)
	if err != nil {
		return err
	}
	if legacyContext == nil && needs {
		return invalid(msgConcurrent)
	}
	contextValue := legacyContext
	if contextValue == nil {
		contextValue = jsonx.NewObject()
	}
	modeValue, err := routersplit.ModeFromContext(raw, contextValue)
	if err != nil {
		return err
	}
	mode, _ := modeValue.(string)
	splitError := func(message, code string) error { return routersplit.NewRouterSplitError(message, code, 409) }
	if mode == "invalid" {
		return splitError(routersplit.Messages[routersplit.CodeInvalid], routersplit.CodeInvalid)
	}
	if mode == "full_tun" {
		if restore {
			message := routersplit.FullTunNoRestorePoint
			if p := point(current); p != nil {
				message = routersplit.Messages[routersplit.CodeRestoreBlocked]
				snapshot, _ := legacy(document{"mihomo": encode(p.Mihomo), "oxidns": encode(p.OxiDNS)})
				full, err := routersplit.BaseIsFullTun(snapshot)
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
	source, err := routersplit.LegacySource(raw, legacyContext)
	if err != nil {
		return err
	}
	if source == nil {
		return nil
	}
	target, err := legacy(candidate)
	if err != nil {
		return err
	}
	full, err := routersplit.BaseIsFullTun(target)
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
	raw, revision, err := s.Repository.GetPublishSnapshot(ctx, op.ProfileID, true)
	if err != nil {
		return nil, err
	}
	config, err := fromLegacy(raw)
	if err != nil {
		return nil, err
	}
	var legacyContext *jsonx.Object
	needs, err := routersplit.NeedsContext(raw)
	if err != nil {
		return nil, err
	}
	if needs {
		legacyContext, err = routersplit.LegacyContext(ctx, s.Repository, op.ProfileID)
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
			ids, err := routersplit.LegacyGatewayAgentIDs(raw, legacyContext)
			if err != nil {
				return "", err
			}
			agentIDs := []string{}
			for _, v := range ids {
				id, ok := v.(string)
				if !ok {
					return "", invalid(msgConcurrent)
				}
				agentIDs = append(agentIDs, id)
			}
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
		_, revision, err = s.Repository.UpdateProfileTransaction(ctx, op.ProfileID, func(tx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			native, err := fromLegacy(current)
			if err != nil {
				return nil, err
			}
			rev, err := s.Repository.ConfigRevision(tx, op.ProfileID)
			if err != nil {
				return nil, err
			}
			_, hasRevision := op.Body["expected_config_revision"]
			_, hasToken := op.Body["preview_token"]
			guarded := op.Restore || hasRevision || hasToken
			if guarded {
				if err = checkRevision(op.Body, rev); err != nil {
					return nil, err
				}
			}
			candidate, err := makeCandidate(native, false)
			if err != nil {
				return nil, err
			}
			if guarded {
				p, err := preview(tx, native, candidate, rev, op.Restore, op.BaseURL)
				if err != nil {
					return nil, err
				}
				if err = checkPreview(op.Body, p); err != nil {
					return nil, err
				}
			}
			if !op.Restore {
				if err = remember(native, candidate); err != nil {
					return nil, err
				}
			}
			for _, key := range []string{"mihomo", "oxidns"} {
				if value, ok := candidate[key]; ok {
					v, err := jsonx.Parse(value)
					if err != nil {
						return nil, err
					}
					current.Set(key, v)
				}
			}
			committed, err = fromLegacy(current)
			if err == nil {
				err = ctx.Err()
			}
			return nil, err
		})
		if err != nil {
			return nil, err
		}
		repo.ResetConfigContext(ctx)
		config = committed
	}
	current, err := legacy(config)
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
		needs, err := routersplit.NeedsContext(current)
		if err != nil {
			return nil, err
		}
		if needs {
			legacyContext, err = routersplit.LegacyContext(ctx, s.Repository, op.ProfileID)
			if err != nil {
				return nil, err
			}
		}
	}
	contextValue := legacyContext
	if contextValue == nil {
		contextValue = jsonx.NewObject()
	}
	state, err := routersplit.RouterSplitState(ctx, s.Repository, op.ProfileID, current, contextValue)
	if err != nil {
		return nil, err
	}
	out["router_split"] = encode(struct {
		Mode    any `json:"mode"`
		Source  any `json:"source"`
		Issues  any `json:"issues"`
		RuleIDs any `json:"route_rule_set_ids"`
	}{state.Value("mode"), state.Value("source"), state.Value("issues"), routersplit.RouteRuleSetIDs(current)})
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
