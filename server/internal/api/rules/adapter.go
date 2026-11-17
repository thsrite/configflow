package rules

// Only this boundary handles legacy repository, syntax and Agent record values.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/agents"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	shared "configflow/internal/rules"
	"configflow/internal/yamlx"
)

func native(value any) (record, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out record
	err = json.Unmarshal(raw, &out)
	return out, err
}
func legacy(value record) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	v, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	out, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("expected configuration object")
	}
	return out, nil
}
func readBody(c *httpx.Ctx, silent bool) (record, error) {
	var value any
	var err error
	if silent {
		value = c.JSONSilent()
	} else {
		value, err = c.JSON()
		if err != nil {
			return nil, err
		}
	}
	if _, ok := value.(*jsonx.Object); !ok {
		return nil, nil
	}
	return native(value)
}

func loadProfile(ctx context.Context, id string) (record, error) {
	value, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	return native(value)
}
func editProfile(ctx context.Context, id string, edit func(record) error) error {
	_, err := repo.UpdateConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		profile, err := native(raw)
		if err != nil {
			return nil, err
		}
		if err = edit(profile); err != nil {
			return nil, err
		}
		next, err := legacy(profile)
		if err != nil {
			return nil, err
		}
		raw.Update(next)
		return nil, nil
	}, id)
	return err
}
func validateSyntax(item record) error {
	raw, err := legacy(item)
	if err != nil {
		return err
	}
	kind, ok := raw.Value("rule_type").(string)
	if !ok {
		return invalid(fmt.Sprintf(msgUnknownKind, jsonx.Str(raw.Value("rule_type"))))
	}
	value, ok := raw.GetOr("value", "").(string)
	if !ok {
		return invalid(msgSingleLine)
	}
	if message := shared.RuleValueError(kind, value); message != "" {
		return invalid(message)
	}
	return nil
}
func reorderSelected(items []record, selection []json.RawMessage, position string) ([]record, []json.RawMessage, error) {
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
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	ordered, unknown := shared.ReorderByIDs(value.([]any), ids, position)
	unknownSet := map[string]bool{}
	for _, id := range unknown {
		unknownSet[id] = true
	}
	missing := []json.RawMessage{}
	for _, raw := range selection {
		text, ok := textValue(raw)
		if !ok || unknownSet[text] {
			missing = append(missing, raw)
		}
	}
	out := make([]record, len(ordered))
	for i, item := range ordered {
		out[i], err = native(item)
		if err != nil {
			return nil, nil, err
		}
	}
	return out, missing, nil
}

func hydrated(profile, item record) (record, error) {
	if item.text("itemType") != "ruleset" {
		return item, nil
	}
	library, err := profile.list("rule_library")
	if err != nil {
		return nil, err
	}
	i := find(library, item.text("library_rule_id"))
	if i < 0 {
		return nil, invalid(msgLibraryReference)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	n, err := oxidns.FromJSON(raw)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(library[i])
	if err != nil {
		return nil, err
	}
	lib, err := oxidns.FromJSON(raw)
	if err != nil {
		return nil, err
	}
	return native(json.RawMessage(oxidns.ToJSON(oxidns.HydrateRuleset(n, lib, profile.text("profile_id")))))
}
func dnsProfile(profile record) (*oxidns.Profile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	return oxidns.ProfileFromJSON(raw)
}
func syncDNS(before, after record, assignments []oxidns.DNSAssignment, base string, strict bool) ([]string, error) {
	old, err := dnsProfile(before)
	if err != nil {
		return nil, err
	}
	next, err := dnsProfile(after)
	if err != nil {
		return nil, err
	}
	result, err := oxidns.ApplyRuleDNS(old, next, assignments, oxidns.RuleDNSApplyOptions{BaseURL: base, Lenient: !strict})
	if err != nil {
		return nil, dnsError(err)
	}
	if result.Changed {
		after["oxidns"] = oxidns.ToJSON(result.Settings)
	}
	return result.Warnings, nil
}
func dnsError(err error) error {
	if _, ok := oxidns.IsSettingsError(err); ok {
		return invalid(err.Error())
	}
	return err
}
func ensureDNSKind(profile, item record, dns string) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	node, err := oxidns.FromJSON(raw)
	if err != nil {
		return err
	}
	if oxidns.Convertible(node) {
		return nil
	}
	kind := item.text("rule_type")
	supported := false
	for _, value := range mosdns.DNSRuleTypes {
		supported = supported || kind == value
	}
	if !supported {
		settings := profile.object("mosdns")
		for _, selector := range []struct{ key, label string }{{"direct_rules", "直连规则"}, {"proxy_rules", "代理规则"}} {
			key, label := selector.key, selector.label
			var ids []string
			_ = json.Unmarshal(settings[key], &ids)
			for _, id := range ids {
				if id == item.text("id") {
					return invalid(fmt.Sprintf(msgMosDNSGuard, label, kind))
				}
			}
		}
	}
	p, err := dnsProfile(profile)
	if err != nil {
		return err
	}
	route, err := oxidns.ExplicitRuleDNS(p, item.text("id"))
	if err != nil {
		return dnsError(err)
	}
	if route != "" && dns != "default" {
		label := oxidns.RouteLabels[routeKind(route)]
		return invalid(fmt.Sprintf(msgOxidnsGuard, label, kind))
	}
	return nil
}
func nativeWithout(profile record, id, base string) (json.RawMessage, error) {
	settings := profile.object("oxidns")
	if settings.text("config_mode") != "native" || settings.text("native_config") == "" {
		return nil, nil
	}
	if _, err := yamlx.Parse([]byte(settings.text("native_config"))); err != nil && !errors.Is(err, yamlx.ErrSyntax) {
		return nil, invalid(err.Error())
	}
	if _, err := oxidns.LoadNativeYAML(settings.text("native_config"), oxidns.NativeLabel); err != nil {
		return nil, nil
	}
	p, err := dnsProfile(profile)
	if err != nil {
		return nil, err
	}
	n, err := oxidns.FromJSON(profile["oxidns"])
	if err != nil {
		return nil, err
	}
	next, err := oxidns.RemoveRule(n, p, id, base)
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
	return oxidns.ToJSON(next), nil
}
func withheld(ctx context.Context, profileID string, ids []string) (*bool, error) {
	p, err := loadProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	view, err := dnsProfile(p)
	if err != nil {
		return nil, err
	}
	eligible := false
	for _, agent := range oxidns.ProfileAgents(view) {
		var row record
		if json.Unmarshal(oxidns.ToJSON(agent), &row) != nil {
			continue
		}
		settings := row.object("rule_sync").object("settings")
		if row.text("service_type") == "oxidns" && (exactTrue(settings["library"]) || exactTrue(settings["rules"])) {
			eligible = true
			break
		}
	}

	if len(ids) == 0 || !eligible {
		v := false
		return &v, nil
	}
	hook := agents.CurrentHooks().PinnedWithheld
	if hook == nil {
		return nil, agents.ErrNotPorted
	}
	v, err := hook(ctx, profileID, ids)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func routeKind(route string) string {
	for i, c := range route {
		if c == ':' {
			return route[:i]
		}
	}
	return route
}

func udpChoice(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", false
	}
	if !jsonx.Truthy(value) {
		return "", true
	}
	text, ok := value.(string)
	return text, ok
}
func missingSelected(ids []json.RawMessage) string {
	raw, err := json.Marshal(ids)
	if err != nil {
		return msgOrderIDs
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return msgOrderIDs
	}
	return fmt.Sprintf(msgOrderMissing, jsonx.Repr(value))
}

func dnsChoice(raw json.RawMessage) (string, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", invalid(msgDNSRoutes)
	}
	choice, ok := value.(string)
	if !ok {
		return "", invalid(fmt.Sprintf(msgDNSUnsupported, jsonx.Str(value)))
	}
	if err := oxidns.CheckDNSChoice(choice); err != nil {
		return choice, dnsError(err)
	}
	return choice, nil
}
