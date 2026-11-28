package trafficstore

import (
	"bytes"
	"encoding/json"
	"maps"
	"math/big"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/model"
	"configflow/internal/rules"
)

// Report is a validated domain-discovery report. Original members retain
// optional values and number spellings for callers that serialize it.
type Report struct {
	Status               string
	MihomoVersion        json.RawMessage
	IPOnlyConns, Dropped *big.Int
	Items                []ReportItem
	raw                  map[string]any
}

// ReportItem is one validated connection aggregate.
type ReportItem struct {
	Host              string
	Port              int64
	Rule, RulePayload *string
	rulePresent       bool
	Policy, Outlet    string
	Counts            [5]*big.Int
	FailKinds         map[string]*big.Int
	raw               map[string]any
}

// MarshalJSON retains original sorted keys, appending absent items and
// dropped in the order introduced by cleaning.
func (r Report) MarshalJSON() ([]byte, error) {
	keys := slices.Sorted(maps.Keys(r.raw))
	for _, k := range []string{"items", "dropped"} {
		if _, ok := r.raw[k]; !ok {
			keys = append(keys, k)
		}
	}
	var f model.Fields
	for _, key := range keys {
		value := r.raw[key]
		switch key {
		case "items":
			items := make([]map[string]any, len(r.Items))
			for i, item := range r.Items {
				items[i] = item.raw
			}
			value = items
		case "dropped":
			if err := f.Set(key, json.RawMessage(r.Dropped.String())); err != nil {
				return nil, err
			}
			continue
		}
		if err := put(&f, key, value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(f)
}

func allowedFields(object map[string]any, allowed map[string]bool) bool {
	for key := range object {
		if !allowed[key] {
			return false
		}
	}
	return true
}

func reportChoice(value any, choices []string) (bool, error) {
	switch v := value.(type) {
	case []any, map[string]any:
		return false, invalidData("a JSON array or object is not a valid choice")
	case string:
		return slices.Contains(choices, v), nil
	default:
		return false, nil
	}
}

func boundedText(value any, limit int, required bool) bool {
	if value == nil {
		return !required
	}
	s, ok := value.(string)
	if !ok || utf8.RuneCountInString(s) > limit || required && s == "" {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func reportCount(object map[string]any, key string) (*big.Int, bool) {
	v, present := object[key]
	if !present {
		return new(big.Int), true
	}
	literal, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	n, ok := new(big.Int).SetString(string(literal), 10)
	return n, ok && n.Sign() >= 0 && n.Cmp(maxCount) <= 0
}

// DecodeReport decodes once with exact names, last-value wins and lossless
// number literals, then validates in protocol order.
func DecodeReport(raw json.RawMessage) (*Report, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var f map[string]any
	if d.Decode(&f) != nil || len(bytes.Trim(raw[d.InputOffset():], " \t\r\n")) != 0 || f == nil || !allowedFields(f, reportFields) {
		return nil, nil
	}
	if f["schema"] != true {
		number, ok := f["schema"].(json.Number)
		if !ok {
			return nil, nil
		}
		version, err := number.Float64()
		if err != nil || version != 1 {
			return nil, nil
		}
	}
	known, err := reportChoice(f["status"], ReportStatuses)
	if err != nil || !known {
		return nil, err
	}
	for _, name := range []string{"mihomo_version", "window_start", "window_end"} {
		if !boundedText(f[name], 64, false) {
			return nil, nil
		}
	}
	ip, ok := reportCount(f, "ip_only_conns")
	if !ok {
		return nil, nil
	}
	dropped, ok := reportCount(f, "dropped")
	if !ok {
		return nil, nil
	}
	items, ok := f["items"].([]any)
	if _, present := f["items"]; present {
		if !ok || len(items) > ReportMaxItems {
			return nil, nil
		}
		f["items"] = nil // keep the original position, release discarded rows
	}
	report := &Report{Status: f["status"].(string), IPOnlyConns: ip, Dropped: dropped, Items: make([]ReportItem, 0, len(items)), raw: f}
	if v, present := f["mihomo_version"]; present {
		report.MihomoVersion, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
	}
	for _, v := range items {
		row, _ := v.(map[string]any)
		item, err := decodeItem(row)
		if err != nil {
			return nil, err
		}
		if item == nil {
			report.Dropped.Add(report.Dropped, big.NewInt(1))
		} else {
			report.Items = append(report.Items, *item)
		}
	}
	return report, nil
}

func decodeItem(f map[string]any) (*ReportItem, error) {
	if f == nil || !allowedFields(f, itemFields) {
		return nil, nil
	}
	host, _ := f["host"].(string)
	if host == "" || host != strings.ToLower(host) || rules.IsValidIP(host) || !rules.IsValidDomain(host, false) {
		return nil, nil
	}
	port, ok := reportCount(f, "port")
	if !ok || port.Int64() > 65535 {
		return nil, nil
	}
	if network, present := f["network"]; present && network != "tcp" && network != "udp" {
		return nil, nil
	}
	known, err := reportChoice(f["outlet"], Outlets)
	if err != nil || !known {
		return nil, err
	}
	if !boundedText(f["rule"], 64, false) || !boundedText(f["rule_payload"], 256, false) || !boundedText(f["policy"], 128, true) {
		return nil, nil
	}
	item := &ReportItem{Host: host, Port: port.Int64(), Policy: f["policy"].(string), Outlet: f["outlet"].(string), raw: f, FailKinds: map[string]*big.Int{}}
	_, item.rulePresent = f["rule"]
	if v, ok := f["rule"].(string); ok {
		item.Rule = &v
	}
	if v, ok := f["rule_payload"].(string); ok {
		item.RulePayload = &v
	}
	for i, name := range countFields {
		n, ok := reportCount(f, name)
		if !ok {
			return nil, nil
		}
		item.Counts[i] = n
	}
	if v, present := f["fail_kinds"]; present {
		kinds, ok := v.(map[string]any)
		if !ok {
			return nil, nil
		}
		for _, key := range slices.Sorted(maps.Keys(kinds)) {
			if !slices.Contains(FailKinds, key) {
				return nil, nil
			}
			n, ok := reportCount(kinds, key)
			if !ok {
				return nil, nil
			}
			item.FailKinds[key] = n
		}
	}
	return item, nil
}
