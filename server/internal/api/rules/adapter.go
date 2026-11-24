package rules

// Rule edits work on the resolved profile record; rule semantics, DNS routing
// and transactions stay with their owners.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/agents/rulesync"
	"configflow/internal/deploy"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	shared "configflow/internal/rules"
	"configflow/internal/yamlx"
)

// requestBody returns the sent JSON object, or nil for any other body.
func requestBody(c *httpx.Ctx, silent bool) (json.RawMessage, error) {
	var raw json.RawMessage
	if silent {
		if !c.BindSilent(&raw) {
			return nil, nil
		}
	} else if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	return raw, nil
}

// decodeRequest decodes an object body into a request DTO; nil for no object.
func decodeRequest[T any](raw json.RawMessage) (*T, error) {
	if raw == nil {
		return nil, nil
	}
	out := new(T)
	if err := jsondecode.Unmarshal(raw, out); err != nil {
		return nil, err
	}
	return out, nil
}

// emptyObject reports a body that is an object without members.
func emptyObject(raw json.RawMessage) bool {
	var compact bytes.Buffer
	return raw == nil || json.Compact(&compact, raw) == nil && compact.String() == "{}"
}

func (s *Service) loadProfile(ctx context.Context, id string) (*model.Config, error) {
	r, err := s.repository(ctx)
	if err != nil {
		return nil, err
	}
	id, err = r.ResolveProfileID(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.ConfigWithOxiDNSAgents(ctx, id, nil)
}

// editProfile edits the resolved profile; the repository stores the members
// that changed.
func (s *Service) editProfile(ctx context.Context, id string, edit func(*model.Config) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := s.repository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateScopedConfig(ctx, id, func(tx context.Context, config *model.Config) error {
		if err := tx.Err(); err != nil {
			return err
		}
		if err := edit(config); err != nil {
			return err
		}
		return tx.Err()
	})
}

// ruleTypeError reports a sent rule member the rule record cannot hold, with
// the message the repository gives for the resulting rule list.
func ruleTypeError(items []model.RuleConfig, cause error) error {
	raw, err := model.Marshal(items)
	if err != nil {
		return err
	}
	return repo.InputError(&model.MemberTypeError{Record: "profile", Member: "rule_configs", Value: raw, Err: cause})
}

func validateSyntax(kind member[string], value member[string]) error {
	if !kind.OK {
		return invalid(fmt.Sprintf(msgUnknownKind, shared.InvalidValueText(kind.Raw)))
	}
	if value.Present() && !value.OK {
		return invalid(msgSingleLine)
	}
	if message := shared.RuleValueError(kind.Value, value.Value); message != "" {
		return invalid(message)
	}
	return nil
}

// textMember is a stored text value as if it had been sent.
func textMember(value *string) member[string] {
	if value == nil {
		return member[string]{Raw: json.RawMessage("null")}
	}
	raw, _ := json.Marshal(*value)
	return member[string]{Raw: raw, Value: *value, OK: true}
}

func reorderSelected(items []model.RuleConfig, selection []json.RawMessage, position string) ([]model.RuleConfig, []json.RawMessage, error) {
	ids := []string{}
	for _, raw := range selection {
		var text string
		if json.Unmarshal(raw, &text) == nil && string(raw) != "null" {
			ids = append(ids, text)
			continue
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return nil, nil, errors.New("invalid rule identifier")
		}
		switch value.(type) {
		case []any, map[string]any:
			return nil, nil, &fault{500, httpx.DescInternal}
		}
	}
	ordered, unknown := shared.Reorder(items, ids, position, func(item model.RuleConfig) string { return item.ID })
	unknownSet := map[string]bool{}
	for _, id := range unknown {
		unknownSet[id] = true
	}
	missing := []json.RawMessage{}
	for _, raw := range selection {
		var text string
		if json.Unmarshal(raw, &text) != nil || string(raw) == "null" || unknownSet[text] {
			missing = append(missing, raw)
		}
	}
	return ordered, missing, nil
}

// hydrated returns a rule-set reference with its shared source, as resolved
// profiles carry it.
func hydrated(config *model.Config, item model.RuleConfig) (model.RuleConfig, error) {
	if item.ItemType != "ruleset" {
		return item, nil
	}
	i := slices.IndexFunc(config.RuleLibrary, func(entry model.RuleLibraryItem) bool { return entry.ID == item.LibraryRuleID })
	if i < 0 {
		return model.RuleConfig{}, invalid(msgLibraryReference)
	}
	return repo.HydratedRuleset(item, config.RuleLibrary[i], config.ProfileID)
}

func dnsProfile(config *model.Config) (*oxidns.Profile, error) { return oxidns.NewTypedProfile(config) }

// syncDNS applies DNS assignments for an edited profile; before is the
// profile view taken before the edit.
func syncDNS(before *oxidns.Profile, after *model.Config, assignments []oxidns.DNSAssignment, base string, strict bool) ([]string, error) {
	next, err := dnsProfile(after)
	if err != nil {
		return nil, err
	}
	result, err := oxidns.ApplyRuleDNS(before, next, assignments, oxidns.RuleDNSApplyOptions{BaseURL: base, Lenient: !strict})
	if err != nil {
		return nil, dnsError(err)
	}
	if result.Changed {
		if err := after.Profile.OxiDNS.SetNode(result.Settings); err != nil {
			return nil, err
		}
	}
	return result.Warnings, nil
}
func dnsError(err error) error {
	if _, ok := oxidns.IsSettingsError(err); ok {
		return invalid(err.Error())
	}
	return err
}
func ensureDNSKind(before *oxidns.Profile, config *model.Config, item model.RuleConfig, dns string) error {
	if oxidns.Convertible(item) {
		return nil
	}
	kind := item.RuleType
	if !slices.Contains(mosdns.DNSRuleTypes, kind) {
		selectors := config.Profile.MosDNS
		for _, selector := range []struct {
			ids   []string
			label string
		}{{selectors.DirectRules, "直连规则"}, {selectors.ProxyRules, "代理规则"}} {
			if slices.Contains(selector.ids, item.ID) {
				return invalid(fmt.Sprintf(msgMosDNSGuard, selector.label, kind))
			}
		}
	}
	route, err := oxidns.ExplicitRuleDNS(before, item.ID)
	if err != nil {
		return dnsError(err)
	}
	if route != "" && dns != "default" {
		label := oxidns.RouteLabels[routeKind(route)]
		return invalid(fmt.Sprintf(msgOxidnsGuard, label, kind))
	}
	return nil
}

// nativeWithout returns native OxiDNS settings without the rule's routing,
// or nil when the settings are not a loadable native configuration.
func nativeWithout(view *oxidns.Profile, head oxidns.SettingsHead, settings *oxidns.Node, id, base string) (*oxidns.Node, error) {
	native := head.NativeConfig
	if head.Mode != "native" || native == "" {
		return nil, nil
	}
	if _, err := yamlx.Parse([]byte(native)); err != nil && !errors.Is(err, yamlx.ErrSyntax) {
		return nil, invalid(err.Error())
	}
	if _, err := oxidns.LoadNativeYAML(native, oxidns.NativeLabel); err != nil {
		return nil, nil
	}
	next, err := oxidns.RemoveRule(settings, view, id, base)
	if err != nil {
		return nil, dnsError(err)
	}
	if next == nil {
		return nil, nil
	}
	next, err = oxidns.NormalizeSettings(next, false)
	if err != nil {
		return nil, dnsError(err)
	}
	return next, nil
}
func (s *Service) withheld(ctx context.Context, profileID string, ids []string, check func(context.Context, string, []string) (bool, error)) (*bool, error) {
	config, err := s.loadProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	bound := config.ProfileID
	if bound == "" {
		bound = defaultProfileID(config)
	}
	eligible := false
	for _, agent := range config.Agents {
		id := agent.ProfileID
		if id == "" {
			id = defaultProfileID(config)
		}
		if !model.Flag(agent.Enabled, true) || id != bound || agent.ServiceType != "oxidns" || agent.RuleSync == nil {
			continue
		}
		settings := agent.RuleSync.Settings
		if model.Flag(settings.Library, false) || model.Flag(settings.Rules, false) {
			eligible = true
			break
		}
	}
	if len(ids) == 0 || !eligible {
		return new(false), nil
	}
	if check == nil {
		check = func(ctx context.Context, profileID string, ruleIDs []string) (bool, error) {
			r, err := s.repository(ctx)
			if err != nil {
				return false, err
			}
			return rulesync.PinnedWithheld(ctx, r, deploy.NewReceipts(r), profileID, ruleIDs)
		}
	}
	v, err := check(ctx, profileID, ids)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func defaultProfileID(config *model.Config) string {
	if id := text(config.SystemConfig.DefaultProfileID); id != "" {
		return id
	}
	return repo.DefaultProfileID
}
func routeKind(route string) string {
	kind, _, _ := strings.Cut(route, ":")
	return kind
}

// udpChoice reads udp_fallback: a falsy value is "follow the client".
func udpChoice(sent member[string]) (string, bool) {
	if !shared.EnabledField(sent.Raw, false) {
		return "", true
	}
	return sent.Value, sent.OK
}
func missingSelected(ids []json.RawMessage) string {
	return fmt.Sprintf(msgOrderMissing, selectedText(ids))
}

func dnsChoice(sent member[string]) (string, error) {
	if len(sent.Raw) == 0 || !json.Valid(sent.Raw) {
		return "", invalid(msgDNSRoutes)
	}
	if !sent.OK {
		return "", invalid(fmt.Sprintf(msgDNSUnsupported, shared.InvalidValueText(sent.Raw)))
	}
	if err := oxidns.CheckDNSChoice(sent.Value); err != nil {
		return sent.Value, dnsError(err)
	}
	return sent.Value, nil
}

func selectedText(ids []json.RawMessage) string {
	stringsOnly := make([]string, len(ids))
	for i, raw := range ids {
		var text string
		if string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
			encoded, _ := json.Marshal(ids)
			return string(encoded)
		}
		stringsOnly[i] = text
	}
	return shared.IdentifierList(stringsOnly)
}
