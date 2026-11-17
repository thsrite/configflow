package provider

import (
	"testing"

	"configflow/internal/yamlx/yamlxtest"
)

func TestN73ProviderReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range loadCorpus(t) {
		sources = append(sources, yamlxtest.TextFields(row.Value("input"), "text", "content")...)
	}
	yamlxtest.Check(t, "provider/corpus", sources)
	sources = nil
	for _, row := range loadRecorded(t) {
		sources = append(sources, yamlxtest.TextFields(row.Value("args"), "text", "content")...)
		sources = append(sources, yamlxtest.TextFields(row.Value("fetch"), "text")...)
	}
	yamlxtest.Check(t, "provider/recorded", sources)
}
