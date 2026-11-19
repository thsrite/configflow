package agents

import (
	"bytes"
	"encoding/binary"
	"mime"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/ianaindex"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

// contentType returns the declared media type and parameters for diagnostics.
func (r *Response) contentType() string { return strings.Join(r.Header.Values("Content-Type"), ", ") }

func (r *Response) encoding() (string, bool) {
	media, params, err := mime.ParseMediaType(r.contentType())
	if err != nil {
		return "", false
	}
	if charset, present := params["charset"]; present {
		return charset, true
	}
	if strings.HasPrefix(media, "text/") {
		return "ISO-8859-1", true
	}
	if media == "application/json" {
		return "UTF-8", true
	}
	return "", false
}

// unicodeEncoding recognizes Unicode byte-order marks and the ASCII JSON
// prefix's NUL layout. Decoding itself belongs to the native x/text codecs.
func unicodeEncoding(data []byte) (encoding.Encoding, int, binary.ByteOrder) {
	switch {
	case bytes.HasPrefix(data, []byte{0, 0, 0xfe, 0xff}):
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM), 4, binary.BigEndian
	case bytes.HasPrefix(data, []byte{0xff, 0xfe, 0, 0}):
		return utf32.UTF32(utf32.LittleEndian, utf32.UseBOM), 4, binary.LittleEndian
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return textunicode.UTF16(textunicode.BigEndian, textunicode.UseBOM), 2, binary.BigEndian
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		return textunicode.UTF16(textunicode.LittleEndian, textunicode.UseBOM), 2, binary.LittleEndian
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		return textunicode.UTF8BOM, 1, nil
	}
	if len(data) >= 4 {
		switch {
		case data[0] == 0 && data[1] == 0:
			return utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM), 4, binary.BigEndian
		case data[0] == 0 && data[2] == 0:
			return textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM), 2, binary.BigEndian
		case data[1] == 0 && data[2] == 0 && data[3] == 0:
			return utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM), 4, binary.LittleEndian
		case data[1] == 0 && data[3] == 0:
			return textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM), 2, binary.LittleEndian
		}
	}
	if len(data) == 2 {
		if data[0] == 0 {
			return textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM), 2, binary.BigEndian
		}
		if data[1] == 0 {
			return textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM), 2, binary.LittleEndian
		}
	}
	return textunicode.UTF8, 1, nil
}

func charsetEncoding(name string) encoding.Encoding {
	// IANA's registry supplies aliases and code pages; Latin-1 stays the real
	// ISO encoding rather than the browser registry's Windows-1252 remapping.
	enc, err := ianaindex.MIME.Encoding(name)
	if err != nil || enc == nil {
		return textunicode.UTF8
	}
	return enc
}

// DecodeUTF8Replace decodes UTF-8 diagnostics through the native Unicode codec.
func DecodeUTF8Replace(data []byte) string {
	decoded, _ := textunicode.UTF8.NewDecoder().Bytes(data)
	return string(decoded)
}

// DecodeJSONBytes supports the historical UTF-8/16/32 JSON storage envelopes.
// Invalid UTF-8, incomplete code units and out-of-range UTF-32 are rejected.
func DecodeJSONBytes(data []byte) (string, bool) {
	enc, width, order := unicodeEncoding(data)
	if width == 1 && !utf8.Valid(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})) {
		return "", false
	}
	if len(data)%width != 0 {
		return "", false
	}
	if width == 4 {
		for i := 0; i < len(data); i += 4 {
			if order.Uint32(data[i:]) > utf8.MaxRune {
				return "", false
			}
		}
	}
	decoded, err := enc.NewDecoder().Bytes(data)
	return string(decoded), err == nil
}

// Text decodes an upstream diagnostic using its declared MIME charset, or
// Unicode transport detection when no charset/default is available.
func (r *Response) Text() string {
	enc, _, _ := unicodeEncoding(r.Body)
	if charset, declared := r.encoding(); declared {
		enc = charsetEncoding(charset)
		if strings.EqualFold(charset, "ISO-8859-1") || strings.EqualFold(charset, "latin-1") {
			enc = charmap.ISO8859_1
		}
	}
	decoded, _ := enc.NewDecoder().Bytes(r.Body)
	return string(decoded)
}
