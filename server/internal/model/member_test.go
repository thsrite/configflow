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

func FuzzMemberJSONEvidence(f *testing.F) {
	for _, seed := range []string{`{}`, `{"url":false}`, `{"url":123,"content":"unrelated"}`, `{"url":"https://example.com/\\u4e2d"}`, `{"url":null}`} {
		f.Add([]byte(seed), byte(0))
	}
	f.Fuzz(func(t *testing.T, data []byte, edit byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		var rule RuleConfig
		if json.Unmarshal(data, &rule) != nil {
			return
		}
		if edit%3 == 1 {
			text := "edited"
			rule.URL = &text
		} else if edit%3 == 2 {
			if err := rule.Clear("url"); err != nil {
				t.Fatal(err)
			}
		}
		encoded, err := rule.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(encoded, &fields) != nil {
			t.Fatal("invalid record JSON")
		}
		actual, present, err := rule.raw.MemberJSON("url", rule.URL)
		want, has := fields["url"]
		if err != nil || present != has || string(actual) != string(want) {
			t.Fatalf("member=%s/%v record=%s/%v error=%v", actual, present, want, has, err)
		}
	})
}

func TestMemberJSONScalarFastPathRetainsEvidence(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enabled":null,"version":null}`, `{"enabled":false,"version":"a<>&\\\"中"}`, `{"enabled":0,"version":7}`, `{"enabled":"false","version":true}`} {
		t.Run(raw, func(t *testing.T) {
			var record Agent
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatal(err)
			}
			check := func() {
				t.Helper()
				encoded, err := record.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				var expected map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &expected); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"version", "enabled"} {
					got, present, err := EncodeMember(&record, name)
					want, exists := expected[name]
					if err != nil || present != exists || string(got) != string(want) {
						t.Fatalf("%s=%s/%v want %s/%v err=%v", name, got, present, want, exists, err)
					}
				}
			}
			check()
			record.Version = "edited<>&\u4e2d"
			record.Enabled = new(true)
			check()
			record.Version = ""
			record.Enabled = nil
			check()
			if err := ZeroMember(&record, "version"); err != nil {
				t.Fatal(err)
			}
			if err := ZeroMember(&record, "enabled"); err != nil {
				t.Fatal(err)
			}
			check()
		})
	}
}

// FuzzMemberJSONScalarMatchesMarshal compares the scalar fast path with the
// general model encoder byte for byte: escapes, HTML characters, U+2028,
// invalid UTF-8 and nil pointers must all encode identically.
func FuzzMemberJSONScalarMatchesMarshal(f *testing.F) {
	for _, seed := range []string{"", "1.0.0", "a<>&\"\\中", "  ", "\x00\x1f\x7f", "\xff\xfe", "\U0001F600", "1e0"} {
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, text string, flag bool) {
		var nilText *string
		var nilFlag *bool
		for _, value := range []any{text, &text, nilText, flag, &flag, nilFlag} {
			got, gotErr := memberJSONValue(value)
			want, wantErr := Marshal(value)
			if (gotErr != nil) != (wantErr != nil) || string(got) != string(want) {
				t.Fatalf("%T %q: fast %q/%v, Marshal %q/%v", value, text, got, gotErr, want, wantErr)
			}
		}
	})
}
