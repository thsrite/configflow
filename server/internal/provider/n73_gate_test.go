package provider

import (
	"testing"

	"configflow/internal/yamlx/yamlxtest"
)

func TestN73ProviderReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range loadCorpus(t) {
		sources = append(sources, yamlxtest.TextFields(row["input"], "text", "content")...)
	}
	yamlxtest.Check(t, "provider/corpus", sources)
	sources = nil
	for _, row := range loadRecorded(t) {
		sources = append(sources, yamlxtest.TextFields(row["args"], "text", "content")...)
		sources = append(sources, yamlxtest.TextFields(row["fetch"], "text")...)
	}
	yamlxtest.Check(t, "provider/recorded", sources)
}
