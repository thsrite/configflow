package difftest

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// The real Flask reference accepts these unused non-JSON numeric literals.
// Native Bind rejects them before any write; strings containing them remain valid.
func TestNativeReorderNonfiniteBoundary(t *testing.T) {
	for _, number := range []string{"NaN", "Infinity", "-Infinity"} {
		t.Run(number, func(t *testing.T) {
			w := newWorld(t, seedGraph(t, "none", false, true), nil)
			body := `{"ids":["s"],"unused":` + number + `}`
			req := request{Method: "POST", Path: "/api/subscriptions/reorder", Body: &body, Headers: map[string]string{"Authorization": w.token}, Control: *w.control}
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.py.stdin.Write(append(data, '\n')); err != nil {
				t.Fatal(err)
			}
			if !w.py.scanner.Scan() {
				t.Fatal("Flask reference ended")
			}
			var python answer
			if err := json.Unmarshal(w.py.scanner.Bytes(), &python); err != nil {
				t.Fatal(err, w.py.scanner.Text())
			}
			if python.Error != "" || python.Status != 200 {
				t.Fatalf("Flask nonfinite control changed: status=%d error=%s", python.Status, python.Error)
			}
			before := normalize(snapshot(t, w.srv))
			got := w.srv.Post(req.Path, httpxtest.Header("Authorization", w.token), httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
			var failure struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			if json.Unmarshal(got.Body, &failure) != nil || failure.Success || failure.Message == "" {
				t.Fatalf("nonfinite rejection: %s", got.Body)
			}
			if after := normalize(snapshot(t, w.srv)); !reflect.DeepEqual(before, after) {
				t.Fatal("native rejection changed storage")
			}
		})
	}
}
