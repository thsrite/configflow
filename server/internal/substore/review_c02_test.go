package substore

import "testing"

// Regression (review C02): _is_yaml_response accepts a mapping with a
// "proxies" key even when another key is not a string (Python:
// isinstance(data, dict) and 'proxies' in data).
func TestIsYAMLResponseWithNonStringKeys(t *testing.T) {
	text := "1: x\n" + "a: b\n" + "proxies: []\n"
	padded := "x: " + string(make([]byte, 0)) + "y\n" + text
	for _, in := range []string{"# " + string(repeat('-', 210)) + "\n" + text, padded} {
		if !isYAMLResponse(in) {
			t.Errorf("isYAMLResponse(%q) = false, want true", in)
		}
	}
	if isYAMLResponse("# " + string(repeat('-', 210)) + "\n1: x\n") {
		t.Error("a mapping without proxies is not a proxies document")
	}
}

func repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
