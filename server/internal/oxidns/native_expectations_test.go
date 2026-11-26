package oxidns

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"sync"
	"testing"

	"bytes"
)

// Frozen Python recordings remain unchanged. These exact input fingerprints
// separately register the coordinator-approved strict request type rejections.
//
//go:embed testdata/native-request-expectations.json
var nativeExpectationData []byte
var nativeExpectations = sync.OnceValues(func() (map[string]struct {
	Message string `json:"message"`
	Reason  string `json:"reason"`
}, error) {
	var rows map[string]struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	err := json.Unmarshal(nativeExpectationData, &rows)
	return rows, err
})

func nativeRequestExpectation(t *testing.T, r row, err error) bool {
	t.Helper()
	if r.Fn != "normalize" {
		return false
	}
	key := argumentFingerprint(t, r.Args)

	rows, e := nativeExpectations()
	if e != nil {
		t.Fatal(e)
	}
	expected, ok := rows[key]
	if !ok {
		return false
	}
	message, settings := IsSettingsError(err)
	return settings && message == expected.Message
}

//go:embed testdata/native-validation-expectations.json
var nativeNumberData []byte
var nativeNumberExpectations = sync.OnceValues(func() (map[string]row, error) {
	var values map[string]row
	err := json.Unmarshal(nativeNumberData, &values)
	return values, err
})

func nativeValidationExpectation(t *testing.T, r row, got *Node, err error) bool {
	t.Helper()
	values, e := nativeNumberExpectations()
	if e != nil {
		t.Fatal(e)
	}
	expected, ok := values[r.Fn+":"+argumentFingerprint(t, r.Args)]
	if !ok {
		return false
	}
	return compareFrozen(expected, got, err) == ""
}
func argumentFingerprint(t *testing.T, args []json.RawMessage) string {
	t.Helper()
	var compact bytes.Buffer
	for _, raw := range args {
		if err := json.Compact(&compact, raw); err != nil {
			t.Fatal(err)
		}
		compact.WriteByte(0)
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:])
}

//go:embed testdata/native-capability-expectations.json
var nativeCapabilityData []byte
var nativeCapabilityExpectations = sync.OnceValues(func() (map[string]row, error) {
	var values map[string]row
	err := json.Unmarshal(nativeCapabilityData, &values)
	return values, err
})

func nativeCapabilityExpectation(t *testing.T, r row, got *Node, err error) bool {
	t.Helper()
	values, e := nativeCapabilityExpectations()
	if e != nil {
		t.Fatal(e)
	}
	expected, ok := values[r.Fn+":"+argumentFingerprint(t, r.Args)]
	return ok && compareFrozen(expected, got, err) == ""
}
