package agents

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"bytes"
	"configflow/internal/testgolden"
)

func nativeDownloadPolicy(t *testing.T) func(string, any, string) string {
	t.Helper()
	var policy struct {
		Archive string `json:"archive_sha256"`
		Rows    []struct {
			Original json.RawMessage `json:"original"`
			SHA      string          `json:"sha256"`
			Native   string          `json:"native"`
		} `json:"rows"`
	}
	raw, err := testgolden.ReadFile(t, "testdata/n810_download_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}

	if policy.Archive != "f48d9d4fe53965be138f860382176ba71d21fc1feb4df4b79f0b568ce8bcc557" {
		t.Fatal("original archive evidence changed")
	}
	canonical := func(value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var parsed any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(parsed); err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	}
	rules := map[string]string{}
	for _, row := range policy.Rows {
		if fmt.Sprintf("%x", sha256.Sum256(canonical(row.Original))) != row.SHA {
			t.Fatalf("complete original conditional row changed: got %x want %s input %s", sha256.Sum256(canonical(row.Original)), row.SHA, canonical(row.Original))
		}
		rules[string(canonical(row.Original))] = row.Native
	}
	return func(kind string, input any, old string) string {
		original := map[string]any{"kind": kind, "input": input, "python": old}
		if native, present := rules[string(canonical(original))]; present {
			return native
		}
		return old
	}
}
func TestN810DownloadPolicyDoesNotAcceptChangedInput(t *testing.T) {
	policy := nativeDownloadPolicy(t)
	old := "raise:OverflowError:synthetic"
	if got := policy("date", "different input", old); got != old {
		t.Fatal("unbound input accepted")
	}
}

func TestN810DownloadOriginalEntriesStayUnchanged(t *testing.T) {
	raw, err := os.ReadFile("testdata/pyref/TestReviewConditionalMatchesWerkzeug.json")
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Entries json.RawMessage `json:"entries"`
	}
	if err = json.Unmarshal(raw, &archive); err != nil {
		t.Fatal(err)
	}
	var entries any
	if err = json.Unmarshal(archive.Entries, &entries); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("testdata/n810_download_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Entries string `json:"entries_sha256"`
	}
	if err = json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(encoded)) != policy.Entries {
		t.Fatal("original Python entries changed")
	}
}
