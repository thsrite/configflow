package rules

// Request records delegate rule semantics to typed owners. Profile writes retain
// the repository compatibility boundary until W1 supplies resolved transactions.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"

	"configflow/internal/agents"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
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
func ruleRecord(value record) (model.RuleConfig, error) {
	raw, err := json.Marshal(value)
	var out model.RuleConfig
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}
func readBody(c *httpx.Ctx, silent bool) (record, error) {
	var raw json.RawMessage
	if silent {
		if !c.BindSilent(&raw) {
			return nil, nil
		}
	} else if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	var out record
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	err := json.Unmarshal(raw, &out)
	return out, err
}

func loadProfile(ctx context.Context, id string) (record, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	id, err = repo.ResolveProfileID(ctx, id)
	if err != nil {
		return nil, err
	}
	value, err := r.Config(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	return native(value)
}
func editProfile(ctx context.Context, id string, edit func(record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := repo.UpdateConfigTransaction(ctx, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		if err := tx.Err(); err != nil {
			return nil, err
		}
		profile, err := native(raw)
		if err != nil {
			return nil, err
		}
		before := maps.Clone(profile)
		if err = edit(profile); err != nil {
			return nil, err
		}
		if err := tx.Err(); err != nil {
			return nil, err
		}
		for key, value := range profile {
			previous, existed := before[key]
			if bytes.Equal(previous, value) {
				continue
			}
			if existed {
				same, err := sameJSONValue(previous, value)
				if err != nil {
					return nil, fmt.Errorf("compare profile field %s: %w", key, err)
				}
				if same {
					continue
				}
			}
			next, err := jsonx.Parse(value)
			if err != nil {
				return nil, err
			}
			raw.Set(key, next)
		}
		return nil, tx.Err()
	}, id)
	return err
}

// sameJSONValue distinguishes real edits from re-encoding a nested object.
// Numbers retain their original tokens; only object member order and separator
// whitespace are immaterial here. The store's raw-byte/revision rules stay intact.
func sameJSONValue(before, after json.RawMessage) (bool, error) {
	var values [2]any
	for i, raw := range []json.RawMessage{before, after} {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&values[i]); err != nil {
			return false, err
		}
	}
	return reflect.DeepEqual(values[0], values[1]), nil
}
func validateSyntax(item record) error {
	kind, ok := textValue(item["rule_type"])
	if !ok {
		return invalid(fmt.Sprintf(msgUnknownKind, shared.InvalidValueText(item["rule_type"])))
	}
	value := ""
	if raw, exists := item["value"]; exists {
		var valid bool
		value, valid = textValue(raw)
		if !valid {
			return invalid(msgSingleLine)
		}
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
	ordered, unknown := shared.Reorder(items, ids, position, func(item record) string { return item.text("id") })
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
	return ordered, missing, nil
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
	if !shared.EnabledField(raw, false) {
		return "", true
	}
	return textValue(raw)
}
func missingSelected(ids []json.RawMessage) string {
	return fmt.Sprintf(msgOrderMissing, selectedText(ids))
}

func dnsChoice(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return "", invalid(msgDNSRoutes)
	}
	choice, ok := textValue(raw)
	if !ok {
		return "", invalid(fmt.Sprintf(msgDNSUnsupported, shared.InvalidValueText(raw)))
	}
	if err := oxidns.CheckDNSChoice(choice); err != nil {
		return choice, dnsError(err)
	}
	return choice, nil
}

func selectedText(ids []json.RawMessage) string {
	stringsOnly := make([]string, len(ids))
	for i, raw := range ids {
		if text, ok := textValue(raw); ok {
			stringsOnly[i] = text
		} else {
			encoded, _ := json.Marshal(ids)
			return string(encoded)
		}
	}
	return shared.IdentifierList(stringsOnly)
}
func rawFlag(item record, key string, fallback bool) bool {
	return shared.EnabledField(item[key], fallback)
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
