package yamlx

import (
	"errors"
	"strings"
	"testing"
)

func TestMergeSourcesValidated(t *testing.T) {
	for _, source := range []string{"<<: 5", "x: &x [1]\n<<: *x", "<<: [{a: 1}, 5]", "x: &x [a]\n<<: [*x]"} {
		_, err := Parse([]byte(source))
		var syntax *SyntaxError
		if !errors.As(err, &syntax) || !errors.Is(err, ErrPolicy) || syntax.Line < 1 || !strings.Contains(err.Error(), "<<") {
			t.Fatalf("%q: %v", source, err)
		}
	}
	for _, source := range []string{"x: &x {a: 1}\n<<: *x", "x: &x {a: 1}\n<<: [*x, {b: 2}]", "<<: []"} {
		if _, err := Parse([]byte(source)); err != nil {
			t.Fatalf("%q: %v", source, err)
		}
	}
}

func TestExplicitLegacySpellingsRejected(t *testing.T) {
	for _, value := range []string{"yes", "no", "on", "off", "y", "n", "YES", "Off"} {
		for _, tag := range []string{"!!bool", "!<tag:yaml.org,2002:bool>"} {
			_, err := Parse([]byte("# header\n" + tag + " " + value))
			var syntax *SyntaxError
			if !errors.Is(err, ErrPolicy) || !errors.As(err, &syntax) || syntax.Line != 2 {
				t.Fatalf("%s %s: %v", tag, value, err)
			}
		}
	}
	for _, value := range []string{"1:20"} {
		if _, err := Parse([]byte("!!int " + value)); !errors.Is(err, ErrPolicy) {
			t.Fatalf("%s: %v", value, err)
		}
	}
	for _, source := range []string{"!!bool true", "!!bool FALSE", "!!int 17", "!!int 0x10", "!!int 0o17", "!!int 0777", "yes", "on", "1:20", "0o777"} {
		if _, err := Parse([]byte(source)); err != nil {
			t.Fatalf("%s: %v", source, err)
		}
	}
}
