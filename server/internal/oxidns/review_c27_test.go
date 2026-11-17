package oxidns

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
)

func TestC27ReviewHelperOwnership(t *testing.T) {
	features := M("cache_enabled", true, "cache_lazy_ttl", 300)
	action := M("action", "upstream", "source", "local", "upstream", M("addr", "udp://192.0.2.53"))
	before := ToJSON(features)
	plugins, err := HelperPlugins([]string{RouteKey(action)}, M("rule", action), features)
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range plugins {
		if isOneOf(get(plugin, "type"), "cache") {
			at(plugin, "args", "lazy_cache_ttl").Value = "0"
		}
	}
	if !bytes.Equal(before, ToJSON(features)) {
		t.Fatal("editing a returned helper changed caller-owned settings")
	}
}

func c27ReviewReferences(t *testing.T) []ruleDNSReference {
	t.Helper()
	file, err := os.Open(testdataDir + "review_c27.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	var rows []ruleDNSReference
	for {
		var reference ruleDNSReference
		if err := decoder.Decode(&reference); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, reference)
	}
	if len(rows) < 400 {
		t.Fatalf("incomplete C27 review references: %d", len(rows))
	}
	return rows
}

func TestC27ReviewPublicBoundaries(t *testing.T) {
	rows := c27ReviewReferences(t)
	generated := 0
	for _, reference := range rows {
		t.Run(reference.Fn+"/"+reference.Test, func(t *testing.T) {
			args := reference.args(t)
			got, err := callRuleDNSReference(t, reference.Fn, args)
			if difference := compare(reference.row, got, err); difference != "" {
				t.Fatal(difference)
			}
			// This matrix's rejections are user-facing Chinese validation/conflict
			// messages, including Python ProfileValidationError (not SettingsError).
			if reference.Err != nil && err != nil && err.Error() != reference.Err[1] {
				t.Fatalf("message go=%q python=%q", err.Error(), reference.Err[1])
			}
			if len(reference.Usages) != 0 {
				var locked *RoutingLockedError
				if !errors.As(err, &locked) || !errors.Is(err, ErrRoutingLocked) {
					t.Fatalf("expected routing lock: %v", err)
				}
				want, err := FromJSON(reference.Usages)
				if err != nil {
					t.Fatal(err)
				}
				if difference := firstDifference(toNode(locked.Usages), want, "usages"); difference != "" {
					t.Fatal(difference)
				}
			}
			if difference := compareAfter(reference.row, args, got); difference != "" {
				t.Fatal(difference)
			}
			if reference.Fn == "apply_rule_dns_boundary" && err == nil && !isTrue(get(got, "generation_rejected")) {
				generated++
			}
		})
	}
	t.Logf("%d new references; %d final generated documents", len(rows), generated)
}
