package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeVisitorPreservesCollisionsAndCredentialDiscovery(t *testing.T) {
	for _, row := range []struct{ input, want string }{
		{`{"secret-key":1,"[REDACTED]":2}`, `{"[REDACTED]":2}`},
		{`{"[REDACTED]":2,"secret-key":1}`, `{"[REDACTED]":1}`},
	} {
		data, err := json.Marshal(SanitizeWithSecrets(json.RawMessage(row.input), NewSecrets([]string{"secret"}), false))
		if err != nil || string(data) != row.want {
			t.Fatalf("%s %v want %s", data, err, row.want)
		}
	}
	raw := json.RawMessage(`{"message":"earlier synthetic-pass","password":"synthetic-pass","keep":{"count":3}}`)
	data := SanitizeCredentials(raw, nil)
	if strings.Contains(string(data), "synthetic-pass") || !strings.Contains(string(data), `"count":3`) {
		t.Fatalf("%s", data)
	}
	if !CollectCredentials(raw, nil).Equal(NewSecrets([]string{"synthetic-pass"})) {
		t.Fatal("raw credential not collected")
	}
}

func TestNativeMapCyclesAreBounded(t *testing.T) {
	input := map[string]any{"password": "synthetic-pass", "ordinary": true}
	input["self"] = input
	data := SanitizeCredentials(input, nil)
	if !json.Valid(data) || strings.Contains(string(data), "synthetic-pass") || !strings.Contains(string(data), `"self":"[REDACTED]"`) {
		t.Fatalf("%s", data)
	}
	if !CollectCredentials(input, nil).Equal(NewSecrets([]string{"synthetic-pass"})) {
		t.Fatal("cycle changed credentials")
	}
}

func FuzzNativeCredentialJSON(f *testing.F) {
	for _, input := range []string{`{"message":"synthetic-secret","password":"synthetic-secret"}`, `{"a":[1,true,null,{"token":"synthetic-secret"}]}`, `{"x":"NaN Infinity","[REDACTED]":1}`} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if !json.Valid([]byte(input)) {
			return
		}
		output := SanitizeCredentials(json.RawMessage(input), NewSecrets([]string{"synthetic-secret"}))
		if !json.Valid(output) {
			t.Fatalf("invalid redacted JSON: %q", output)
		}
		// Values may move after a key collision, but a known secret cannot leave.
		if strings.Contains(string(output), "synthetic-secret") {
			t.Fatalf("known secret escaped: %q", output)
		}
	})
}
