package agents

import (
	"encoding/json"
	"testing"
)

func TestN810InstallCommandExactNativeFields(t *testing.T) {
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"normal", `{"name":"sample","port":8080,"install_core":false}`, true},
		{"case alias", `{"Name":"sample"}`, false},
		{"null flag", `{"install_core":null}`, false},
		{"quoted port", `{"port":"8080"}`, false},
		{"float port", `{"port":8080.0}`, false},
		{"overflow", `{"port":18446744073709551616}`, false},
		{"null", `null`, false},
		{"array", `[]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			params, err := installCommandParams(json.RawMessage(test.body))
			if (err == nil) != test.valid {
				t.Fatalf("params=%v error=%v", params, err)
			}
		})
	}
}

func FuzzN810InstallCommandBinding(f *testing.F) {
	for _, body := range []string{`{}`, `null`, `{"port":8080}`, `{"port":1e100}`, `{"name":"sample","install_core":true}`} {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body string) {
		params, err := installCommandParams(json.RawMessage(body))
		if err != nil {
			return
		}
		for key := range params {
			if !installCommandFields[key] {
				t.Fatalf("unknown field %q admitted", key)
			}
		}
		for _, key := range []string{"port", "mihomo_agent_port", "oxidns_agent_port"} {
			if _, present := params[key]; present {
				if _, err := installPort(params, key, 8080, true); err != nil {
					t.Fatalf("invalid port admitted: %v", err)
				}
			}
		}
	})
}
