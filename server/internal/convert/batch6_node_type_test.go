package convert

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestBatch6R5StoredNodeType(t *testing.T) {
	for _, value := range []string{`5`, `false`, `[]`} {
		t.Run(value, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(`{"name":"N","type":`+value+`,"server":"example.com","port":80}`), &node); err != nil {
				t.Fatal(err)
			}
			before, err := model.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := NodeProxy(t.Context(), node, nil)
			want := fmt.Sprintf(msgIdentityString, msgNodeTypeField, value)
			if err == nil || err.Error() != want || proxy != nil {
				t.Fatalf("want %q and no proxy; got %+v, %v", want, proxy, err)
			}
			after, err := model.Marshal(node)
			if err != nil || string(before) != string(after) {
				t.Fatalf("rejection changed stored node: %s %v", after, err)
			}
		})
	}
}

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
