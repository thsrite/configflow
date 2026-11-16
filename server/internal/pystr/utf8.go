package pystr

import "fmt"

// UTF8DecodeError is str(UnicodeDecodeError) of CPython's strict UTF-8
// decoder (Objects/stringlib/codecs.h) for the first invalid sequence of
// data, "" when data is valid UTF-8.
func UTF8DecodeError(data []byte) string {
	continuation := func(b byte) bool { return b >= 0x80 && b < 0xC0 }
	for start := 0; start < len(data); {
		lead := data[start]
		rest := len(data) - start
		code := -1 // -1 valid; 0 truncated; 1 invalid start; n>1: invalid continuation at offset n-1
		size := 1
		switch {
		case lead < 0x80:
		case lead < 0xC2:
			code = 1
		case lead < 0xE0:
			size = 2
			if rest < 2 {
				code = 0
			} else if !continuation(data[start+1]) {
				code = 2
			}
		case lead < 0xF0:
			size = 3
			bad2 := func(b byte) bool {
				return !continuation(b) || (lead == 0xE0 && b < 0xA0) || (lead == 0xED && b >= 0xA0)
			}
			switch {
			case rest < 2:
				code = 0
			case bad2(data[start+1]):
				code = 2
			case rest < 3:
				code = 0
			case !continuation(data[start+2]):
				code = 3
			}
		case lead < 0xF5:
			size = 4
			bad2 := func(b byte) bool {
				return !continuation(b) || (lead == 0xF0 && b < 0x90) || (lead == 0xF4 && b >= 0x90)
			}
			switch {
			case rest < 2:
				code = 0
			case bad2(data[start+1]):
				code = 2
			case rest < 3:
				code = 0
			case !continuation(data[start+2]):
				code = 3
			case rest < 4:
				code = 0
			case !continuation(data[start+3]):
				code = 4
			}
		default:
			code = 1
		}
		if code < 0 {
			start += size
			continue
		}
		end, reason := start+1, "invalid start byte"
		switch {
		case code == 0:
			end, reason = len(data), "unexpected end of data"
		case code > 1:
			end, reason = start+code-1, "invalid continuation byte"
		}
		if end == start+1 {
			return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", data[start], start, reason)
		}
		return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", start, end-1, reason)
	}
	return ""
}
