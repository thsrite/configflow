package convert_test

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
)

// TestSurgeLoonRecordedPythonTests replays every call of the Surge and Loon
// converter functions the Python test suite makes (testdata/c19_record.py, a
// pytest plugin; generate_surge_config, generate_loon_config,
// convert_node_to_*, convert_proxies_to_*_text, convert_proxy_group_to_*,
// get_proxy_group_nodes, loon._build_rules). Each subtest is named by the
// Python test that made the call; Sub-Store answers what it answered in
// Python, and the converters must give the same bytes, errors and mutations.
func TestSurgeLoonRecordedPythonTests(t *testing.T) {
	rows := loadJSONLines(t, "testdata/surge_loon_recorded.jsonl.gz")
	failures, unsupported := 0, 0
	counts := map[string]int{}
	index := map[string]int{}
	for _, entry := range rows {
		test := entry.GetString("test")
		fn := entry.GetString("fn")
		counts[fn]++
		name := fmt.Sprintf("%s#%d", strings.TrimPrefix(test, "backend/"), index[test])
		index[test]++
		if entry.Has("unsupported") {
			unsupported++
			continue
		}
		rec := &recordedEnv{env: entry.GetObject("env")}
		restore := convert.SetMihomoEnv(rec.mihomoEnv(corpusEnv(nil, nil)))
		msg := runSurgeLoonRow(t, rowOf(entry))
		restore()
		if msg != "" {
			failures++
			if failures <= 20 {
				t.Errorf("%s (%s): %s", name, fn, msg)
			}
		}
	}
	t.Logf("%d recorded calls from %d Python tests %v; %d unsupported; %d differ", len(rows), len(index), counts,
		unsupported, failures)
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
}
