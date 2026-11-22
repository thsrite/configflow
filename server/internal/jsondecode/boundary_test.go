package jsondecode_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
)

// boundaryDTO has exact names next to their case aliases' fields, nested and
// opaque members, so repeats and aliases at every level reach a DTO.
type boundaryDTO struct {
	ID    string            `json:"id"`
	Upper string            `json:"ID"`
	Child *child            `json:"child"`
	List  []child           `json:"list"`
	Map   map[string]child  `json:"map"`
	Raw   json.RawMessage   `json:"raw"`
	Any   any               `json:"any"`
	Codec reviewRawCodec    `json:"codec"`
	Pairs map[string]string `json:"pairs"`
}

func boundaryOracle(t *testing.T, raw []byte) {
	t.Helper()
	// Last-value semantics: encoding/json decodes a repeated map key into a
	// fresh element, so a generic value is the oracle; re-encoding it gives the
	// same document without repeats, which must decode to the same DTO.
	var native any
	nativeErr := json.Unmarshal(raw, &native)
	var generic any
	if err := jsondecode.Unmarshal(raw, &generic); (err == nil) != (nativeErr == nil) {
		t.Fatalf("validity %v, native %v", err, nativeErr)
	}
	if nativeErr != nil {
		var dto boundaryDTO
		if err := jsondecode.Decode(raw, &dto); err == nil {
			t.Fatal("invalid document decoded")
		}
		return
	}
	if !reflect.DeepEqual(generic, native) {
		t.Fatalf("generic %#v, native %#v", generic, native)
	}
	// The canonical document keeps number spellings (json.Number) for Decode.
	var numbers any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&numbers); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(numbers)
	if err != nil {
		t.Fatal(err)
	}
	for _, decode := range []func([]byte, any) error{jsondecode.Unmarshal, jsondecode.Decode} {
		var got, want boundaryDTO
		gotErr, wantErr := decode(raw, &got), decode(canonical, &want)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("error %v, canonical %v", gotErr, wantErr)
		}
		if gotErr != nil {
			continue
		}
		// Opaque values keep their own bytes; compare them as JSON values.
		for _, pair := range [][2]*json.RawMessage{{&got.Raw, &want.Raw}} {
			var a, b any
			_ = json.Unmarshal(*pair[0], &a)
			_ = json.Unmarshal(*pair[1], &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("raw %s, canonical %s", *pair[0], *pair[1])
			}
			*pair[0], *pair[1] = nil, nil
		}
		var a, b any
		_ = json.Unmarshal(got.Codec, &a)
		_ = json.Unmarshal(want.Codec, &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("codec %s, canonical %s", got.Codec, want.Codec)
		}
		got.Codec, want.Codec = nil, nil
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("input %s\ngot %#v\ncanonical %#v", raw, got, want)
		}
	}
}

func TestBoundaryRepeatsAliasesUTF8Depth(t *testing.T) {
	tests := []struct {
		name, raw string
		check     func(*testing.T, boundaryDTO, error)
	}{
		{"nested repeat replaces whole object", `{"child":{"id":"old","enabled":true},"list":[{"id":"a","id":"b"}],"child":{"id":"new"}}`, func(t *testing.T, v boundaryDTO, err error) {
			if err != nil || *v.Child != (child{ID: "new"}) || v.List[0].ID != "b" {
				t.Fatalf("%#v %v", v, err)
			}
		}},
		{"escaped repeat", `{"id":"a","id":"b"}`, func(t *testing.T, v boundaryDTO, err error) {
			if err != nil || v.ID != "b" {
				t.Fatalf("%#v %v", v, err)
			}
		}},
		{"case alias is its own member", `{"id":"lower","ID":"upper","Id":"neither"}`, func(t *testing.T, v boundaryDTO, err error) {
			if err != nil || v.ID != "lower" || v.Upper != "upper" {
				t.Fatalf("%#v %v", v, err)
			}
		}},
		{"invalid UTF-8 replaced", "{\"id\":\"a\xffb\"}", func(t *testing.T, v boundaryDTO, err error) {
			if err != nil || v.ID != "a�b" {
				t.Fatalf("%#v %v", v, err)
			}
		}},
		{"repeat under an invalid UTF-8 name", "{\"map\":{\"\xce\":{\"id\":\"x\",\"id\":\"y\"}}}", func(t *testing.T, v boundaryDTO, err error) {
			if err != nil || v.Map["\uFFFD"].ID != "y" {
				t.Fatalf("%#v %v", v, err)
			}
		}},
		{"repeat with invalid UTF-8", "{\"id\":\"x\",\"id\":\"a\xffb\"}", func(t *testing.T, v boundaryDTO, err error) {
			if err != nil || v.ID != "a�b" {
				t.Fatalf("%#v %v", v, err)
			}
		}},
		{"too deep", `{"any":` + strings.Repeat("[", 10001) + strings.Repeat("]", 10001) + `}`, func(t *testing.T, _ boundaryDTO, err error) {
			if err == nil {
				t.Fatal("nesting beyond encoding/json's limit accepted")
			}
		}},
		{"deep repeat", `{"any":` + strings.Repeat(`{"k":1,"k":`, 2000) + `0` + strings.Repeat("}", 2000) + `}`, func(t *testing.T, v boundaryDTO, err error) {
			if err != nil {
				t.Fatal(err)
			}
			depth := 0
			for value := v.Any; ; depth++ {
				m, ok := value.(map[string]any)
				if !ok {
					break
				}
				if len(m) != 1 {
					t.Fatalf("depth %d: %#v", depth, m)
				}
				value = m["k"]
			}
			if depth != 2000 {
				t.Fatalf("depth %d", depth)
			}
		}},
		{"trailing data", `{"id":"a","id":"b"} {}`, func(t *testing.T, _ boundaryDTO, err error) {
			if _, ok := err.(*json.SyntaxError); !ok {
				t.Fatalf("want *json.SyntaxError, got %T %v", err, err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v boundaryDTO
			tt.check(t, v, jsondecode.Unmarshal([]byte(tt.raw), &v))
			boundaryOracle(t, []byte(tt.raw))
		})
	}
}

// FuzzBoundary checks repeated members (also escaped and nested), case
// aliases, invalid UTF-8 and nesting depth against encoding/json's generic
// last-value decoding and against the same document without repeats (opaque
// and custom-codec values compared as JSON values: they keep their bytes).
func FuzzBoundary(f *testing.F) {
	for _, seed := range []string{
		`{"id":"a","id":"b","ID":"c"}`,
		`{"child":{"id":"x","enabled":true},"child":{"id":"y"}}`,
		`{"map":{"k":{"id":"1","enabled":true},"k":{"id":"2"}},"pairs":{"a":"1","a":"2"}}`,
		`{"raw":{ "a" : 1, "a":{"b":2,"b":3}},"codec":{"x":[1,{"y":1,"y":2}]}}`,
		`{"any":[{"n":1,"n":1.50}],"list":[{"id":"a"},{"ID":"b","id":"c","id":"d"}]}`,
		"{\"id\":\"\xc3\x28\",\"i\\u0064\":\"\xed\xa0\x80\"}",
		`{"any":` + strings.Repeat("[", 64) + strings.Repeat("]", 64) + `}`,
		`{"id":`, `{"id":1,}`, `[]`, `null`, `{"child":null,"child":{"id":"z"}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 16<<10 {
			t.Skip()
		}
		boundaryOracle(t, raw)
	})
}

// TestBoundaryCodecsSeeRepeats: repeats inside values owned by a custom codec
// reach it unchanged, so a codec that refuses repeats (model records, the
// Agent protocol's model.Fields) still refuses them.
func TestBoundaryCodecsSeeRepeats(t *testing.T) {
	var dto boundaryDTO
	raw := `{"codec": {"x":1, "x":2},"raw": {"y":1, "y":2},"id":"a","id":"b"}`
	if err := jsondecode.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatal(err)
	}
	if string(dto.Codec) != `{"x":1, "x":2}` || string(dto.Raw) != `{"y":1, "y":2}` || dto.ID != "b" {
		t.Fatalf("%#v", dto)
	}
}
