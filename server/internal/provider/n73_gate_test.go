package provider

import (
	"configflow/internal/yamlx/yamltest"
	"testing"
)

func TestN73ProviderReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range loadCorpus(t) {
		sources = append(sources, yamltest.TextFields(row.Value("input"), "text", "content")...)
	}
	yamltest.Check(t, "provider/corpus", sources)
	sources = nil
	for _, row := range loadRecorded(t) {
		sources = append(sources, yamltest.TextFields(row.Value("args"), "text", "content")...)
		sources = append(sources, yamltest.TextFields(row.Value("fetch"), "text")...)
	}
	yamltest.Check(t, "provider/recorded", sources)
}
