package devicenotes_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"configflow/internal/devicenotes"
	"configflow/internal/oxidns"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

func dnSemantic(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if k == "native_config" || k == "template_base_config" {
				if text, ok := item.(string); ok {
					var doc any
					if yamlx.Unmarshal([]byte(text), &doc) == nil {
						out[k] = doc
						continue
					}
				}
			}
			out[k] = dnSemantic(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = dnSemantic(item)
		}
		return out
	}
	return v
}

func runDeviceNotesOp(t *testing.T, op, config, name string, rows json.RawMessage) map[string]any {
	t.Helper()
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		t.Fatal(err)
	}
	notes, policies := cfg["device_notes"], json.RawMessage(`[]`)
	if len(notes) == 0 {
		notes = json.RawMessage(`{}`)
	}
	var settings map[string]json.RawMessage
	if len(cfg["oxidns"]) > 0 {
		if err := json.Unmarshal(cfg["oxidns"], &settings); err != nil {
			t.Fatal(err)
		}
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
		p, e := oxidns.ProfileFromJSON([]byte(config))
		if e != nil {
			t.Fatal(e)
		}
		s := p.Settings()
		if s == nil {
			s = oxidns.M()
		}
		var parsed []devicenotes.Policy
		parsed, err = devicenotes.ParsePolicies(rows)
		if err == nil {
			out, e := oxidns.SavePolicyRows(p, s, parsed, "http://127.0.0.1:5001")
			err = e
			if err == nil {
				result = map[string]any{
					"same":     out == s,
					"settings": json.RawMessage(oxidns.ToJSON(out)),
					"notes":    p.DeviceNotesJSON(),
				}
			}
		}
	case "prepare":
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
	default:
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
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	raw, err := json.Marshal(map[string]any{"value": result})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeviceNotesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	configs := []string{
		`{}`,
		`{"device_notes":{"192.0.2.1":" New ","::1":"","192.0.2.0/24":"LAN"},"oxidns":{"device_policies":[{"ip":"192.0.2.1","name":"Old","mode":"smart"},{"ip":"::1","name":"Hide","mode":"direct"}]}}`,
		`{"device_notes":{"192.0.2.1":""},"oxidns":{"device_policies":[{"ip":"192.0.2.1","name":"Old","mode":"smart"}]}}`,
		`{"device_notes":{"::ffff:192.0.2.1":"Mapped","2001:db8::1":"IPv6"}}`,
		`{"device_notes":{"192.0.2.1":"中文<>&"},"oxidns":{"device_policies":[{"ip":"192.0.2.1","name":"Legacy\u007f","mode":"smart"}]}}`,
		`{"device_notes":{"192.0.2.1":"line\u2028sep\u2029end\\u2028"},"oxidns":{"device_policies":[{"ip":"192.0.2.1","mode":"smart"}]}}`,
		`{"device_notes":null}`,
		`{"device_notes":[]}`,
		`{"device_notes":{"bad":"n"}}`,
		`{"device_notes":{"192.0.2.1":false}}`,
		`{"device_notes":{"192.0.2.1":"\n"}}`,
		`{"device_notes":{"192.0.2.7/24":"a","192.0.2.0/24":"b"}}`,
		`{"oxidns":{"device_policies":false}}`,
		`{"oxidns":{"device_policies":[{"ip":"192.0.2.1","mode":"smart","extra":true}]}}`,
	}
	many := map[string]string{}
	for i := range devicenotes.MaxNotes {
		many["2001:db8::"+strconv.FormatInt(int64(i+1), 16)] = "n"
	}
	rawMany, _ := json.Marshal(map[string]any{
		"device_notes": many,
		"oxidns":       map[string]any{"device_policies": []any{map[string]any{"ip": "192.0.2.1", "name": "Legacy", "mode": "smart"}}},
	})
	configs = append(configs, string(rawMany))

	rows := json.RawMessage(`[{"ip":"192.0.2.1","name":"","mode":"smart"},{"ip":"192.0.2.2","name":"new","mode":"direct"}]`)
	ops := []string{"stored_names", "read_notes", "policy_names_revision", "set", "prepare", "save"}

	for idx, cfg := range configs {
		for _, op := range ops {
			names := []string{"Renamed"}
			if op == "set" {
				names = []string{"Renamed", ""}
			}
			for _, name := range names {
				actual := runDeviceNotesOp(t, op, cfg, name, rows)
				key, _ := json.Marshal(map[string]string{"op": op, "cfg": strconv.Itoa(idx), "name": name})
				val, _ := json.Marshal(dnSemantic(actual))
				ref.Assert(t, key, val)
			}
		}
	}

	// Names-only save preserves settings and unknown draft fields.
	nameOnlyCfg := `{"device_notes":{},"oxidns":{"revision":8,"future":{"keep":1},"device_policies":[{"ip":"192.0.2.1","name":"Old","mode":"smart"}]}}`
	nameOnlyRows := json.RawMessage(`[{"ip":"192.0.2.1","name":"Renamed","mode":"smart"}]`)
	actual := runDeviceNotesOp(t, "save", nameOnlyCfg, "", nameOnlyRows)
	key, _ := json.Marshal(map[string]string{"op": "name-only-save"})
	val, _ := json.Marshal(dnSemantic(actual))
	ref.Assert(t, key, val)
}
