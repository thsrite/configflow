package routers

import (
	"testing"

	"configflow/internal/yamlx/yamlxtest"
)

func TestN73DesiredReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range yamlxtest.Rows(t, "testdata/r01_corpus.jsonl.gz") {
		if row.GetString("fn") == "compute_desired" {
			sources = append(sources, yamlxtest.TextFields(row.GetObject("in"), "custom_config")...)
		}
	}
	if len(sources) == 0 {
		t.Fatal("no desired reader inputs")
	}
	yamlxtest.Check(t, "routers/FakeIPNetwork", sources)
}
