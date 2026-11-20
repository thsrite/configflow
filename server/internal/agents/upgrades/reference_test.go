package upgrades

import (
	"encoding/json"
	"os"
	"testing"
)

// TestReferenceUsable checks Usable against address_follow.usable on the
// corpus of testdata/gen_a15_reference.py (CPython 3.11 ipaddress).
func TestReferenceUsable(t *testing.T) {
	data, err := os.ReadFile("testdata/a15_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Usable []struct {
			Input  any     `json:"input"`
			Output *string `json:"output"`
		} `json:"usable"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if len(ref.Usable) < 40 {
		t.Fatalf("%d cases", len(ref.Usable))
	}
	for _, c := range ref.Usable {
		input, _ := c.Input.(string)
		want := ""
		if c.Output != nil {
			want = *c.Output
		}
		if got := Usable(input); got != want {
			t.Errorf("usable(%#v) = %q, Python %q", c.Input, got, want)
		}
	}
}
