package routers

import (
	"bytes"
	"configflow/internal/devhttp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/routers/routerfake"
)

type r06Case struct {
	DuplicateName  bool                  `json:"-"`
	Timeout        bool                  `json:"timeout,omitempty"`
	Name           string                `json:"-"`
	Vendor         string                `json:"vendor"`
	Action         string                `json:"action"`
	Values         *RouteValues          `json:"values,omitempty"`
	Payload        json.RawMessage       `json:"payload,omitempty"`
	Router         Router                `json:"router"`
	ExpectedChange map[string]any        `json:"expected_change,omitempty"`
	ROS            routerfake.State      `json:"-"`
	IK             routerfake.IkuaiState `json:"-"`
}
type r06Error struct {
	Class   string `json:"class"`
	Message string `json:"message"`
}
type r06Outcome struct {
	Result  any               `json:"result"`
	Error   *r06Error         `json:"error"`
	Records []json.RawMessage `json:"records"`
}

func r06JSON(v any) json.RawMessage { data, _ := json.Marshal(v); return data }
func r06Normalize(v any) any        { var out any; _ = json.Unmarshal(r06JSON(v), &out); return out }
func r06Equivalent(a, b any) bool   { return reflect.DeepEqual(r06Normalize(a), r06Normalize(b)) }
func r06StripHashes(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			if (k == "revision" && x["cidr"] != nil && x["snapshot"] == nil) || (k == "fingerprint" && x["summary"] != nil && x["action"] != nil) {
				if s, ok := item.(string); ok && len(s) == 64 {
					x[k] = "<opaque-sha256>"
				}
			} else if k != "snapshot" {
				r06StripHashes(item)
			}
		}
	case []any:
		for _, item := range x {
			r06StripHashes(item)
		}
	}
}
func r06Base(vendor, action string) r06Case {
	tc := r06Case{Vendor: vendor, Action: action, Values: &RouteValues{CIDR: "198.51.100.0/24", Gateway: "192.0.2.3", Enabled: true, Comment: "用户 & backup\n备注"}, Router: Router{RouterSettings: RouterSettings{ID: "r1", Type: vendor, Endpoint: "http://example.com", Username: "synthetic-admin", VerifyTLS: true}}}
	tc.ROS.Tables = map[string][]routerfake.Row{"/ip/route": {{".id": "*10", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.1", "routing-table": "main", "disabled": "false", "comment": "office", "distance": "17", "scope": "20", "target-scope": "12", "check-gateway": "ping", "pref-src": "192.0.2.2", "suppress-hw-offload": "true", "dynamic": "false", "static": "true", "active": "true", "blackhole": "false", "immediate-gw": "192.0.2.1%ether1", "hw-offloaded": "false"}}}
	tc.IK.Tables = map[string][]routerfake.Row{"static_rt": {{"id": 1, "enabled": "yes", "tagname": "RT_1", "ip_type": "4", "interface": "lan4", "dst_addr": "203.0.113.0", "gateway": "192.0.2.1", "netmask": "255.255.255.0", "prio": 7, "comment": "office", "status": 1}}}
	return tc
}
func r06Cases() []r06Case {
	cases := []r06Case{}
	add := func(tc r06Case, name string) { tc.Name = tc.Vendor + "/" + name; cases = append(cases, tc) }
	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, action := range []string{"read", "checked_read", "add", "update", "delete"} {
			add(r06Base(vendor, action), action)
		}
		for _, action := range []string{"add", "update", "delete"} {
			for _, failure := range []string{"reject", "lost", "readback", "persistent_read", "undo_lost", "undo_failed", "foreign_after_write", "id_reused"} {
				tc := r06Base(vendor, action)
				method := map[string]string{"add": "PUT", "update": "PATCH", "delete": "DELETE"}[action]
				path := "/ip/route"
				if action != "add" {
					path += "/*10"
				}
				apiAction := map[string]string{"add": "add", "update": "edit", "delete": "del"}[action]
				if vendor == "routeros" {
					switch failure {
					case "reject":
						tc.ROS.Faults = []routerfake.Fault{{Method: method, Path: path, Count: 1, Status: 400}}
					case "lost":
						tc.ROS.Faults = []routerfake.Fault{{Method: method, Path: path, Count: 1, AfterCommit: true, LoseReply: true}}
					case "readback", "persistent_read", "undo_lost", "undo_failed":
						tc.ROS.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 3, Status: 503}}
						if failure == "persistent_read" {
							tc.ROS.Faults = append(tc.ROS.Faults, routerfake.Fault{Method: "GET", Path: "/ip/route", Count: 4, Status: 503})
						}
						if failure == "undo_lost" || failure == "undo_failed" {
							undoMethod, undoPath := method, path
							count := 2
							if action == "delete" {
								undoMethod, undoPath, count = "PUT", "/ip/route", 1
							}
							if action == "add" {
								undoMethod, undoPath, count = "DELETE", "/ip/route/*65", 1
							}
							f := routerfake.Fault{Method: undoMethod, Path: undoPath, Count: count, AfterCommit: failure == "undo_lost", LoseReply: failure == "undo_lost"}
							if failure == "undo_failed" {
								f.Status = 400
							}
							tc.ROS.Faults = append(tc.ROS.Faults, f)
						}
					case "foreign_after_write":
						if action == "delete" {
							tc.ROS.Mutations = []routerfake.Mutation{{Method: method, Path: path, Count: 1, Table: "/ip/route", Append: routerfake.Row{".id": "*99", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.99", "comment": "foreign"}}}
							tc.ROS.Faults = []routerfake.Fault{{Method: method, Path: path, Count: 1, AfterCommit: true, LoseReply: true}}
						} else {
							id := "*10"
							if action == "add" {
								id = "*65"
							}
							tc.ROS.Mutations = []routerfake.Mutation{{Method: method, Path: path, Count: 1, Table: "/ip/route", ID: id, Set: routerfake.Row{"distance": "77"}}}
						}
					case "id_reused":
						if action == "add" {
							continue
						}
						tc.ROS.Mutations = []routerfake.Mutation{{Method: method, Path: path, Count: 1, Table: "/ip/route", ID: "*10", Set: routerfake.Row{"comment": "foreign", "gateway": "192.0.2.99"}}}
						if action == "delete" {
							tc.ROS.Mutations[0].Append = routerfake.Row{".id": "*10", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.99", "comment": "foreign"}
						}
						tc.ROS.Faults = []routerfake.Fault{{Method: method, Path: path, Count: 1, AfterCommit: true, LoseReply: true}}
					}
				} else {
					switch failure {
					case "reject":
						code := 2
						tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: apiAction, Count: 1, Code: &code, Message: "rejected"}}
					case "lost":
						tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: apiAction, Count: 1, LoseReply: true}}
					case "readback", "persistent_read", "undo_lost", "undo_failed":
						tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "show", Count: 3, Drop: true}}
						if failure == "persistent_read" {
							tc.IK.Faults = append(tc.IK.Faults, routerfake.IkuaiFault{Func: "static_rt", Action: "show", Count: 4, Drop: true})
						}
						if failure == "undo_lost" || failure == "undo_failed" {
							undoAction, count := apiAction, 2
							if action == "delete" {
								undoAction, count = "add", 1
							}
							if action == "add" {
								undoAction, count = "del", 1
							}
							f := routerfake.IkuaiFault{Func: "static_rt", Action: undoAction, Count: count, LoseReply: failure == "undo_lost"}
							if failure == "undo_failed" {
								code := 2
								f.Code = &code
								f.Message = "rejected"
							}
							tc.IK.Faults = append(tc.IK.Faults, f)
						}
					case "foreign_after_write", "id_reused":
						id := int64(1)
						if action == "add" {
							id = 2
						}
						m := routerfake.IkuaiMutation{Func: "static_rt", Action: apiAction, Count: 1, OnWrite: true, Table: "static_rt", ID: id, Set: routerfake.Row{"prio": 99}}
						if action == "delete" {
							m.Append = routerfake.Row{"id": 1, "enabled": "yes", "tagname": "foreign", "ip_type": "4", "interface": "auto", "dst_addr": "203.0.113.0", "gateway": "192.0.2.99", "netmask": "255.255.255.0", "prio": 7, "comment": "foreign"}
							tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: apiAction, Count: 1, LoseReply: true}}
						}
						if failure == "id_reused" && action != "delete" {
							m.Set = routerfake.Row{"tagname": "foreign", "comment": "foreign"}
						}
						tc.IK.Mutations = []routerfake.IkuaiMutation{m}
					}
				}
				add(tc, action+"/"+failure)
			}
		}
		for _, action := range []string{"update", "delete"} {
			tc := r06Base(vendor, action)
			if vendor == "routeros" {
				tc.ROS.Mutations = []routerfake.Mutation{{Method: "GET", Path: "/ip/route", Count: 2, Table: "/ip/route", ID: "*10", Set: routerfake.Row{"distance": "99"}}}
			} else {
				tc.IK.Mutations = []routerfake.IkuaiMutation{{Func: "static_rt", Action: "show", Count: 2, Table: "static_rt", ID: 1, Set: routerfake.Row{"prio": 99}}}
			}
			add(tc, action+"/preflight_drift")
		}
		tc := r06Base(vendor, "update")
		tc.Values = &RouteValues{CIDR: "203.0.113.0/24", Gateway: "192.0.2.1", Enabled: true, Comment: "office"}
		add(tc, "noop")
		for _, action := range []string{"add", "update", "delete"} {
			tc := r06Base(vendor, "plan")
			tc.Payload = r06JSON(map[string]any{"action": action, "values": tc.Values})
			if action == "delete" {
				tc.Payload = r06JSON(map[string]string{"action": action})
			}
			add(tc, "plan/"+action)
		}
	}
	rosFields := map[string]any{"dynamic": "true", "connect": "true", "static": "false", "ecmp": "true", "blackhole": "true", "invalid": "true", "routing-table": "backup", "dst-address": "2001:db8::/32", "gateway": "ether1", "vrf-interface": "vrf1", "future": "x", "disabled": "maybe", "comment": map[string]any{"x": 1}, ".id": "*invalid"}
	for field, value := range rosFields {
		tc := r06Base("routeros", "read")
		tc.ROS.Tables["/ip/route"][0][field] = value
		add(tc, "readonly/"+field)
	}
	ikFields := map[string]any{"future": "x", "ip_type": "6", "gateway": true, "prio": "7", "enabled": "maybe", "interface": "", "tagname": "", "dst_addr": "blackhole", "comment": []any{}}
	for field, value := range ikFields {
		tc := r06Base("ikuai", "read")
		tc.IK.Tables["static_rt"][0][field] = value
		add(tc, "readonly/"+field)
	}
	// Review F4: a non-string interface is also an incomplete interface.
	tc := r06Base("ikuai", "read")
	tc.IK.Tables["static_rt"][0]["interface"] = 4
	add(tc, "readonly/interface_number")
	return append(append(cases, r06ExtraCases()...), r06ReconciliationCases()...)
}

func TestR06PythonBoundary(t *testing.T) {
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Fatal("Python reference unavailable:", err)
	}
	cwd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(cwd, "../../.."))
	ros := routerfake.New(nil)
	rs := httptest.NewServer(ros)
	defer rs.Close()
	ik := routerfake.NewIkuai(routerfake.IkuaiState{})
	is := httptest.NewServer(ik)
	defer is.Close()
	cases := r06Cases()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			endpoint := rs.URL
			reset := func() { ros.Reset(tc.ROS) }
			snapshot := func() (any, any) { s := ros.Snapshot(); return s.Calls, s.Tables }
			if tc.Vendor == "ikuai" {
				endpoint = is.URL
				reset = func() { ik.Reset(tc.IK) }
				snapshot = func() (any, any) { s := ik.Snapshot(); return s.Calls, s.Tables }
			}
			reset()
			cmd := exec.Command(python, filepath.Join(cwd, "testdata/r06_reference.py"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R06_ENDPOINT="+endpoint)
			cmd.Stdin = bytes.NewReader(r06JSON(tc))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Python: %v %s", err, stderr.String())
			}
			var py r06Outcome
			if err := json.Unmarshal(output, &py); err != nil {
				t.Fatalf("Python JSON: %v %s", err, output)
			}
			pyCalls, pyTables := snapshot()
			reset()
			goResult := r06Run(tc, endpoint)
			goCalls, goTables := snapshot()
			if tc.DuplicateName {
				if goResult.Error == nil || goResult.Error.Class != "RouterConflictError" || goResult.Error.Message != messageEditIKUniqueName {
					t.Fatalf("duplicate-name safety difference not observed: Python=%#v Go=%#v", py.Error, goResult.Error)
				}
				pythonWrote := false
				for _, call := range pyCalls.([]routerfake.IkuaiCall) {
					if body, ok := call.Body.(map[string]any); ok && body["action"] != nil && body["action"] != "show" {
						pythonWrote = true
					}
				}
				if !pythonWrote {
					t.Fatal("Python no longer attempts the ambiguous mutation")
				}
				state := ik.Snapshot()
				for _, call := range state.Calls {
					if body, ok := call.Body.(map[string]any); ok && body["action"] != nil && body["action"] != "show" {
						t.Fatal("ambiguous identity wrote to device")
					}
				}
				if len(state.Tables["static_rt"]) != 2 || !r06Equivalent(state.Tables["static_rt"], tc.IK.Tables["static_rt"]) {
					t.Fatal("ambiguous identity changed the scene")
				}
				return
			}
			a, b := r06Normalize(py), r06Normalize(goResult)
			r06StripHashes(a)
			r06StripHashes(b)
			if tc.Name == "routeros/readonly/comment" {
				// Python repr of a malformed device field is outside functional parity.
				pyRow := a.(map[string]any)["result"].([]any)[0].(map[string]any)
				goRow := b.(map[string]any)["result"].([]any)[0].(map[string]any)
				if pyRow["editable"] != false || goRow["editable"] != false || goRow["comment"] != "" {
					t.Fatal("malformed comment became editable")
				}
				pyRow["comment"] = goRow["comment"]
			}
			for _, pair := range []struct {
				name string
				a, b any
			}{{"outcome", a, b}, {"requests", pyCalls, goCalls}, {"tables", pyTables, goTables}} {
				if !r06Equivalent(pair.a, pair.b) {
					t.Errorf("%s mismatch\nPython %s\nGo     %s", pair.name, r06JSON(pair.a), r06JSON(pair.b))
				}
			}
		})
	}
	t.Logf("%d real Python boundary cases", len(cases))
}
func r06Run(tc r06Case, endpoint string) r06Outcome {
	out := r06Outcome{Records: []json.RawMessage{}}
	var editor RouteEditor
	var driver Driver
	if tc.Vendor == "ikuai" {
		d := NewIkuai(endpoint, "synthetic-admin", "synthetic-secret", false)
		if tc.Timeout {
			d.client = r06ShortHTTP{d.client}
		}
		editor, driver = d, d
	} else {
		d := NewRouterOS(endpoint, "synthetic-admin", "synthetic-secret", false)
		if tc.Timeout {
			d.client = r06ShortHTTP{d.client}
		}
		editor, driver = d, d
	}
	defer driver.Close()
	var result any
	var err error
	if tc.Action == "normalize" {
		result, err = NormalizeRouteRequest(tc.Payload)
	} else {
		var rows []*EditableRoute
		rows, err = editor.ReadRoutes(context.Background())
		if err == nil {
			switch tc.Action {
			case "read":
				result = rows
			case "checked_read":
				result, err = ReadRoutes(context.Background(), driver)
			case "plan":
				var payload map[string]any
				_ = json.Unmarshal(tc.Payload, &payload)
				if payload["action"] != "add" {
					payload["id"] = rows[0].ID
					payload["revision"] = rows[0].Public().Revision
				}
				result, err = BuildRoutePlan(tc.Router, rows, r06JSON(payload))
			default:
				var expected *EditableRoute
				if tc.Action != "add" {
					expected = rows[0]
				}
				if expected != nil && tc.ExpectedChange != nil {
					if cidr, ok := tc.ExpectedChange["cidr"].(string); ok {
						expected.CIDR = cidr
					}
				}
				if d, ok := driver.(*Ikuai); ok {
					result, err = d.mutateRoute(context.Background(), tc.Action, expected, tc.Values, "RT0123456789abc")
				} else {
					result, err = editor.MutateRoute(context.Background(), tc.Action, expected, tc.Values)
				}
			}
		}
	}
	if err != nil {
		class := "crash"
		if e, ok := errors.AsType[*RouterError](err); ok {
			class = map[ErrorKind]string{KindRouter: "RouterError", KindConflict: "RouterConflictError", KindRollback: "RouterRollbackError", KindUncertainWrite: "UncertainWrite", KindRejected: "_Rejected"}[e.Kind]
		} else if _, ok := errors.AsType[*ValidationError](err); ok {
			class = "RouteEditValidationError"
		}
		out.Error = &r06Error{class, err.Error()}
	} else {
		out.Result = result
	}
	return out
}

func TestR06ComparatorMutations(t *testing.T) {
	original := r06Normalize(map[string]any{"result": map[string]any{"changed": true}, "request": map[string]any{"gateway": "192.0.2.1"}, "table": []any{map[string]any{"id": 1, "tagname": "ours"}}})
	for _, field := range []string{"result", "request", "table"} {
		t.Run(field, func(t *testing.T) {
			mutated := r06Normalize(original).(map[string]any)
			mutated[field] = "deliberately-wrong"
			if r06Equivalent(original, mutated) {
				t.Fatal("comparator missed mutation")
			}
		})
	}
}

func r06ExtraCases() []r06Case {
	cases := []r06Case{}
	add := func(tc r06Case, name string) { tc.Name = tc.Vendor + "/" + name; cases = append(cases, tc) }
	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, action := range []string{"add", "update"} {
			tc := r06Base(vendor, action)
			if vendor == "routeros" {
				tc.ROS.Tables["/ip/route"] = append(tc.ROS.Tables["/ip/route"], routerfake.Row{".id": "*99", "dst-address": "198.51.100.0/24", "gateway": "192.0.2.99", "disabled": "true"})
			} else {
				row := routerfake.Row{}
				for k, v := range tc.IK.Tables["static_rt"][0] {
					row[k] = v
				}
				row["id"], row["dst_addr"], row["enabled"] = 99, "198.51.100.0", "no"
				row["tagname"] = "RT99"
				tc.IK.Tables["static_rt"] = append(tc.IK.Tables["static_rt"], row)
			}
			add(tc, action+"/duplicate_disabled")
		}
		tc := r06Base(vendor, "read")
		if vendor == "routeros" {
			tc.ROS.Tables["/ip/route"] = append(tc.ROS.Tables["/ip/route"], tc.ROS.Tables["/ip/route"][0])
		} else {
			tc.IK.Tables["static_rt"] = append(tc.IK.Tables["static_rt"], tc.IK.Tables["static_rt"][0])
		}
		add(tc, "duplicate_ids")
		for _, field := range []string{"dynamic", "runtime", "missing_comment"} {
			tc := r06Base(vendor, "update")
			if field == "dynamic" {
				if vendor == "routeros" {
					tc.ROS.Tables["/ip/route"][0]["dynamic"] = "true"
				} else {
					tc.IK.Tables["static_rt"][0]["ip_type"] = "6"
				}
			} else if field == "runtime" {
				if vendor == "routeros" {
					tc.ROS.Mutations = []routerfake.Mutation{{Method: "GET", Path: "/ip/route", Count: 2, Table: "/ip/route", ID: "*10", Set: routerfake.Row{"active": "false", "inactive": "true"}}}
				} else {
					tc.IK.Mutations = []routerfake.IkuaiMutation{{Func: "static_rt", Action: "show", Count: 2, Table: "static_rt", ID: 1, Set: routerfake.Row{"status": 99}}}
				}
			} else {
				if vendor == "routeros" {
					delete(tc.ROS.Tables["/ip/route"][0], "comment")
				} else {
					delete(tc.IK.Tables["static_rt"][0], "prio")
				}
			}
			add(tc, "update/"+field)
		}
		tc = r06Base(vendor, "update")
		tc.ExpectedChange = map[string]any{"cidr": "192.0.2.0/24"}
		add(tc, "update/expected_identity_changed")
		for _, comment := range []string{"cf-user", "  user  ", "备注😀\n\"$&"} {
			tc := r06Base(vendor, "plan")
			tc.Values.Comment = comment
			tc.Payload = r06JSON(map[string]any{"action": "add", "values": tc.Values})
			add(tc, "plan/comment/"+comment)
		}
	}
	tc := r06Base("ikuai", "read")
	row := tc.IK.Tables["static_rt"][0]
	tc.IK.Tables["static_rt"] = nil
	for i := 1; i <= 1001; i++ {
		r := routerfake.Row{}
		for k, v := range row {
			r[k] = v
		}
		r["id"] = i
		r["tagname"] = fmt.Sprintf("RT%d", i)
		tc.IK.Tables["static_rt"] = append(tc.IK.Tables["static_rt"], r)
	}
	add(tc, "pagination_1001")
	tc = r06Base("ikuai", "add")
	tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 1, Corrupt: routerfake.Row{"prio": 99}}}
	add(tc, "add/unprovable_new_row")
	tc = r06Base("ikuai", "add")
	tc.IK.Mutations = []routerfake.IkuaiMutation{{Func: "static_rt", Action: "add", Count: 1, OnWrite: true, Table: "static_rt", Append: routerfake.Row{"id": 99, "tagname": "RT0123456789abc", "dst_addr": "198.51.100.0"}}}
	add(tc, "add/ambiguous_name")
	tc = r06Base("routeros", "add")
	tc.ROS.Faults = []routerfake.Fault{{Method: "PUT", Path: "/ip/route", Count: 1, AfterCommit: true, Body: routerfake.Row{"ret": "untrusted"}}}
	add(tc, "add/missing_returned_id")
	for i, raw := range []string{`null`, `[]`, `{}`, `{"action":"flush"}`, `{"action":"add","future":true}`, `{"action":"delete","id":"*1"}`, `{"action":"delete","id":"*1","revision":"` + strings.Repeat("a", 64) + `","values":{}}`, `{"action":"add","id":"*1"}`, `{"action":"add","values":null}`} {
		tc = r06Base("routeros", "normalize")
		tc.Payload = json.RawMessage(raw)
		add(tc, fmt.Sprintf("normalize/%d", i))
	}
	base := RouteValues{CIDR: "198.51.100.9/24", Gateway: "192.0.2.3", Enabled: true, Comment: "user"}
	for key, list := range map[string][]any{"cidr": {"2001:db8::/32", "bad", true}, "gateway": {"0.0.0.0", "127.0.0.1", "224.0.0.1", "255.255.255.255", "ether1", "2001:db8::1", 3}, "enabled": {"true", nil, 1}, "comment": {"bad\x00", strings.Repeat("😀", 513), nil, strings.Repeat("😀", 512)}} {
		for i, v := range list {
			values := r06Normalize(base).(map[string]any)
			values[key] = v
			tc = r06Base("routeros", "normalize")
			tc.Payload = r06JSON(map[string]any{"action": "add", "values": values})
			add(tc, fmt.Sprintf("normalize/%s/%d", key, i))
		}
	}
	return cases
}

func r06ReconciliationCases() []r06Case {
	cases := []r06Case{}
	add := func(tc r06Case, name string) { tc.Name = tc.Vendor + "/" + name; cases = append(cases, tc) }
	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, action := range []string{"add", "update", "delete"} {
			timeoutCase := r06Base(vendor, action)
			timeoutCase.Timeout = true
			if vendor == "routeros" {
				method := map[string]string{"add": "PUT", "update": "PATCH", "delete": "DELETE"}[action]
				path := "/ip/route"
				if action != "add" {
					path += "/*10"
				}
				timeoutCase.ROS.Faults = []routerfake.Fault{{Method: method, Path: path, Count: 1, AfterCommit: true, DelayMS: 500}}
			} else {
				apiAction := map[string]string{"add": "add", "update": "edit", "delete": "del"}[action]
				timeoutCase.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: apiAction, Count: 1, DelayMS: 500}}
			}
			add(timeoutCase, action+"/timeout_after_commit")
		}
		for _, action := range []string{"add", "update", "delete"} {
			tc := r06Base(vendor, action)
			if vendor == "routeros" {
				tc.ROS.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 2, Status: 503}}
			} else {
				tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "show", Count: 2, Drop: true}}
			}
			add(tc, action+"/preflight_read_failed")
		}
		tc := r06Base(vendor, "add")
		if vendor == "routeros" {
			tc.ROS.Mutations = []routerfake.Mutation{{Method: "PUT", Path: "/ip/route", Count: 1, Table: "/ip/route", Append: routerfake.Row{".id": "*99", "dst-address": "198.51.100.0/24", "gateway": "192.0.2.99", "comment": "foreign"}}}
		} else {
			tc.IK.Mutations = []routerfake.IkuaiMutation{{Func: "static_rt", Action: "add", Count: 1, OnWrite: true, Table: "static_rt", Append: routerfake.Row{"id": 99, "enabled": "yes", "tagname": "foreign", "ip_type": "4", "interface": "auto", "dst_addr": "198.51.100.0", "gateway": "192.0.2.99", "netmask": "255.255.255.0", "prio": 1, "comment": "foreign"}}}
		}
		add(tc, "add/new_foreign_duplicate")
		tc = r06Base(vendor, "update")
		if vendor == "routeros" {
			tc.ROS.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 3, Status: 503}, {Method: "GET", Path: "/ip/route", Count: 5, Status: 503}}
		} else {
			tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "show", Count: 3, Drop: true}, {Func: "static_rt", Action: "show", Count: 5, Drop: true}}
		}
		add(tc, "update/undo_read_unverified")
	}
	for _, change := range []routerfake.Row{{"gateway": "192.0.2.1%ether1"}, {"gateway": "192.0.2.1,192.0.2.2"}, {"gateway": "2001:db8::1"}, {"disabled": true}, {"static": "yes"}, {"scope": 30}} {
		tc := r06Base("routeros", "read")
		for k, v := range change {
			tc.ROS.Tables["/ip/route"][0][k] = v
		}
		add(tc, fmt.Sprintf("readonly_or_native/%v", change))
	}
	tc := r06Base("routeros", "add")
	tc.ROS.Tables["/ip/route"][0]["dst-address"] = "198.51.100.0/24"
	tc.ROS.Tables["/ip/route"][0]["routing-table"] = "backup"
	add(tc, "add/other_table_allowed")
	tc = r06Base("routeros", "update")
	tc.ROS.Tables["/ip/route"] = []routerfake.Row{{".id": "*10", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.1", "active": "true", "inactive": "false", "static": "true", "dynamic": "false", "disabled": "false", "distance": "1", "routing-table": "main", "scope": "30", "target-scope": "10"}}
	add(tc, "update/firmware_minimal_static")
	tc = r06Base("ikuai", "read")
	tc.IK.Tables["static_rt"][0]["netmask"] = "255.255.255.0 (display)"
	add(tc, "read/netmask_suffix")
	tc = r06Base("ikuai", "delete")
	row := routerfake.Row{}
	for k, v := range tc.IK.Tables["static_rt"][0] {
		row[k] = v
	}
	row["id"], row["tagname"] = 99, "foreign"
	tc.IK.Mutations = []routerfake.IkuaiMutation{{Func: "static_rt", Action: "del", Count: 1, OnWrite: true, Table: "static_rt", Append: row}}
	tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "del", Count: 1, LoseReply: true}}
	add(tc, "delete/new_duplicate_destination")
	tc = r06Base("ikuai", "delete")
	row = routerfake.Row{}
	for k, v := range tc.IK.Tables["static_rt"][0] {
		row[k] = v
	}
	row["id"], row["dst_addr"] = 99, "192.0.2.0"
	tc.IK.Mutations = []routerfake.IkuaiMutation{{Func: "static_rt", Action: "del", Count: 1, OnWrite: true, Table: "static_rt", Append: row}}
	tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "del", Count: 1, LoseReply: true}}
	add(tc, "delete/restoration_name_occupied")
	for _, action := range []string{"update", "delete"} {
		tc := r06Base("ikuai", action)
		tc.DuplicateName = true
		row := routerfake.Row{}
		for k, v := range tc.IK.Tables["static_rt"][0] {
			row[k] = v
		}
		row["id"], row["dst_addr"] = 99, "192.0.2.0"
		tc.IK.Tables["static_rt"] = append(tc.IK.Tables["static_rt"], row)
		add(tc, action+"/duplicate_name_fails_closed")
	}

	return cases
}

type r06ShortHTTP struct{ DeviceHTTP }

func (c r06ShortHTTP) Do(ctx context.Context, method, target string, body any, _ time.Duration) (*devhttp.Response, error) {
	return c.DeviceHTTP.Do(ctx, method, target, body, 200*time.Millisecond)
}
