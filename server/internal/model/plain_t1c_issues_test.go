package model

import (
	"slices"
	"testing"
)

// TestMosDNSMatchLoadIssues: a custom match of the object form reports its
// wrong-typed and undeclared members as load issues (paths under the list
// element); the text form and a valid object report none.
func TestMosDNSMatchLoadIssues(t *testing.T) {
	tests := []struct {
		name, raw string
		want      []FieldIssue
	}{
		{"valid", `{"custom_matches":[{"id":"m","enabled":true,"matches":"qname example.com","exec":"accept"},"legacy text"]}`, nil},
		{"wrong type", `{"custom_matches":["legacy",{"id":"m","exec":[]}]}`, []FieldIssue{{"mosdns.custom_matches[1].exec", IssueType}}},
		{"undeclared", `{"custom_matches":[{"id":"m","future":true}]}`, []FieldIssue{{"mosdns.custom_matches[0].future", IssueUnknown}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var settings MosDNSSettings
			issues, err := DecodeStored([]byte(tt.raw), &settings, "mosdns")
			if err != nil {
				t.Fatal(err)
			}
			var got []FieldIssue
			for _, issue := range issues {
				if IssueUnder([]FieldIssue{issue}, "mosdns.custom_matches") {
					got = append(got, issue)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("issues %v, want %v", got, tt.want)
			}
		})
	}
}
