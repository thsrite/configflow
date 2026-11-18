package difftest

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// Nonfinite request rejection is explicit, rather than normalized away. The
// unchanged Flask route still accepts an irrelevant nonfinite member on run/read.
func TestN87NativeNonfiniteRequests(t *testing.T) {
	w := newWorld(t)
	var commands []command
	for _, word := range []string{"NaN", "Infinity", "-Infinity"} {
		commands = append(commands,
			command{Method: "POST", Path: "/api/tasks/run", Body: `{"key":"subscription:s","extra":` + word + `}`},
			command{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{},"extra":` + word + `}`},
			command{Method: "PUT", Path: "/api/tasks/interval", Body: `{"key":"subscription:s","interval_seconds":` + word + `}`})
	}
	commands = w.commands(commands)
	want := python(t, w, commands)
	for i, c := range commands {
		t.Run(c.Path+"/"+c.Body, func(t *testing.T) {
			got := send(t, w, c)
			if got.Status != 400 || !json.Valid(got.Body) || bytes.Contains(got.Body, []byte("NaN")) || bytes.Contains(got.Body, []byte("Infinity")) {
				t.Fatalf("native bad request: %d %s", got.Status, got.Body)
			}
			expectedStatus := 200
			if c.Path == "/api/tasks/run" {
				expectedStatus = 202
			} else if c.Path == "/api/tasks/interval" {
				expectedStatus = 400
			}
			if want[i].Status != expectedStatus {
				t.Fatalf("Flask changed: %d %s", want[i].Status, want[i].Body)
			}
			var expectedBody json.RawMessage
			switch c.Path {
			case "/api/tasks/run":
				expectedBody = []byte(`{"success":false,"message":"请指定任务"}`)
			case "/api/notifications/read":
				expectedBody = []byte(`{"success":false,"message":"请指定要标为已读的通知及其版本。"}`)
			default:
				expectedBody = []byte(`{"success":false,"message":"请指定任务和更新间隔"}`)
			}
			if !bytes.Equal(mustCompact(t, got.Body), mustCompact(t, expectedBody)) {
				t.Fatalf("400 diagnostic: %s", got.Body)
			}
			// Rejection must not change storage (run's supplied owner is a no-op).
			if !equalState(t, w, got.State, want[i].State) {
				t.Fatal("request rejection changed storage")
			}
		})
	}
}
func mustCompact(t *testing.T, data []byte) []byte {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func equalState(t *testing.T, w *world, a, b json.RawMessage) bool {
	t.Helper()
	return reflect.DeepEqual(canonical(t, a, w.s.DataDir), canonical(t, b, w.pyDir))
}
