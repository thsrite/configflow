package configexport_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/configexport"
	"configflow/internal/model"
)

func TestOxiDNSRedactionMatchesPython(t *testing.T) {
	cases := []json.RawMessage{
		[]byte(`null`), []byte(`[]`), []byte(`"opaque scalar"`),
		[]byte(`{"custom_config":"password: secret","native_config":"secret","additional_files":[{"name":"secret"}],"local_dns":["private"],"name":"visible"}`),
		[]byte(`{"Password":"secret","username":"admin","token":null,"secret":false,"key":0,"url":"https://example.com/private","headers":{"Authorization":"credential"},"nested":[{"private-key":"credential","key":-0.0}]}`),
		[]byte(`{"rule_actions":{"r":{"type":"dns","upstream":{"addr":"https://example.com/dns","password":"credential","port":443}}},"rule_dns_parked":{"a":{"upstream":{"addr":"192.0.2.1","headers":{"x":"secret"}}}}}`),
	}
	deep := `{"secret":1}`
	for range 66 {
		deep = `{"child":` + deep + `}`
	}
	cases = append(cases, []byte(deep))
	input, _ := json.Marshal(cases)
	home, _ := os.UserHomeDir()
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import json,sys;from backend.common.config_export import redact_oxidns_settings;json.dump([redact_oxidns_settings(v) for v in json.load(sys.stdin)],sys.stdout)`)
	cmd.Dir = "../../.."
	cmd.Stdin = bytes.NewReader(input)
	reference, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python: %v %s", err, reference)
	}
	var want []json.RawMessage
	if err = json.Unmarshal(reference, &want); err != nil {
		t.Fatal(err)
	}
	for i, data := range cases {
		var value model.Dynamic
		if err = value.UnmarshalJSON(data); err != nil {
			t.Fatal(err)
		}
		out, err := configexport.RedactOxiDNS(value)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(out)
		var a, b interface{}
		json.Unmarshal(got, &a)
		json.Unmarshal(want[i], &b)
		canonicalA, _ := json.Marshal(a)
		canonicalB, _ := json.Marshal(b)
		if !bytes.Equal(canonicalA, canonicalB) {
			t.Fatalf("case %d: %s != %s", i, canonicalA, canonicalB)
		}
		original, _ := value.MarshalJSON()
		if !bytes.Equal(data, original) {
			t.Fatal("mutated source")
		}
	}
}

func FuzzShareableOxiDNS(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"rule_actions":{"x":{"upstream":{"addr":"private","token":"credential"}}}}`, `{"native_config":"secret","additional_files":[]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 1<<18 {
			return
		}
		var value model.Dynamic
		if err := value.UnmarshalJSON([]byte(text)); err != nil {
			return
		}
		result, err := configexport.RedactOxiDNS(value)
		if err != nil {
			t.Fatal(err)
		}
		data, err := result.MarshalJSON()
		if err != nil || !json.Valid(data) {
			t.Fatal("invalid output")
		}
		if strings.Contains(string(data), `"native_config":"secret"`) {
			t.Fatal("opaque text leaked")
		}
	})
}
