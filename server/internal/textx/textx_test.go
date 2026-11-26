package textx

import (
	"slices"
	"strings"
	"testing"
)

func TestASCIIDigits(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false}, {"0", true}, {"0123456789", true}, {"1a", false}, {"+1", false}, {" 1", false}, {"١", false},
	}
	for _, tt := range tests {
		if got := ASCIIDigits(tt.in); got != tt.want {
			t.Errorf("ASCIIDigits(%q) = %t", tt.in, got)
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
		ends []string
	}{
		{"empty", "", nil, nil},
		{"no break", "a", []string{"a"}, []string{"a"}},
		{"final break", "a\n", []string{"a"}, []string{"a\n"}},
		{"crlf and cr", "a\r\nb\rc\nd", []string{"a", "b", "c", "d"}, []string{"a\r\n", "b\r", "c\n", "d"}},
		{"blank lines", "\n\n", []string{"", ""}, []string{"\n", "\n"}},
		{"lone cr before lf pair", "a\r\r\nb", []string{"a", "", "b"}, []string{"a\r", "\r\n", "b"}},
		{"unicode separators stay", "a\vb\fc\u0085d e f\x1cg", []string{"a\vb\fc\u0085d e f\x1cg"}, []string{"a\vb\fc\u0085d e f\x1cg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitLines(tt.in, false); !slices.Equal(got, tt.want) {
				t.Errorf("SplitLines = %q, want %q", got, tt.want)
			}
			got := SplitLines(tt.in, true)
			if !slices.Equal(got, tt.ends) {
				t.Errorf("SplitLines keepEnds = %q, want %q", got, tt.ends)
			}
			if strings.Join(got, "") != tt.in {
				t.Errorf("keepEnds lines do not rebuild the text")
			}
		})
	}
}
