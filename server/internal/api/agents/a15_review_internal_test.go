package agents

import (
	"os"
	"testing"
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
	root := parseJSONMap(t, data)
	for _, item := range mapList(root, "lines") {
		row := item.(map[string]any)
		if got := pyArgInt(mapStr(row, "value"), 100).String(); got != nativeQueryUnit(t, row) {
			t.Errorf("int(%q) → %s, Python %s", mapStr(row, "value"), got, mapStr(row, "lines"))
		}
	}
}
