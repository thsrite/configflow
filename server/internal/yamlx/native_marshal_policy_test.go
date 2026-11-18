package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMarshalRejectsUnparseableTaggedScalars(t *testing.T) {
	for _, tt := range []struct {
		value  string
		reject bool
	}{
		{"1e400", true}, {"-1e400", true}, {"1e1000000000", true}, {"-1e1000000000", true},
		{"1e-400", false}, {"1e-1000000000", false}, {"1.25", false}, {"1e308", false},
		{"-0.0", false}, {"100000000000000000000", false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			source := []byte(`{"value":` + tt.value + `}`)
			node, err := FromJSON(source)
			if err != nil {
				t.Fatal(err)
			}
			before, err := ToJSON(node)
			if err != nil || !bytes.Equal(before, source) {
				t.Fatalf("JSON text changed: %s %v", before, err)
			}
			output, err := MarshalChecked(t, node)
			if tt.reject {
				if len(output) != 0 || !errors.Is(err, ErrPolicy) || errors.Is(err, ErrSyntax) {
					t.Fatalf("output=%q error=%v", output, err)
				}
				if !strings.HasPrefix(err.Error(), fmt.Sprintf(msgExplicitScalar, "!!float", 0, "")) {
					t.Fatalf("missing Chinese policy context: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := Parse(output)
				if err != nil || !Equal(node, parsed) {
					t.Fatalf("not closed: %s %v", output, err)
				}
			}
			after, err := ToJSON(node)
			if err != nil || !bytes.Equal(after, source) {
				t.Fatalf("Marshal changed its input JSON: %s %v", after, err)
			}
		})
	}
}
