package difftest

import "testing"

func TestN810IntegerComparisonCannotHideUnrelatedFailures(t *testing.T) {
	original := response{status: 200, body: []byte("original script")}
	refusal := response{status: 400, body: []byte(`{"success":false,"message":"Invalid installer integer"}`)}
	for _, path := range []string{"/api/agents/install-script?port=8080", "/api/agents/install-script?name=%EF%BC%91%EF%BC%92", "/api/agents/install-script?unknown=999999999999999999999999999999999999999999999999999999999999999999999999"} {
		if approvedInstallerIntegerRefusal(installerRequest{method: "GET", path: path}, original, refusal) {
			t.Fatal("native integer exception hid an unrelated refusal")
		}
	}
	changed := refusal
	changed.body = []byte(`{"success":false,"message":"changed"}`)
	if approvedInstallerIntegerRefusal(installerRequest{method: "GET", path: "/api/agents/install-script?port=%EF%BC%91%EF%BC%92"}, original, changed) {
		t.Fatal("native integer exception hid a different refusal")
	}
}
