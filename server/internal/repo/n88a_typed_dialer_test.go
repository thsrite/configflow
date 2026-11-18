package repo

import (
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func TestTypedDialerNumericProxyString(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"id":"n","name":"N","proxy_string":5}`), &node); err != nil {
		t.Fatal(err)
	}
	if node.ProxyString == nil || *node.ProxyString != "5" {
		t.Fatal("shared scalar codec did not decode proxy_string")
	}
	if value, err := nodeRawDialer(&node); value != nil || err != nil {
		t.Fatalf("typed entry lost numeric text: %v / %v", value, err)
	}
	if value, err := legacyRawDialer(jsonx.Obj("id", "n", "name", "N", "proxy_string", 5)); value != nil || err != nil {
		t.Fatalf("import adapter lost numeric text: %v / %v", value, err)
	}
}
