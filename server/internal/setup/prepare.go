package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"unicode/utf8"

	"bytes"
	"configflow/internal/gateway"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"
)

type resolvedRule struct {
	Identity, Route, ID string
	Item                model.RuleLibraryItem
}

func readString(fields map[string]json.RawMessage, key, fallback string) (string, bool) {
	raw, present := fields[key]
	if !present {
		return fallback, true
	}
	var value string
	valid := string(raw) != "null" && json.Unmarshal(raw, &value) == nil
	return value, valid
}
func readBool(fields map[string]json.RawMessage, key string, fallback bool) (bool, bool) {
	raw, present := fields[key]
	if !present {
		return fallback, true
	}
	var value bool
	valid := string(raw) != "null" && json.Unmarshal(raw, &value) == nil
	return value, valid
}
func encodeFields(value any) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	err = json.Unmarshal(raw, &fields)
	return fields, err
}
func boundary(fields map[string]json.RawMessage) (*model.Config, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return model.DecodeConfig(raw)
}

func (s *Service) selections(ctx context.Context, record *Record, fields map[string]json.RawMessage) (Selections, error) {
	shared, err := sharedSnapshot(ctx, s.repository)
	if err != nil {
		return Selections{}, err
	}
	options := s.catalog.options(shared)
	chosen := s.catalog.recommended(options)
	if record.DomainRules != nil {
		chosen = *record.DomainRules
	}
	if raw, ok := fields["domain_rules"]; ok {
		keys, err := bodyFields(raw)
		if err != nil || len(keys) != 2 || keys["direct"] == nil || keys["proxy"] == nil {
			return chosen, failure(msgDomainSelection, 400)
		}
		direct, err := selectedIDs(map[string]json.RawMessage{"direct": keys["direct"]}, "direct")
		if err != nil {
			return chosen, failure(msgDomainOptions, 400)
		}
		proxy, err := selectedIDs(map[string]json.RawMessage{"proxy": keys["proxy"]}, "proxy")
		if err != nil {
			return chosen, failure(msgDomainOptions, 400)
		}
		chosen = Selections{Direct: direct, Proxy: proxy}
	}
	for _, values := range [][]string{chosen.Direct, chosen.Proxy} {
		for _, value := range values {
			if value == "" || utf8.RuneCountInString(value) > 256 {
				return chosen, failure(msgDomainOptions, 400)
			}
		}
	}
	chosen = s.catalog.canonical(shared, chosen)
	available := map[string]bool{}
	for _, item := range options {
		available[item.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range chosen.Direct {
		seen[id] = true
	}
	for _, id := range chosen.Proxy {
		if seen[id] {
			return chosen, failure(msgDomainOverlap, 400)
		}
	}
	for _, values := range [][]string{chosen.Direct, chosen.Proxy} {
		for _, id := range values {
			if !available[id] {
				return chosen, failure(msgDomainUnavailable, 409)
			}
		}
	}
	return chosen, nil
}
func (s *Service) resolve(ctx context.Context, chosen Selections) ([]resolvedRule, error) {
	selected := map[string]bool{}
	for _, values := range [][]string{chosen.Direct, chosen.Proxy} {
		for _, id := range values {
			selected[id] = true
		}
	}
	builtins, err := s.catalog.ensureLibrary(ctx, s.repository, selected)
	if err != nil {
		return nil, err
	}
	shared, err := sharedSnapshot(ctx, s.repository)
	if err != nil {
		return nil, err
	}
	library := map[string]model.RuleLibraryItem{}
	for _, item := range shared.RuleLibrary {
		library[item.ID] = item
	}
	result := []resolvedRule{}
	for _, selection := range []struct {
		route string
		ids   []string
	}{{"direct", chosen.Direct}, {"proxy", chosen.Proxy}} {
		for _, identity := range selection.ids {
			var item model.RuleLibraryItem
			id := ""
			if strings.HasPrefix(identity, "builtin:") {
				item = builtins[identity]
				id = "setup_" + strings.ReplaceAll(identity, ":", "_")
			} else {
				item = library[strings.TrimPrefix(identity, "library:")]
				sum := sha256.Sum256([]byte(identity))
				id = "setup_domain_" + hex.EncodeToString(sum[:])[:20]
			}
			if item.ID == "" {
				return nil, failure(msgDomainUnavailable, 409)
			}
			if item.Behavior == nil {
				item.Behavior = pointer("domain")
			}
			if item.Format == nil {
				item.Format = pointer("text")
			}
			if item.SourceType == nil {
				kind := "url"
				if text(item.Content) != "" {
					kind = "content"
				}
				item.SourceType = &kind
			}
			result = append(result, resolvedRule{identity, selection.route, id, item})
		}
	}
	order := map[string]int{}
	for i, item := range s.catalog.Items {
		order[item.Identity] = i
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, aok := order[result[i].Identity]
		b, bok := order[result[j].Identity]
		if !aok {
			a = len(order)
		}
		if !bok {
			b = len(order)
		}
		return a < b
	})
	return result, nil
}

// Prepare writes a generated strategy only while this session still owns its
// fields. Unrelated router/receipt revisions and independent base edits survive.
func (s *Service) Prepare(ctx context.Context, id string, raw json.RawMessage, baseURL string) (json.RawMessage, error) {
	return s.change(ctx, id, raw, func(held context.Context, record *Record, fields map[string]json.RawMessage) error {
		return s.prepare(held, record, fields, baseURL)
	})
}
func (s *Service) prepare(ctx context.Context, record *Record, body map[string]json.RawMessage, baseURL string) error {
	if !allowed(body, "dns_fallback", "traffic_fallback", "local_dns", "domain_rules", "template", "query_recorder", "controller_secret") {
		return failure(msgPrepareFields, 400)
	}
	template, valid := readString(body, "template", "basic")
	if !valid || template != "basic" {
		return failure(msgRemovedTemplate, 400)
	}
	dnsFallback, validDNS := readString(body, "dns_fallback", "mihomo")
	trafficFallback, validTraffic := readString(body, "traffic_fallback", "proxy")
	if !validDNS || !validTraffic || dnsFallback != "mihomo" && dnsFallback != "direct" || trafficFallback != "proxy" && trafficFallback != "direct" {
		return failure(msgFallback, 400)
	}
	query := !enabled(record.Prepared)
	if record.QueryRecorder != nil {
		query = *record.QueryRecorder
	}
	query, valid = readBool(body, "query_recorder", query)
	if !valid {
		return failure(msgQueryRecorder, 400)
	}
	if !s.owners.DNS.ready() {
		return unavailable()
	}
	local := s.owners.DNS.DefaultLocalDNS
	if record.LocalDNS != nil {
		local = *record.LocalDNS
	}
	local, valid = readString(body, "local_dns", local)
	if !valid {
		if _, err := s.owners.DNS.LocalUpstreams(""); err != nil {
			return ownerValidation(err)
		}
		return failure(msgLocalDNS, 400)
	}
	if _, err := s.owners.DNS.LocalUpstreams(local); err != nil {
		return ownerValidation(err)
	}
	local = strings.TrimSpace(local)
	selections, err := s.selections(ctx, record, body)
	if err != nil {
		return err
	}
	config, _, err := s.repository.PublishConfig(ctx, record.ProfileID, false)
	if err != nil {
		return err
	}
	configFields, err := encodeFields(config)
	if err != nil {
		return err
	}
	shared, err := s.repository.Shared(ctx)
	if err != nil {
		return err
	}
	sharedFields, err := encodeFields(shared)
	if err != nil {
		return err
	}
	for _, key := range []string{"subscriptions", "nodes", "subscription_aggregations", "rule_library"} {
		configFields[key] = sharedFields[key]
	}
	count, subscriptionNodes, manualNodes, err := resourceNodes(ctx, configFields, record)
	if err != nil {
		return err
	}
	resolved, err := s.resolve(ctx, selections)
	if err != nil {
		return err
	}
	rules := []model.RuleConfig{}
	for i, cidr := range []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		rules = append(rules, model.RuleConfig{ID: fmt.Sprintf("setup_lan_%d", i), ItemType: "rule", RuleType: "IP-CIDR", Value: cidr, Policy: pointer("DIRECT"), NoResolve: pointer(true), Enabled: pointer(true)})
	}
	choices := []ruleChoice{}
	for _, item := range resolved {
		choices = append(choices, ruleChoice{item.Identity, item.Route})
	}
	groups, policies, final, err := s.catalog.strategy(choices, trafficFallback, record.SubscriptionIDs, record.NodeIDs, subscriptionNodes, manualNodes)
	if err != nil {
		return err
	}
	direct, proxy := []string{}, []string{}
	for i, item := range resolved {
		rules = append(rules, model.RuleConfig{ID: item.ID, ItemType: "ruleset", LibraryRuleID: item.Item.ID, Enabled: pointer(true), Policy: pointer(policies[i])})
		if item.Route == "direct" {
			direct = append(direct, item.ID)
		} else {
			proxy = append(proxy, item.ID)
		}
	}
	rules = append(rules, model.RuleConfig{ID: "setup_final", ItemType: "rule", RuleType: "MATCH", Policy: pointer(final), Enabled: pointer(true)})
	stored, err := s.repository.Profile(ctx, record.ProfileID)
	if err != nil {
		return err
	}
	settings, err := gateway.SettingsOf(&model.Config{Profile: stored})
	if err != nil {
		return err
	}
	state, marker := routersplit.ReadMarkerProfile(stored)
	legacy := record.Goal == "router" && state == routersplit.StateAbsent && settings.Enabled
	fullTUN := record.Goal == "router" && !legacy
	dns := oxidns.DefaultSettings()
	for _, setting := range []struct{ key, value string }{{"local_dns", local}, {"remote_dns", s.owners.DNS.DNSTarget}, {"fallback_dns", s.owners.DNS.DNSTarget}} {
		yamlx.Set(dns, setting.key, yamlx.Str(setting.value))
	}
	setStrings := func(key string, values []string) {
		items := []*yamlx.Node{}
		for _, value := range values {
			items = append(items, yamlx.Str(value))
		}
		yamlx.Set(dns, key, yamlx.Seq(items...))
	}
	setStrings("direct_rulesets", direct)
	setStrings("proxy_rulesets", proxy)
	forward := "forward_remote"
	if dnsFallback == "direct" {
		forward = "forward_local"
	}
	yamlx.Set(dns, "default_forward", yamlx.Str(forward))
	yamlx.Set(yamlx.Get(dns, "rule_update"), "enabled", yamlx.Bool(len(resolved) > 0))
	yamlx.Set(yamlx.Get(dns, "query_recorder"), "enabled", yamlx.Bool(query))
	reserved := map[string]string{}
	addReserved := func(value, label string) {
		_, port, err := net.SplitHostPort(value)
		if err == nil {
			reserved[port] = label
		}
	}
	addReserved(s.owners.DNS.DNSTarget, fmt.Sprintf(msgMihomoDNSPort, s.owners.DNS.DNSTarget))
	listen := nodeString(yamlx.Get(dns, "listen"))
	addReserved(listen, fmt.Sprintf(msgOxiDNSPort, listen))
	api := yamlx.Get(dns, "api")
	if yamlx.Equal(yamlx.Get(api, "enabled"), yamlx.Bool(true)) {
		listen = nodeString(yamlx.Get(api, "listen"))
		addReserved(listen, fmt.Sprintf(msgOxiDNSAPIPort, listen))
	}
	mihomo := map[string]json.RawMessage{}
	if raw := configFields["mihomo"]; len(raw) > 0 && json.Unmarshal(raw, &mihomo) != nil {
		return failure(msgBaseMapping, 400)
	}
	if mihomo == nil {
		mihomo = map[string]json.RawMessage{}
	}
	source, valid := readString(mihomo, "custom_config", "")
	if !valid {
		return failure(msgBaseText, 400)
	}
	var secret *string
	if raw, exists := body["controller_secret"]; exists && string(raw) != "null" && string(raw) != `""` && string(raw) != "false" && string(raw) != "0" {
		var entered string
		if json.Unmarshal(raw, &entered) != nil {
			return failure(msgSecretFormat, 400)
		}
		secret = &entered
	}
	base, changes, err := s.mihomoBase(source, fullTUN, reserved, secret)
	if err != nil {
		return err
	}
	mihomo["custom_config"], _ = json.Marshal(base)
	if fullTUN {
		ids := map[string]bool{}
		for _, rule := range rules {
			ids[rule.ID] = true
		}
		keep := []string{}
		markerFields := map[string]json.RawMessage{}
		if state == routersplit.StateValid {
			markerFields, err = encodeFields(marker)
			if err != nil {
				return err
			}
			for _, id := range marker.RouteRuleSetIDs {
				if ids[id] {
					keep = append(keep, id)
				}
			}
		} else {
			markerFields["version"] = json.RawMessage(`1`)
			markerFields["takeover"] = json.RawMessage(`"full_tun"`)
			markerFields["origin"] = json.RawMessage(`"setup"`)
		}
		markerFields["route_rule_set_ids"], _ = json.Marshal(keep)
		mihomo[routersplit.MarkerKey], _ = json.Marshal(markerFields)
		mihomo["linux_gateway"] = json.RawMessage(`{"enabled":false,"rule_set_ids":[]}`)
	}
	fields := map[string]json.RawMessage{}
	fields["proxy_groups"], err = json.Marshal(groups)
	if err != nil {
		return err
	}
	fields["rule_configs"], err = json.Marshal(rules)
	if err != nil {
		return err
	}
	fields["oxidns"], err = yamlx.ToJSON(dns)
	if err != nil {
		return err
	}
	fields["mihomo"], err = json.Marshal(mihomo)
	if err != nil {
		return err
	}
	for key, value := range fields {
		configFields[key] = value
	}
	latestShared, err := s.repository.Shared(ctx)
	if err != nil {
		return err
	}
	configFields["rule_library"], err = json.Marshal(latestShared.RuleLibrary)
	if err != nil {
		return err
	}
	hydrated := append([]model.RuleConfig{}, rules...)
	for i := range hydrated {
		for _, item := range resolved {
			if hydrated[i].ID == item.ID {
				hydrated[i].Name = item.Item.Name
				hydrated[i].URL = item.Item.URL
				hydrated[i].SourceType = item.Item.SourceType
				hydrated[i].Behavior = item.Item.Behavior
				hydrated[i].Format = item.Item.Format
				hydrated[i].Content = item.Item.Content
				if text(item.Item.SourceType) == "content" {
					hydrated[i].URL = pointer("/api/profiles/" + record.ProfileID + "/rule-library/content/" + item.Item.ID)
				}
			}
		}
	}
	configFields["rule_configs"], err = json.Marshal(hydrated)
	if err != nil {
		return err
	}
	candidate, err := boundary(configFields)
	if err != nil {
		return err
	}
	if record.Goal == "gateway" {
		editor, err := gateway.NewConfig(candidate).EditorState(false)
		if err != nil {
			return err
		}
		eligible := map[string]bool{}
		for _, item := range resolved {
			if item.Route == "proxy" && (item.Identity == "builtin:telegram" || item.Identity == "builtin:googlefcm") {
				eligible[item.ID] = true
			}
		}
		takeover := []string{}
		for _, item := range editor.Candidates {
			if item.Available && eligible[item.ID] {
				takeover = append(takeover, item.ID)
			}
		}
		gatewayFields := map[string]json.RawMessage{"enabled": json.RawMessage(`false`)}
		if raw := mihomo["linux_gateway"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &gatewayFields); err != nil {
				return err
			}
		}
		gatewayFields["rule_set_ids"], _ = json.Marshal(takeover)
		mihomo["linux_gateway"], _ = json.Marshal(gatewayFields)
		fields["mihomo"], _ = json.Marshal(mihomo)
		configFields["mihomo"] = fields["mihomo"]
		candidate, err = boundary(configFields)
		if err != nil {
			return err
		}
	}
	storedRaw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	storedFields, err := encodeFields(stored)
	if err != nil {
		return err
	}
	unchanged := true
	for key, value := range fields {
		if !equalJSONValue(storedFields[key], value) {
			unchanged = false
		}
	}
	edited := false
	if enabled(record.Prepared) && !currentDigest(record) {
		return failure(msgForeignEdit, 409)
	}
	if record.OwnedDigest == nil {
		var revision model.Int
		if json.Unmarshal(storedFields["_revision"], &revision) != nil || record.ProfileRevision == nil {
			return ErrCorruptRecord
		}
		edited = revision != *record.ProfileRevision
	} else {
		without := record.OwnedDigestWithoutBase != nil
		digest, err := OwnedDigest(storedRaw, without)
		if err != nil {
			return err
		}
		expected := text(record.OwnedDigest)
		if without {
			expected = text(record.OwnedDigestWithoutBase)
		}
		edited = digest != expected
	}
	if edited && !unchanged {
		return failure(msgForeignEdit, 409)
	}
	if err := validateGenerated(ctx, candidate, baseURL); err != nil {
		return err
	}
	if !unchanged {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err = s.repository.UpdateProfile(ctx, record.ProfileID, func(commit context.Context, current *model.Profile) error {
			now, err := encodeFields(current)
			if err != nil {
				return err
			}
			if !equalJSONValue(now["_revision"], storedFields["_revision"]) {
				return failure(msgProfileChanged, 409)
			}
			for key, value := range fields {
				now[key] = value
			}
			for key, value := range fields {
				if err := current.SetJSON(key, value); err != nil {
					return err
				}
			}
			return commit.Err()
		})
		if err != nil {
			return err
		}
		if record.MaxStep > 3 {
			if err := record.clear("installation", "route_instructions", "router_verification", "config_revision"); err != nil {
				return err
			}
			record.MaxStep = 3
		}
		record.MihomoChanges = &changes
	}
	saved, err := s.repository.Profile(ctx, record.ProfileID)
	if err != nil {
		return err
	}
	savedRaw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	digest, err := OwnedDigest(savedRaw, false)
	if err != nil {
		return err
	}
	without, err := OwnedDigest(savedRaw, true)
	if err != nil {
		return err
	}
	revision, err := s.repository.ConfigRevision(ctx, record.ProfileID)
	if err != nil {
		return err
	}
	parsed, err := yamlx.Parse([]byte(base))
	if err != nil {
		return err
	}
	controller := nodeString(yamlx.Get(parsed, "external-controller"))
	if err := record.clear("template"); err != nil {
		return err
	}
	record.Prepared, record.Completed = pointer(true), pointer(false)
	record.DNSFallback, record.TrafficFallback, record.LocalDNS = &dnsFallback, &trafficFallback, &local
	record.DomainRules, record.QueryRecorder = &selections, &query
	record.NodeCount = pointer(model.Int(count))
	record.ProfileRevision = pointer(model.Int(saved.Revision))
	record.ConfigRevision = pointer(model.Int(revision))
	record.OwnedDigest, record.OwnedDigestWithoutBase = &digest, &without
	record.DigestVersion = pointer(digestVersion)
	record.ControllerSecretSaved, record.ControllerAddress = pointer(true), &controller
	if record.Goal == "router" {
		record.LegacyTakeover = &legacy
	}
	record.Step = 3
	if record.MaxStep < 3 {
		record.MaxStep = 3
	}
	return nil
}

func equalJSONValue(a, b json.RawMessage) bool {
	left, err := canonicalDigestJSON(a)
	if err != nil {
		return false
	}
	right, err := canonicalDigestJSON(b)
	return err == nil && bytes.Equal(left, right)
}
