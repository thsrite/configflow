package subs

import (
	"errors"
	"fmt"
	"testing"

	"configflow/internal/jsonx"
)

// corpusConvert mirrors conv() of testdata/gen.py, the stand-in for
// convert_node_to_mihomo used in the differential run.
func corpusConvert(node *jsonx.Object) *jsonx.Object {
	if node.Value("type") == "bad" {
		return nil
	}
	out := jsonx.Obj("name", node.Value("name"), "type", getOr(node, "type", "ss"), "server", node.Value("server"), "port", node.Value("port"))
	if p := node.GetObject("params"); p != nil {
		out.Update(p.DeepCopy())
	}
	return out
}

func TestDialersCorpus(t *testing.T) {
	rows := loadCorpus(t, "dialers.jsonl.gz")
	native := nativeDialersExpectations(t)
	oracle := nativeReaderCases(t)
	kinds := map[string]int{}
	errorsSeen := 0
	for i, row := range rows {
		kinds[row.Fn]++
		switch row.Fn {
		case "extract_subscription_dialers":
			if expected, changed := changedNativeCase(t, oracle, fmt.Sprintf("subs/dialers/%d", i)); changed {
				if expected.Has("error") {
					row.Out = jsonx.Obj("error", expected.GetString("message"))
				} else {
					row.Out = expected.Value("value")
				}
			}
			if entry := native.GetObject(fmt.Sprintf("dialers/%d", i)); entry != nil {
				expected := entry.GetObject("expected")
				if expected.Has("error") {
					row.Out = jsonx.Obj("error", expected.Value("error"))
				} else {
					row.Out = expected.Value("value")
				}
			}
			got, err := ExtractSubscriptionDialers(row.In.(string))
			if _, isErr := isErrorOut(row.Out); isErr {
				if err == nil {
					t.Fatalf("row %d extract: want error, got %s", i, compact(t, got))
				}
				errorsSeen++
				continue
			}
			var g any
			if got != nil {
				g = got
			}
			if err != nil {
				t.Fatalf("row %d extract(%q): unexpected error %v", i, row.In, err)
			}
			if gs, ws := compact(t, g), compact(t, row.Out); gs != ws {
				t.Fatalf("row %d extract(%q)\n got  %s\n want %s", i, row.In, gs, ws)
			}
		case "adapt_subscription_proxies":
			in := asObj(row.In)
			proxies, _ := in.Value("proxies").([]any)
			got := AdaptSubscriptionProxies(in.GetString("subscription"), proxies, asObj(in.Value("main")), asObj(in.Value("record")), in.GetBool("chains"))
			if gs, ws := compact(t, got), compact(t, row.Out); gs != ws {
				t.Fatalf("row %d adapt %s\n got  %s\n want %s", i, compact(t, row.In), gs, ws)
			}
		case "add_dialer_groups":
			in := asObj(row.In)
			caches := in.GetObject("caches")
			main := in.GetObject("main")
			deps := DialerDeps{
				LoadCache: func(id string) *jsonx.Object {
					c := asObj(caches.Value(id))
					if c == nil {
						return nil
					}
					return c.DeepCopy()
				},
				ConvertNode: func(n *jsonx.Object) *jsonx.Object { return corpusConvert(n.DeepCopy()) },
			}
			report, err := AddDialerGroups(main, in.GetObject("config").DeepCopy(), deps)
			want := asObj(row.Out)
			wantReport := want.Value("report")
			if msg, isErr := isErrorOut(wantReport); isErr {
				errorsSeen++
				if err == nil {
					t.Fatalf("row %d add_dialer_groups: want error %q, got report %s", i, msg, compact(t, report))
				}
				var de *DialerReferenceError
				if errors.As(err, &de) && de.Message != msg {
					t.Fatalf("row %d add_dialer_groups: error %q, want %q", i, de.Message, msg)
				}
			} else if err != nil {
				t.Fatalf("row %d add_dialer_groups: unexpected error %v", i, err)
			} else {
				var r any
				if report != nil {
					r = report
				}
				if gs, ws := compact(t, r), compact(t, wantReport); gs != ws {
					t.Fatalf("row %d add_dialer_groups report\n in   %s\n got  %s\n want %s", i, compact(t, row.In), gs, ws)
				}
			}
			if gs, ws := compact(t, main), compact(t, want.Value("main")); gs != ws {
				t.Fatalf("row %d add_dialer_groups main\n in   %s\n got  %s\n want %s", i, compact(t, row.In), gs, ws)
			}
		}
	}
	t.Logf("%v, %d Python exceptions", kinds, errorsSeen)
}
