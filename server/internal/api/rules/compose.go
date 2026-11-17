package rules

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/oxidns"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
)

type pasteRow struct {
	Line        int
	Kind, Value string
	NoResolve   bool
}
type fieldError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}
type pasteError struct{ Fields []fieldError }

func (e *pasteError) Error() string {
	return fmt.Sprintf(msgLineError, e.Fields[0].Line, e.Fields[0].Message)
}
func pasteRows(text, defaultKind string) ([]pasteRow, []string, error) {
	type inputLine struct {
		number int
		text   string
	}
	lines := []inputLine{}
	for i, raw := range rulefetch.RuleLines(text) {
		line := strings.TrimSpace(raw)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "//") {
			lines = append(lines, inputLine{i + 1, line})
		}
	}
	if len(lines) > 200 {
		return nil, nil, invalid(msgPasteLimit)
	}
	rows := []pasteRow{}
	warnings := []string{}
	fields := []fieldError{}
	type identity struct{ kind, value string }
	seen := map[identity]bool{}
	for _, line := range lines {
		row := pasteRow{Line: line.number, Kind: defaultKind, Value: line.text}
		text := line.text
		if strings.Contains(text, ",") {
			parts := strings.Split(text, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			row.Kind = strings.ToUpper(parts[0])
			row.Value = parts[1]
			extras := parts[2:]
			if shared.AllowsCommaPayload(row.Kind) {
				_, payload, _ := strings.Cut(text, ",")
				row.Value = strings.TrimSpace(payload)
				extras = nil
			} else if row.Kind == "MATCH" {
				row.Value = ""
				extras = parts[1:]
			}
			for _, extra := range extras {
				if strings.EqualFold(extra, "no-resolve") {
					row.NoResolve = true
				} else if extra != "" {
					warnings = append(warnings, fmt.Sprintf(msgIgnorePolicy, row.Line, extra))
				}
			}
		} else if strings.HasPrefix(text, "+.") {
			row.Kind = "DOMAIN-SUFFIX"
			row.Value = strings.TrimSpace(text[2:])
		} else if strings.HasPrefix(text, "*.") {
			row.Kind = "DOMAIN-SUFFIX"
			row.Value = strings.TrimSpace(text[2:])
			warnings = append(warnings, fmt.Sprintf(msgWildcard, text, row.Value))
		} else if len(text) >= 5 && strings.EqualFold(text[:5], "full:") {
			row.Kind = "DOMAIN"
			row.Value = strings.TrimSpace(text[5:])
		}
		if message := shared.RuleValueError(row.Kind, row.Value); message != "" {
			fields = append(fields, fieldError{row.Line, message})
			continue
		}
		key := identity{row.Kind, row.Value}
		if !seen[key] {
			seen[key] = true
			rows = append(rows, row)
		}
	}
	if len(fields) > 0 {
		return rows, warnings, &pasteError{fields}
	}
	return rows, warnings, nil
}
func newID(prefix string, used map[string]bool) (string, error) {
	for {
		var bytes [6]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return "", fmt.Errorf("generate rule identity: %w", err)
		}
		id := prefix + "_" + hex.EncodeToString(bytes[:])
		if !used[id] {
			used[id] = true
			return id, nil
		}
	}
}
func nodeOf(value record) (*oxidns.Node, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return oxidns.FromJSON(raw)
}

type skippedRule struct {
	Line    int    `json:"line"`
	Value   string `json:"value"`
	ID      string `json:"existing_id"`
	Index   int    `json:"index"`
	Enabled bool   `json:"enabled"`
}
type composeData struct {
	Rules   []record `json:"rules"`
	Library record   `json:"library"`
}
type composeResult struct {
	Success        bool                          `json:"success"`
	Data           composeData                   `json:"data"`
	DNS            map[string]oxidns.RuleDNSRule `json:"dns"`
	Skipped        []skippedRule                 `json:"skipped"`
	Warnings       []string                      `json:"warnings"`
	Draft          bool                          `json:"draft"`
	PinnedWithheld *bool                         `json:"pinned_withheld,omitempty"`
}

// Compose atomically creates rules, shared sources and DNS choices. Remote
// sources are validated before the transaction; cache failures after it do not
// turn an accepted save into a failure.
func (s *Service) Compose(ctx context.Context, id string, body record, base string) (composeResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := composeResult{Success: true, Data: composeData{Rules: []record{}}, DNS: map[string]oxidns.RuleDNSRule{}, Skipped: []skippedRule{}, Warnings: []string{}, Draft: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if body == nil {
		return out, invalid(msgComposeObject)
	}
	source := body.object("source")
	rule := body.object("rule")
	kind := source.text("kind")
	rawDNS := body["dns"]
	if len(rawDNS) == 0 {
		rawDNS = stringJSON("default")
	}
	dns, err := dnsChoice(rawDNS)
	if err != nil {
		return out, err
	}
	dnsIP := dns
	if raw := body["dns_ip"]; len(raw) > 0 && string(raw) != "null" {
		dnsIP, err = dnsChoice(raw)
		if err != nil {
			return out, err
		}
	}
	policy, ok := textValue(rule["policy"])
	if !ok || strings.TrimSpace(policy) == "" {
		return out, invalid(msgPolicy)
	}
	position := defaultText(rule, "position", "top")
	if position != "top" && position != "bottom" {
		return out, invalid(msgPosition)
	}
	common := record{"policy": stringJSON(policy), "enabled": boolJSON(notFalse(rule["enabled"])), "remark": stringJSON(rule.text("remark")), "group_name": stringJSON(rule.text("group_name"))}
	rows := []pasteRow{}
	libraryIDs := []string{}
	var library record
	switch kind {
	case "values":
		text, ok := textValue(source["text"])
		if !ok {
			return out, invalid(msgPaste)
		}
		rows, out.Warnings, err = pasteRows(text, strings.ToUpper(strings.TrimSpace(source.text("rule_type"))))
		if err != nil {
			return out, err
		}
		if len(rows) == 0 {
			return out, invalid(msgNoRules)
		}
		for _, row := range rows {
			choice := dns
			if oxidns.IsIPRuleType(row.Kind) {
				choice = dnsIP
			}
			n, _ := nodeOf(record{"itemType": stringJSON("rule"), "rule_type": stringJSON(row.Kind)})
			if choice != "default" && !oxidns.Convertible(n) {
				return out, invalid(fmt.Sprintf(msgUnsupportedLineDNS, row.Line, row.Kind))
			}
		}
	case "library":
		if json.Unmarshal(source["library_rule_ids"], &libraryIDs) != nil || len(libraryIDs) < 1 || len(libraryIDs) > 50 {
			return out, invalid(msgSelectLibraries)
		}
		seen := map[string]bool{}
		for _, id := range libraryIDs {
			if id == "" || seen[id] {
				return out, invalid(msgSelectLibraries)
			}
			seen[id] = true
		}
	case "new_library":
		library, err = prepareLibrary(source["library"])
		if err != nil {
			return out, err
		}
	default:
		return out, invalid(msgRuleSourceKind)
	}
	var prepared []byte
	if library != nil && defaultText(library, "source_type", "url") == "url" {
		prepared, err = s.prepareRemote(ctx, library)
		if err != nil {
			return out, err
		}
		if prepared == nil {
			return out, invalid(msgInvalidSource)
		}
	}
	edit := func(before, p record) error {
		if !knownPolicy(p, policy) {
			return invalid(fmt.Sprintf(msgPolicyUnknown, policy))
		}
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		used := map[string]bool{}
		for _, item := range items {
			used[item.text("id")] = true
		}
		added := []record{}
		assignments := []oxidns.DNSAssignment{}
		if kind == "values" {
			type key struct{ kind, value string }
			type found struct {
				index int
				item  record
			}
			existing := map[key]found{}
			for i, item := range items {
				if item.text("itemType") == "ruleset" {
					continue
				}
				k := key{item.text("rule_type"), item.text("value")}
				if _, exists := existing[k]; !exists {
					existing[k] = found{i + 1, item}
				}
			}
			for _, row := range rows {
				if match, ok := existing[key{row.Kind, row.Value}]; ok {
					out.Skipped = append(out.Skipped, skippedRule{row.Line, row.Value, match.item.text("id"), match.index, notFalse(match.item["enabled"])})
					continue
				}
				identity, err := newID("rule", used)
				if err != nil {
					return err
				}
				item := common.clone()
				item["id"] = stringJSON(identity)
				item["itemType"] = stringJSON("rule")
				item["rule_type"] = stringJSON(row.Kind)
				item["value"] = stringJSON(row.Value)
				item["no_resolve"] = boolJSON(row.NoResolve || (oxidns.IsIPRuleType(row.Kind) && exactTrue(rule["no_resolve"])))
				added = append(added, item)
				choice := dns
				if oxidns.IsIPRuleType(row.Kind) {
					choice = dnsIP
				}
				assignments = append(assignments, oxidns.DNSAssignment{RuleID: identity, Choice: choice})
			}
		} else {
			libs, err := p.list("rule_library")
			if err != nil {
				return err
			}
			selected := libraryIDs
			if library != nil {
				selected = []string{library.text("id")}
			}
			for _, identity := range selected {
				if find(libs, identity) < 0 {
					return missing(msgLibraryReference)
				}
				for i, item := range items {
					if item.text("itemType") == "ruleset" && item.text("library_rule_id") == identity {
						return libraryInUse(item.text("id"), i+1)
					}
				}
				id, err := newID("ruleset", used)
				if err != nil {
					return err
				}
				item := common.clone()
				item["id"] = stringJSON(id)
				item["itemType"] = stringJSON("ruleset")
				item["library_rule_id"] = stringJSON(identity)
				item["no_resolve"] = boolJSON(exactTrue(rule["no_resolve"]))
				added = append(added, item)
				assignments = append(assignments, oxidns.DNSAssignment{RuleID: id, Choice: dns})
			}
		}
		converted := []record{}
		capable := map[string]bool{}
		for _, item := range added {
			out.Data.Rules = append(out.Data.Rules, item.clone())
			next, err := hydrated(p, item)
			if err != nil {
				return err
			}
			converted = append(converted, next)
			n, err := nodeOf(next)
			if err != nil {
				return err
			}
			capable[next.text("id")] = oxidns.Convertible(n)
		}
		if position == "top" {
			items = append(converted, items...)
		} else {
			items = append(items, converted...)
		}
		if err := p.set("rule_configs", items); err != nil {
			return err
		}
		selected := []oxidns.DNSAssignment{}
		for _, assignment := range assignments {
			if assignment.Choice != "default" && capable[assignment.RuleID] {
				selected = append(selected, assignment)
			}
		}
		if len(selected) > 0 {
			warnings, err := syncDNS(before, p, selected, base, true)
			if err != nil {
				return err
			}
			out.Warnings = append(out.Warnings, warnings...)
		}
		return nil
	}
	if library == nil {
		err = editProfile(ctx, id, func(p record) error { return edit(p.clone(), p) })
	} else {
		err = editSharedProfile(ctx, id, func(shared record) error {
			entries, err := shared.list("rule_library")
			if err != nil {
				return err
			}
			used := map[string]bool{}
			for _, item := range entries {
				if item.text("name") == library.text("name") {
					return invalid(fmt.Sprintf(msgLibraryExists, library.text("name")))
				}
				used[item.text("id")] = true
			}
			identity, err := newID("lib", used)
			if err != nil {
				return err
			}
			library["id"] = stringJSON(identity)
			return shared.set("rule_library", append(entries, library.clone()))
		}, edit)
	}
	if err != nil {
		return out, err
	}
	out.Data.Library = library
	if library != nil {
		if err := s.cacheComposed(ctx, library, prepared); err != nil {
			logger.Warning(msgComposedCacheLog)
			out.Warnings = append(out.Warnings, msgComposedCacheWarning)
		}
	}
	profile, err := loadProfile(ctx, id)
	if err != nil {
		return out, err
	}
	p, err := dnsProfile(profile)
	if err != nil {
		return out, err
	}
	ids := []string{}
	for _, item := range out.Data.Rules {
		ids = append(ids, item.text("id"))
	}
	out.DNS, err = oxidns.RuleDNSEntries(p, ids, oxidns.RuleDNSReadOptions{})
	if err == nil {
		out.PinnedWithheld = s.pinned(ctx, id, ids)
	}
	return out, dnsError(err)
}
