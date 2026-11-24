package difftest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
)

// These requests differ solely because Python's loader accepts non-JSON numeric
// words. Keep its real status and message as evidence; native binding rejects
// before business validation or a transaction, including unused members.
func TestNativeNonFiniteRequestDifferences(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 enables Flask differential")
	}
	seed := t.TempDir()
	runPython(t, seed, "seed", "1") // No case reaches the Agent endpoint.
	py := startPython(t, seed)
	goServer := httpxtest.New(t, httpxtest.Options{})
	domaindiscovery.NewHandler(goServer.Repo, nil).Register(goServer.App.Router)
	cases := []struct {
		method, path, fields string
		pythonStatus         int
	}{
		{"PUT", "settings", `"auto_probe":true`, 200},
		{"POST", "rulesets/init", `"targets":["direct"]`, 200},
		{"POST", "apply", `"items":[]`, 400},
		{"POST", "undo", `"value":"bad host","target":"proxy"`, 400},
		{"POST", "ignore", `"values":[]`, 400},
		{"DELETE", "ignore", `"values":["none.example.net"]`, 200},
		{"POST", "probe", `"domains":[]`, 400},
		{"POST", "region/check", `"services":false`, 400},
		{"POST", "region/route", `"kind":"other"`, 400},
		{"POST", "website/identify", `"domains":["bad host"]`, 400},
		{"PUT", "website/note", `"domain":"example.com","note":"test"`, 200},
	}
	server := httptest.NewServer(goServer.Handler())
	defer server.Close()
	for _, tc := range cases {
		for _, word := range []string{"NaN", "Infinity", "-Infinity"} {
			t.Run(tc.method+tc.path+word, func(t *testing.T) {
				body := fmt.Sprintf(`{%s,"unused":%s}`, tc.fields, word)
				pc, pv := call(t, py, tc.method, "/api/domain-discovery/"+tc.path, body)
				gc, gv := call(t, server.URL, tc.method, "/api/domain-discovery/"+tc.path, body)
				if pc != tc.pythonStatus || gc != http.StatusBadRequest {
					t.Fatalf("Python=%d %v Go=%d %v", pc, pv, gc, gv)
				}
				data, err := json.Marshal(gv)
				if err != nil || !json.Valid(data) {
					t.Fatalf("invalid Go JSON: %s %v", data, err)
				}
				object, ok := gv.(map[string]any)
				if !ok || object["success"] != false || object["message"] != "请求必须是 JSON 对象" {
					t.Fatalf("native validation: %v", gv)
				}
			})
		}
	}
}
