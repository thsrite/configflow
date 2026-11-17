package oxidns

import "testing"

// TestC22C26ReviewReferences compares freshly generated Python public-boundary
// references without accepted-difference waivers. Outputs compare structurally;
// each input's post-state retains exact strings, including native YAML text.
func TestC22C26ReviewReferences(t *testing.T) {
	parallelBoundary(t)
	functions := map[string]bool{}
	for _, name := range []string{"load_native_yaml", "validate_native_config", "ensure_local_dns_watchdog", "apply_common_settings", "read_common_settings", "apply_policies", "read_policies", "prepare_editor", "preview_template"} {
		functions[name] = true
	}
	rows := readRows(t, "review_c22c26.jsonl.gz", functions)
	if len(rows) < 80 {
		t.Fatalf("incomplete reference set: %d rows", len(rows))
	}
	for _, r := range rows {
		t.Run(r.Fn+"/"+r.Test, func(t *testing.T) {
			t.Parallel()
			args := r.args(t)
			got, err, supported := call(t, r, args)
			if !supported {
				t.Fatalf("missing public boundary: %s", r.Fn)
			}
			if difference := compare(r, got, err); difference != "" {
				t.Fatal(difference)
			}
			if len(r.ArgsAfter) != len(args) {
				t.Fatal("reference omits argument post-state")
			}
			for i, raw := range r.ArgsAfter {
				want, err := FromJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				if difference := firstDifference(args[i], want, "argument"); difference != "" {
					t.Fatalf("argument %d: %s", i, difference)
				}
			}
		})
	}
}
