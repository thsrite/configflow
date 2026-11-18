package rulefetch

import (
	"os"
	"testing"

	"configflow/internal/model"
)

// The DNS consumer must reach the formal reader, including validated migration,
// rather than the pre-rules JSON transcoder shipped on its standalone branch.
func TestIntegratedTypedCacheReaderMigrates(t *testing.T) {
	ctx, _, legacy, old, native := migrationFixture(t)
	var rule model.RuleConfig
	if err := decodeLegacy(legacy, &rule); err != nil {
		t.Fatal(err)
	}
	data, err := ReadCacheWithFormat(ctx, rule, &SourceConfig{}, "yaml")
	if err != nil || string(data) != "payload: [example.com]\n" {
		t.Fatalf("typed reader: %q %v", data, err)
	}
	for _, path := range []string{old, native} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(data) {
			t.Fatalf("validated migration %s: %q %v", path, got, err)
		}
	}
}
