package onboarding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

func noLoop(*yamlx.Node, []netip.Addr) (bool, error) { return false, nil }
func pythonRows(t *testing.T) []map[string]json.RawMessage {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs real Python")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "server/internal/gateway/onboarding/testdata/reference.py")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python: %v\n%s", err, stderr.String())
	}
	rows := []map[string]json.RawMessage{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var row map[string]json.RawMessage
		if err = json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("Python boundary corpus is empty")
	}
	return rows
}
func normalized(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for key, value := range x {
				if s, ok := value.(string); ok && (key == "custom_config" || key == "native_config") && s != "" {
					node, err := yamlx.Parse([]byte(s))
					if err == nil {
						b, err := yamlx.ToJSON(node)
						if err != nil {
							t.Fatal(err)
						}
						var parsed any
						if err = json.Unmarshal(b, &parsed); err != nil {
							t.Fatal(err)
						}
						x[key] = walk(parsed)
						continue
					}
				}
				x[key] = walk(value)
			}
		case []any:
			for i, value := range x {
				x[i] = walk(value)
			}
		}
		return v
	}
	return walk(value)
}
func equalJSON(t *testing.T, label string, got, want json.RawMessage) {
	t.Helper()
	a, b := normalized(t, got), normalized(t, want)
	if !reflect.DeepEqual(a, b) {
		ga, _ := json.Marshal(a)
		wa, _ := json.Marshal(b)
		t.Errorf("%s\ngot  %s\nwant %s", label, ga, wa)
	}
}

// TestPythonPreparation compares real candidates, generated service documents,
// read-state and install readiness. DNS-loop wiring is tested after C34 merges;
// this independent corpus contains no Mihomo loop and injects that seam only.
func TestPythonPreparation(t *testing.T) {
	gateway.Wire()
	mihomoenv.Wire()
	rows := pythonRows(t)
	t.Logf("real Python boundary cases: %d", len(rows))
	for _, row := range rows {
		name := stringValue(row["name"])
		t.Run(name, func(t *testing.T) {
			config := object(row["config"])
			before := encode(config)
			options := Options{BaseURL: "https://panel.example", Takeover: stringValue(row["takeover"]), DNSLoop: noLoop}
			state, err := ReadState(config, 7, options)
			if expected, ok := row["state_error"]; ok {
				if err == nil || err.Error() != stringValue(expected) {
					t.Errorf("state err %v want %s", err, expected)
				}
			} else if err != nil {
				t.Errorf("state: %v", err)
			} else {
				equalJSON(t, "state", encode(state), row["state"])
			}
			form, err := ParseRequest(row["body"])
			if err != nil {
				if expected, ok := row["error"]; !ok || err.Error() != stringValue(expected) {
					t.Errorf("input err %v expected %s", err, expected)
				}
				return
			}
			candidate, report, err := Prepare(context.Background(), config, form, options)
			if err != nil {
				t.Fatal(err)
			}
			if expected, ok := row["error"]; ok {
				t.Fatalf("expected input rejection %s got report %+v", expected, report)
			}
			wantReport := row["report"]
			if name == "null-policy" {
				// Explicit JSON null policy renders as JSON text 'null' rather than Python's 'None'.
				wantReport = bytes.ReplaceAll(wantReport, []byte("'None'"), []byte("'null'"))
			}
			equalJSON(t, "report", encode(report), wantReport)
			equalJSON(t, "candidate", encode(candidate), row["candidate"])
			if report.Ready {
				generated, err := generate(context.Background(), candidate, options.BaseURL)
				if err != nil {
					t.Fatal(err)
				}
				data, err := yamlx.ToJSON(generated)
				if err != nil {
					t.Fatal(err)
				}
				equalJSON(t, "generated Mihomo", data, row["mihomo"])
				p, err := oxidns.ProfileFromJSON(encode(candidate))
				if err != nil {
					t.Fatal(err)
				}
				output, err := oxidns.Generate(p, options.BaseURL)
				if err != nil {
					t.Fatal(err)
				}
				node, err := yamlx.Parse([]byte(output))
				if err != nil {
					t.Fatal(err)
				}
				data, err = yamlx.ToJSON(node)
				if err != nil {
					t.Fatal(err)
				}
				equalJSON(t, "generated OxiDNS", data, row["oxidns"])
				ready, err := AssertReady(context.Background(), candidate, options)
				if expected, ok := row["readiness_error"]; ok {
					if err == nil || err.Error() != stringValue(expected) {
						t.Errorf("readiness %v want %s", err, expected)
					}
				} else if err != nil {
					t.Errorf("readiness %v", err)
				} else {
					equalJSON(t, "readiness", encode(ready), row["readiness"])
				}
			}
			if !bytes.Equal(before, encode(config)) {
				t.Fatal("read/prepare mutated source")
			}
		})
	}
}
