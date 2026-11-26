package oxidns

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestC29NativePythonBoundary(t *testing.T) {
	rows := readRows(t, "c29_native.jsonl.gz", map[string]bool{"apply_native_settings": true})
	if len(rows) < 200 {
		t.Fatal("incomplete native public references")
	}
	generated := 0
	for i, reference := range rows {
		t.Run(reference.Test+"/"+stringIndex(i), func(t *testing.T) {
			args := reference.args(t)
			var got *Node
			var err error
			if !isStr(args[1]) {
				err = fail(NativeConfigTextMessage)
			} else {
				var draft *NativeDraft
				draft, err = PrepareNativeDraft(args[0], str(args[1]), profileOf(args[2]), str(args[3]))
				if err == nil {
					var options NativeApplyOptions
					options, err = DecodeNativeOptions(reference.Args[4], reference.Args[5])
					if err == nil {
						var result NativeApplied
						result, err = draft.Apply(options)
						if err == nil {
							got = toNode(result.View)
							set(got, "settings", result.Settings)
							set(got, "notices", strs(result.Notices))
							generated++
						}
					}
				}
			}
			if difference := compare(t, reference, got, err); difference != "" {
				t.Fatal(difference)
			}
			if len(reference.References) > 0 {
				var referenced *ReferenceError
				if !errors.As(err, &referenced) {
					t.Fatal("missing reference diagnostics")
				}
				encoded, marshalErr := json.Marshal(referenced.References)
				actual, actualErr := FromJSON(encoded)
				expected, expectedErr := FromJSON(reference.References)
				if marshalErr != nil || actualErr != nil || expectedErr != nil {
					t.Fatal("reference encoding failed")
				}
				if difference := firstDifference(actual, expected, "references"); difference != "" {
					t.Fatal(difference)
				}
			}
			for index, raw := range reference.ArgsAfter {
				expected, err := FromJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				if difference := firstDifference(args[index], expected, "input"); difference != "" {
					t.Fatal(difference)
				}
			}
		})
	}
	if generated < 80 {
		t.Fatal("insufficient successful complete-form drafts")
	}
	t.Logf("C29 actual Python boundaries=%d successful=%d", len(rows), generated)
}

func stringIndex(i int) string { data, _ := json.Marshal(i); return string(data) }
