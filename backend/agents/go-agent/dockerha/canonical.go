package dockerha

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The compact Compose installation owner is the SHA-256 of its binding in
// a fixed canonical form: keys sorted, no whitespace, every character
// outside printable ASCII escaped as \uXXXX (lower-case hex, UTF-16 pairs).
// Records written by earlier images use this form, so it must not change.
// Binding values are strings and integers only.

var integerText = regexp.MustCompile(`\A-?(?:0|[1-9][0-9]*)\z`)

// bindingOwner returns the owner digest of binding, or false when a value
// is not a string or an integer (no installation writes such a binding).
func bindingOwner(binding map[string]any) (string, bool) {
	keys := make([]string, 0, len(binding))
	for key := range binding {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		writeCanonicalString(&out, key)
		out.WriteByte(':')
		switch value := binding[key].(type) {
		case string:
			writeCanonicalString(&out, value)
		case int:
			out.WriteString(strconv.Itoa(value))
		case json.Number:
			if !integerText.MatchString(string(value)) {
				return "", false
			}
			out.WriteString(string(value))
		default:
			return "", false
		}
	}
	out.WriteByte('}')
	sum := sha256.Sum256([]byte(out.String()))
	return hex.EncodeToString(sum[:]), true
}

func writeCanonicalString(out *strings.Builder, text string) {
	out.WriteByte('"')
	for _, r := range text {
		switch {
		case r == '"':
			out.WriteString(`\"`)
		case r == '\\':
			out.WriteString(`\\`)
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\r':
			out.WriteString(`\r`)
		case r == '\t':
			out.WriteString(`\t`)
		case r == '\b':
			out.WriteString(`\b`)
		case r == '\f':
			out.WriteString(`\f`)
		case r >= 0x20 && r <= 0x7e:
			out.WriteRune(r)
		case r > 0xffff:
			high, low := utf16.EncodeRune(r)
			fmt.Fprintf(out, `\u%04x\u%04x`, high, low)
		default:
			fmt.Fprintf(out, `\u%04x`, r)
		}
	}
	out.WriteByte('"')
}
