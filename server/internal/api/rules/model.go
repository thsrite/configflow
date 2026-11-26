// Package rules serves profile rule composition, DNS choices and public cached
// providers. Shared syntax, fetching, matching and routing remain with their owners.
package rules

import (
	"encoding/json"
	"errors"
	"reflect"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

// member is one request member; see jsondecode.Member.
type member[T any] = jsondecode.Member[T]

// ruleWire is a rule without the stored record's lenient codec: decoding a
// request into a copy of the stored rule replaces exactly the members sent.
type ruleWire model.RuleConfig

// ruleInput is a rule (or rule-set reference) sent by the rules page, decoded
// onto a base rule: members that were not sent keep the base value. The
// members validation reads, replaces or drops are kept as sent (shadowing
// ruleWire's): a message may quote a rejected value, and a dropped member's
// type does not matter.
type ruleInput struct {
	ruleWire
	ID             member[string] `json:"id"`
	ItemType       member[string] `json:"itemType"`
	LibraryRuleID  member[string] `json:"library_rule_id"`
	RuleType       member[string] `json:"rule_type"`
	Value          member[string] `json:"value"`
	Policy         member[string] `json:"policy"`
	UDPFallback    member[string] `json:"udp_fallback"`
	NTPDirect      member[bool]   `json:"ntp_direct"`
	Name           member[string] `json:"name"`
	URL            member[string] `json:"url"`
	Content        member[string] `json:"content"`
	SourceType     member[string] `json:"source_type"`
	Behavior       member[string] `json:"behavior"`
	Format         member[string] `json:"format"`
	LibraryEnabled member[bool]   `json:"library_enabled"`
	// err is the first other member of a type the rule record cannot hold.
	err error
}

// decodeRule decodes a rule request onto base.
func decodeRule(data []byte, base model.RuleConfig) *ruleInput {
	in := &ruleInput{ruleWire: ruleWire(base)}
	detach(&in.ruleWire)
	in.err = jsondecode.Unmarshal(data, in)
	return in
}

// detach gives every pointer member of a decoding target its own copy:
// decoding into a pointer member writes through it, and the stored rule the
// target was copied from must not change.
func detach[T any](target *T) {
	value := reflect.ValueOf(target).Elem()
	for i := range value.NumField() {
		field := value.Field(i)
		if field.Kind() == reflect.Pointer && !field.IsNil() {
			own := reflect.New(field.Elem().Type())
			own.Elem().Set(field.Elem())
			field.Set(own)
		}
	}
}

// sentText is a text member as sent, or the base value when it was not sent
// (an empty base value counts as absent, as the stored record omits it).
func sentText(sent member[string], base string) member[string] {
	if sent.Present() || base == "" {
		return sent
	}
	return okMember(base)
}

// sentOptional is sentText for an optional base value.
func sentOptional[T any](sent member[T], base *T) member[T] {
	if sent.Present() || base == nil {
		return sent
	}
	return okMember(*base)
}

func okMember[T any](value T) member[T] {
	raw, _ := json.Marshal(value)
	return member[T]{Raw: raw, Value: value, OK: true}
}

// errMemberType marks a sent rule member the rule record cannot hold.
var errMemberType = errors.New("rule member of another type")

// rule returns the rule with the sent members applied.
func (in *ruleInput) rule() (model.RuleConfig, error) {
	out := model.RuleConfig(in.ruleWire)
	err := in.err
	fail := func() {
		if err == nil {
			err = errMemberType
		}
	}
	text := func(sent member[string], target *string) {
		switch {
		case !sent.Present():
		case sent.OK:
			*target = sent.Value
		case sent.Null():
			*target = ""
		default:
			fail()
		}
	}
	optional := func(sent member[string], target **string, nullValue *string) {
		switch {
		case !sent.Present():
		case sent.OK:
			*target = new(sent.Value)
		case sent.Null():
			*target = nullValue
		default:
			fail()
		}
	}
	flag := func(sent member[bool], target **bool) {
		switch {
		case !sent.Present():
		case sent.OK:
			*target = new(sent.Value)
		case sent.Null():
			*target = nil
		default:
			fail()
		}
	}
	text(in.ID, &out.ID)
	text(in.ItemType, &out.ItemType)
	text(in.LibraryRuleID, &out.LibraryRuleID)
	text(in.RuleType, &out.RuleType)
	text(in.Name, &out.Name)
	optional(in.Value, &out.Value, nil)
	// The stored record reads a null policy or behavior as an empty value.
	optional(in.Policy, &out.Policy, new(""))
	optional(in.Behavior, &out.Behavior, new(""))
	optional(in.UDPFallback, &out.UDPFallback, nil)
	optional(in.URL, &out.URL, nil)
	optional(in.Content, &out.Content, nil)
	optional(in.SourceType, &out.SourceType, nil)
	optional(in.Format, &out.Format, nil)
	flag(in.NTPDirect, &out.NTPDirect)
	flag(in.LibraryEnabled, &out.LibraryEnabled)
	return out, err
}

type mutationResult struct {
	Success        bool              `json:"success"`
	Data           *model.RuleConfig `json:"data,omitempty"`
	Warnings       []string          `json:"warnings,omitempty"`
	PinnedWithheld *bool             `json:"pinned_withheld,omitempty"`
}
type fault struct {
	status  int
	message string
}

func (e *fault) Error() string     { return e.message }
func missing(message string) error { return &fault{404, message} }

func text(value *string) string { return model.TextValue(value) }

func find(items []model.RuleConfig, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

// orderRequest selects rules by id, or (legacy clients) by the ids of a
// rule_configs list. Identifiers are any JSON values: only text names a rule.
type orderRequest struct {
	IDs         member[[]json.RawMessage] `json:"ids"`
	RuleConfigs member[[]*struct {
		ID json.RawMessage `json:"id"`
	}] `json:"rule_configs"`
	Position member[string] `json:"position"`
	BaseURL  member[string] `json:"base_url"`
}

func (r *orderRequest) selection() ([]json.RawMessage, error) {
	if r.IDs.Null() {
		if !r.RuleConfigs.OK {
			return nil, invalid(msgOrderObjects)
		}
		ids := make([]json.RawMessage, len(r.RuleConfigs.Value))
		for i, item := range r.RuleConfigs.Value {
			if item == nil {
				return nil, invalid(msgOrderObjects)
			}
			ids[i] = item.ID
			if len(ids[i]) == 0 {
				ids[i] = json.RawMessage("null")
			}
		}
		return ids, nil
	}
	if !r.IDs.OK {
		return nil, invalid(msgOrderIDs)
	}
	return r.IDs.Value, nil
}
