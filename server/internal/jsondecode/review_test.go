package jsondecode_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/jsondecode"
)

func TestReviewNativeTags(t *testing.T) {
	type dto struct {
		Dash    string `json:"-,omitempty"`
		Invalid string `json:"invalid\\tag"`
		Number  int    `json:"number,string,omitempty"`
	}
	raw := []byte(`{"-":"kept","Invalid":"default","number":"12"}`)
	var want, got dto
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if err := jsondecode.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; native %#v", got, want)
	}
}

func TestReviewDynamicTargets(t *testing.T) {
	for _, wrap := range []struct {
		name string
		make func(*child) any
		raw  string
	}{
		{"interface", func(p *child) any { var v any = p; return &v }, `{"id":"ok","ID":"bad"}`},
		{"field", func(p *child) any {
			return &struct {
				Value any `json:"value"`
			}{p}
		}, `{"value":{"id":"ok","ID":"bad"}}`},
		{"slice-capacity", func(p *child) any { values := []any{p}; values = values[:0]; return &values }, `[{"id":"ok","ID":"bad"}]`},
		{"slice", func(p *child) any { return &[]any{p} }, `[{"id":"ok","ID":"bad"}]`},
		{"array", func(p *child) any { return &[1]any{p} }, `[{"id":"ok","ID":"bad"}]`},
	} {
		t.Run(wrap.name, func(t *testing.T) {
			p := &child{}
			if err := jsondecode.Unmarshal([]byte(wrap.raw), wrap.make(p)); err != nil {
				t.Fatal(err)
			}
			if p.ID != "ok" {
				t.Fatalf("case alias reached concrete interface target: %#v", p)
			}
		})
	}
}

type reviewCodec struct {
	Name  string
	Calls *[]string
	Fail  bool
}

func (v *reviewCodec) UnmarshalJSON(raw []byte) error {
	*v.Calls = append(*v.Calls, v.Name+":"+string(raw))
	if v.Fail {
		return errors.New("review codec failure")
	}
	return nil
}

func TestReviewCustomCodecOrder(t *testing.T) {
	type dto struct{ First, Second, Third reviewCodec }
	for _, fail := range []bool{false, true} {
		for range 32 {
			var nativeCalls, exactCalls []string
			makeDTO := func(calls *[]string) dto {
				return dto{reviewCodec{"first", calls, false}, reviewCodec{"second", calls, fail}, reviewCodec{"third", calls, false}}
			}
			want, got := makeDTO(&nativeCalls), makeDTO(&exactCalls)
			raw := []byte(`{"Third":3,"ignored":{},"Second":2,"First":1}`)
			wantErr := json.Unmarshal(raw, &want)
			gotErr := jsondecode.Unmarshal(raw, &got)
			if (wantErr == nil) != (gotErr == nil) || !reflect.DeepEqual(nativeCalls, exactCalls) {
				t.Fatalf("fail=%v: calls %v, native %v; errors %v / %v", fail, exactCalls, nativeCalls, gotErr, wantErr)
			}
		}
	}
}

// FuzzReviewNativeSemantics compares exact-key, duplicate-free JSON with the
// native decoder. Seeds cover embedded fields, pointers, maps, raw/custom values,
// omitempty/string tags and non-zero destinations. Mutations may change types,
// but case aliases and duplicate objects are excluded from this native oracle.
func FuzzReviewNativeSemantics(f *testing.F) {
	for _, seed := range []string{
		`{"name":"ok","Default":"plain","number":"12","child":{"id":"a"},"list":[{"id":"b"}],"map":{"entry":{"id":"c"}},"raw": { "x":1e0 },"-":"dash"}`,
		`{"child":null,"list":[null],"number":"bad"}`,
		`{"name":42,"child":{"enabled":true},"Default":true}`,
		`null`, `[]`, `{}`, `{"list":[{},{}],"map":null}`,
		`{"codec": { "x":1e0 },"text":"exact","dynamic":{"id":"live"},"array":[{},{}]}`,
		`{"codec":null,"text":{},"dynamic":null,"array":[]}`,
		`{"keys":{"entry":{"id":"key"}},"raw":{"x":true}}`,
	} {
		f.Add(seed)
	}
	type dto struct {
		*Embedded
		Default string
		Number  int                  `json:"number,string,omitempty"`
		Child   *child               `json:"child"`
		List    []*child             `json:"list"`
		Map     map[string]child     `json:"map"`
		Raw     json.RawMessage      `json:"raw"`
		Dash    string               `json:"-,omitempty"`
		Codec   reviewRawCodec       `json:"codec"`
		Text    reviewText           `json:"text"`
		Dynamic any                  `json:"dynamic"`
		Array   [1]*child            `json:"array"`
		Keys    map[reviewText]child `json:"keys"`
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 || !reviewOracleInput([]byte(raw)) {
			t.Skip()
		}
		makeDTO := func() dto {
			return dto{Embedded: &Embedded{Name: "before"}, Child: &child{ID: "before"}, Map: map[string]child{"old": {ID: "old"}}, Dynamic: &child{ID: "dynamic"}}
		}
		for _, numbers := range []bool{false, true} {
			want, got := makeDTO(), makeDTO()
			var wantErr, gotErr error
			if numbers {
				d := json.NewDecoder(strings.NewReader(raw))
				d.UseNumber()
				wantErr = d.Decode(&want)
				gotErr = jsondecode.Decode([]byte(raw), &got)
			} else {
				wantErr = json.Unmarshal([]byte(raw), &want)
				gotErr = jsondecode.Unmarshal([]byte(raw), &got)
			}
			if !reviewErrorsEqual(gotErr, wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("numbers=%v input %s\ngot %#v (%v); native %#v (%v)", numbers, raw, got, gotErr, want, wantErr)
			}
		}
	})
}

func reviewOracleInput(raw []byte) bool {
	// Round-trip through maps rejects duplicate keys and case variants without
	// implementing another decoder. Compare object key counts using token parsing.
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	allowed := map[string]bool{"name": true, "Default": true, "number": true, "child": true, "list": true, "map": true, "raw": true, "-": true, "id": true, "enabled": true, "entry": true, "x": true, "codec": true, "text": true, "dynamic": true, "array": true, "keys": true}
	var check func(any, bool) bool
	check = func(v any, opaque bool) bool {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if !opaque && !allowed[k] {
					return false
				}
				if !check(x, opaque) {
					return false
				}
			}
		case []any:
			for _, x := range v {
				if !check(x, opaque) {
					return false
				}
			}
		}
		return true
	}
	return check(value, false) && reviewNoDuplicates(raw)
}

func reviewNoDuplicates(raw []byte) bool {
	// Duplicate policy intentionally differs from encoding/json.
	var v reviewUnique
	return json.Unmarshal(raw, &v) == nil
}

type reviewUnique struct{}

func (*reviewUnique) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 {
		return errors.New("empty")
	}
	switch raw[0] {
	case '{':
		var pairs map[string]json.RawMessage
		if err := json.Unmarshal(raw, &pairs); err != nil {
			return err
		}
		// Count direct members, including duplicates, with the native decoder.
		d := json.NewDecoder(bytes.NewReader(raw))
		_, _ = d.Token()
		count := 0
		for d.More() {
			_, _ = d.Token()
			var x reviewUnique
			if err := d.Decode(&x); err != nil {
				return err
			}
			count++
		}
		if count != len(pairs) {
			return errors.New("duplicate")
		}
	case '[':
		var values []reviewUnique
		return json.Unmarshal(raw, &values)
	}
	return nil
}

// reviewRawCodec is deliberately opaque: even duplicate/uppercase members and
// whitespace must reach the codec intact.
type reviewRawCodec []byte

func (v *reviewRawCodec) UnmarshalJSON(raw []byte) error { *v = append((*v)[:0], raw...); return nil }

type reviewText string

func (v *reviewText) UnmarshalText(raw []byte) error { *v = reviewText(raw); return nil }

func TestReviewOpaqueBufferAndSyntax(t *testing.T) {
	type dto struct {
		First reviewRawCodec `json:"first"`
		Last  reviewRawCodec `json:"last"`
	}
	for _, raw := range []string{
		`{"first": { "X":1e0,"X":2,"padding":"` + strings.Repeat("a", 4096) + `" },"last": { "X":3 }}`,
		`{"first":null,"last":[1,2,3]}`,
		`{"first":1} {}`, `{"first":1,}`, `{"first":`, `{"UNKNOWN":[}`,
	} {
		var want, got dto
		wantErr := json.Unmarshal([]byte(raw), &want)
		gotErr := jsondecode.Unmarshal([]byte(raw), &got)
		if reflect.TypeOf(wantErr) != reflect.TypeOf(gotErr) || !reflect.DeepEqual(got, want) {
			t.Fatalf("input %.100s: got %q %v; native %q %v", raw, got, gotErr, want, wantErr)
		}
	}
}

func TestReviewEmbeddedDominance(t *testing.T) {
	type Shared struct {
		Value string `json:"value"`
	}
	type A struct{ Shared }
	type B struct{ Shared }
	type Plain struct{ Value string }
	type Tagged struct {
		Other string `json:"Value"`
	}
	type private struct {
		Name string `json:"name"`
	}
	for _, tc := range []struct {
		name, raw string
		make      func() any
	}{
		{"shared ambiguity", `{"value":"ignored"}`, func() any {
			typ := reflect.StructOf([]reflect.StructField{
				{Name: "A", Type: reflect.TypeFor[A](), Anonymous: true},
				{Name: "B", Type: reflect.TypeFor[B](), Anonymous: true},
			})
			return reflect.New(typ).Interface()
		}},
		{"tagged dominance", `{"Value":"tagged"}`, func() any {
			return &struct {
				Plain
				Tagged
			}{}
		}},
		{"shallow dominance", `{"value":"direct"}`, func() any {
			typ := reflect.StructOf([]reflect.StructField{
				{Name: "A", Type: reflect.TypeFor[A](), Anonymous: true},
				{Name: "Value", Type: reflect.TypeFor[string](), Tag: `json:"value"`},
			})
			return reflect.New(typ).Interface()
		}},
		{"unexported value", `{"name":"valid"}`, func() any { return &struct{ private }{} }},
		{"unexported nil pointer", `{"name":"error"}`, func() any { return &struct{ *private }{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, got := tc.make(), tc.make()
			wantErr := json.Unmarshal([]byte(tc.raw), want)
			gotErr := jsondecode.Unmarshal([]byte(tc.raw), got)
			if reflect.TypeOf(wantErr) != reflect.TypeOf(gotErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v %v; native %#v %v", got, gotErr, want, wantErr)
			}
		})
	}
}

func reviewErrorsEqual(got, want error) bool {
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		return false
	}
	// Rewriting an object to ignore unknown fields can move byte offsets. Field
	// identity and native error class must still match.
	var a, b *json.UnmarshalTypeError
	if errors.As(got, &a) && errors.As(want, &b) {
		return a.Value == b.Value && a.Type == b.Type && a.Struct == b.Struct && a.Field == b.Field
	}
	return (got == nil && want == nil) || (got != nil && want != nil && got.Error() == want.Error())
}

func TestReviewDuplicateDTOValues(t *testing.T) {
	for _, raw := range []string{
		`{"child":{"id":"old","enabled":true},"child":{"id":"last"}}`,
		`{"child":false,"child":{"id":"last"}}`,
		`{"child":{"id":"old","enabled":true},"ch\u0069ld":{"id":"last"}}`,
	} {
		var got envelope
		if err := jsondecode.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		if got.Child != (child{ID: "last"}) {
			t.Fatalf("earlier object leaked: %#v", got.Child)
		}
	}
}

func TestReviewMapKeyOrder(t *testing.T) {
	// Distinct JSON strings can name the same native map key. Their source
	// order must survive preparation, just like custom codec call order.
	raw := []byte(`{"01":{"id":"first","ignored":true},"1":{"id":"last"}}`)
	for range 128 {
		var want, got map[int]child
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if err := jsondecode.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v; native %#v", got, want)
		}
	}
}
