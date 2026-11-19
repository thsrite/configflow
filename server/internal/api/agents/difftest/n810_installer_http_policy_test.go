package difftest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
)

// n810InstallerBinding loads immutable Python evidence once per live suite.
func n810InstallerBinding(t *testing.T) func(installerRequest, response, []string) {
	t.Helper()
	rows := map[string][]byte{}
	seen := map[string]bool{}
	recording := os.Getenv("N810_RECORD_INSTALLER_HTTP")
	if recording == "" {
		for _, row := range n810InstallerRows(t) {
			if _, duplicate := rows[row.SHA]; duplicate {
				t.Fatal("duplicate installer original row")
			}
			rows[row.SHA] = n810Canonical(row.Original)
		}
	}
	t.Cleanup(func() {
		if !t.Failed() && recording == "" && os.Getenv("CONFIGFLOW_INSTALLER_FUZZ_ITERATIONS") == "" && len(seen) != len(rows) {
			t.Errorf("bound installer originals used %d of %d", len(seen), len(rows))
		}
	})
	return func(r installerRequest, python response, mask []string) {
		t.Helper()
		input := n810InstallerInput(r)
		input.mask = mask
		original := map[string]any{"request": n810RequestKey(input), "python": n810Result(t, input, python, nil)}
		encoded := n810Canonical(original)
		sha := fmt.Sprintf("%x", sha256.Sum256(encoded))
		if recording != "" {
			file, err := os.OpenFile(recording, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := json.NewEncoder(file).Encode(n810APIRow{Original: original, SHA: sha, Kind: "installer-native-http"}); err != nil {
				t.Fatal(err)
			}
			return
		}
		if !bytes.Equal(rows[sha], encoded) {
			t.Fatal("unbound original installer HTTP input/result", r.path)
		}
		seen[sha] = true
	}
}
func n810InstallerRows(t *testing.T) []n810APIRow {
	t.Helper()
	file, err := os.Open("testdata/n810_installer_http_python.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	raw, err := io.ReadAll(zipped)
	if err != nil {
		t.Fatal(err)
	}
	var rows []n810APIRow
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row n810APIRow
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(n810Canonical(row.Original))) != row.SHA || row.Kind != "installer-native-http" {
			t.Fatal("original installer HTTP row changed")
		}
		rows = append(rows, row)
	}
	return rows
}
func TestN810InstallerHTTPBindingRejectsChangedInput(t *testing.T) {
	rows := n810InstallerRows(t)
	if len(rows) == 0 {
		t.Fatal("missing original installer rows")
	}
	row := rows[0]
	row.Original["request"].(map[string]any)["body"] = "different input"
	if fmt.Sprintf("%x", sha256.Sum256(n810Canonical(row.Original))) == row.SHA {
		t.Fatal("original installer input binding ignored")
	}
}
