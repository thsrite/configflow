package routers

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"
)

// Keep fields from newer receipt versions across a read-modify-write.
func syncExtra(raw []byte, known string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, key := range strings.Fields(known) {
		delete(fields, key)
	}
	return fields, nil
}
func syncMergeJSON(known []byte, extra map[string]json.RawMessage) ([]byte, error) {
	fields := maps.Clone(extra)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(known, &values); err != nil {
		return nil, err
	}
	maps.Copy(fields, values)
	return json.Marshal(fields)
}

// UnmarshalJSON preserves unknown fields in a stored sync receipt.
func (r *RuleSyncReceipt) UnmarshalJSON(raw []byte) error {
	type plain RuleSyncReceipt
	var value plain
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	extra, err := syncExtra(raw, "status message checked_at synced_at updated_at baseline rules added removed log _rule_order")
	if err != nil {
		return err
	}
	value.Extra = extra
	*r = RuleSyncReceipt(value)
	if len(r.RuleOrder) == 0 {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		rules := fields["rules"]
		if len(rules) == 0 {
			var base map[string]json.RawMessage
			_ = json.Unmarshal(fields["baseline"], &base)
			rules = base["rules"]
		}
		r.RuleOrder = syncObjectOrder(rules)
	}
	return nil
}

// MarshalJSON writes native JSON while retaining opaque receipt metadata.
func (r RuleSyncReceipt) MarshalJSON() ([]byte, error) {
	type plain RuleSyncReceipt
	raw, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	return syncMergeJSON(raw, r.Extra)
}

// UnmarshalJSON retains unknown baseline fields from compatible versions.
func (b *RuleSyncBaseline) UnmarshalJSON(raw []byte) error {
	type plain RuleSyncBaseline
	var value plain
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	extra, err := syncExtra(raw, "control rules desired static_resources policy nat_list")
	if err != nil {
		return err
	}
	value.Extra = extra
	*b = RuleSyncBaseline(value)
	return nil
}

// MarshalJSON preserves fields the current sync baseline does not own.
func (b RuleSyncBaseline) MarshalJSON() ([]byte, error) {
	type plain RuleSyncBaseline
	raw, err := json.Marshal(plain(b))
	if err != nil {
		return nil, err
	}
	return syncMergeJSON(raw, b.Extra)
}

// UnmarshalJSON preserves unknown source-record metadata.
func (r *RuleSyncRecord) UnmarshalJSON(raw []byte) error {
	type plain RuleSyncRecord
	var value plain
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	extra, err := syncExtra(raw, "version ipv4 ipv6 ip6_count ip_count domain_count ignored_count invalid_count name source_type library_id")
	if err != nil {
		return err
	}
	value.Extra = extra
	*r = RuleSyncRecord(value)
	return nil
}

// MarshalJSON retains opaque source metadata alongside its native counters.
func (r RuleSyncRecord) MarshalJSON() ([]byte, error) {
	type plain RuleSyncRecord
	raw, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	return syncMergeJSON(raw, r.Extra)
}
func syncObjectOrder(raw []byte) []string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	result := []string{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil
		}
		result = append(result, key)
	}
	return result
}
