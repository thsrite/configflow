package oxidns

import (
	"encoding/json"
	"testing"
)

// TestStorageRoundTrip: OxiDNS settings and profile documents written by
// Python load and are written back without loss (key order, int/float/bool
// kinds, unknown fields, nested native fragments), and normalizing a stored
// native draft keeps every stored field.
func TestStorageRoundTrip(t *testing.T) {
	rows := readRows(t, "c26_recorded.jsonl.gz", map[string]bool{"prepare_editor": true, "apply_policies": true, "read_policies": true})
	rows = append(rows, readRows(t, "c24_recorded.jsonl.gz", map[string]bool{"read_common_settings": true, "apply_common_settings": true})...)
	checked := 0
	for _, r := range rows {
		for _, raw := range r.Args {
			var generic any
			if json.Unmarshal(raw, &generic) != nil {
				continue
			}
			if _, ok := generic.(map[string]any); !ok {
				continue
			}
			n, err := FromJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			back, err := FromJSON(ToJSON(n))
			if err != nil {
				t.Fatal(err)
			}
			if d := firstDifference(back, n, "doc"); d != "" || keysOrder(back) != keysOrder(n) {
				t.Fatalf("%s: round trip changed the document: %s", r.Test, d)
			}
			var again any
			if err := json.Unmarshal(ToJSON(n), &again); err != nil {
				t.Fatalf("%s: invalid JSON written: %v", r.Test, err)
			}
			checked++
		}
		if r.Fn != "prepare_editor" || r.Err != nil {
			continue
		}
		settings := r.args(t)[0]
		if !equal(get(settings, "config_mode"), S("native")) {
			continue
		}
		normalized, err := NormalizeSettings(clone(settings), false)
		if err != nil {
			continue
		}
		for _, key := range keys(settings) {
			if !has(normalized, key) {
				t.Fatalf("%s: normalizing dropped stored field %s", r.Test, key)
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("only %d stored documents checked", checked)
	}
	t.Logf("%d stored documents round-tripped", checked)
}

func keysOrder(n *Node) string {
	out := ""
	var walk func(*Node)
	walk = func(v *Node) {
		for i, c := range v.Content {
			if isMap(v) && i%2 == 0 {
				out += c.Value + ","
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return out
}
