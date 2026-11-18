package oxidns

import "testing"

func TestSchemaRejectsNonFiniteExponentBeforeRationalExpansion(t *testing.T) {
	for _, source := range []string{"1e1000000000", "-1e1000000000"} {
		t.Run(source, func(t *testing.T) {
			value, err := FromJSON([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := ratOf(value); ok {
				t.Fatal("non-finite float reached rational arithmetic")
			}
			if issues := ValidateSchema(value, &Schema{Type: typeList{"number"}}, nil); len(issues) != 1 || issues[0].Code != "type" {
				t.Fatalf("non-finite schema input: %+v", issues)
			}
		})
	}
}
