package convert_test

import (
	"configflow/internal/convert"
	"fmt"
	"testing"
)

func TestReviewN89GenerationCallbackSamples(t *testing.T) {
	// Public generation/download entries from the immutable Python archive:
	// normal providers and private providers with scoped filters. Use complete
	// output, report and input-mutation comparison, allowing only indexed D2 paths.
	selected := map[int]string{300: "generate", 301: "generate", 799: "generate", 828: "generate", 927: "provider_downloads", 1020: "provider_downloads"}
	refs := providerPathReferences(t)
	oracle := loadWriterOracle(t)
	checked := 0
	for i, row := range loadMihomoCorpus(t) {
		fn, ok := selected[i]
		if !ok {
			continue
		}
		checked++
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if row.fn != fn || row.errClass != "" {
				t.Fatal("normal generation sample drifted")
			}
			row = providerPathReference(t, refs, i, row)
			if difference := runMihomoRow(t, row, oracle); difference != "" {
				t.Fatal(difference)
			}
		})
	}
	if checked != len(selected) {
		t.Fatal("generation samples missing")
	}
}

func TestReviewN89ClientFilterGenerationSamples(t *testing.T) {
	t.Cleanup(convert.SetMihomoEnv(corpusEnv(nil, nil)))
	rows := loadJSONLines(t, "testdata/surge_loon_corpus.jsonl.gz")
	// These two original normal profiles include filtered candidate nodes.
	for i, fn := range []string{"surge", "loon"} {
		t.Run(fn, func(t *testing.T) {
			row := rowOf(rows[i])
			if row.fn != fn || row.errClass != "" {
				t.Fatal("normal client sample drifted")
			}
			if difference := runSurgeLoonRow(t, row); difference != "" {
				t.Fatal(difference)
			}
		})
	}
}
