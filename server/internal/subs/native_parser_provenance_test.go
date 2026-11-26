package subs

import (
	"encoding/base64"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// TestN89VMessReadsTypedViewAndOpenParameterTokens (O18 T1c; was
// TestN89VMessRetainsTypeEvidenceAndOpenParameterTokens): the typed node view
// reads leniently (a list address is no server); the R5 type evidence on the
// node (model.Issues) no longer exists; the parse result carries the
// wrong-typed server as WrongText. Open parameter tokens stay exact.
func TestN89VMessReadsTypedViewAndOpenParameterTokens(t *testing.T) {
	source := base64.StdEncoding.EncodeToString([]byte(`{"ps":5,"add":[],"port":"443","id":123,"scy":null,"net":"ws","path":null}`))
	parsed := ParseList("vmess://" + source)
	if len(parsed) != 1 {
		t.Fatal("missing VMess node")
	}
	node := parsed[0].Node
	if node.Name != "5" || node.Server != "" || model.IntValue(node.Port) != 443 {
		t.Fatalf("typed view %#v", node)
	}
	// The list address is kept as evidence so client generation refuses it.
	if wrong := parsed[0].WrongText; wrong == nil || wrong.Member != "server" || string(wrong.Value) != "[]" {
		t.Fatalf("wrong-typed server evidence %#v", parsed[0].WrongText)
	}
	params, err := node.Params.Node()
	if err != nil {
		t.Fatal(err)
	}
	uuid := yamlx.Get(params, "uuid")
	if uuid == nil || uuid.ShortTag() != "!!int" || uuid.Value != "123" {
		t.Fatal("opaque parameter token was coerced")
	}
	if value := yamlx.Get(params, "cipher"); value == nil || value.ShortTag() != "!!null" {
		t.Fatal("opaque null cipher was replaced")
	}
	path := yamlx.Get(yamlx.Get(params, "ws-opts"), "path")
	if path == nil || path.ShortTag() != "!!null" {
		t.Fatal("opaque null path was replaced")
	}
}
