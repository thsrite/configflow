package difftest

import "testing"

func TestMalformedRouteInputHTTPParity(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, request := range []struct{ path, body string }{{"/api/proxy-groups/preview-regex", `[]`}, {"/api/proxy-groups/preview-regex", `null`}, {"/api/proxy-groups/preview-regex", `false`}, {"/api/proxy-groups/preview-regex", `""`}, {"/api/proxy-groups/preview-regex", `[1]`}, {"/api/rule-library/test-single", `null`}, {"/api/rule-library/test-single", `[]`}, {"/api/rule-library/test-single", `{"url":1}`}, {"/api/rule-library/test-single", `{"url":false}`}, {"/api/rule-library/cache", `null`}, {"/api/rule-library/cache", `[]`}, {"/api/rule-library/cache", `{"rule_ids":[null,42,{},[]]}`}} {
		w.call("POST", request.path, request.body, true)
	}
}
