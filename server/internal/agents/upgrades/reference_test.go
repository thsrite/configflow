package upgrades

import (
	"os"
	"testing"

	"configflow/internal/jsonx"
)

// TestReferenceUsable checks Usable against address_follow.usable on the
// corpus of testdata/gen_a15_reference.py (CPython 3.11 ipaddress).
func TestReferenceUsable(t *testing.T) {
	data, err := os.ReadFile("testdata/a15_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	cases := value.(*jsonx.Object).GetList("usable")
	if len(cases) < 40 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, item := range cases {
		c := item.(*jsonx.Object)
		want, _ := c.Value("output").(string)
		if got := Usable(c.Value("input")); got != want {
			t.Errorf("usable(%s) = %q, Python %q", jsonx.Repr(c.Value("input")), got, want)
		}
	}
}
