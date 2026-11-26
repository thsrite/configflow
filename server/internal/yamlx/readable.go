package yamlx

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// ReadableUnicode restores printable supplementary characters in quoted YAML
// scalars. A literal backslash in a regex, comment or block remains literal.
func ReadableUnicode(source string) string {
	if !strings.Contains(source, "\\U") {
		return source
	}
	raw := []byte(source)
	doc, err := Parse(raw)
	if err != nil {
		return source
	}
	index := NewSource(raw)
	var out bytes.Buffer
	from := 0
	walkEditNodes(doc, func(n *Node) {
		if n.Kind != yaml.ScalarNode || n.Style&yaml.DoubleQuotedStyle == 0 {
			return
		}
		start, ok := index.positions.offset(n)
		if !ok || start < from || start >= len(raw) || raw[start] != '"' {
			return
		}
		for i := start + 1; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] != '\\' {
				continue
			}
			i++
			if i+9 > len(raw) || raw[i] != 'U' {
				continue
			}
			v, err := strconv.ParseUint(source[i+1:i+9], 16, 32)
			if err != nil || v < 0x10000 || v > utf8.MaxRune || !unicode.IsPrint(rune(v)) {
				continue
			}
			out.Write(raw[from : i-1])
			out.WriteRune(rune(v))
			i += 8
			from = i + 1
		}
	})
	if from == 0 {
		return source
	}
	out.Write(raw[from:])
	return out.String()
}
