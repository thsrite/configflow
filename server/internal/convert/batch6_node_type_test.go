package convert

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestBatch6R5UnusedStoredNodeType(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"name":"N","type":5,"proxy_string":"type: http\nserver: example.com\nport: 80\n"}`), &node); err != nil {
		t.Fatal(err)
	}
	proxy, err := NodeProxy(t.Context(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	kind := yamlx.Get(proxy, "type")
	if kind == nil || kind.ShortTag() != "!!str" || kind.Value != "http" {
		t.Fatalf("unused stored type affected proxy_string: %+v", kind)
	}
}
