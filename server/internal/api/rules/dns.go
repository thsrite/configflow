package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"configflow/internal/lifecycle"
	"configflow/internal/oxidns"
)

type routeChoice struct {
	id  string
	raw json.RawMessage
}

func orderedChoices(raw json.RawMessage) ([]routeChoice, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid(msgDNSRoutes)
	}
	out := []routeChoice{}
	positions := map[string]int{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, invalid(msgDNSRoutes)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, invalid(msgDNSRoutes)
		}
		name, ok := key.(string)
		if !ok {
			return nil, invalid(msgDNSRoutes)
		}
		if i, present := positions[name]; present {
			out[i].raw = value
		} else {
			positions[name] = len(out)
			out = append(out, routeChoice{name, value})
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, invalid(msgDNSRoutes)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalid(msgDNSRoutes)
	}
	if len(out) == 0 {
		return nil, invalid(msgDNSRoutes)
	}
	return out, nil
}

type dnsReadResult struct {
	Success bool `json:"success"`
	oxidns.RuleDNSState
}
type dnsWriteResult struct {
	Success        bool                          `json:"success"`
	Rules          map[string]oxidns.RuleDNSRule `json:"rules"`
	Skipped        []string                      `json:"skipped"`
	Warnings       []string                      `json:"warnings"`
	PinnedWithheld *bool                         `json:"pinned_withheld,omitempty"`
}

// DNSState returns the rule page's current OxiDNS and MosDNS routing read model.
func (s *Service) DNSState(ctx context.Context, id, base string) (dnsReadResult, error) {
	profile, err := loadProfile(ctx, id)
	if err != nil {
		return dnsReadResult{}, err
	}
	p, err := dnsProfile(profile)
	if err != nil {
		return dnsReadResult{}, err
	}
	state, err := oxidns.ReadRuleDNS(p, oxidns.RuleDNSReadOptions{})
	return dnsReadResult{true, state}, dnsError(err)
}

// SetDNS changes up to 200 explicit per-rule DNS choices in one transaction.
func (s *Service) SetDNS(ctx context.Context, id string, body record, base string) (dnsWriteResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := dnsWriteResult{Success: true, Rules: map[string]oxidns.RuleDNSRule{}, Skipped: []string{}, Warnings: []string{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	choices, err := orderedChoices(body["routes"])
	if err != nil {
		return out, err
	}
	if len(choices) > 200 {
		return out, invalid(msgDNSLimit)
	}
	assignments := []oxidns.DNSAssignment{}
	for _, entry := range choices {
		choice, err := dnsChoice(entry.raw)
		if err != nil {
			return out, err
		}
		assignments = append(assignments, oxidns.DNSAssignment{RuleID: entry.id, Choice: choice})
	}
	changed := []string{}
	err = editProfile(ctx, id, func(p record) error {
		before := p.clone()
		items, err := p.list("rule_configs")
		if err != nil {
			return err
		}
		accepted := []oxidns.DNSAssignment{}
		for _, entry := range assignments {
			index := find(items, entry.RuleID)
			if exactTrue(body["skip_unsupported"]) && index >= 0 && entry.Choice != "default" {
				raw, err := json.Marshal(items[index])
				if err != nil {
					return err
				}
				node, err := oxidns.FromJSON(raw)
				if err != nil {
					return err
				}
				if !oxidns.Convertible(node) {
					out.Skipped = append(out.Skipped, entry.RuleID)
					continue
				}
			}
			accepted = append(accepted, entry)
			changed = append(changed, entry.RuleID)
		}
		out.Warnings, err = syncDNS(before, p, accepted, base, true)
		return err
	})
	if err != nil {
		return out, err
	}
	profile, err := loadProfile(ctx, id)
	if err != nil {
		return out, err
	}
	p, err := dnsProfile(profile)
	if err != nil {
		return out, err
	}
	out.Rules, err = oxidns.RuleDNSEntries(p, changed, oxidns.RuleDNSReadOptions{})
	if err == nil {
		out.PinnedWithheld = s.pinned(ctx, id, changed)
	}
	return out, dnsError(err)
}
