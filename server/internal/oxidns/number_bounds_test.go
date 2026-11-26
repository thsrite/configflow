package oxidns

import (
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestZeroQuantitiesKeepBooleanDistinct(t *testing.T) {
	for _, value := range []*Node{I(0), Float(0)} {
		if !numericZero(value) {
			t.Fatal("numeric zero missed")
		}
	}
	for _, value := range []*Node{B(false), B(true), S("0"), I(1), Float(1)} {
		if numericZero(value) {
			t.Fatal("nonzero/nonnumeric treated as zero")
		}
	}
}
func TestTimeConstraintDefersWrongDayContainerToSchema(t *testing.T) {
	plugin := M("tag", "schedule", "type", "time", "args", M("periods", L(M("weekdays", S(" ")))))
	if issues := collectPluginIssues(plugin, 0); len(issues) != 0 {
		t.Fatalf("misleading cross-field error: %+v", issues)
	}
	issues := ValidateNativeConfig(M("plugins", L(plugin)))
	found := false
	for _, issue := range issues {
		if issue.Code == "type" && issue.Path.String() == "plugins[0].args.periods[0].weekdays" {
			found = true
		}
	}
	if !found {
		t.Fatalf("wrong type silently accepted: %+v", issues)
	}
}

func TestAliasedSequenceSchemaVisitsEveryChild(t *testing.T) {
	target := L(S("invalid integer"))
	alias := &Node{Kind: yaml.AliasNode, Alias: target}
	issues := ValidateSchema(alias, &Schema{Type: typeList{"array"}, Items: &Schema{Type: typeList{"integer"}}}, nil)
	if len(issues) != 1 || issues[0].Code != "type" || issues[0].Path.String() != "[0]" {
		t.Fatalf("aliased invalid child skipped: %+v", issues)
	}
}
