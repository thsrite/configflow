package contentsync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"configflow/internal/deploy/pushdiff"
)

func TestReviewN73DUnsafeYAMLListing(t *testing.T) {
	if os.Getenv("CONFIGFLOW_REVIEW_N73D_CONTENT_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReviewN73DUnsafeYAMLListing$")
		command.Env = append(os.Environ(), "CONFIGFLOW_REVIEW_N73D_CONTENT_CHILD=1")
		start := time.Now()
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("content listing alias guard failed or exceeded 10s: %v\n%s", err, output)
		}
		t.Logf("isolated content listing completed in %s", time.Since(start))
		return
	}
	var chain strings.Builder
	chain.WriteString("payload:\n  - &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "  - &l%d [*l%d, *l%d]\n", i, i-1, i-1)
	}
	chain.WriteString("  - *l40\n")
	for _, test := range []struct {
		source string
		count  int
	}{{chain.String(), 42}, {"payload: &p [*p]\n", 0}} {
		source := test.source
		f := newFixture(t, nil)
		f.cfg["test_rule"] = source
		f.status()
		f.spawn.run(t)
		result := f.diff()
		requireStatus(t, result.Result, "changed", "")
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var diff pushdiff.Diff
		if err := json.Unmarshal(raw, &diff); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range diff.Files {
			if file.Path == "rules/example.list" {
				found = true
				if file.Summary == nil || file.Summary.Kind != "rules" || file.Summary.After == nil || *file.Summary.After != test.count {
					t.Fatalf("lexical rule summary changed: %+v", file)
				}
			}
		}
		if !found {
			t.Fatal("missing unsafe file summary")
		}
	}
}
