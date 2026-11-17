package yamlxtest

import (
	"testing"
)

// Every frozen source must execute even when no current caller selects it.
// Convert's injected-value audit alone does not call Classify.
func TestAllFrozenReaderSources(t *testing.T) {
	records, err := frozenReaders()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for id, row := range records {
		category, err := Classify(row.Source)
		if err != nil {
			t.Errorf("frozen source %s: %v", id, err)
			continue
		}
		counts[category]++
	}
	if t.Failed() {
		return
	}
	t.Logf("executed=%d unique historical sources; classes=%v unknown=0", len(records), counts)
}
