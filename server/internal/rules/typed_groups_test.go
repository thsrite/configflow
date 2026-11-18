package rules

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type groupExpectedRow struct {
	Input  json.RawMessage `json:"input"`
	Native json.RawMessage `json:"native"`
	Class  string          `json:"class"`
}
type groupExpectations struct {
	ArchiveSHA256 string              `json:"archive_sha256"`
	ReviewSHA256  string              `json:"review_sha256"`
	Groups        []groupExpectedRow  `json:"groups"`
	Lower         []*groupExpectedRow `json:"lower"`
	ReviewLower   []groupExpectedRow  `json:"review_lower"`
}

func groupReference(t *testing.T) groupExpectations {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_group_expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var result groupExpectations
	if json.Unmarshal(data, &result) != nil {
		t.Fatal("invalid group reference")
	}
	for path, want := range map[string]string{"testdata/corpus.jsonl.gz": result.ArchiveSHA256, "testdata/review_c09.json.gz": result.ReviewSHA256} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			t.Fatal("original group reference archive drift")
		}
	}
	return result
}
func groupRow(t *testing.T, row groupExpectedRow, original *jsonx.Object) *jsonx.Object {
	t.Helper()
	input, err := jsonx.Parse(row.Input)
	if err != nil || !jsonx.Equal(input, original) {
		t.Fatal("group reference row input drift")
	}
	switch row.Class {
	case "unchanged", "R5", "D2", "R5+D2":
	default:
		t.Fatalf("unknown group classification %q", row.Class)
	}
	native, err := jsonx.Parse(row.Native)
	if err != nil {
		t.Fatal(err)
	}
	if row.Class == "unchanged" {
		object := native.(*jsonx.Object)
		if original.GetString("kind") == "group" {
			for key, value := range object.All() {
				if !jsonx.Equal(value, original.Value(key)) {
					t.Fatal("unchanged group reference changed an original field")
				}
			}
		} else if !jsonx.Equal(native, original.Value("result")) {
			t.Fatal("unchanged lower reference drift")
		}
	}
	return native.(*jsonx.Object)
}
func TestLowerProviderCopyRejectsAliasExplosion(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("v0: &v0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "v%d: &v%d [*v%d, *v%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("proxy-providers: {Synthetic: {type: http, extension: *v40}}\nproxy-groups: [{name: Group, use: [Synthetic, Other]}]\n")
	// Parse itself rejects hostile graphs before the native lowering can copy.
	if _, err := yamlx.Parse([]byte(chain.String())); err == nil {
		t.Fatal("alias amplification accepted")
	}
	if _, err := yamlx.Parse([]byte("x: &x [*x]\n")); err == nil {
		t.Fatal("self alias accepted")
	}
}
func TestTypedGroupDeleteFieldsUsesExplicitRemoval(t *testing.T) {
	var group model.ProxyGroup
	if err := json.Unmarshal([]byte(`{"id":"g","url":null,"interval":false,"extension":{"v":1e2}}`), &group); err != nil {
		t.Fatal(err)
	}
	var patch model.Fields
	_ = patch.UnmarshalJSON([]byte(`{"delete_fields":["url","interval"]}`))
	merged, err := MergeGroup(group, patch)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := model.Marshal(merged)
	if string(data) != `{"id":"g","extension":{"v":1e2}}` {
		t.Fatalf("lost opaque bytes/deletion: %s", data)
	}
	old, _ := model.Marshal(group)
	if string(old) != `{"id":"g","url":null,"interval":false,"extension":{"v":1e2}}` {
		t.Fatal("mutated input")
	}
}
func FuzzTypedGroupCandidates(f *testing.F) {
	for _, seed := range []string{`{"manual_nodes":["DIRECT"]}`, `{"proxies_order":[false]}`, `{"regex":false}`, `{"type":"chain"}`, `{"subscriptions":null}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 65536 {
			return
		}
		var group model.ProxyGroup
		if json.Unmarshal([]byte(text), &group) != nil {
			return
		}
		_ = SelectMembers(group)
		_ = RenderGroup(group, GroupCatalog{})
		_ = CheckCandidates(group, GroupCatalog{}, nil)
	})
}
