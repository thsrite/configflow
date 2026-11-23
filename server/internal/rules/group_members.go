package rules

import (
	"bytes"
	"encoding/json/jsontext"
	"reflect"
	"slices"
	"strings"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

// GroupMembers are the policy group members whose sent JSON the group checks
// read: a plain group record cannot hold a member of the wrong type (O18 T1c),
// so a request is checked before it is decoded. Element members of a list
// keep their own sent JSON.
type GroupMembers struct {
	ManualNodes             jsondecode.Member[[]jsondecode.Member[string]] `json:"manual_nodes"`
	Subscriptions           jsondecode.Member[[]jsondecode.Member[string]] `json:"subscriptions"`
	Aggregations            jsondecode.Member[[]jsondecode.Member[string]] `json:"aggregations"`
	IncludeGroups           jsondecode.Member[[]jsondecode.Member[string]] `json:"include_groups"`
	Proxies                 jsondecode.Member[[]jsondecode.Member[string]] `json:"proxies"`
	ProxiesOrder            jsondecode.Member[[]OrderEntry]                `json:"proxies_order"`
	FollowGroup             jsondecode.Member[string]                      `json:"follow_group"`
	Source                  jsondecode.Member[string]                      `json:"source"`
	Regex                   jsondecode.Member[string]                      `json:"regex"`
	AggregationRegex        jsondecode.Member[string]                      `json:"aggregation_regex"`
	ExcludeRegex            jsondecode.Member[string]                      `json:"exclude_regex"`
	AggregationExcludeRegex jsondecode.Member[string]                      `json:"aggregation_exclude_regex"`
}

// OrderEntry is one proxies_order entry as sent: an object of exactly the
// members type and id.
type OrderEntry struct {
	Object   bool
	Members  int
	Type, ID jsondecode.Member[string]
}

// UnmarshalJSON records the entry's shape; it never returns an error.
func (e *OrderEntry) UnmarshalJSON(data []byte) error {
	*e = OrderEntry{}
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	if dec.PeekKind() != '{' {
		return nil
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil
	}
	e.Object = true
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			e.Object = false
			return nil
		}
		name := token.String()
		value, err := dec.ReadValue()
		if err != nil {
			e.Object = false
			return nil
		}
		e.Members++
		switch name {
		case "type":
			_ = e.Type.UnmarshalJSON(value)
		case "id":
			_ = e.ID.UnmarshalJSON(value)
		}
	}
	return nil
}

// DecodeGroupMembers reads the members of a group object as sent.
func DecodeGroupMembers(data []byte) (GroupMembers, error) {
	var out GroupMembers
	err := jsondecode.Unmarshal(data, &out)
	return out, err
}

// GroupMembersOf is a stored group's members as the record writes them.
func GroupMembersOf(group model.ProxyGroup) GroupMembers {
	data, _ := model.Marshal(group)
	out, _ := DecodeGroupMembers(data)
	return out
}

// sameSent compares two members as sent; null counts as absent.
func sameSent(left, right reflect.Value) bool {
	l, r := left.FieldByName("Raw").Bytes(), right.FieldByName("Raw").Bytes()
	if string(l) == "null" {
		l = nil
	}
	if string(r) == "null" {
		r = nil
	}
	return bytes.Equal(l, r)
}

// sameSources reports equal candidate source members.
func sameSources(a, b GroupMembers) bool {
	left, right := reflect.ValueOf(a), reflect.ValueOf(b)
	for i := range left.NumField() {
		name, _, _ := strings.Cut(left.Type().Field(i).Tag.Get("json"), ",")
		if slices.Contains(SourceFields, name) && !sameSent(left.Field(i), right.Field(i)) {
			return false
		}
	}
	return true
}

// candidateLists are the candidate ID list members in check order.
func (m GroupMembers) candidateLists() []struct {
	name string
	sent jsondecode.Member[[]jsondecode.Member[string]]
} {
	return []struct {
		name string
		sent jsondecode.Member[[]jsondecode.Member[string]]
	}{{"manual_nodes", m.ManualNodes}, {"subscriptions", m.Subscriptions}, {"aggregations", m.Aggregations}, {"include_groups", m.IncludeGroups}, {"proxies", m.Proxies}}
}

// filters are the name filter members in FilterFields order.
func (m GroupMembers) filters() []struct {
	name string
	sent jsondecode.Member[string]
} {
	return []struct {
		name string
		sent jsondecode.Member[string]
	}{{"regex", m.Regex}, {"aggregation_regex", m.AggregationRegex}, {"exclude_regex", m.ExcludeRegex}, {"aggregation_exclude_regex", m.AggregationExcludeRegex}}
}

// DeleteFields validates a group patch's delete_fields: distinct health
// fields, none also assigned by the patch.
func DeleteFields(sent jsondecode.Member[[]string], assigned func(field string) bool) ([]string, error) {
	if !sent.Present() {
		return nil, nil
	}
	if !sent.OK {
		return nil, candidateError(msgDeleteFields)
	}
	seen := map[string]bool{}
	for _, field := range sent.Value {
		if !slices.Contains(HealthFields, field) || seen[field] {
			return nil, candidateError(msgDeleteFields)
		}
		seen[field] = true
	}
	for _, field := range sent.Value {
		if assigned(field) {
			return nil, candidateError(msgAssignDelete)
		}
	}
	return sent.Value, nil
}
