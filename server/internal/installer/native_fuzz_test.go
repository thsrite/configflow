package installer

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
)

func FuzzInstallerNativeProtocols(f *testing.F) {
	for _, seed := range []string{`null`, `true`, `7`, `"credential"`, `{"id":"foreign"}`, `["nested"]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, extra []byte) {
		if len(extra) > 16384 || !json.Valid(extra) {
			return
		}
		original := bytes.Clone(extra)
		var fields model.Fields
		if err := json.Unmarshal([]byte(nativeInstallRequest), &fields); err != nil {
			t.Fatal(err)
		}
		base, err := DecodeGuideRequest([]byte(nativeInstallRequest))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"Server_URL", "Agent_Name", "OXIDNS_PORT", "CREATE_HOST_IDENTITY"} {
			if err := fields.Set(key, extra); err != nil {
				return
			}
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		request, err := DecodeGuideRequest(raw)
		if err != nil || !reflect.DeepEqual(base, request) {
			t.Fatalf("unknown exact keys changed DTO: %v", err)
		}
		id := strings.Repeat("a", 32)
		var sessionFields model.Fields
		if err := sessionFields.Set("id", json.RawMessage(`"`+id+`"`)); err != nil {
			t.Fatal(err)
		}
		if err := sessionFields.Set("ID", extra); err != nil {
			return
		}
		data, err := json.Marshal(sessionFields)
		if err != nil {
			t.Fatal(err)
		}
		session, err := decodeInstallationSession(data)
		if err != nil || session.id != id {
			t.Fatalf("case alias changed session ownership: %v", err)
		}
		if !bytes.Equal(extra, original) {
			t.Fatal("decoder mutated input")
		}
	})
}
