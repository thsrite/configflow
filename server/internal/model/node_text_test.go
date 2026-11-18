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
			if err := json.Unmarshal(source, &node); err != nil {
				t.Fatal(err)
			}
			if node.Name != literal || node.Server != literal || IssueUnder(Issues(node), "name") || IssueUnder(Issues(node), "server") {
				t.Fatalf("numeric text lost: name=%q server=%q issues=%v", node.Name, node.Server, Issues(node))
			}
			if node.Enabled != nil || !IssueUnder(Issues(node), "enabled") {
				t.Fatal("numeric bool was accepted")
			}
			stored, err := Marshal(node)
			if err != nil || !bytes.Equal(stored, source) {
				t.Fatalf("R2 storage changed: %s, %v", stored, err)
			}
			if err := SetMemberJSON(&node, &node.raw, "server", json.RawMessage("456.75")); err != nil || node.Server != "456.75" || IssueUnder(Issues(node), "server") {
				t.Fatalf("SetMemberJSON lost numeric text: %q %v", node.Server, err)
			}
		})
	}
	var node Node
	if err := json.Unmarshal([]byte(`{"name":null,"server":null}`), &node); err != nil || node.Name != "" || node.Server != "" || len(Issues(node)) != 0 {
		t.Fatalf("null should read as absent: %+v %v", node, err)
	}
}
