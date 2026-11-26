package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func legacyScalarJSON(v any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("encode model value: %w", err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}

func checkScalarJSON(t *testing.T, v any) {
	t.Helper()
	got, err := scalarJSON(v)
	want, wantErr := legacyScalarJSON(v)
	if !bytes.Equal(got, want) || fmt.Sprint(err) != fmt.Sprint(wantErr) ||
		reflect.TypeOf(errors.Unwrap(err)) != reflect.TypeOf(errors.Unwrap(wantErr)) {
		t.Fatalf("scalarJSON(%T %v) = %q, %v; want %q, %v", v, v, got, err, want, wantErr)
	}
}

type namedScalarString string
type scalarTextMarshaler string

// MarshalText returns text distinct from the underlying string.
func (s scalarTextMarshaler) MarshalText() ([]byte, error) {
	return []byte("text:" + string(s)), nil
}

func TestScalarJSONLegacyBytes(t *testing.T) {
	plain := "field_name"
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"plain", "field_name"},
		{"string pointer", &plain},
		{"nil string pointer", (*string)(nil)},
		{"nil", nil},
		{"named string", namedScalarString(plain)},
		{"text marshaler", scalarTextMarshaler("<&>\u2028")},
		{"unsupported complex", complex128(1 + 2i)},
		{"empty", ""},
		{"html", "<> &"},
		{"js separators", "\u2028\u2029"},
		{"controls", "\x00\x1f\x7f\b\f\n\r\t\"\\"},
		{"invalid UTF-8", "\xff\xfe\xe2\x80"},
		{"float long exponent", 1e21},
		{"float short exponent", 1e-7},
		{"negative zero", math.Copysign(0, -1)},
		{"float32", float32(1.2345678e-7)},
		{"nan", math.NaN()},
		{"infinity", math.Inf(1)},
		{"negative infinity", float32(math.Inf(-1))},
		{"bytes", []byte{0, 1, 0xfe, 0xff}},
		{"nil bytes", []byte(nil)},
		{"byte array", [4]byte{0, 1, 0xfe, 0xff}},
	} {
		t.Run(tt.name, func(t *testing.T) { checkScalarJSON(t, tt.value) })
	}
}

func FuzzScalarJSONLegacyBytes(f *testing.F) {
	f.Add("field_name", uint64(0))
	f.Add("<>&\u2028\u2029\x00\"\\\xff", math.Float64bits(1e21))
	f.Add("\xfe\xff", math.Float64bits(math.Copysign(0, -1)))
	f.Fuzz(func(t *testing.T, s string, bits uint64) {
		checkScalarJSON(t, s)
		checkScalarJSON(t, math.Float64frombits(bits))
		checkScalarJSON(t, math.Float32frombits(uint32(bits)))
	})
}

func BenchmarkScalarJSON(b *testing.B) {
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"ASCII", "checked_at"},
		{"escaped", "\u2028\u2029<>&"},
		{"float64", 1e-7},
	} {
		for _, impl := range []struct {
			name   string
			encode func(any) ([]byte, error)
		}{
			{"legacy", legacyScalarJSON},
			{"optimized", scalarJSON},
		} {
			b.Run(tt.name+"/"+impl.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := impl.encode(tt.value); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
