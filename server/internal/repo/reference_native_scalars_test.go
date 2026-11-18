package repo

import (
"errors"
"os"
"testing"
"configflow/internal/jsonx"
)

func nativeReferenceScalar(t *testing.T, record *jsonx.Object, err error) bool {
	t.Helper()
	data, readErr := os.ReadFile("testdata/reference_native_scalars.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	value, readErr := jsonx.Parse(data)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, item := range value.([]any) {
		fixture := item.(*jsonx.Object)
		if fixture.GetString("label") != record.GetString("label") {
			continue
		}
		if !jsonx.Equal(record, fixture.Value("record")) {
			t.Fatal("native reference input/Python outcome changed")
		}
		if fixture.Value("native_error") == nil {
			if err != nil {
				t.Fatalf("native reference %s: %v", fixture.GetString("label"), err)
			}
		} else if err == nil || err.Error() != fixture.GetString("native_error") ||
			(fixture.GetString("native_class") == "ProfileValidationError" && !errors.Is(err, ErrValidation)) ||
			(fixture.GetString("native_class") == "DialerReferenceError" && !errors.Is(err, ErrInUse)) {
			t.Fatalf("native reference %s: %v", fixture.GetString("label"), err)
		}
		return true
	}
	return false
}

