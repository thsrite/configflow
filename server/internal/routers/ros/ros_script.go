package ros

import "strings"

// ScriptString quotes a Client console string using the escaping accepted by
// /execute. Dollar signs are escaped because they otherwise start variables.
func ScriptString(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '$':
			b.WriteString(`\$`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ScriptValue converts a string-valued Client parameter into console syntax.
// Disabled is a yes/no flag on the console although REST accepts true/false.
func ScriptValue(key, value string) string {
	if key == "disabled" {
		switch Comparable(key, value) {
		case "true":
			return "yes"
		case "false":
			return "no"
		}
	}
	return ScriptString(value)
}
