package oxidns

import (
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Fresh references use no accepted-difference waiver. Compare rejection,
// Chinese diagnostics, structured native issues, values and argument post-state.
func TestC05ReviewSettingsReferences(t *testing.T) {
	parallelBoundary(t)
	rows := readRows(t, "review_c05.jsonl.gz", map[string]bool{"normalize": true, "safe_relative_path": true})
	if len(rows) < 1500 {
		t.Fatalf("incomplete C05 settings references: %d", len(rows))
	}
	for _, r := range rows {
		t.Run(r.Test, func(t *testing.T) {
			t.Parallel()
			args := r.args(t)
			var result *Node
			var err error
			if r.Fn == "safe_relative_path" {
				var text string
				text, err = SafeRelativePath(args[0], str(args[1]))
				result = S(text)
			} else {
				result, err = NormalizeSettings(args[0], boolArg(args[1]))
			}
			if difference := compare(t, r, result, err); difference != "" {
				t.Fatal(difference)
			}
			if len(r.ArgsAfter) != len(args) {
				t.Fatal("missing argument post-state")
			}
			for i, raw := range r.ArgsAfter {
				want, err := FromJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				if difference := firstDifference(args[i], want, "argument"); difference != "" {
					t.Fatal(difference)
				}
			}
		})
	}
}

func TestC05ReviewDefaultsAndNormalizationAreIndependent(t *testing.T) {
	t.Parallel()
	want := clone(DefaultSettings())
	repositoryDefaults := repo.GetDefaultOxidnsSettings()
	encoded, err := jsonx.Compact(repositoryDefaults)
	if err != nil {
		t.Fatal(err)
	}
	fromRepository, err := FromJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if difference := firstDifference(fromRepository, want, "repo defaults"); difference != "" {
		t.Fatal(difference)
	}
	// Mutating returned defaults and a normalized result must not mutate the
	// caller's nested data, the repository factory or future normalizations.
	input := M("api", M("ssl", M("extra", L("original"))), "unknown", M("list", L("original")))
	before := clone(input)
	first, err := NormalizeSettings(input, true)
	if err != nil {
		t.Fatal(err)
	}
	at(first, "api", "ssl", "extra").Content[0].Value = "changed"
	at(first, "unknown", "list").Content[0].Value = "changed"
	if difference := firstDifference(input, before, "input"); difference != "" {
		t.Fatal(difference)
	}
	second, err := NormalizeSettings(input, true)
	if err != nil {
		t.Fatal(err)
	}
	if str(items(at(second, "unknown", "list"))[0]) != "original" {
		t.Fatal("normalized results share unknown data")
	}
	mutated := DefaultSettings()
	set(get(mutated, "api"), "username", S("changed"))
	repositoryDefaults.GetObject("api").Set("username", "changed")
	if difference := firstDifference(DefaultSettings(), want, "defaults"); difference != "" {
		t.Fatal(difference)
	}
	if repo.GetDefaultOxidnsSettings().GetObject("api").GetString("username") != "" {
		t.Fatal("repository defaults share nested data")
	}
}

// These suite recordings were previously marked pending C23 in C05. Run every
// normalization directly: no PendingError, Unicode or native-library waivers.
func TestC05ReviewRecordedNormalization(t *testing.T) {
	parallelBoundary(t)
	rows := readRows(t, "recorded.jsonl.gz", map[string]bool{"normalize": true})
	if len(rows) < 1400 {
		t.Fatalf("incomplete normalize recordings: %d", len(rows))
	}
	var workers sync.WaitGroup
	for shard := range boundaryShards {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := shard; i < len(rows); i += boundaryShards {
				r := rows[i]
				args, err := r.decodeArgs()
				if err != nil {
					t.Errorf("row %d: decode arguments: %v", i, err)
					continue
				}
				got, err := NormalizeSettings(args[0], len(args) < 2 || boolArg(args[1]))
				if difference := compare(t, r, got, err); difference != "" {
					t.Errorf("row %d (%s): %s", i, r.Test, difference)
				}
			}
		}()
	}
	workers.Wait()
	t.Logf("%d recorded normalizations checked without pending rows or waivers", len(rows))
}
