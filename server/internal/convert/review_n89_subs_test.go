package convert_test

import (
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Sample full outputs from the frozen pre-migration manifest. Selecting sources
// with subscriptions exercises the changed parser/environment wiring in each engine.
func TestReviewN89SubscriptionGenerationSamples(t *testing.T) {
	t.Cleanup(convert.SetMihomoEnvironment(corpusEnv(nil, nil)))
	data, err := os.ReadFile("testdata/n88_generation_bytes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture generationByteFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 4548 {
		t.Fatalf("frozen byte manifest has %d cases", len(fixture.Cases))
	}
	rows := map[string][]*jsonx.Object{}
	for name, expected := range fixture.Sources {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil || generationDigest(data) != expected {
			t.Fatal("frozen generation source drift:", name, err)
		}
		rows[name] = loadJSONLines(t, "testdata/"+name)
	}
	seen := map[string]bool{}
	for _, sample := range fixture.Cases {
		entry := rows[sample.Source][sample.Row]
		engine := entry.GetString("fn")
		if seen[engine] || len(entry.GetObject("args").GetObject("profile").GetList("subscriptions")) == 0 {
			continue
		}
		t.Run(fmt.Sprintf("%s/%s/%d", engine, sample.Source, sample.Row), func(t *testing.T) {
			output, err := generateByteCase(t, entry)
			if err != nil || generationDigest([]byte(output)) != sample.SHA256 {
				t.Fatalf("full generation changed: err=%v", err)
			}
		})
		seen[engine] = true
	}
	for _, engine := range []string{"generate", "surge", "loon"} {
		if !seen[engine] {
			t.Fatal("no subscription sample for", engine)
		}
	}
}
