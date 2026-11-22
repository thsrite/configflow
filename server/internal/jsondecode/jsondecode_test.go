package jsondecode_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

type child struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
type envelope struct {
	Version string           `json:"version"`
	Child   child            `json:"child"`
	List    []*child         `json:"list"`
	Map     map[string]child `json:"map"`
	Raw     json.RawMessage  `json:"raw"`
}

func TestExactMembers(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      envelope
	}{
		{"ignored-case-type", `{"VERSION":[],"CHILD":42}`, envelope{}},
		{"last-map-object", `{"map":{"entry":{"id":"old","enabled":true},"entry":{"id":"last"}}}`, envelope{Map: map[string]child{"entry": {ID: "last"}}}},
		{"case-only", `{"Version":"bad","Child":{"id":"bad"}}`, envelope{}},
		{"mixed-case", `{"version":"ok","VERSION":"bad","child":{"ID":"bad","id":"ok","ENABLED":true},"list":[{"ID":"bad","id":"ok"}],"map":{"UPPER":{"ID":"bad","id":"ok"}}}`, envelope{Version: "ok", Child: child{ID: "ok"}, List: []*child{{ID: "ok"}}, Map: map[string]child{"UPPER": {ID: "ok"}}}},
		{"last-complete-object", `{"child":{"id":"stale","enabled":true},"child":{"id":"last"}}`, envelope{Child: child{ID: "last"}}},
		{"escaped-exact-key", `{"\u0076ersion":"ok","VERSIOn":"bad"}`, envelope{Version: "ok"}},
		{"unicode-fold", `{"verſion":"bad"}`, envelope{}},
		{"opaque-bytes", `{"raw": { "ID" : 1e0, "ID": 2 }}`, envelope{Raw: json.RawMessage(`{ "ID" : 1e0, "ID": 2 }`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got envelope
			if err := jsondecode.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestCustomModelPolicyAndProvenance(t *testing.T) {
	raw := []byte(`{ "version":"ok", "Version" : "extension" }`)
	var a model.Agent
	if err := jsondecode.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if a.Version != "ok" {
		t.Fatalf("wrong version: %#v", a.Version)
	}
	got, err := model.Marshal(&a)
	if err != nil {
		t.Fatal(err)
	}
	// Agent records are plain (O18 T1b): the case alias is dropped.
	if string(got) != `{"version":"ok"}` {
		t.Fatalf("record changed: %s", got)
	}
	if err := jsondecode.Unmarshal([]byte(`{"version":"a","version":"b"}`), &a); err == nil {
		t.Fatal("model duplicate policy bypassed")
	}
}

func TestDecodeNumberAndValidation(t *testing.T) {
	var got struct {
		Value any `json:"value"`
	}
	if err := jsondecode.Decode([]byte(`{"value":9007199254740993,"VALUE":0}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Value != json.Number("9007199254740993") {
		t.Fatalf("number: %#v", got)
	}
	for _, raw := range []string{`{} {}`, `{"value":1,}`, `[`} {
		if err := jsondecode.Decode([]byte(raw), &got); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := jsondecode.Unmarshal([]byte(`{}`), nil); err == nil {
		t.Fatal("nil destination accepted")
	}
}

type Embedded struct {
	Name string `json:"name"`
}
type Left struct {
	Conflict string `json:"same"`
}
type Right struct {
	Conflict string `json:"same"`
}

func TestEmbeddedAndDeclaredNames(t *testing.T) {
	var got struct {
		*Embedded
		Default string
		Number  int    `json:"number,string"`
		Ignored string `json:"-"`
	}
	if err := jsondecode.Unmarshal([]byte(`{"name":"ok","NAME":"bad","same":"ambiguous","Default":"ok","default":"bad","number":"12","Ignored":"bad"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Embedded == nil || got.Name != "ok" || got.Default != "ok" || got.Number != 12 || got.Ignored != "" {
		t.Fatalf("wrong embedding: %#v", got)
	}
}

func TestEmbeddedAmbiguity(t *testing.T) {
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "Left", Type: reflect.TypeFor[Left](), Anonymous: true},
		{Name: "Right", Type: reflect.TypeFor[Right](), Anonymous: true},
	})
	dst := reflect.New(typ)
	if err := jsondecode.Unmarshal([]byte(`{"same":"ambiguous"}`), dst.Interface()); err != nil {
		t.Fatal(err)
	}
	if !dst.Elem().IsZero() {
		t.Fatalf("ambiguous embedded field accepted: %#v", dst.Elem().Interface())
	}
}
