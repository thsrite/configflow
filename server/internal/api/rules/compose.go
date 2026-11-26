package rules

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
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

type skippedRule struct {
	Line    int    `json:"line"`
	Value   string `json:"value"`
	ID      string `json:"existing_id"`
	Index   int    `json:"index"`
	Enabled bool   `json:"enabled"`
}
type composeData struct {
	Rules   []model.RuleConfig     `json:"rules"`
	Library *model.RuleLibraryItem `json:"library"`
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

// composeRequest is the one-page rule form: a source, the rule fields every
// added rule shares, and the DNS choice for domain (and IP) rules.
type composeRequest struct {
	Source  member[composeSource] `json:"source"`
	Rule    member[composeRule]   `json:"rule"`
	DNS     member[string]        `json:"dns"`
	DNSIP   member[string]        `json:"dns_ip"`
	BaseURL member[string]        `json:"base_url"`
}

// composeSource is one of: pasted values, existing shared libraries or a new
// shared library (decoded by prepareLibrary).
type composeSource struct {
	Kind           member[string]   `json:"kind"`
	Text           member[string]   `json:"text"`
	RuleType       member[string]   `json:"rule_type"`
	LibraryRuleIDs member[[]string] `json:"library_rule_ids"`
	Library        json.RawMessage  `json:"library"`
}
type composeRule struct {
	Policy    member[string] `json:"policy"`
	Position  member[string] `json:"position"`
	Enabled   member[bool]   `json:"enabled"`
	Remark    member[string] `json:"remark"`
	GroupName member[string] `json:"group_name"`
	NoResolve member[bool]   `json:"no_resolve"`
}

// isTrue and notFalse read a sent switch: only JSON true is on; only JSON false is off.
func isTrue(sent member[bool]) bool   { return sent.OK && sent.Value }
func notFalse(sent member[bool]) bool { return !sent.OK || sent.Value }

// Compose atomically creates rules, shared sources and DNS choices. Remote
// sources are validated before the transaction; cache failures after it do not
// turn an accepted save into a failure.
func (s *Service) Compose(ctx context.Context, id string, body *composeRequest, base string) (composeResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := composeResult{Success: true, Data: composeData{Rules: []model.RuleConfig{}}, DNS: map[string]oxidns.RuleDNSRule{}, Skipped: []skippedRule{}, Warnings: []string{}, Draft: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if body == nil {
		return out, invalid(msgComposeObject)
	}
	source, rule := body.Source.Value, body.Rule.Value
	kind := source.Kind.Value
	sentDNS := body.DNS
	if !sentDNS.Present() {
		sentDNS = okMember("default")
	}
	dns, err := dnsChoice(sentDNS)
	if err != nil {
		return out, err
	}
	dnsIP := dns
	if !body.DNSIP.Null() {
		dnsIP, err = dnsChoice(body.DNSIP)
		if err != nil {
			return out, err
		}
	}
	policy := rule.Policy.Value
	if !rule.Policy.OK || strings.TrimSpace(policy) == "" {
		return out, invalid(msgPolicy)
	}
	position := rule.Position.Or("top")
	if position != "top" && position != "bottom" {
		return out, invalid(msgPosition)
	}
	common := model.RuleConfig{Policy: new(policy), Enabled: new(notFalse(rule.Enabled)), Remark: new(rule.Remark.Value), GroupName: new(rule.GroupName.Value)}
	rows := []pasteRow{}
	libraryIDs := []string{}
	var library *libraryInput
	switch kind {
	case "values":
		if !source.Text.OK {
			return out, invalid(msgPaste)
		}
		rows, out.Warnings, err = pasteRows(source.Text.Value, strings.ToUpper(strings.TrimSpace(source.RuleType.Value)))
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
			if choice != "default" && !oxidns.Convertible(model.RuleConfig{ItemType: "rule", RuleType: row.Kind}) {
				return out, invalid(fmt.Sprintf(msgUnsupportedLineDNS, row.Line, row.Kind))
			}
		}
	case "library":
		libraryIDs = source.LibraryRuleIDs.Value
		if !source.LibraryRuleIDs.OK || len(libraryIDs) < 1 || len(libraryIDs) > 50 {
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
		library, err = prepareLibrary(source.Library)
		if err != nil {
			return out, err
		}
	default:
		return out, invalid(msgRuleSourceKind)
	}
	var prepared []byte
	if library != nil && library.remote() {
		prepared, err = s.prepareRemote(ctx, library.item)
		if err != nil {
			return out, err
		}
		if prepared == nil {
			return out, invalid(msgInvalidSource)
		}
	}
	edit := func(before *oxidns.Profile, config *model.Config) error {
		if !knownPolicy(config, policy) {
			return invalid(fmt.Sprintf(msgPolicyUnknown, policy))
		}
		items := config.Profile.RuleConfigs
		used := map[string]bool{}
		for _, item := range items {
			used[item.ID] = true
		}
		added := []model.RuleConfig{}
		assignments := []oxidns.DNSAssignment{}
		if kind == "values" {
			type key struct{ kind, value string }
			type found struct {
				index int
				item  model.RuleConfig
			}
			existing := map[key]found{}
			for i, item := range items {
				if item.ItemType == "ruleset" {
					continue
				}
				k := key{item.RuleType, text(item.Value)}
				if _, exists := existing[k]; !exists {
					existing[k] = found{i + 1, item}
				}
			}
			for _, row := range rows {
				if match, ok := existing[key{row.Kind, row.Value}]; ok {
					out.Skipped = append(out.Skipped, skippedRule{row.Line, row.Value, match.item.ID, match.index, model.Flag(match.item.Enabled, true)})
					continue
				}
				identity, err := newID("rule", used)
				if err != nil {
					return err
				}
				item := common
				item.ID = identity
				item.ItemType = "rule"
				item.RuleType = row.Kind
				item.Value = new(row.Value)
				item.NoResolve = new(row.NoResolve || (oxidns.IsIPRuleType(row.Kind) && isTrue(rule.NoResolve)))
				added = append(added, item)
				choice := dns
				if oxidns.IsIPRuleType(row.Kind) {
					choice = dnsIP
				}
				assignments = append(assignments, oxidns.DNSAssignment{RuleID: identity, Choice: choice})
			}
		} else {
			selected := libraryIDs
			if library != nil {
				selected = []string{library.item.ID}
			}
			for _, identity := range selected {
				if !slices.ContainsFunc(config.RuleLibrary, func(entry model.RuleLibraryItem) bool { return entry.ID == identity }) {
					return missing(msgLibraryReference)
				}
				for i, item := range items {
					if item.ItemType == "ruleset" && item.LibraryRuleID == identity {
						return libraryInUse(item.ID, i+1)
					}
				}
				id, err := newID("ruleset", used)
				if err != nil {
					return err
				}
				item := common
				item.ID = id
				item.ItemType = "ruleset"
				item.LibraryRuleID = identity
				item.NoResolve = new(isTrue(rule.NoResolve))
				added = append(added, item)
				assignments = append(assignments, oxidns.DNSAssignment{RuleID: id, Choice: dns})
			}
		}
		converted := []model.RuleConfig{}
		capable := map[string]bool{}
		for _, item := range added {
			out.Data.Rules = append(out.Data.Rules, item)
			next, err := hydrated(config, item)
			if err != nil {
				return err
			}
			converted = append(converted, next)
			capable[next.ID] = oxidns.Convertible(next)
		}
		if position == "top" {
			items = append(converted, items...)
		} else {
			items = append(slices.Clone(items), converted...)
		}
		config.Profile.RuleConfigs = items
		selected := []oxidns.DNSAssignment{}
		for _, assignment := range assignments {
			if assignment.Choice != "default" && capable[assignment.RuleID] {
				selected = append(selected, assignment)
			}
		}
		if len(selected) > 0 {
			warnings, err := syncDNS(before, config, selected, base, true)
			if err != nil {
				return err
			}
			out.Warnings = append(out.Warnings, warnings...)
		}
		return nil
	}
	if library == nil {
		err = s.editProfile(ctx, id, func(config *model.Config) error {
			before, err := profileView(config)
			if err != nil {
				return err
			}
			return edit(before, config)
		})
	} else {
		err = s.editSharedProfile(ctx, id, func(shared *model.Shared) error {
			used := map[string]bool{}
			for _, item := range shared.RuleLibrary {
				if item.Name == library.item.Name {
					return invalid(fmt.Sprintf(msgLibraryExists, library.item.Name))
				}
				used[item.ID] = true
			}
			identity, err := newID("lib", used)
			if err != nil {
				return err
			}
			library.item.ID = identity
			entries := append(slices.Clone(shared.RuleLibrary), library.item)
			if library.err != nil {
				return libraryTypeError(entries, library.err)
			}
			shared.RuleLibrary = entries
			return nil
		}, edit)
	}
	if err != nil {
		return out, err
	}
	if library != nil {
		out.Data.Library = new(library.item)
		if err := s.cacheComposed(ctx, library.item, prepared); err != nil {
			logger.Warning(msgComposedCacheLog)
			out.Warnings = append(out.Warnings, msgComposedCacheWarning)
		}
	}
	config, err := s.loadProfile(ctx, id)
	if err != nil {
		return out, err
	}
	p, err := dnsProfile(config)
	if err != nil {
		return out, err
	}
	ids := []string{}
	for _, item := range out.Data.Rules {
		ids = append(ids, item.ID)
	}
	out.DNS, err = oxidns.RuleDNSEntries(p, ids, oxidns.RuleDNSReadOptions{})
	if err == nil {
		out.PinnedWithheld = s.pinned(ctx, id, ids)
	}
	return out, dnsError(err)
}
