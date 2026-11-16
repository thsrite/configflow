package mosdns

import (
	"fmt"
	"strings"
	"testing"
)

// TestRecordedPythonTests replays every top-level call of the MosDNS
// converter functions the whole Python test suite makes
// (testdata/c21_record.py, a pytest plugin). Each failure is named by the
// Python test that made the call; Go must give the same bytes, values and
// errors.
func TestRecordedPythonTests(t *testing.T) {
	rows := loadRows(t, "testdata/recorded.jsonl.gz")
	failures, accepted, unsupported, notExpressible := 0, 0, 0, 0
	counts := map[string]int{}
	tests := map[string]bool{}
	for _, entry := range rows {
		test := entry.GetString("test")
		fn := entry.GetString("fn")
		tests[test] = true
		counts[fn]++
		if entry.Has("unsupported") {
			unsupported++
			continue
		}
		msg, acc, ok := compareRow(entry)
		switch {
		case !ok:
			notExpressible++
		case acc:
			accepted++
		case msg != "":
			failures++
			if failures <= 20 {
				t.Errorf("%s (%s): %s", strings.TrimPrefix(test, "backend/"), fn, msg)
			}
		}
	}
	t.Logf("%d recorded calls from %d Python tests %v; %d unsupported, %d not expressible, %d accepted, %d differ",
		len(rows), len(tests), counts, unsupported, notExpressible, accepted, failures)
	if failures > 0 || notExpressible > 0 {
		t.Errorf("%s", fmt.Sprintf("%d differ, %d not expressible of %d recorded calls", failures, notExpressible, len(rows)))
	}
}
