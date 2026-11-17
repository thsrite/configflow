package routers

import (
	"configflow/internal/yamlx/yamltest"
	"testing"
)

func TestN73DesiredReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range yamltest.Rows(t, "testdata/r01_corpus.jsonl.gz") {
		if row.GetString("fn") == "compute_desired" {
			sources = append(sources, yamltest.TextFields(row.GetObject("in"), "custom_config")...)
		}
	}
	if len(sources) == 0 {
		t.Fatal("no desired reader inputs")
	}
	yamltest.Check(t, "routers/FakeIPNetwork", sources)
}
