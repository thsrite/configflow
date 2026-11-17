package settings

import (
	"strconv"

	"configflow/internal/httpx"
)

// invalidUnicode checks escaped UTF-16 pairs before encoding/json can replace
// them with U+FFFD. Escaped backslashes and valid surrogate pairs stay untouched.
func invalidUnicode(c *httpx.Ctx) bool {
	data, err := c.Data()
	if err != nil {
		return false
	}
	quoted := false
	unit := func(at int) (uint64, bool) {
		if at+6 > len(data) || data[at] != '\\' || data[at+1] != 'u' {
			return 0, false
		}
		value, err := strconv.ParseUint(string(data[at+2:at+6]), 16, 16)
		return value, err == nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[i] != '\\' {
			continue
		}
		if value, ok := unit(i); ok {
			if value >= 0xdc00 && value <= 0xdfff {
				return true
			}
			if value >= 0xd800 && value <= 0xdbff {
				low, ok := unit(i + 6)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return true
				}
				i += 6
			}
			i += 5
		} else {
			i++
		}
	}
	return false
}
