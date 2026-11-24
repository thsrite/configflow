package rules

import (
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"encoding/json"
	"errors"
	"strings"
)

// IsIPv4 reports the address family.
func (d *Destination) IsIPv4() bool { return d.network.Addr().Is4() }

// CheckCandidates validates the final merged group under the write owner's
// transaction. Malformed source arrays cannot gain validity through R5 fallback.
func CheckCandidates(group model.ProxyGroup, catalog GroupCatalog, original *model.ProxyGroup) error {
	var before *GroupMembers
	if original != nil {
		before = new(GroupMembersOf(*original))
	}
	return CheckCandidateMembers(group, GroupMembersOf(group), catalog, original, before)
}

// CheckFilters validates edited filter types, grandfathering only unchanged
// stored values.
func CheckFilters(group model.ProxyGroup, original *model.ProxyGroup) error {
	var before *GroupMembers
	if original != nil {
		before = new(GroupMembersOf(*original))
	}
	return CheckFilterMembers(GroupMembersOf(group), before)
}

// IsCandidateValidationError reports whether err is a CandidateValidationError.
func IsCandidateValidationError(err error) bool {
	var target *CandidateValidationError
	return errors.As(err, &target)
}

// MergeGroup applies a request patch (members as sent) to a stored group:
// the patch's filters and delete_fields are checked, then its members replace
// the stored ones and the deleted health fields are removed.
func MergeGroup(original model.ProxyGroup, patch model.Fields) (model.ProxyGroup, error) {
	var zero model.ProxyGroup
	data, err := patch.MarshalJSON()
	if err != nil {
		return zero, err
	}
	sent, err := DecodeGroupMembers(data)
	if err != nil {
		return zero, err
	}
	if err := CheckFilterMembers(sent, nil); err != nil {
		return zero, err
	}
	var request struct {
		DeleteFields jsondecode.Member[[]string] `json:"delete_fields"`
	}
	if err := jsondecode.Unmarshal(data, &request); err != nil {
		return zero, err
	}
	deletes, err := DeleteFields(request.DeleteFields, patch.Has)
	if err != nil {
		return zero, err
	}
	data, err = model.Marshal(original)
	if err != nil {
		return zero, err
	}
	var members model.Fields
	if err := members.UnmarshalJSON(data); err != nil {
		return zero, err
	}
	for field, value := range patch.All() {
		if field != "delete_fields" {
			if err := members.Set(field, value); err != nil {
				return zero, err
			}
		}
	}
	for _, field := range deletes {
		members.Delete(field)
	}
	data, err = members.MarshalJSON()
	if err != nil {
		return zero, err
	}
	var merged model.ProxyGroup
	err = json.Unmarshal(data, &merged)
	return merged, err
}

func trimFilter(value string) string { return strings.TrimSpace(value) }

// MatchIP matches an IP address against an IP, IP-CIDR or IP-CIDR6 rule.
func MatchIP(ipText, ruleType, ruleValue string) bool {
	ip, ok := parseAddr(ipText)
	return ok && matchAddr(ip, ruleType, ruleValue)
}
