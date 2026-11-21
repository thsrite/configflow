package difftest

import (
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// O9 round 2 L3: real Flask accepts manual nodes with line breaks / control
// characters in fields spliced into Surge/Loon INI configs; Go rejects them
// with 400 naming the offending field and leaves storage unchanged, while
// clean nodes continue to match Flask strictly.
func TestO9R2NodeControlCharBoundaryAgainstFlask(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		path      string
		body      string
		wantField string
	}{
		{
			name:      "POST name with LF",
			method:    "POST",
			path:      "/api/nodes",
			body:      `{"id":"bad1","name":"HK\n[Rule]\nFINAL,DIRECT","type":"ss","server":"192.0.2.1","port":8388,"params":{"cipher":"aes-256-gcm","password":"pw"}}`,
			wantField: "name",
		},
		{
			name:      "POST params.password with U+2028",
			method:    "POST",
			path:      "/api/nodes",
			body:      `{"id":"bad2","name":"HK","type":"trojan","server":"192.0.2.1","port":443,"params":{"password":"pw\u2028FINAL,DIRECT"}}`,
			wantField: "params.password",
		},
		{
			name:      "PUT name with LF",
			method:    "PUT",
			path:      "/api/nodes/relay",
			body:      `{"name":"relay\n[Rule]\nFINAL,DIRECT","type":"http","server":"192.0.2.1","port":80}`,
			wantField: "name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, seedGraph(t, "none", false, true), nil)
			req := request{Method: tc.method, Path: tc.path, Body: &tc.body, Headers: map[string]string{"Authorization": w.token}, Control: *w.control}
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
				t.Fatalf("Flask L3 baseline changed: status=%d error=%s", python.Status, python.Error)
			}

			before := normalize(snapshot(t, w.srv))
			var got *httpxtest.Result
			if tc.method == "POST" {
				got = w.srv.Post(tc.path, httpxtest.Header("Authorization", w.token), httpxtest.Body([]byte(tc.body), "application/json")).WantStatus(400)
			} else {
				got = w.srv.Put(tc.path, httpxtest.Header("Authorization", w.token), httpxtest.Body([]byte(tc.body), "application/json")).WantStatus(400)
			}
			var failure struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			wantMsg := "节点字段「" + tc.wantField + "」不能包含换行、回车或控制字符"
			if json.Unmarshal(got.Body, &failure) != nil || failure.Success || failure.Message != wantMsg {
				t.Fatalf("Go rejection = %s, want %q", got.Body, wantMsg)
			}
			if after := normalize(snapshot(t, w.srv)); !reflect.DeepEqual(before, after) {
				t.Fatal("Go L3 rejection modified storage")
			}
		})
	}

	// Negative control: clean node saves identically on both Flask and Go.
	t.Run("negative control: clean node matches Flask", func(t *testing.T) {
		w := newWorld(t, seedGraph(t, "none", false, true), nil)
		w.call("POST", "/api/nodes", `{"id":"clean","name":"HK 01","type":"ss","server":"192.0.2.9","port":8388,"params":{"cipher":"aes-256-gcm","password":"clean"}}`, true)
		w.call("PUT", "/api/nodes/clean", `{"name":"HK 02","type":"ss","server":"192.0.2.9","port":8388,"params":{"cipher":"aes-256-gcm","password":"clean"}}`, true)
	})

	// Negative control: Mihomo parameter values may span lines (ca-str PEM);
	// they save identically on Flask and Go (review batch-20).
	t.Run("negative control: multi-line params match Flask", func(t *testing.T) {
		w := newWorld(t, seedGraph(t, "none", false, true), nil)
		w.call("POST", "/api/nodes", `{"id":"pem","name":"PEM 01","type":"trojan","server":"192.0.2.9","port":443,"params":{"password":"clean","ca-str":"-----BEGIN CERTIFICATE-----\nU1lOVEhFVElD\n-----END CERTIFICATE-----\n"}}`, true)
		w.call("PUT", "/api/nodes/pem", `{"name":"PEM 02","type":"trojan","server":"192.0.2.9","port":443,"params":{"password":"pw\r\nline2","ca-str":"-----BEGIN CERTIFICATE-----\r\nU1lOVEhFVElD\r\n-----END CERTIFICATE-----"}}`, true)
	})
}
