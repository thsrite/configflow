package repo

import (
	"errors"
	"os"
	"testing"

	"configflow/internal/testjson"
)

func nativeReferenceScalar(t *testing.T, record *testObj, err error) bool {
	t.Helper()
	data, readErr := os.ReadFile("testdata/reference_native_scalars.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	value := parseJSONVal(t, data)
	for _, item := range value.([]any) {
		fixture := item.(*testObj)
		if fixture.GetString("label") != record.GetString("label") {
			continue
		}
		if !testjson.Equal(mustMarshalJSON(t, fixture.Value("record")), mustMarshalJSON(t, record)) {
			t.Fatal("native reference input/Python outcome changed")
		}
		if fixture.Value("native_error") == nil {
			if err != nil {
				t.Fatalf("native reference %s: %v", fixture.GetString("label"), err)
			}
		} else if err == nil || err.Error() != fixture.GetString("native_error") ||
			(fixture.GetString("native_class") == "ProfileValidationError" && !errors.Is(err, ErrValidation)) ||
			(fixture.GetString("native_class") == "ProfileInUse" && !errors.Is(err, ErrInUse)) {
			t.Fatalf("native reference %s: %v", fixture.GetString("label"), err)
		}
		return true
	}
	return false
}
