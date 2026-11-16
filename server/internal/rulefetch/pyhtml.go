package rulefetch

// CPython 3.11.17's html.unescape and the part of html.parser.HTMLParser
// (feed without close) that decides which start tags reach
// handle_starttag, for website_identity._Meta. Every delimiter of the
// parser's regular expressions is ASCII, so the scan works on UTF-8 bytes;
// str.lower()/str.strip() use internal/pystr.

import (
	"strings"
	"unicode/utf8"

	"configflow/internal/pystr"
)

// pyUnescape is html.unescape(s): numeric references with CPython's
// replacement rules (invalid code points dropped, surrogates and values
// beyond U+10FFFF as U+FFFD, the Windows-1252 table for 0x80–0x9F) and named
// references of html.entities.html5, the longest known prefix for a name
// without ";".
func pyUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		replacement, width := unescapeAt(s, i+1)
		if width == 0 {
			b.WriteByte('&')
			i++
			continue
		}
		b.WriteString(replacement)
		i += 1 + width
	}
	return b.String()
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isASCIIDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// unescapeAt matches _charref at s[i:] (after the "&"): the replacement and
// the number of bytes consumed, 0 for no match.
func unescapeAt(s string, i int) (string, int) {
	n := len(s)
	if i < n && s[i] == '#' {
		j := i + 1
		hex := j < n && (s[j] == 'x' || s[j] == 'X') && j+1 < n && isHexDigit(s[j+1])
		if !hex && !(j < n && isASCIIDigit(s[j])) {
			return "", 0 // `[^\t\n\f <&#;]` cannot start with "#"
		}
		base := 10
		if hex {
			base = 16
			j++
		}
		num, overflow := 0, false
		for j < n && ((base == 10 && isASCIIDigit(s[j])) || (base == 16 && isHexDigit(s[j]))) {
			digit := int(s[j] - '0')
			if s[j] >= 'a' {
				digit = int(s[j]-'a') + 10
			} else if s[j] >= 'A' {
				digit = int(s[j]-'A') + 10
			}
			if !overflow {
				num = num*base + digit
				if num > 0x10FFFF {
					overflow = true
				}
			}
			j++
		}
		if j < n && s[j] == ';' {
			j++
		}
		var replacement string
		switch {
		case overflow:
			replacement = "�"
		case invalidCharrefs[num] != "":
			replacement = invalidCharrefs[num]
		case num >= 0xD800 && num <= 0xDFFF:
			replacement = "�"
		case invalidCodepoints[num]:
			replacement = ""
		default:
			replacement = string(rune(num))
		}
		return replacement, j - i
	}
	// [^\t\n\f <&#;]{1,32};?  (32 code points)
	j, count := i, 0
	for j < n && count < 32 {
		c := s[j]
		if c == '\t' || c == '\n' || c == '\f' || c == ' ' || c == '<' || c == '&' || c == '#' || c == ';' {
			break
		}
		j += runeWidth(s, j)
		count++
	}
	if count == 0 {
		return "", 0
	}
	if j < n && s[j] == ';' {
		j++
	}
	name := s[i:j]
	if value, ok := html5Entities[name]; ok {
		return value, j - i
	}
	// The longest matching prefix (in code points, at least 2).
	runes := []rune(name)
	for x := len(runes) - 1; x > 1; x-- {
		if value, ok := html5Entities[string(runes[:x])]; ok {
			return value + string(runes[x:]), j - i
		}
	}
	return "&" + name, j - i
}

func runeWidth(s string, i int) int {
	_, size := utf8.DecodeRuneInString(s[i:])
	return size
}

// asciiEqualFold compares under re.IGNORECASE|re.ASCII.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 32
		}
		if y >= 'A' && y <= 'Z' {
			y += 32
		}
		if x != y {
			return false
		}
	}
	return true
}

func isParserSpace(c byte) bool { return c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == ' ' }

// tagNameEnd is `[a-zA-Z][^\t\n\r\f />]*` from p (s[p] is a letter).
func tagNameEnd(s string, p int) int {
	q := p + 1
	for q < len(s) && !isParserSpace(s[q]) && s[q] != '/' && s[q] != '>' {
		q++
	}
	return q
}

// attrValueEnd matches `[\t\n\r\f ]*=[\t\n\r\f ]*('[^']*'|"[^"]*"|(?!['"])[^>\t\n\r\f ]*)`
// at q with the backtracking of Python's re: the end of the group and the
// value (group 3), ok false when the group does not match. A quote without
// its closing quote fails the alternatives; giving back one space after "="
// then lets the bare alternative match an empty value.
func attrValueEnd(s string, q int) (end int, value string, ok bool) {
	n := len(s)
	v := q
	for v < n && isParserSpace(s[v]) {
		v++
	}
	if v >= n || s[v] != '=' {
		return 0, "", false
	}
	v++
	afterEquals := v
	for v < n && isParserSpace(s[v]) {
		v++
	}
	if v < n && (s[v] == '\'' || s[v] == '"') {
		if closing := strings.IndexByte(s[v+1:], s[v]); closing >= 0 {
			end = v + 1 + closing + 1
			return end, s[v:end], true
		}
		if v > afterEquals {
			return v - 1, "", true
		}
		return 0, "", false
	}
	w := v
	for w < n && s[w] != '>' && !isParserSpace(s[w]) {
		w++
	}
	return w, s[v:w], true
}

// attrNameStart reports whether an attribute name can start at q:
// `(?<=['"\t\n\r\f /])[^\t\n\r\f />]`.
func attrNameStart(s string, q int) bool {
	if q <= 0 || q >= len(s) {
		return false
	}
	prev := s[q-1]
	if !(prev == '\'' || prev == '"' || prev == '/' || isParserSpace(prev)) {
		return false
	}
	c := s[q]
	return !isParserSpace(c) && c != '/' && c != '>'
}

// attrNameEnd is `[^\t\n\r\f /=>]*` after the first name character at q.
func attrNameEnd(s string, q int) int {
	q++
	for q < len(s) && !isParserSpace(s[q]) && s[q] != '/' && s[q] != '=' && s[q] != '>' {
		q++
	}
	return q
}

// locateTagEnd is locatetagend.match(rawdata, p).end().
func locateTagEnd(s string, p int) int {
	n := len(s)
	q := tagNameEnd(s, p)
	for q < n && (isParserSpace(s[q]) || s[q] == '/') {
		q++
	}
	for attrNameStart(s, q) {
		q = attrNameEnd(s, q)
		if end, _, ok := attrValueEnd(s, q); ok {
			q = end
		}
		for q < n && (isParserSpace(s[q]) || s[q] == '/') {
			q++
		}
	}
	if q < n && s[q] == '>' {
		q++
	}
	return q
}

// skipSpaceOrSlash is `(?:[\t\n\r\f ]|/(?!>))*`.
func skipSpaceOrSlash(s string, q int) int {
	for q < len(s) && (isParserSpace(s[q]) || (s[q] == '/' && !(q+1 < len(s) && s[q+1] == '>'))) {
		q++
	}
	return q
}

type htmlAttr struct {
	name  string
	value *string
}

// parseStartTag is HTMLParser.parse_starttag(i): the end position (-1 when
// the tag is incomplete), the lowercased tag, its attributes, whether it is
// a start tag at all (handle_data otherwise) and whether it is "<.../>".
func parseStartTag(s string, i int) (endpos int, tag string, attrs []htmlAttr, isTag, startEnd bool) {
	endpos = locateTagEnd(s, i+1)
	if s[endpos-1] != '>' {
		return -1, "", nil, false, false
	}
	nameEnd := tagNameEnd(s, i+1)
	tag = pystr.Lower(s[i+1 : nameEnd])
	k := skipSpaceOrSlash(s, nameEnd)
	for k < endpos && attrNameStart(s, k) {
		nameEnd := attrNameEnd(s, k)
		attr := htmlAttr{name: pystr.Lower(s[k:nameEnd])}
		k = nameEnd
		if end, value, ok := attrValueEnd(s, k); ok {
			if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' ||
				value[0] == '"' && value[len(value)-1] == '"') {
				value = value[1 : len(value)-1]
			}
			if value != "" {
				value = pyUnescape(value)
			}
			attr.value = &value
			k = end
		}
		attrs = append(attrs, attr)
		k = skipSpaceOrSlash(s, k)
	}
	end := pystr.Strip(s[k:endpos])
	if end != ">" && end != "/>" {
		return endpos, tag, attrs, false, false
	}
	return endpos, tag, attrs, true, strings.HasSuffix(end, "/>")
}

var (
	cdataElements  = map[string]bool{"script": true, "style": true, "xmp": true, "iframe": true, "noembed": true, "noframes": true}
	rcdataElements = map[string]bool{"textarea": true, "title": true}
)

// cdataEnd is the start of `</elem(?=[\t\n\r\f />])` (ASCII case-insensitive)
// at or after i, -1 when there is none.
func cdataEnd(s string, i int, elem string) int {
	for j := i; j+2+len(elem) < len(s); j++ {
		if s[j] == '<' && s[j+1] == '/' && asciiEqualFold(s[j+2:j+2+len(elem)], elem) {
			c := s[j+2+len(elem)]
			if isParserSpace(c) || c == '/' || c == '>' {
				return j
			}
		}
	}
	return -1
}

func indexFrom(s string, i int, sub string) int {
	if i > len(s) {
		return -1
	}
	if at := strings.Index(s[i:], sub); at >= 0 {
		return i + at
	}
	return -1
}

func isASCIILetterByte(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// metaStartTags runs HTMLParser.feed(value) (no close) and calls handle for
// every start tag (also the start half of "<.../>") in order.
func metaStartTags(s string, handle func(tag string, attrs []htmlAttr)) {
	n := len(s)
	i := 0
	cdata := ""
	for i < n {
		var j int
		switch {
		case cdata == "plaintext":
			return // interesting = \Z: the rest is data
		case cdata != "":
			if j = cdataEnd(s, i, cdata); j < 0 {
				return
			}
		default:
			if j = indexFrom(s, i, "<"); j < 0 {
				return
			}
		}
		i = j
		k := -1
		switch {
		case i+1 < n && isASCIILetterByte(s[i+1]):
			endpos, tag, attrs, isTag, startEnd := parseStartTag(s, i)
			k = endpos
			if isTag {
				handle(tag, attrs)
				if !startEnd {
					if cdataElements[tag] || tag == "plaintext" || rcdataElements[tag] {
						cdata = tag
					}
				}
			}
		case strings.HasPrefix(s[i:], "</"):
			k = parseEndTag(s, i)
			if k >= 0 && i+2 < n && isASCIILetterByte(s[i+2]) {
				cdata = "" // clear_cdata_mode after a real end tag
			}
		case strings.HasPrefix(s[i:], "<!--"):
			k = parseComment(s, i)
		case strings.HasPrefix(s[i:], "<?"):
			if gt := indexFrom(s, i+2, ">"); gt >= 0 {
				k = gt + 1
			}
		case strings.HasPrefix(s[i:], "<!"):
			k = parseHTMLDeclaration(s, i)
		case i+1 < n:
			k = i + 1
		}
		if k < 0 {
			return
		}
		i = k
	}
}

// parseEndTag is HTMLParser.parse_endtag(i) (end position or -1).
func parseEndTag(s string, i int) int {
	if indexFrom(s, i+2, ">") < 0 {
		return -1
	}
	if !(i+2 < len(s) && isASCIILetterByte(s[i+2])) {
		if i+3 <= len(s) && s[i+2] == '>' {
			return i + 3
		}
		return indexFrom(s, i+2, ">") + 1 // parse_bogus_comment
	}
	j := locateTagEnd(s, i+2)
	if s[j-1] != '>' {
		return -1
	}
	return j
}

// parseComment is HTMLParser.parse_comment(i): `--!?>` searched from i+4,
// else `-?>` right at i+4.
func parseComment(s string, i int) int {
	for j := i + 4; j+2 < len(s); j++ {
		if s[j] == '-' && s[j+1] == '-' {
			if s[j+2] == '>' {
				return j + 3
			}
			if s[j+2] == '!' && j+3 < len(s) && s[j+3] == '>' {
				return j + 4
			}
		}
	}
	rest := s[min(i+4, len(s)):]
	switch {
	case strings.HasPrefix(rest, "->"):
		return i + 6
	case strings.HasPrefix(rest, ">"):
		return i + 5
	}
	return -1
}

// parseHTMLDeclaration is HTMLParser.parse_html_declaration(i).
func parseHTMLDeclaration(s string, i int) int {
	rest := s[i:]
	switch {
	case strings.HasPrefix(rest, "<![CDATA["):
		if j := indexFrom(s, i+9, "]]>"); j >= 0 {
			return j + 3
		}
		return -1
	case len(rest) >= 9 && asciiEqualFold(rest[:9], "<!doctype"):
		if gt := indexFrom(s, i+9, ">"); gt >= 0 {
			return gt + 1
		}
		return -1
	case strings.HasPrefix(rest, "<!["):
		if j := indexFrom(s, i+3, ">"); j >= 0 {
			return j + 1
		}
		return -1
	}
	if gt := indexFrom(s, i+2, ">"); gt >= 0 { // parse_bogus_comment
		return gt + 1
	}
	return -1
}

// metaContent is _meta_content: the content attribute of the last <meta>
// start tag HTMLParser reports (dict(attrs).get('content') or the empty string), cleaned.
func metaContent(value string) string {
	content := ""
	metaStartTags(value, func(tag string, attrs []htmlAttr) {
		if pystr.Lower(tag) != "meta" {
			return
		}
		var found *string
		for _, attr := range attrs {
			if attr.name == "content" {
				found = attr.value
			}
		}
		content = ""
		if found != nil {
			content = *found
		}
	})
	return CleanText(content, 200)
}
