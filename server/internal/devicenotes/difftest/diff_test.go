// Package difftest compares shared display-name operations with real Python.
package difftest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/devicenotes"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

func TestDeviceNotesBoundary(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	cmd := exec.Command(python, script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	must(t, cmd.Start())
	t.Cleanup(func() {
		input.Close()
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Log(stderr.String())
		}
	})
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	configs := []string{
		`{}`,
		`{"device_notes":{"192.0.2.1":" New ","::1":"","192.0.2.0/24":"LAN"},"oxidns":{"device_policies":[{"ip":"192.0.2.1","name":"Old","mode":"smart"},{"ip":"::1","name":"Hide","mode":"direct"}]}}`,
		`{"device_notes":{"192.0.2.1":""},"oxidns":{"device_policies":[{"ip":"192.0.2.1","name":"Old","mode":"smart"}]}}`,
		`{"device_notes":{"::ffff:192.0.2.1":"Mapped","2001:db8::1":"IPv6"}}`,
		`{"device_notes":{"192.0.2.1":"中文<>&"},"oxidns":{"device_policies":[{"ip":"192.0.2.1","name":"Legacy\u007f","mode":"smart"}]}}`,
		`{"device_notes":{"192.0.2.1":"line\u2028sep\u2029end\\u2028"},"oxidns":{"device_policies":[{"ip":"192.0.2.1","mode":"smart"}]}}`,
		`{"device_notes":null}`, `{"device_notes":[]}`, `{"device_notes":{"bad":"n"}}`, `{"device_notes":{"192.0.2.1":false}}`, `{"device_notes":{"192.0.2.1":"\n"}}`,
		`{"device_notes":{"192.0.2.7/24":"a","192.0.2.0/24":"b"}}`,
		`{"oxidns":{"device_policies":false}}`,
		`{"oxidns":{"device_policies":[{"ip":"192.0.2.1","mode":"smart","extra":true}]}}`,
	}
	// Capacity boundary is deterministic: removed legacy names fill available space.
	many := map[string]string{}
	for i := range devicenotes.MaxNotes {
		many["2001:db8::"+hexAddress(i+1)] = "n"
	}
	raw, _ := json.Marshal(map[string]any{"device_notes": many, "oxidns": map[string]any{"device_policies": []any{map[string]any{"ip": "192.0.2.1", "name": "Legacy", "mode": "smart"}}}})
	configs = append(configs, string(raw))
	for index, config := range configs {
		for _, op := range []string{"stored_names", "read_notes", "policy_names_revision", "set", "prepare", "save"} {
			t.Run(op+"/"+hexAddress(index), func(t *testing.T) {
				for _, name := range []string{"Renamed", ""} {
					request := map[string]any{"op": op, "config": json.RawMessage(config), "ip": "192.0.2.1", "name": name, "rows": json.RawMessage(`[{"ip":"192.0.2.1","name":"","mode":"smart"},{"ip":"192.0.2.2","name":"new","mode":"direct"}]`)}
					raw, _ := json.Marshal(request)
					_, err := input.Write(append(raw, '\n'))
					must(t, err)
					if !scanner.Scan() {
						t.Fatalf("oracle: %v %s", scanner.Err(), stderr.String())
					}
					var expected map[string]any
					must(t, json.Unmarshal(scanner.Bytes(), &expected))
					actual := run(t, op, config, name, request["rows"].(json.RawMessage))
					if !reflect.DeepEqual(semantic(expected), semantic(actual)) {
						t.Fatalf("%s", difference(expected, actual, ""))
					}
					if op != "set" {
						break
					}
				}
			})
		}
	}
	// Names-only saving must retain exact settings and every unknown draft field.
	config := `{"device_notes":{},"oxidns":{"revision":8,"future":{"keep":1},"device_policies":[{"ip":"192.0.2.1","name":"Old","mode":"smart"}]}}`
	rows := json.RawMessage(`[{"ip":"192.0.2.1","name":"Renamed","mode":"smart"}]`)
	raw, _ = json.Marshal(map[string]any{"op": "save", "config": json.RawMessage(config), "rows": rows})
	input.Write(append(raw, '\n'))
	if !scanner.Scan() {
		t.Fatal(stderr.String())
	}
	var expected map[string]any
	must(t, json.Unmarshal(scanner.Bytes(), &expected))
	actual := run(t, "save", config, "", rows)
	if !reflect.DeepEqual(semantic(expected), semantic(actual)) {
		t.Fatalf("name-only Python=%s Go=%s", scanner.Text(), encode(actual))
	}
}
func hexAddress(n int) string {
	const digits = "0123456789abcdef"
	s := ""
	for {
		s = string(digits[n%16]) + s
		n /= 16
		if n == 0 {
			return s
		}
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func encode(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func run(t *testing.T, op, config, name string, rows json.RawMessage) map[string]any {
	t.Helper()
	var cfg map[string]json.RawMessage
	must(t, json.Unmarshal([]byte(config), &cfg))
	notes, policies := cfg["device_notes"], json.RawMessage(`[]`)
	if len(notes) == 0 {
		notes = json.RawMessage(`{}`)
	}
	var settings map[string]json.RawMessage
	if len(cfg["oxidns"]) > 0 {
		must(t, json.Unmarshal(cfg["oxidns"], &settings))
		if value, ok := settings["device_policies"]; ok {
			policies = value
		}
	}
	var result any
	var err error
	switch op {
	case "set":
		result, err = devicenotes.Set(notes, policies, "192.0.2.1", name)
	case "save":
		// This is the only storage-adapter boundary; compare the mutated document too.
		p, e := oxidns.ProfileFromJSON([]byte(config))
		must(t, e)
		s := p.Settings()
		if s == nil {
			s = oxidns.M()
		}
		rowNode, e := oxidns.FromJSON(rows)
		must(t, e)
		out, e := oxidns.SavePolicyRows(p, s, rowNode, "http://127.0.0.1:5001")
		err = e
		if err == nil {
			result = map[string]any{"same": out == s, "settings": json.RawMessage(oxidns.ToJSON(out)), "notes": p.DeviceNotesJSON()}
		}
	default:
		if op == "prepare" {
			var parsed []devicenotes.Policy
			parsed, err = devicenotes.ParsePolicies(rows)
			if err == nil {
				var state devicenotes.State
				state, err = devicenotes.New(notes, policies)
				if err == nil {
					var names devicenotes.Names
					parsed, names, err = state.Prepare(parsed)
					result = []any{parsed, names}
				}
			}
		} else {
			var state devicenotes.State
			state, err = devicenotes.New(notes, policies)
			if err == nil {
				switch op {
				case "stored_names":
					result = state.StoredNames()
				case "read_notes":
					result = state.ReadNames()
				case "policy_names_revision":
					result, err = state.Revision()
				}
			}
		}
	}
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var out map[string]any
	must(t, json.Unmarshal([]byte(encode(map[string]any{"value": result})), &out))
	return out
}

// Native drafts are parsed consumer data; indentation is not a contract.
func semantic(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if key == "native_config" || key == "template_base_config" {
				if text, ok := item.(string); ok {
					var doc any
					if yamlx.Unmarshal([]byte(text), &doc) == nil {
						v[key] = doc
						continue
					}
				}
			}
			v[key] = semantic(item)
		}
	case []any:
		for i, item := range v {
			v[i] = semantic(item)
		}
	}
	return value
}

func difference(a, b any, path string) string {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return fmt.Sprintf("%s types %T != %T", path, a, b)
	}
	switch v := a.(type) {
	case map[string]any:
		for k, x := range v {
			if y, ok := b.(map[string]any)[k]; !ok {
				return path + "." + k + " missing"
			} else if !reflect.DeepEqual(x, y) {
				return difference(x, y, path+"."+k)
			}
		}
		for k := range b.(map[string]any) {
			if _, ok := v[k]; !ok {
				return path + "." + k + " extra"
			}
		}
	case []any:
		w := b.([]any)
		if len(v) != len(w) {
			return fmt.Sprintf("%s lengths %d != %d", path, len(v), len(w))
		}
		for i, x := range v {
			if !reflect.DeepEqual(x, w[i]) {
				return difference(x, w[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
}
