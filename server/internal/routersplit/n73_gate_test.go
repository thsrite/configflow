package routersplit

import (
	"testing"

	"configflow/internal/yamlx/yamlxtest"
)

func TestN73SplitReaderMigrationGate(t *testing.T) {
	for _, path := range []string{"testdata/r02_recorded.jsonl.gz", "testdata/r02_corpus.jsonl.gz", "testdata/r02_edge.jsonl.gz"} {
		byCaller := map[string][]string{}
		for _, row := range loadRecorded(t, path) {
			if source, ok := rowYAML(row); ok {
				byCaller[row.Fn] = append(byCaller[row.Fn], source)
			}
		}
		for caller, sources := range byCaller {
			yamlxtest.Check(t, path+"/"+caller, sources)
		}
	}
}
