package model

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestGenericNumericTextAndQuotedIntegers(t *testing.T) {
	var protocol struct {
		Password String `json:"password"`
		Port     Int    `json:"port"`
	}
	if err := json.Unmarshal([]byte(`{"password":123,"port":"443"}`), &protocol); err != nil || protocol.Password != "123" || int64(protocol.Port) != 443 {
		t.Fatalf("protocol codec values lost: %+v %v", protocol, err)
	}
	var router Router
	if err := json.Unmarshal([]byte(`{"name":9007199254740993}`), &router); err != nil || router.Name != "9007199254740993" {
		t.Fatalf("generic string field lost number: %q %v", router.Name, err)
	}
	var group ProxyGroup
	if err := json.Unmarshal([]byte(`{"interval":"300","manual_nodes":[5],"follow_group":7}`), &group); err != nil || group.Interval == nil || *group.Interval != 300 {
		t.Fatalf("generic interval lost quoted integer: %v %v", group.Interval, err)
	}
	if group.ManualNodes == nil || len(*group.ManualNodes) != 1 || (*group.ManualNodes)[0] != "5" || group.FollowGroup == nil || *group.FollowGroup != "7" {
		t.Fatalf("pointer/list scalar decoding diverged: %+v", group)
	}
	var names OrderedMap[String]
	if _, err := DecodeStored([]byte(`{"entry":9007199254740993}`), &names, ""); err != nil {
		t.Fatal(err)
	}
	if value, ok := names.Get("entry"); !ok || value != "9007199254740993" {
		t.Fatalf("map scalar decoding diverged: %q %t", value, ok)
	}
	var node Node
	if issues, err := DecodeStored([]byte(`{"port":"443"}`), &node, ""); err != nil || IntValue(node.Port) != 443 || IssueUnder(issues, "port") {
		t.Fatalf("quoted node port lost: %v %v", node.Port, err)
	}
}

func TestScalarTextBoundariesAndPreservation(t *testing.T) {
	for _, literal := range []string{`"443"`, `"000443"`, `"-12"`, `"9223372036854775807"`, `"-9223372036854775808"`} {
		var value Int
		if err := json.Unmarshal([]byte(literal), &value); err != nil {
			t.Fatalf("valid decimal %s: %v", literal, err)
		}
	}
	for _, literal := range []string{`"+443"`, `" 443"`, `"443 "`, `"443.0"`, `"4e2"`, `"１２"`, `"١"`, `"-"`, `""`, `"9223372036854775808"`, `"-9223372036854775809"`} {
		var value Int
		if err := json.Unmarshal([]byte(literal), &value); !errors.Is(err, ErrType) {
			t.Fatalf("invalid decimal %s: %v", literal, err)
		}
	}
	var text String
	for _, literal := range []string{"123.0", "1e3", "123.50"} {
		if err := json.Unmarshal([]byte(literal), &text); err != nil || string(text) != literal {
			t.Fatalf("numeric spelling changed: %s -> %q / %v", literal, text, err)
		}
	}
	if err := json.Unmarshal([]byte(`9007199254740993.125`), &text); err != nil || text != "9007199254740993.125" {
		t.Fatalf("shared string codec lost precision: %q %v", text, err)
	}
	for _, literal := range []string{`true`, `[]`, `{}`} {
		if err := json.Unmarshal([]byte(literal), &text); !errors.Is(err, ErrType) {
			t.Fatalf("non-text accepted: %s %v", literal, err)
		}
	}
	// A plain record reads text members with the same number-literal rule;
	// a switch of another type keeps its truth value (1 is on) and is
	// reported, as is an undeclared member, which is dropped.
	source := []byte(`{"name":12.5,"count":"443","enabled":1,"children":[{"name":9007199254740993}],"future":{"keep":true}}`)
	var record sampleRecord
	issues, err := DecodeStored(source, &record, "")
	if err != nil || record.Name != "12.5" || record.Count != 443 || record.Children[0].Name != "9007199254740993" || record.Enabled == nil || !*record.Enabled {
		t.Fatalf("record scalar rules lost values: %+v %v", record, err)
	}
	if len(issues) != 2 || issues[0] != (FieldIssue{Path: "enabled", Kind: IssueType}) || issues[1] != (FieldIssue{Path: "future", Kind: IssueUnknown}) {
		t.Fatalf("issues=%v", issues)
	}
	if output, err := Marshal(record); err != nil || string(output) != `{"name":"12.5","count":443,"enabled":true,"children":[{"name":"9007199254740993"}]}` {
		t.Fatalf("written as %s %v", output, err)
	}
}
