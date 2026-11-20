package rulefetch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Archived fixtures use the original quoting when constructing source text.
func jsonDumps(value string, ascii bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
	out := strings.TrimSuffix(buf.String(), "\n")
	if !ascii {
		return out
	}
	var b strings.Builder
	for _, r := range out {
		if r < 0x80 {
			b.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&b, `\u%04x`, r)
		} else {
			r1, r2 := r-0x10000, r-0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r1>>10), 0xdc00+(r2&0x3ff))
		}
	}
	return b.String()
}
