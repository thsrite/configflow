package oxidns

import (
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestRecordedPythonTests replays every top-level call of the C05/C22
// functions (and of the rule_dns / device policy / validation functions
// ported early) that the whole Python test suite makes
// (testdata/c22_record.py, a pytest plugin). Each failure is named by the
// Python test that made the call. Rows that need a function of a later
// module (C23, C25, C26, routers) are counted as pending; their porters
// make them pass.
func TestRecordedPythonTests(t *testing.T) {
	replay(t, "testdata/recorded.jsonl.gz", func(entry *jsonx.Object) string {
		return strings.TrimPrefix(entry.GetString("test"), "backend/")
	})
}
