package trafficstore

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/rules"
)

type referenceReport struct {
	Status               string
	MihomoVersion        json.RawMessage
	IPOnlyConns, Dropped *big.Int
	Items                []referenceItem
	raw                  model.Fields
}

// referenceItem is one validated connection aggregate.
type referenceItem struct {
	Host              string
	Port              int64
	Rule, RulePayload *string
	rulePresent       bool
	Policy, Outlet    string
	Counts            [5]*big.Int
	FailKinds         map[string]*big.Int
	raw               json.RawMessage
}

// MarshalJSON retains the released cleaned-report shape, including omitted optional fields.
func (r referenceReport) MarshalJSON() ([]byte, error) {
	fields := r.raw
	items := make([]json.RawMessage, len(r.Items))
	for i, item := range r.Items {
		items[i] = item.raw
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	if err = fields.Set("items", raw); err != nil {
		return nil, err
	}
	if err = fields.Set("dropped", json.RawMessage(r.Dropped.String())); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
func referenceBounded(raw json.RawMessage, limit int, required bool) bool {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return !required
	}
	var value string
	if jsondecode.Unmarshal(raw, &value) != nil || utf8.RuneCountInString(value) > limit || required && value == "" {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func referenceString(raw json.RawMessage) string {
	var text string
	_ = jsondecode.Unmarshal(raw, &text)
	return text
}
func referenceCountJSON(raw json.RawMessage, defaultZero bool) (*big.Int, bool) {
	if len(raw) == 0 && defaultZero {
		return new(big.Int), true
	}
	text := string(raw)
	if text == "" {
		return nil, false
	}
	for i, r := range text {
		if i == 0 && r == '-' {
			continue
		}
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(text, 10)
	return n, ok && n.Sign() >= 0 && n.Cmp(maxCount) <= 0
}
func referenceOneOf(raw json.RawMessage, choices []string) (bool, error) {
	if len(raw) > 0 && (raw[0] == '[' || raw[0] == '{') {
		return false, invalidData("a JSON array or object is not a valid choice")
	}
	var value string
	if jsondecode.Unmarshal(raw, &value) != nil {
		return false, nil
	}
	for _, choice := range choices {
		if value == choice {
			return true, nil
		}
	}
	return false, nil
}
func referenceWithin(f model.Fields, allowed map[string]bool) bool {
	for key := range f.All() {
		if !allowed[key] {
			return false
		}
	}
	return true
}

// referenceDecodeReport validates exact Agent JSON keys and drops individual malformed items.
func referenceDecodeReport(raw json.RawMessage) (*referenceReport, error) {
	// Agent objects use the JSON decoder's last complete member value. Stored
	// documents still use the strict Fields codec; do not relax file validation.
	var value map[string]any
	if jsondecode.Decode(raw, &value) != nil || value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var f model.Fields
	if jsondecode.Unmarshal(raw, &f) != nil || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' || !referenceWithin(f, reportFields) {
		return nil, nil
	}
	schema := member(f, "schema")
	var version float64
	if string(schema) == "true" {
		version = 1
	} else if jsondecode.Unmarshal(schema, &version) != nil {
		return nil, nil
	}
	if version != 1 {
		return nil, nil
	}
	known, err := referenceOneOf(member(f, "status"), ReportStatuses)
	if err != nil || !known {
		return nil, err
	}
	for _, name := range []string{"mihomo_version", "window_start", "window_end"} {
		if !referenceBounded(member(f, name), 64, false) {
			return nil, nil
		}
	}
	ip, ok := referenceCountJSON(member(f, "ip_only_conns"), true)
	if !ok {
		return nil, nil
	}
	dropped, ok := referenceCountJSON(member(f, "dropped"), true)
	if !ok {
		return nil, nil
	}
	items := []json.RawMessage{}
	if v := member(f, "items"); len(v) > 0 {
		if jsondecode.Unmarshal(v, &items) != nil || items == nil {
			return nil, nil
		}
	}
	if len(items) > ReportMaxItems {
		return nil, nil
	}
	report := &referenceReport{Status: referenceString(member(f, "status")), MihomoVersion: member(f, "mihomo_version"), IPOnlyConns: ip, Dropped: dropped, Items: []referenceItem{}, raw: f}
	for _, raw := range items {
		item, err := referenceDecodeItem(raw)
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
func referenceDecodeItem(raw json.RawMessage) (*referenceItem, error) {
	var f model.Fields
	if jsondecode.Unmarshal(raw, &f) != nil || len(raw) == 0 || raw[0] != '{' || !referenceWithin(f, itemFields) {
		return nil, nil
	}
	host := referenceString(member(f, "host"))
	if host == "" || host != strings.ToLower(host) || rules.IsValidIP(host) || !rules.IsValidDomain(host, false) {
		return nil, nil
	}
	port, ok := referenceCountJSON(member(f, "port"), true)
	if !ok || !port.IsInt64() || port.Int64() > 65535 {
		return nil, nil
	}
	network := member(f, "network")
	if len(network) > 0 && referenceString(network) != "tcp" && referenceString(network) != "udp" {
		return nil, nil
	}
	known, err := referenceOneOf(member(f, "outlet"), Outlets)
	if err != nil || !known {
		return nil, err
	}
	if !referenceBounded(member(f, "rule"), 64, false) || !referenceBounded(member(f, "rule_payload"), 256, false) || !referenceBounded(member(f, "policy"), 128, true) {
		return nil, nil
	}
	item := &referenceItem{Host: host, Port: port.Int64(), Policy: referenceString(member(f, "policy")), Outlet: referenceString(member(f, "outlet")), raw: bytes.Clone(raw), FailKinds: map[string]*big.Int{}}
	item.rulePresent = f.Has("rule")
	if v := member(f, "rule"); len(v) > 0 && string(v) != "null" {
		text := referenceString(v)
		item.Rule = &text
	}
	if v := member(f, "rule_payload"); len(v) > 0 && string(v) != "null" {
		text := referenceString(v)
		item.RulePayload = &text
	}
	for i, name := range countFields {
		n, ok := referenceCountJSON(member(f, name), true)
		if !ok {
			return nil, nil
		}
		item.Counts[i] = n
	}
	kinds := model.Fields{}
	if v := member(f, "fail_kinds"); len(v) > 0 {
		if v[0] != '{' || jsondecode.Unmarshal(v, &kinds) != nil {
			return nil, nil
		}
	}
	for key, raw := range kinds.All() {
		if !set(FailKinds...)[key] {
			return nil, nil
		}
		n, ok := referenceCountJSON(raw, false)
		if !ok {
			return nil, nil
		}
		item.FailKinds[key] = n
	}
	return item, nil
}

func compareReportDecoder(t *testing.T, raw []byte) {
	t.Helper()
	ref, refErr := referenceDecodeReport(raw)
	got, err := DecodeReport(raw)
	if (refErr != nil) != (err != nil) || refErr != nil && refErr.Error() != err.Error() || (ref == nil) != (got == nil) {
		t.Fatalf("%q: got %v, %v; reference %v, %v", raw, got, err, ref, refErr)
	}
	if got == nil {
		return
	}
	wantBytes, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, err := json.Marshal(got)
	if err != nil || !bytes.Equal(wantBytes, gotBytes) || got.Status != ref.Status || !bytes.Equal(got.MihomoVersion, ref.MihomoVersion) || got.IPOnlyConns.Cmp(ref.IPOnlyConns) != 0 || got.Dropped.Cmp(ref.Dropped) != 0 || len(got.Items) != len(ref.Items) {
		t.Fatalf("%q: got %s (%v), want %s", raw, gotBytes, err, wantBytes)
	}
	for i, v := range ref.Items {
		want := ReportItem{Host: v.Host, Port: v.Port, Rule: v.Rule, RulePayload: v.RulePayload, rulePresent: v.rulePresent, Policy: v.Policy, Outlet: v.Outlet, Counts: v.Counts, FailKinds: v.FailKinds}
		actual := got.Items[i]
		actual.raw = nil
		if !reflect.DeepEqual(want, actual) {
			t.Fatalf("item %d: %+v, want %+v", i, actual, want)
		}
	}
}

func TestReportDecoderMatchesReference(t *testing.T) {
	for _, raw := range []string{
		`{"items":["invalid"],"schema":true,"status":"running","window_start":null}`,
		`{"schema":1.00000000000000001,"status":"running","items":[{"host":"a.example.com","outlet":"direct","policy":"P","rule":null,"conns":-0,"up":9007199254740992}]}`,
		`{"schema":9,"schema":1e0,"st\u0061tus":[],"status":"running","items":null,"items":[{"host":false,"host":"a.example.com","network":"t\u0063p","outlet":"proxy","policy":"<&>","rule":"Match","fail_kinds":{"timeout":null,"timeout":9}}],"dropped":3}`,
		`{"schema":1,"status":{"unknown":true},"items":null}`, `{"schema":1,"status":["running"],"unknown":null}`, `{"schema":1,"status":"running","items":[{"host":"a.example.com","port":99999,"outlet":{}},{"host":"a.example.com","outlet":{}}]}`,
		`{"schema":1,"status":"running","items":[{"host":"192.0.2.1"},{"host":"a.example.com","outlet":"direct","policy":"P","rule_payload":"\ud800","fails":1.0},null,{},[]]}`,
		`{"schema":1,"status":"running","items":null}`, `{"schema":1,"status":"running","dropped":9007199254740993}`,
		`{"schema":true,"status":"running"}` + "\v", `{"schema":1,"status":"running"} {}`,
	} {
		compareReportDecoder(t, []byte(raw))
	}
}

func FuzzReportDecoder(f *testing.F) {
	for _, raw := range []string{`{"schema":true,"schema":1,"status":"running","items":[]}`, `{"host":"a.example.com","outlet":"direct","policy":"P","rule":"Match","fail_kinds":{"reset":2},"conns":2,"up":-0}`, `{"host":"a.example.com","port":1,"outlet":[]}`, `"text"`, `null`, `{"schema":1,"status":[]}`, `{"items":`} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		compareReportDecoder(t, raw)
		body := append([]byte(`{"schema":true,"status":"running","items":[`), raw...)
		compareReportDecoder(t, append(body, ']', '}'))
	})
}
