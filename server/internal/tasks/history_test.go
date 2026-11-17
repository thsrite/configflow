package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func TestHistoryCountersAgainstPython(t *testing.T) {
	inputs := []json.RawMessage{[]byte(`null`), []byte(`false`), []byte(`true`), []byte(`""`), []byte(`[]`), []byte(`{}`), []byte(`0`), []byte(`-17`), []byte(`3.75`), []byte(`-3.75`), []byte(`1e2`), []byte(`"  +42  "`), []byte(`"9223372036854775808123"`), []byte(`9007199254740993`), []byte(`"bad"`), []byte(`[1]`), []byte(`{"x":1}`), []byte(`"1_000"`), []byte(`" +01_002 "`), []byte(`"-9_223_372_036_854_775_808_123"`), []byte(`"1__0"`), []byte(`"_1"`), []byte(`"1_"`), []byte(`"0x10"`), []byte(`"0b10"`)}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,sys
from backend.routes.tasks import _history
result=[]
for value in json.load(sys.stdin):
    try: result.append({'count':_history({'run_count':value})['run_count']})
    except (ValueError,TypeError,OverflowError): result.append({'error':True})
json.dump(result,sys.stdout)`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = bytes.NewReader(data)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python history: %v", err)
	}
	var expected []struct {
		Count json.RawMessage `json:"count"`
		Error bool            `json:"error"`
	}
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	for i, raw := range inputs {
		t.Run(string(raw), func(t *testing.T) {
			value, err := jsonx.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			row, err := History(jsonx.Obj("run_count", value))
			if expected[i].Error {
				if err == nil {
					t.Fatal("invalid counter accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonx.Compact(row.Value("run_count"))
			if err != nil || !bytes.Equal(got, expected[i].Count) {
				t.Fatalf("got %s, want %s: %v", got, expected[i].Count, err)
			}
		})
	}
}
