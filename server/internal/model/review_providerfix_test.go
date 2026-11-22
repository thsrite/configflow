package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestReviewProviderFixScalarContainersAndUnrelatedEdit: numeric scalar
// tokens in text members keep their literal text. O18 T1c: ProxyGroup is a
// plain record, so a quoted boolean in a switch reads by truthiness with a
// type issue, the undeclared member is dropped, and an unrelated edit changes
// only that member of Go's encoding (the stored source bytes are no longer kept).
func TestReviewProviderFixScalarContainersAndUnrelatedEdit(t *testing.T) {
	source := []byte(`{"name":"G","interval":"000443","follow_group":1E+03,"manual_nodes":[123.0,9007199254740993],"enabled":"true","future":{"keep":false}}`)
	var group ProxyGroup
	issues, err := DecodeStored(source, &group, "")
	if err != nil {
		t.Fatal(err)
	}
	if group.Interval == nil || *group.Interval != 443 || group.FollowGroup == nil || *group.FollowGroup != "1E+03" || group.ManualNodes == nil || (*group.ManualNodes)[0] != "123.0" || (*group.ManualNodes)[1] != "9007199254740993" {
		t.Fatal("pointer/list scalar tokens changed")
	}
	if group.Enabled == nil || !*group.Enabled || !IssueUnder(issues, "enabled") || !IssueUnder(issues, "future") {
		t.Fatalf("quoted boolean or undeclared member: %v %v", group.Enabled, issues)
	}
	encoded, err := Marshal(group)
	if err != nil || bytes.Contains(encoded, []byte("future")) || !bytes.Contains(encoded, []byte(`"enabled":true`)) {
		t.Fatalf("plain encoding: %s %v", encoded, err)
	}
	group.Name = "renamed"
	renamed, err := Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(renamed, bytes.Replace(encoded, []byte(`"G"`), []byte(`"renamed"`), 1)) {
		t.Fatalf("unrelated edit changed scalar storage: %s", renamed)
	}
	var values Map[String]
	if err := json.Unmarshal([]byte(`{"decimal":123.0,"exponent":1E+03,"bool":true}`), &values); err != nil {
		t.Fatal(err)
	}
	if value, _ := values.Get("decimal"); value != "123.0" {
		t.Fatal("map lost decimal token")
	}
	if value, _ := values.Get("exponent"); value != "1E+03" {
		t.Fatal("map lost exponent token")
	}
	if value, _ := values.Get("bool"); value != "" {
		t.Fatal("map accepted boolean as text")
	}
}
