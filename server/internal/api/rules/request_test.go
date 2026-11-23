package rules

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
)

func TestRuleInputReplacesOnlySentMembers(t *testing.T) {
	base := model.RuleConfig{ID: "r", ItemType: "rule", RuleType: "DOMAIN", Value: new("a.example"), Policy: new("DIRECT"), Remark: new("keep")}
	tests := []struct {
		name, body string
		check      func(model.RuleConfig) bool
		typeErr    bool
	}{
		{"absent keeps base", `{}`, func(r model.RuleConfig) bool { return *r.Remark == "keep" && *r.Policy == "DIRECT" }, false},
		{"text replaced", `{"remark":"x","value":"b.example"}`, func(r model.RuleConfig) bool { return *r.Remark == "x" && *r.Value == "b.example" }, false},
		{"null policy is empty", `{"policy":null}`, func(r model.RuleConfig) bool { return r.Policy != nil && *r.Policy == "" }, false},
		{"null value clears", `{"value":null}`, func(r model.RuleConfig) bool { return r.Value == nil }, false},
		{"wrong shadowed member", `{"value":5}`, func(model.RuleConfig) bool { return true }, true},
		{"wrong other member", `{"enabled":"yes"}`, func(model.RuleConfig) bool { return true }, true},
		{"unknown member ignored", `{"future":{"keep":true}}`, func(r model.RuleConfig) bool { return *r.Remark == "keep" }, false},
		{"member names are exact", `{"Remark":"x"}`, func(r model.RuleConfig) bool { return *r.Remark == "keep" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeRule([]byte(tc.body), base).rule()
			if (err != nil) != tc.typeErr || !tc.check(got) {
				t.Fatalf("rule %+v err %v", got, err)
			}
		})
	}
}

func TestNormalizedDropsMembersTheKindDoesNotKeep(t *testing.T) {
	config := &model.Config{Profile: &model.Profile{}, RuleLibrary: []model.RuleLibraryItem{{ID: "lib"}}}
	in := decodeRule([]byte(`{"itemType":"ruleset","library_rule_id":"lib","url":5,"udp_fallback":"DIRECT","ntp_direct":"x"}`), model.RuleConfig{})
	if err := normalized(in, nil, config); err != nil {
		t.Fatal(err)
	}
	got, err := in.rule()
	if err != nil || got.URL != nil || got.UDPFallback != nil || got.NTPDirect != nil || got.LibraryRuleID != "lib" {
		t.Fatalf("rule %+v err %v", got, err)
	}
	in = decodeRule([]byte(`{"rule_type":"MATCH","ntp_direct":"x"}`), model.RuleConfig{})
	if err := normalized(in, nil, config); err == nil || err.Error() != msgNTP {
		t.Fatalf("MATCH ntp: %v", err)
	}
	in = decodeRule([]byte(`{"rule_type":"DOMAIN","value":"a.example","ntp_direct":"x"}`), model.RuleConfig{})
	if err := normalized(in, nil, config); err != nil {
		t.Fatal(err)
	}
	if got, err := in.rule(); err != nil || got.ItemType != "rule" || got.NTPDirect != nil {
		t.Fatalf("rule %+v err %v", got, err)
	}
	if err := ruleTypeError([]model.RuleConfig{{ID: "a", ItemType: "rule"}}, errMemberType); err == nil || err.Error() != "rule_configs 类型不正确" {
		t.Fatalf("type error message: %v", err)
	} else if !errors.Is(err, err) {
		t.Fatal("unreachable")
	}
}

// TestReviewBatch44DecodeRuleLeavesBaseUntouched: a request decoded onto a
// stored rule must not write through the rule's pointer members (detach), and
// detach only copies pointers, so the rule record must not gain members that
// decoding could write through otherwise (slices, maps, interfaces, nested
// records).
func TestReviewBatch44DecodeRuleLeavesBaseUntouched(t *testing.T) {
	typ := reflect.TypeFor[model.RuleConfig]()
	for i := range typ.NumField() {
		field := typ.Field(i)
		switch kind := field.Type.Kind(); kind {
		case reflect.String:
		case reflect.Pointer:
			if elem := field.Type.Elem().Kind(); elem != reflect.String && elem != reflect.Bool {
				t.Errorf("RuleConfig.%s points to %s: detach copies only the pointer", field.Name, elem)
			}
		default:
			t.Errorf("RuleConfig.%s is a %s: decodeRule would write through the stored rule", field.Name, kind)
		}
	}
	base := model.RuleConfig{ID: "r", ItemType: "rule", RuleType: "DOMAIN"}
	value := reflect.ValueOf(&base).Elem()
	body := map[string]any{}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Pointer {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Type.Elem().Kind() == reflect.Bool {
			value.Field(i).Set(reflect.ValueOf(new(true)))
			body[name] = false
		} else {
			value.Field(i).Set(reflect.ValueOf(new("stored")))
			body[name] = "sent"
		}
	}
	want, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	in := decodeRule(data, base)
	if in.err != nil {
		t.Fatal(in.err)
	}
	// The wire copy holds the sent values (the request reached every member).
	if got := model.RuleConfig(in.ruleWire); got.Enabled == nil || *got.Enabled || got.Remark == nil || *got.Remark != "sent" {
		t.Fatalf("decoded %+v", got)
	}
	if got, _ := json.Marshal(base); string(got) != string(want) {
		t.Fatalf("stored rule changed by decoding a request:\n got %s\nwant %s", got, want)
	}
}
