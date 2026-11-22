package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNodeNumericText(t *testing.T) {
	for _, literal := range []string{"123", "1.25", "1e3", "9007199254740993", "9007199254740993.125"} {
		t.Run(literal, func(t *testing.T) {
			source := []byte(`{"name":` + literal + `,"server":` + literal + `,"enabled":1,"unknown":{"keep":true}}`)
			var node Node
			issues, err := DecodeStored(source, &node, "")
			if err != nil {
				t.Fatal(err)
			}
			if node.Name != literal || node.Server != literal || IssueUnder(issues, "name") || IssueUnder(issues, "server") {
				t.Fatalf("numeric text lost: name=%q server=%q issues=%v", node.Name, node.Server, issues)
			}
			// A number in a switch keeps its truth value and is reported (T1b).
			// A flat node keeps its other members as protocol parameters
			// (O18 T1c decision A), so "unknown" is not an undeclared member.
			if node.Enabled == nil || !*node.Enabled || !IssueUnder(issues, "enabled") || IssueUnder(issues, "unknown") {
				t.Fatalf("numeric switch or protocol parameter: %v %v", node.Enabled, issues)
			}
			stored, err := Marshal(node)
			if err != nil || !bytes.Contains(stored, []byte(`"unknown":{"keep":true}`)) || !bytes.Contains(stored, []byte(`"server":"`+literal+`"`)) {
				t.Fatalf("plain record encoding: %s, %v", stored, err)
			}
		})
	}
	var node Node
	if err := json.Unmarshal([]byte(`{"name":null,"server":null}`), &node); err != nil || node.Name != "" || node.Server != "" {
		t.Fatalf("null should read as absent: %+v %v", node, err)
	}
}
