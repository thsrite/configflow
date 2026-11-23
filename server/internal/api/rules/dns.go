package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/oxidns"
)

type routeChoice struct {
	id     string
	choice member[string]
}

// routeChoices is a JSON object of rule ID to DNS choice, in sent order; a
// repeated ID keeps its first position and its last choice.
type routeChoices struct {
	list []routeChoice
	err  error
}

// UnmarshalJSON records the choices; a malformed object is reported by the
// handler, never by decoding.
func (r *routeChoices) UnmarshalJSON(data []byte) error {
	r.list, r.err = orderedChoices(data)
	return nil
}

// dnsWriteRequest is the body of a DNS change.
type dnsWriteRequest struct {
	Routes          member[routeChoices] `json:"routes"`
	SkipUnsupported member[bool]         `json:"skip_unsupported"`
	BaseURL         member[string]       `json:"base_url"`
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
		var value member[string]
		if err := decoder.Decode(&value); err != nil {
			return nil, invalid(msgDNSRoutes)
		}
		name, ok := key.(string)
		if !ok {
			return nil, invalid(msgDNSRoutes)
		}
		if i, present := positions[name]; present {
			out[i].choice = value
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
func (s *Service) SetDNS(ctx context.Context, id string, body *dnsWriteRequest, base string) (dnsWriteResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := dnsWriteResult{Success: true, Rules: map[string]oxidns.RuleDNSRule{}, Skipped: []string{}, Warnings: []string{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if body == nil || !body.Routes.Present() {
		return out, invalid(msgDNSRoutes)
	}
	routes := body.Routes.Value
	if body.Routes.Null() {
		routes.err = invalid(msgDNSRoutes)
	}
	if routes.err != nil {
		return out, routes.err
	}
	choices := routes.list
	if len(choices) > 200 {
		return out, invalid(msgDNSLimit)
	}
	assignments := []oxidns.DNSAssignment{}
	for _, entry := range choices {
		choice, err := dnsChoice(entry.choice)
		if err != nil {
			return out, err
		}
		assignments = append(assignments, oxidns.DNSAssignment{RuleID: entry.id, Choice: choice})
	}
	skip := isTrue(body.SkipUnsupported)
	changed := []string{}
	err := editProfile(ctx, id, func(config *model.Config) error {
		before, err := profileView(config)
		if err != nil {
			return err
		}
		items := config.Profile.RuleConfigs
		accepted := []oxidns.DNSAssignment{}
		changed = changed[:0]
		for _, entry := range assignments {
			index := find(items, entry.RuleID)
			if skip && index >= 0 && entry.Choice != "default" && !oxidns.Convertible(items[index]) {
				out.Skipped = append(out.Skipped, entry.RuleID)
				continue
			}
			accepted = append(accepted, entry)
			changed = append(changed, entry.RuleID)
		}
		out.Warnings, err = syncDNS(before, config, accepted, base, true)
		return err
	})
	if err != nil {
		return out, err
	}
	config, err := loadProfile(ctx, id)
	if err != nil {
		return out, err
	}
	p, err := dnsProfile(config)
	if err != nil {
		return out, err
	}
	out.Rules, err = oxidns.RuleDNSEntries(p, changed, oxidns.RuleDNSReadOptions{})
	if err == nil {
		out.PinnedWithheld = s.pinned(ctx, id, changed)
	}
	return out, dnsError(err)
}
