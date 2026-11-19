package installer

import (
	"compress/gzip"
	"configflow/internal/installer/installertest"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

type n810ReferenceRow struct {
	Archive    string          `json:"archive"`
	ArchiveSHA string          `json:"archive_sha256"`
	RowSHA     string          `json:"row_sha256"`
	Original   json.RawMessage `json:"original"`
	Python     json.RawMessage `json:"python"`
	Native     json.RawMessage `json:"native"`
}

func referenceSHA(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func readCompressed(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
func referenceKey(raw []byte) (string, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", err
	}
	data, err := jsonx.Sorted(value)
	return referenceSHA(data), err
}
func bindReferenceRow(row n810ReferenceRow, archives map[string]map[string][]byte) error {
	original := archives[row.Archive][row.RowSHA]
	if len(original) == 0 {
		return fmt.Errorf("reference row is not in frozen archive")
	}
	have, err := referenceKey(original)
	if err != nil {
		return err
	}
	want, err := referenceKey(row.Original)
	if err != nil {
		return err
	}
	if have != want {
		return fmt.Errorf("reference input/output binding differs")
	}
	return nil
}
func loadN810References(filename string, count int) (map[string]any, error) {
	raw, err := readCompressed("testdata/" + filename)
	if err != nil {
		return nil, err
	}
	var fixture struct {
		Rows []n810ReferenceRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return nil, err
	}
	archives := map[string]map[string][]byte{}
	archiveHashes := map[string]string{}
	result := map[string]any{}
	for _, row := range fixture.Rows {
		if _, ok := archives[row.Archive]; !ok {
			archive, err := os.ReadFile("testdata/" + row.Archive)
			if err != nil {
				return nil, err
			}
			if referenceSHA(archive) != row.ArchiveSHA {
				return nil, fmt.Errorf("frozen archive SHA changed")
			}
			lines := archive
			if strings.HasSuffix(row.Archive, ".gz") {
				lines, err = readCompressed("testdata/" + row.Archive)
			}
			if err != nil {
				return nil, err
			}
			indexed := map[string][]byte{}
			for _, line := range strings.Split(strings.TrimSuffix(string(lines), "\n"), "\n") {
				indexed[referenceSHA([]byte(line))] = []byte(line)
			}
			archives[row.Archive] = indexed
			archiveHashes[row.Archive] = referenceSHA(archive)
		}
		if row.ArchiveSHA != archiveHashes[row.Archive] {
			return nil, fmt.Errorf("reference archive binding differs")
		}
		if err := bindReferenceRow(row, archives); err != nil {
			return nil, err
		}
		key, err := referenceKey(row.Original)
		if err != nil {
			return nil, err
		}
		native, err := jsonx.Parse(row.Native)
		if err != nil {
			return nil, err
		}
		result[key] = native
	}
	if len(result) != count {
		return nil, fmt.Errorf("expected %d exact native rows, got %d", count, len(result))
	}
	return result, nil
}

var n810PreflightReferences = sync.OnceValues(func() (map[string]any, error) { return loadN810References("n810_preflight_native.json.gz", 19) })
var n810SessionReferences = sync.OnceValues(func() (map[string]any, error) { return loadN810References("n810_session_native.json.gz", 70) })

var n810ScalarReferences = sync.OnceValues(func() (map[string]any, error) { return loadN810References("n810_scalar_native.json.gz", 600) })

func n810ScalarMatch(row *jsonx.Object, output string, err error) (bool, string) {
	refs, problem := n810ScalarReferences()
	if problem != nil {
		return true, problem.Error()
	}
	raw, problem := jsonx.Sorted(row)
	if problem != nil {
		return true, problem.Error()
	}
	value, present := refs[referenceSHA(raw)]
	if !present {
		return false, ""
	}
	expected := value.(*jsonx.Object)
	if failure := expected.GetObject("error"); failure != nil {
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Message != failure.GetString("message") || output != "" {
			return true, fmt.Sprintf("native integer refusal differs: %v", err)
		}
		return true, ""
	}
	if err != nil {
		return true, fmt.Sprintf("native integer output refused: %v", err)
	}
	output, problem = installertest.PythonBaseline(output)
	if problem != nil {
		return true, problem.Error()
	}
	if row.GetString("fn") == "with_install_output" {
		if output != expected.GetString("output") {
			return true, "native layout output differs"
		}
		return true, ""
	}
	output, problem = historicalEmbeddedJSON(output)
	if problem != nil {
		return true, problem.Error()
	}
	if output != expected.GetString("output") {
		return true, "native integer output differs"
	}
	return true, ""
}
func n810SessionExpected(row *jsonx.Object) (*jsonx.Object, bool, error) {
	refs, err := n810SessionReferences()
	if err != nil {
		return nil, false, err
	}
	raw, err := jsonx.Sorted(row)
	if err != nil {
		return nil, false, err
	}
	value, ok := refs[referenceSHA(raw)]
	if !ok {
		return nil, false, nil
	}
	return value.(*jsonx.Object), true, nil
}
func n810PreflightExpected(row *jsonx.Object) (any, bool, error) {
	references, err := n810PreflightReferences()
	if err != nil {
		return nil, false, err
	}
	raw, err := jsonx.Sorted(row)
	if err != nil {
		return nil, false, err
	}
	value, ok := references[referenceSHA(raw)]
	return value, ok, nil
}

func TestN810ReferenceBindingRejectsChangedOriginal(t *testing.T) {
	row := n810ReferenceRow{Archive: "synthetic", RowSHA: "row", Original: json.RawMessage(`{"input":1,"out":2}`)}
	archives := map[string]map[string][]byte{"synthetic": {"row": []byte(`{"input":1,"out":2}`)}}
	if err := bindReferenceRow(row, archives); err != nil {
		t.Fatal(err)
	}
	row.Original = json.RawMessage(`{"input":3,"out":2}`)
	if bindReferenceRow(row, archives) == nil {
		t.Fatal("changed original bypassed archive binding")
	}
}

func TestN810PreflightMachineCountersFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		change evidenceField
		check  string
	}{
		{"interface digits", evidenceField{"ipv4", 0, "２: eth0 inet 192.0.2.10/24 scope global\n"}, "ipv4"},
		{"ss digits", evidenceField{"tcp53", 0, "LISTEN ² 0 0.0.0.0:53 0.0.0.0:*\n"}, "tcp53"},
		{"ss separator", evidenceField{"tcp53", 0, "LISTEN\x1f0 0 0.0.0.0:53 0.0.0.0:*\n"}, "tcp53"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report, err := EvaluatePreflight(preflightBlock(preflightEvidence(test.change)), "host")
			if err != nil {
				t.Fatal(err)
			}
			if report.Status == "pass" {
				t.Fatal("malformed machine output passed preflight")
			}
			found := false
			for _, check := range report.Checks {
				if check.ID == test.check {
					found = true
					if check.Status != "unknown" {
						t.Fatalf("status %s", check.Status)
					}
				}
			}
			if !found {
				t.Fatal("required check missing")
			}
		})
	}
}

var n810PathReferences = sync.OnceValues(func() (map[string]any, error) { return loadN810References("n810_path_native.json.gz", 1) })

func n810PathRefusal(row *jsonx.Object, err error) (bool, error) {
	refs, problem := n810PathReferences()
	if problem != nil {
		return false, problem
	}
	raw, problem := jsonx.Sorted(row)
	if problem != nil {
		return false, problem
	}
	expected, present := refs[referenceSHA(raw)]
	if !present {
		return false, nil
	}
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Message != expected.(*jsonx.Object).GetString("message") {
		return true, fmt.Errorf("native path refusal differs: %v", err)
	}
	return true, nil
}
