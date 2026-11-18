package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMemberJSONMatchesRecordEvidence(t *testing.T) {
	for _, source := range []string{`{}`, `{"url":null}`, `{"url":false}`, `{"url":0}`, `{"url":"https://example.com/\\u4e2d"}`} {
		t.Run(source, func(t *testing.T) {
			var rule RuleConfig
			if err := json.Unmarshal([]byte(source), &rule); err != nil {
				t.Fatal(err)
			}
			check := func() {
				t.Helper()
				data, err := rule.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(data, &fields)
				got, present, err := rule.raw.MemberJSON("url", rule.URL)
				want, has := fields["url"]
				if err != nil || present != has || string(got) != string(want) {
					t.Fatalf("member=%s/%v record=%s/%v err=%v", got, present, want, has, err)
				}
			}
			check()
			text := "changed"
			rule.URL = &text
			check()
			if err := rule.Clear("url"); err != nil {
				t.Fatal(err)
			}
			check()
		})
	}
	var rule RuleConfig
	_ = json.Unmarshal([]byte(`{"url":false}`), &rule)
	if _, _, err := rule.raw.MemberJSON("url", 0); !errors.Is(err, ErrType) {
		t.Fatal("wrong typed field accepted")
	}
	value, _, _ := rule.raw.MemberJSON("url", rule.URL)
	value[0] = 't'
	again, _, _ := rule.raw.MemberJSON("url", rule.URL)
	if string(again) != "false" {
		t.Fatal("borrowed stored evidence")
	}
}
func BenchmarkMemberJSONUnrelatedPayload(b *testing.B) {
	for _, size := range []int{0, 1 << 20} {
		b.Run(strings.Repeat("x", size/(1<<20)+1), func(b *testing.B) {
			raw, _ := json.Marshal(map[string]string{"url": "https://example.com/rules", "content": strings.Repeat("x", size)})
			var rule RuleConfig
			_ = json.Unmarshal(raw, &rule)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _, err := rule.raw.MemberJSON("url", rule.URL)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
