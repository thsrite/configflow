package yamltest

import "testing"

func TestClassifyImplicitUppercaseHex(t *testing.T) {
	for _, source := range []string{"k: 0X1F\n", "mixed-port: 0X1F\nmode: rule\n"} {
		if got, err := Classify(source); err != nil || got != Typing {
			t.Fatalf("actual loader type change was hidden by writer: %s: %v", got, err)
		}
	}
}
