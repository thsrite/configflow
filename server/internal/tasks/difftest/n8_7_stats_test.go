package difftest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN87OverviewOverflowAgainstFlask(t *testing.T) {
	w := newWorld(t)
	path, err := w.s.Repo.ProfilePath("default", "stats_history.json")
	if err != nil {
		t.Fatal(err)
	}
	name, err := filepath.Rel(w.s.DataDir, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(filepath.Join(w.pyDir, name))} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	input := json.RawMessage(`{"samples":[{"at":"2026-11-18T10:00:00Z","subscriptions":0,"nodes":1e400,"proxyGroups":0,"rules":0}]}`)
	commands := w.commands([]command{{Method: "GET", Path: "/api/stats/overview"}, {Method: "GET", Path: "/api/stats/overview", Write: map[string]json.RawMessage{name: input}}})
	// The unchanged oracle's state contains Python's Infinity. Quote it only in
	// this explicit difference test; the normal differential decoder stays strict.
	rawOut := pythonOutput(t, w, commands)
	if !bytes.Contains(rawOut, []byte(": Infinity")) {
		t.Fatal("Python did not emit Infinity for overflowing point")
	}
	quoted := bytes.ReplaceAll(rawOut, []byte(": Infinity"), []byte(`: "__Infinity__"`))
	var rows []answer
	if err := json.Unmarshal(quoted, &rows); err != nil || len(rows) != 2 {
		t.Fatalf("parse oracle rows: %v (len=%d)", err, len(rows))
	}
	first, second := rows[0], rows[1]
	compare(t, w, commands[0], send(t, w, commands[0]), first, true)
	var secondBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(second.Body, &secondBody)
	if second.Status != 500 || !strings.Contains(secondBody.Error, "infinity") {
		t.Fatalf("Flask overflow expectation changed: status=%d body=%s", second.Status, second.Body)
	}
	expected := second
	expected.Status = 200
	var firstBody map[string]any
	if err := json.Unmarshal(first.Body, &firstBody); err != nil {
		t.Fatal(err)
	}
	data := firstBody["data"].(map[string]any)
	for key, history := range map[string][]any{"subscriptions": {0, 1}, "nodes": {nil, 0}, "proxyGroups": {0, 0}, "rules": {0, 0}} {
		series := data[key].(map[string]any)
		series["history"] = history
	}
	expected.Body, err = json.Marshal(firstBody)
	if err != nil {
		t.Fatal(err)
	}
	var stateDoc map[string]any
	if err := json.Unmarshal(expected.State, &stateDoc); err != nil {
		t.Fatal(err)
	}
	files := stateDoc["files"].(map[string]any)
	saved := files[filepath.ToSlash(name)].(map[string]any)
	samples := saved["samples"].([]any)
	sample := samples[0].(map[string]any)
	if sample["nodes"] != "__Infinity__" {
		t.Fatalf("Python did not preserve its overflowing point: %v", sample["nodes"])
	}
	sample["nodes"] = json.RawMessage("1e400")
	expected.State, err = json.Marshal(stateDoc)
	if err != nil {
		t.Fatal(err)
	}
	compare(t, w, commands[1], send(t, w, commands[1]), expected, true)
}
