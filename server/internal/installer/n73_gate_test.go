package installer

import (
	"strings"
	"testing"

	"configflow/internal/yamlx/yamlxtest"
)

func TestN73ComposeReaderMigrationGate(t *testing.T) {
	references, err := nativeArtifactReferences()
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for key, value := range references {
		if strings.HasPrefix(key, "yaml:") {
			sources = append(sources, value)
		}
	}
	for _, path := range []string{"testdata/docker_ha_compose.jsonl.gz", "testdata/docker_ha_compose_recorded.jsonl.gz", "testdata/docker_ha_compose_review.jsonl.gz"} {
		for _, row := range readComposeLines(t, path) {
			if out := mapList(row, "out"); len(out) > 0 {
				if text, ok := out[0].(string); ok {
					sources = append(sources, text)
				}
			}
			sources = append(sources, yamlxtest.TextFields(mapObj(row, "out"), "compose")...)
		}
	}
	if len(sources) == 0 {
		t.Fatal("no Compose reader inputs")
	}
	yamlxtest.Check(t, "installer/readCompose+annotateCompose", sources)
}
