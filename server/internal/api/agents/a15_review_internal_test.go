package agents

import (
	"os"
	"testing"

	"configflow/internal/jsonx"
)

// TestA15ReviewArgIntLikeWerkzeug: request.args.get('lines', 100, type=int)
// is CPython 3.11's int() (Unicode 14 digits: Kawi/Nag Mundari digits of
// Unicode 15 are refused; non-ASCII white space; underscores; 4300 digits
// counting zeros), the default on ValueError. References from
// agents/upgrades/testdata/gen_a15_review.py.
func TestA15ReviewArgIntLikeWerkzeug(t *testing.T) {
	data, err := os.ReadFile("../../agents/upgrades/testdata/a15_review.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range value.(*jsonx.Object).GetList("lines") {
		row := item.(*jsonx.Object)
		if got := pyArgInt(row.GetString("value"), 100).String(); got != nativeQueryUnit(t, row) {
			t.Errorf("int(%s) → %s, Python %s", jsonx.Repr(row.GetString("value")), got, row.GetString("lines"))
		}
	}
}
