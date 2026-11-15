package agents

// How requests turns an Agent's response body into response.text and
// response.json() (requests 2.31, the version the Python backend pins), so
// that diagnostics.response_failure and every response.json() call see the
// same text as in Python.

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// contentType is response.headers.get("Content-Type") (or ""): repeated header
// fields are joined with ", " as urllib3 does.
func (r *Response) contentType() string {
	return strings.Join(r.Header.Values("Content-Type"), ", ")
}

// encoding is requests.utils.get_encoding_from_headers: the charset
// parameter, else ISO-8859-1 for any "text" type, else utf-8 for
// application/json; ok=false is None.
func (r *Response) encoding() (string, bool) {
	header := r.contentType()
	if header == "" {
		return "", false
	}
	tokens := strings.Split(header, ";")
	contentType := strings.TrimSpace(tokens[0])
	params := map[string]string{}
	for _, param := range tokens[1:] {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}
		key, value := param, ""
		if index := strings.Index(param, "="); index >= 0 {
			key = strings.Trim(param[:index], "\"' ")
			value = strings.Trim(param[index+1:], "\"' ")
		}
		params[strings.ToLower(key)] = value
	}
	if charset, ok := params["charset"]; ok {
		return strings.Trim(charset, "'\""), true
	}
	if strings.Contains(contentType, "text") {
		return "ISO-8859-1", true
	}
	if strings.Contains(contentType, "application/json") {
		return "utf-8", true
	}
	return "", false
}

// pyCodec is the Python codec a name refers to, for the codecs decoded here
// (codecs.lookup normalisation and the aliases of encodings/aliases.py).
func pyCodec(name string) string {
	var b strings.Builder
	pending := false
	for _, r := range strings.ToLower(name) {
		if r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pending && b.Len() > 0 {
				b.WriteByte('_')
			}
			pending = false
			b.WriteRune(r)
		} else {
			pending = true
		}
	}
	normalized := b.String()
	switch strings.ReplaceAll(normalized, ".", "_") {
	case "utf_8", "utf8", "u8", "utf", "utf8_ucs2", "utf8_ucs4", "cp65001":
		return "utf-8"
	case "utf_8_sig":
		return "utf-8-sig"
	case "latin_1", "iso8859_1", "iso_8859_1", "latin1", "l1", "8859", "cp819", "iso_ir_100", "csisolatin1",
		"iso_8859_1_1987", "latin", "iso8859":
		return "latin-1"
	case "ascii", "us_ascii", "646", "ansi_x3_4_1968", "ansi_x3_4_1986", "cp367", "csascii", "ibm367",
		"iso646_us", "iso_646_irv_1991", "iso_ir_6", "us":
		return "ascii"
	case "utf_16", "u16", "utf16":
		return "utf-16"
	case "utf_16_le", "utf_16le", "unicodelittleunmarked":
		return "utf-16-le"
	case "utf_16_be", "utf_16be", "unicodebigunmarked":
		return "utf-16-be"
	case "utf_32", "u32", "utf32":
		return "utf-32"
	case "utf_32_le", "utf_32le":
		return "utf-32-le"
	case "utf_32_be", "utf_32be":
		return "utf-32-be"
	}
	return ""
}

// pyDecode is bytes.decode(codec, errors): strict=false is errors='replace'.
// ok=false is a UnicodeDecodeError (strict) or an unknown codec.
func pyDecode(b []byte, codec string, strict bool) (string, bool) {
	switch codec {
	case "utf-8-sig":
		return decodeUTF8(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}), strict)
	case "utf-8":
		return decodeUTF8(b, strict)
	case "latin-1":
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		return string(runes), true
	case "ascii":
		var out strings.Builder
		for _, c := range b {
			if c >= 0x80 {
				if strict {
					return "", false
				}
				out.WriteRune(utf8.RuneError)
				continue
			}
			out.WriteByte(c)
		}
		return out.String(), true
	case "utf-16", "utf-16-le", "utf-16-be":
		order := binary.ByteOrder(binary.LittleEndian)
		if codec == "utf-16-be" {
			order = binary.BigEndian
		}
		if codec == "utf-16" && len(b) >= 2 {
			if b[0] == 0xFE && b[1] == 0xFF {
				order, b = binary.BigEndian, b[2:]
			} else if b[0] == 0xFF && b[1] == 0xFE {
				b = b[2:]
			}
		}
		return decodeUTF16Units(b, order, strict)
	case "utf-32", "utf-32-le", "utf-32-be":
		order := binary.ByteOrder(binary.LittleEndian)
		if codec == "utf-32-be" {
			order = binary.BigEndian
		}
		if codec == "utf-32" && len(b) >= 4 {
			if bytes.HasPrefix(b, []byte{0, 0, 0xFE, 0xFF}) {
				order, b = binary.BigEndian, b[4:]
			} else if bytes.HasPrefix(b, []byte{0xFF, 0xFE, 0, 0}) {
				b = b[4:]
			}
		}
		var out strings.Builder
		for len(b) > 0 {
			if len(b) < 4 {
				if strict {
					return "", false
				}
				out.WriteRune(utf8.RuneError)
				break
			}
			r := order.Uint32(b)
			b = b[4:]
			if r > unicode.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
				if strict {
					return "", false
				}
				r = utf8.RuneError
			}
			out.WriteRune(rune(r))
		}
		return out.String(), true
	}
	return "", false
}

func decodeUTF16Units(b []byte, order binary.ByteOrder, strict bool) (string, bool) {
	var out strings.Builder
	for len(b) > 0 {
		if len(b) < 2 {
			if strict {
				return "", false
			}
			out.WriteRune(utf8.RuneError)
			break
		}
		unit := rune(order.Uint16(b))
		b = b[2:]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF && len(b) >= 2:
			low := rune(order.Uint16(b))
			if low >= 0xDC00 && low <= 0xDFFF {
				out.WriteRune(utf16.DecodeRune(unit, low))
				b = b[2:]
				continue
			}
			fallthrough
		case unit >= 0xD800 && unit <= 0xDFFF:
			if strict {
				return "", false
			}
			out.WriteRune(utf8.RuneError)
		default:
			out.WriteRune(unit)
		}
	}
	return out.String(), true
}

// decodeUTF8 is CPython's UTF-8 decoder: with errors='replace' every maximal
// subpart of an ill-formed sequence becomes one U+FFFD (a truncated
// three-byte sequence is one replacement, a stray byte is one each).
func decodeUTF8(b []byte, strict bool) (string, bool) {
	if utf8.Valid(b) {
		return string(b), true
	}
	if strict {
		return "", false
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.WriteRune(r)
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += maximalSubpart(b[i:])
	}
	return out.String(), true
}

// maximalSubpart is the length of the longest prefix of b that starts a
// well-formed UTF-8 sequence (at least 1), b not starting with one.
func maximalSubpart(b []byte) int {
	lead := b[0]
	var need int
	low, high := byte(0x80), byte(0xBF)
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead == 0xE0:
		need, low = 2, 0xA0
	case lead == 0xED:
		need, high = 2, 0x9F
	case lead >= 0xE1 && lead <= 0xEF:
		need = 2
	case lead == 0xF0:
		need, low = 3, 0x90
	case lead == 0xF4:
		need, high = 3, 0x8F
	case lead >= 0xF1 && lead <= 0xF3:
		need = 3
	default:
		return 1
	}
	n := 1
	for n <= need && n < len(b) {
		c := b[n]
		if n == 1 && (c < low || c > high) || n > 1 && (c < 0x80 || c > 0xBF) {
			break
		}
		n++
	}
	return n
}

// apparentCodec stands in for requests' apparent_encoding (charset_normalizer)
// when the headers name no charset: a byte order mark or the NUL pattern of
// UTF-16/32 decides as it does there; anything else is taken to be UTF-8.
func apparentCodec(data []byte) string {
	if len(data) >= 4 {
		if codec := guessJSONUTF(data); codec != "" && codec != "utf-8" {
			return codec
		}
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return "utf-8-sig"
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return "utf-16"
	}
	return "utf-8"
}

// guessJSONUTF is requests.utils.guess_json_utf.
func guessJSONUTF(data []byte) string {
	sample := data
	if len(sample) > 4 {
		sample = sample[:4]
	}
	switch {
	case bytes.Equal(sample, []byte{0xFF, 0xFE, 0, 0}) || bytes.Equal(sample, []byte{0, 0, 0xFE, 0xFF}):
		return "utf-32"
	case bytes.HasPrefix(sample, []byte{0xEF, 0xBB, 0xBF}):
		return "utf-8-sig"
	case bytes.HasPrefix(sample, []byte{0xFF, 0xFE}) || bytes.HasPrefix(sample, []byte{0xFE, 0xFF}):
		return "utf-16"
	}
	nulls := bytes.Count(sample, []byte{0})
	switch nulls {
	case 0:
		return "utf-8"
	case 2:
		if len(sample) == 4 && sample[0] == 0 && sample[2] == 0 {
			return "utf-16-be"
		}
		if len(sample) == 4 && sample[1] == 0 && sample[3] == 0 {
			return "utf-16-le"
		}
	case 3:
		if len(sample) == 4 && bytes.Equal(sample[:3], []byte{0, 0, 0}) {
			return "utf-32-be"
		}
		if len(sample) == 4 && bytes.Equal(sample[1:], []byte{0, 0, 0}) {
			return "utf-32-le"
		}
	}
	return ""
}
