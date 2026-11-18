package convert_test

import (
	"configflow/internal/convert"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

type clientBytePatch struct {
	Line    int      `json:"line"`
	Old     []string `json:"old"`
	New     []string `json:"new"`
	Classes []string `json:"classes"`
}
type clientByteException struct {
	Row       int               `json:"row"`
	Fn        string            `json:"fn"`
	OldSHA    string            `json:"old_sha256"`
	NativeSHA string            `json:"native_sha256"`
	Patches   []clientBytePatch `json:"patches"`
}
type clientByteExceptions struct {
	Source    string                `json:"source"`
	SourceSHA string                `json:"source_sha256"`
	Counts    map[string]int        `json:"counts"`
	Cases     []clientByteException `json:"cases"`
}

func readClientByteExceptions(t *testing.T) map[int]clientByteException {
	t.Helper()
	data, err := os.ReadFile("testdata/n88c_byte_exceptions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture clientByteExceptions
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/" + fixture.Source)
	if err != nil || generationDigest(source) != fixture.SourceSHA {
		t.Fatal("client exception source changed", err)
	}
	if len(fixture.Cases) != 107 {
		t.Fatalf("explicit client exception coverage changed: %d", len(fixture.Cases))
	}
	result := map[int]clientByteException{}
	counts := map[string]int{}
	for _, entry := range fixture.Cases {
		if _, ok := result[entry.Row]; ok {
			t.Fatal("duplicate client exception")
		}
		result[entry.Row] = entry
		classes := map[string]bool{}
		for _, patch := range entry.Patches {
			if len(patch.Classes) == 0 {
				t.Fatal("unclassified client patch")
			}
			for _, class := range patch.Classes {
				classes[class] = true
			}
		}
		for class := range classes {
			counts[class]++
		}
	}
	a, _ := json.Marshal(counts)
	b, _ := json.Marshal(fixture.Counts)
	if string(a) != string(b) {
		t.Fatal("client classification counts changed")
	}
	return result
}
func explicitClientBytes(t *testing.T, entry *jsonx.Object, exception clientByteException) string {
	t.Helper()
	if entry.GetString("fn") != exception.Fn {
		t.Fatal("client exception target moved")
	}
	out := entry.GetString("out")
	if generationDigest([]byte(out)) != exception.OldSHA {
		t.Fatal("Python source output changed")
	}
	lines := strings.SplitAfter(out, "\n")
	// Patches refer to original source line numbers; reverse application keeps
	// each remaining offset unchanged. There is no content-wide normalization.
	for i := len(exception.Patches) - 1; i >= 0; i-- {
		patch := exception.Patches[i]
		end := patch.Line + len(patch.Old)
		if patch.Line < 0 || end > len(lines) || strings.Join(lines[patch.Line:end], "") != strings.Join(patch.Old, "") {
			t.Fatal("explicit client patch source moved")
		}
		tail := append([]string{}, lines[end:]...)
		lines = append(append(lines[:patch.Line], patch.New...), tail...)
	}
	out = strings.Join(lines, "")
	if generationDigest([]byte(out)) != exception.NativeSHA {
		t.Fatal("explicit client native expectation changed")
	}
	return out
}

func typedClientByteCase(t *testing.T, entry *jsonx.Object) (string, error) {
	t.Helper()
	args := entry.GetObject("args")
	source := args.GetObject("profile")
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var profile model.Profile
	var shared model.Shared
	var system model.System
	if err = json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &shared); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &system); err != nil {
		t.Fatal(err)
	}
	config := &model.Config{Profile: &profile, ProfileID: source.GetString("profile_id"), Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary, SystemConfig: system.SystemConfig, Backup: system.Backup, Agents: system.Agents, HasAgents: source.Has("agents")}
	if entry.GetString("fn") == "surge" {
		return convert.SurgeConfig(context.Background(), config, args.GetString("base_url"))
	}
	return convert.LoonConfig(context.Background(), config, args.GetString("base_url"))
}
