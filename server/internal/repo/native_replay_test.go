package repo

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

// Native expectations apply only to these original golden inputs; every other
// row still uses the Python boundary unchanged.
func nativeCompatReplay(t *testing.T, index int, record *jsonx.Object, got any, err error) bool {
	t.Helper()
	data, readErr := os.ReadFile("testdata/compat_native_yaml.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	value, readErr := jsonx.Parse(data)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, item := range value.([]any) {
		c := item.(*jsonx.Object)
		if c.GetInt("index") != int64(index) {
			continue
		}
		inputID := fmt.Sprintf("%x", sha256.Sum256([]byte(compatText(t, []any{record.Value("fn"), record.Value("input")}))))
		if inputID != c.GetString("input_id") {
			t.Fatalf("native case %d input changed", index)
		}
		if want := c.GetString("error_class"); want != "" {
			if err == nil || !isHelperClass(err, want) || err.Error() != c.GetString("error") {
				t.Fatalf("native case %d: %v", index, err)
			}
		} else if err != nil || compatText(t, got) != compatText(t, c.Value("ok")) {
			t.Fatalf("native case %d: %s %v", index, compatText(t, got), err)
		}
		return true
	}
	return false
}
