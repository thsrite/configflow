package yamlx

import (
	"bytes"
	"errors"
	"testing"
)

func TestLargeJSONIntegersSurviveYAML(t *testing.T) {
	for _, source := range []string{
		`{"cache_size":100000000000000000000}`,
		`[-100000000000000000000,18446744073709551615,18446744073709551616,-0]`,
	} {
		t.Run(source, func(t *testing.T) {
			n, err := FromJSON([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			data, err := MarshalChecked(t, n)
			if err != nil {
				t.Fatal(err)
			}
			again, err := Parse(data)
			if err != nil {
				t.Fatalf("%s: %v", data, err)
			}
			got, err := ToJSON(again)
			if err != nil || !bytes.Equal(got, []byte(source)) || !Equal(n, again) {
				t.Fatalf("round trip: %s: %v", got, err)
			}
		})
	}
}

func TestExplicitIntegerNativeAndDecimalRules(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"!!int 0777", "511"}, {"!!int 0o17", "15"}, {"!!int 0X1F", "31"},
		{"!!int +100000000000000000000", "100000000000000000000"},
		{"!!int 099999999999999999999999", "99999999999999999999999"},
	} {
		n := parseNative(t, tc.source)
		got, err := ToJSON(n)
		if err != nil || string(got) != tc.want {
			t.Fatalf("%s: %s %v", tc.source, got, err)
		}
	}
	for _, source := range []string{"!!int 1:20", "!!int abc", "!!int 0xFFFFFFFFFFFFFFFFFFF", "!!int 1_000000000000000000000"} {
		if _, err := Parse([]byte(source)); !errors.Is(err, ErrPolicy) {
			t.Fatalf("%s: %v", source, err)
		}
	}
}
