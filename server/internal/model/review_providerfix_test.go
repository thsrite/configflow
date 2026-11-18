package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReviewProviderFixScalarContainersAndUnrelatedEdit(t *testing.T) {
	source := []byte(`{"name":"G","interval":"000443","follow_group":1E+03,"manual_nodes":[123.0,9007199254740993],"enabled":"true","future":{"keep":false}}`)
	var group ProxyGroup
	if err := json.Unmarshal(source, &group); err != nil {
		t.Fatal(err)
	}
	if group.Interval == nil || *group.Interval != 443 || group.FollowGroup == nil || *group.FollowGroup != "1E+03" || group.ManualNodes == nil || (*group.ManualNodes)[0] != "123.0" || (*group.ManualNodes)[1] != "9007199254740993" {
		t.Fatal("pointer/list scalar tokens changed")
	}
	if group.Enabled != nil || !IssueUnder(Issues(group), "enabled") {
		t.Fatal("quoted boolean became valid")
	}
	encoded, err := Marshal(group)
	if err != nil || !bytes.Equal(source, encoded) {
		t.Fatalf("no-op changed storage: %s %v", encoded, err)
	}
	group.Name = "renamed"
	encoded, err = Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, bytes.Replace(source, []byte(`"G"`), []byte(`"renamed"`), 1)) {
		t.Fatalf("unrelated edit changed scalar storage: %s", encoded)
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
