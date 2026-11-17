package oxidns

import "testing"

func TestBoundaryComparisonRemainsStrict(t *testing.T) {
	duplicate := M("a", I(1))
	duplicate.Content = append(duplicate.Content, S("a"), I(2))
	tests := []struct {
		name      string
		got, want *Node
		yamlText  bool
		equal     bool
	}{
		{"same", M("key", I(1)), M("key", I(1)), false, true},
		{"mapping order", M("a", I(1), "b", I(2)), M("b", I(2), "a", I(1)), false, true},
		{"int float", I(1), Float(1), false, false},
		{"int bool", I(0), B(false), false, false},
		{"value", I(1), I(2), false, false},
		{"extra key", M("a", I(1), "b", I(2)), M("a", I(1)), false, false},
		{"missing key", M("a", I(1)), M("a", I(1), "b", I(2)), false, false},
		{"duplicate canonical value", duplicate, M("a", I(2)), false, true},
		{"duplicate first value differs", duplicate, M("a", I(1)), false, false},
		{"sequence order", L(I(1), I(2)), L(I(2), I(1)), false, false},
		{"sequence length", L(I(1)), L(I(1), I(2)), false, false},
		{"plain text", S("a: 1"), S("a:  1"), false, false},
		{"YAML format", S("a: 1\n"), S("{a: 1}"), true, true},
		{"YAML value", S("a: 1"), S("a: 2"), true, false},
		{"nested YAML format", M("native_config", "a: 1\n"), M("native_config", "{a: 1}"), false, true},
		{"nested YAML type", M("native_config", "a: true"), M("native_config", "a: 1"), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := firstDifference(canon(tt.got, tt.yamlText), canon(tt.want, tt.yamlText), "out")
			difference := canonicalDifference(tt.got, tt.want, tt.yamlText, "out")
			if (previous == "") != tt.equal || (difference == "") != tt.equal {
				t.Fatalf("comparison changed: previous=%q optimized=%q equal=%t", previous, difference, tt.equal)
			}
		})
	}
}
