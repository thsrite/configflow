package difftest

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
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
	// The unchanged oracle's state contains Python's Infinity. Parse it only in
	// this explicit difference test; the normal differential decoder stays strict.
	value, err := jsonx.Parse(pythonOutput(t, w, commands))
	if err != nil {
		t.Fatal(err)
	}
	rows := value.([]any)
	first, second := rows[0].(*jsonx.Object), rows[1].(*jsonx.Object)
	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var firstAnswer answer
	if err = json.Unmarshal(firstBytes, &firstAnswer); err != nil {
		t.Fatal(err)
	}
	compare(t, w, commands[0], send(t, w, commands[0]), firstAnswer, true)
	if second.GetInt("status") != 500 || !strings.Contains(second.GetObject("body").GetString("error"), "infinity") {
		t.Fatal("Flask overflow expectation changed", jsonx.Repr(second.GetObject("body")))
	}
	expected := second.Clone()
	expected.Set("status", 200)
	body := first.GetObject("body").Clone()
	data := body.GetObject("data").Clone()
	body.Set("data", data)
	for key, history := range map[string][]any{"subscriptions": {0, 1}, "nodes": {nil, 0}, "proxyGroups": {0, 0}, "rules": {0, 0}} {
		series := data.GetObject(key).Clone()
		series.Set("history", history)
		data.Set(key, series)
	}
	expected.Set("body", body)
	state := expected.GetObject("state").Clone()
	files := state.GetObject("files").Clone()
	state.Set("files", files)
	saved := files.GetObject(filepath.ToSlash(name)).Clone()
	files.Set(filepath.ToSlash(name), saved)
	samples := append([]any{}, saved.GetList("samples")...)
	saved.Set("samples", samples)
	sample := samples[0].(*jsonx.Object).Clone()
	samples[0] = sample
	number, ok := sample.Value("nodes").(jsonx.Number)
	if !ok || !math.IsInf(number.Float64(), 1) {
		t.Fatal("Python did not preserve its overflowing point")
	}
	sample.Set("nodes", jsonx.Number("1e400"))
	expected.Set("state", state)
	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var expectedAnswer answer
	if err = json.Unmarshal(expectedBytes, &expectedAnswer); err != nil {
		t.Fatal(err)
	}
	compare(t, w, commands[1], send(t, w, commands[1]), expectedAnswer, true)
}
