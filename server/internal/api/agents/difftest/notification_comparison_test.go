package difftest

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// notificationStoreComparison ignores record field order, which is native JSON
// formatting. Incident insertion order still matters for equal-time display
// ties, so retain it separately along with the complete parsed store. Arrays,
// values, number notation and field presence are not normalized here.
func notificationStoreComparison(t *testing.T, data []byte) string {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	var store struct {
		Items model.OrderedMap[json.RawMessage] `json:"items"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for key := range store.Items.All() {
		keys = append(keys, key)
	}
	encoded, err := json.Marshal(struct {
		IncidentOrder []string `json:"incident_order"`
		Value         any      `json:"value"`
	}{keys, value})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestNotificationStoreComparisonPreservesDisplayAndData(t *testing.T) {
	const original = `{"items":{"a":{"message":"first","occurrences":9007199254740993,"extra":[1,2]},"b":{"message":"second"}}}`
	for _, test := range []struct {
		name, other string
		equal       bool
	}{
		{"record field order", `{"items":{"a":{"extra":[1,2],"occurrences":9007199254740993,"message":"first"},"b":{"message":"second"}}}`, true},
		{"incident display tie order", `{"items":{"b":{"message":"second"},"a":{"message":"first","occurrences":9007199254740993,"extra":[1,2]}}}`, false},
		{"message value", `{"items":{"a":{"message":"changed","occurrences":9007199254740993,"extra":[1,2]},"b":{"message":"second"}}}`, false},
		{"array order", `{"items":{"a":{"message":"first","occurrences":9007199254740993,"extra":[2,1]},"b":{"message":"second"}}}`, false},
		{"large integer precision", `{"items":{"a":{"message":"first","occurrences":9007199254740992,"extra":[1,2]},"b":{"message":"second"}}}`, false},
		{"number versus string", `{"items":{"a":{"message":"first","occurrences":"9007199254740993","extra":[1,2]},"b":{"message":"second"}}}`, false},
		{"field absent", `{"items":{"a":{"message":"first","occurrences":9007199254740993},"b":{"message":"second"}}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			equal := notificationStoreComparison(t, []byte(original)) == notificationStoreComparison(t, []byte(test.other))
			if equal != test.equal {
				t.Fatalf("equal=%v; want %v", equal, test.equal)
			}
		})
	}
}
