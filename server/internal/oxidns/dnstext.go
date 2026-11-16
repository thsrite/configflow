package oxidns

import (
	"encoding/hex"
	"math/big"
	"regexp"
	"strings"

	"configflow/internal/pystr"
)

// The parts of dnspython 2.7.0 that oxidns_plugin_constraints'
// _zone_records_error uses: the zone-file tokenizer (dns.tokenizer), name
// parsing (dns.name.from_text with the IDNA 2003 codec), TTLs (dns.ttl),
// and dns.rdata.from_text for the record types OxiDNS accepts in text form.
//
// Exceptions the caller catches (ValueError, TypeError, DNSException) are
// panic(dnsFail{}); struct.error of a "\DDD" escape above 255 in an ASCII
// name is raised as Python's error (the caller does not catch it).

type dnsFail struct{}

func failDNS() { panic(dnsFail{}) }

const (
	tokEOF = iota
	tokEOL
	tokWhitespace
	tokIdentifier
	tokQuoted
	tokComment
	tokDelimiter
)

type dnsToken struct {
	ttype     int
	value     string
	hasEscape bool
	comment   *string
}

func (t dnsToken) isEOLOrEOF() bool { return t.ttype == tokEOL || t.ttype == tokEOF }

// pyIsDigitRune is str.isdigit() for one character.
func pyIsDigitRune(r rune) bool { return pystr.IsDigit(string(r)) }

// digitInt is int(c) for one character that isdigit() accepted: a decimal
// digit's value, else the ValueError.
func digitInt(r rune) int {
	d := pystr.Decimal(r)
	if d < 0 {
		failDNS()
	}
	return d
}

// unescape is Token.unescape().
func (t dnsToken) unescape() dnsToken {
	if !t.hasEscape {
		return t
	}
	value := []rune(t.value)
	var b strings.Builder
	l := len(value)
	for i := 0; i < l; {
		c := value[i]
		i++
		if c == '\\' {
			if i >= l {
				failDNS()
			}
			c = value[i]
			i++
			if pyIsDigitRune(c) {
				if i >= l {
					failDNS()
				}
				c2 := value[i]
				i++
				if i >= l {
					failDNS()
				}
				c3 := value[i]
				i++
				if !(pyIsDigitRune(c2) && pyIsDigitRune(c3)) {
					failDNS()
				}
				codepoint := digitInt(c)*100 + digitInt(c2)*10 + digitInt(c3)
				if codepoint > 255 {
					failDNS()
				}
				c = rune(codepoint)
			}
		}
		b.WriteRune(c)
	}
	return dnsToken{ttype: t.ttype, value: b.String()}
}

// unescapeToBytes is Token.unescape_to_bytes().value.
func (t dnsToken) unescapeToBytes() []byte {
	value := []rune(t.value)
	var out []byte
	l := len(value)
	for i := 0; i < l; {
		c := value[i]
		i++
		if c == '\\' {
			if i >= l {
				failDNS()
			}
			c = value[i]
			i++
			if pyIsDigitRune(c) {
				if i >= l {
					failDNS()
				}
				c2 := value[i]
				i++
				if i >= l {
					failDNS()
				}
				c3 := value[i]
				i++
				if !(pyIsDigitRune(c2) && pyIsDigitRune(c3)) {
					failDNS()
				}
				codepoint := digitInt(c)*100 + digitInt(c2)*10 + digitInt(c3)
				if codepoint > 255 {
					failDNS()
				}
				out = append(out, byte(codepoint))
				continue
			}
		}
		out = append(out, []byte(string(c))...)
	}
	return out
}

type dnsTokenizer struct {
	input         []rune
	pos           int
	ungottenChar  *rune
	ungottenToken *dnsToken
	multiline     int
	quoting       bool
	eof           bool
}

func newDNSTokenizer(text string) *dnsTokenizer { return &dnsTokenizer{input: []rune(text)} }

const noChar rune = -1

func (t *dnsTokenizer) getChar() rune {
	if t.ungottenChar != nil {
		c := *t.ungottenChar
		t.ungottenChar = nil
		return c
	}
	if t.eof {
		return noChar
	}
	if t.pos >= len(t.input) {
		t.eof = true
		return noChar
	}
	c := t.input[t.pos]
	t.pos++
	return c
}

func (t *dnsTokenizer) ungetChar(c rune) {
	if t.ungottenChar != nil {
		failDNS() // UngetBufferFull
	}
	t.ungottenChar = &c
}

func (t *dnsTokenizer) skipWhitespace() int {
	skipped := 0
	for {
		c := t.getChar()
		if c != ' ' && c != '\t' {
			if c != '\n' || t.multiline == 0 {
				t.ungetChar(c)
				return skipped
			}
		}
		skipped++
	}
}

func (t *dnsTokenizer) isDelimiter(c rune) bool {
	if t.quoting {
		return c == '"'
	}
	switch c {
	case ' ', '\t', '\n', ';', '(', ')', '"':
		return true
	}
	return false
}

// get is Tokenizer.get(want_leading) (want_comment is never used).
func (t *dnsTokenizer) get(wantLeading bool) dnsToken {
	if t.ungottenToken != nil {
		utoken := *t.ungottenToken
		t.ungottenToken = nil
		switch utoken.ttype {
		case tokWhitespace:
			if wantLeading {
				return utoken
			}
		case tokComment:
		default:
			return utoken
		}
	}
	skipped := t.skipWhitespace()
	if wantLeading && skipped > 0 {
		return dnsToken{ttype: tokWhitespace, value: " "}
	}
	var token strings.Builder
	ttype := tokIdentifier
	hasEscape := false
	for {
		c := t.getChar()
		if c == noChar || t.isDelimiter(c) {
			if c == noChar && t.quoting {
				failDNS()
			}
			if token.Len() == 0 && ttype != tokQuoted {
				switch c {
				case '(':
					t.multiline++
					t.skipWhitespace()
					continue
				case ')':
					if t.multiline <= 0 {
						failDNS()
					}
					t.multiline--
					t.skipWhitespace()
					continue
				case '"':
					if !t.quoting {
						t.quoting = true
						ttype = tokQuoted
						continue
					}
					t.quoting = false
					t.skipWhitespace()
					continue
				case '\n':
					return dnsToken{ttype: tokEOL, value: "\n"}
				case ';':
					var comment strings.Builder
					for {
						c = t.getChar()
						if c == '\n' || c == noChar {
							break
						}
						comment.WriteRune(c)
					}
					text := comment.String()
					if c == noChar {
						if t.multiline > 0 {
							failDNS()
						}
						return dnsToken{ttype: tokEOF, comment: &text}
					}
					if t.multiline > 0 {
						t.skipWhitespace()
						continue
					}
					return dnsToken{ttype: tokEOL, value: "\n", comment: &text}
				case noChar:
				default:
					token.WriteRune(c)
					ttype = tokDelimiter
				}
			} else {
				t.ungetChar(c)
			}
			break
		} else if t.quoting && c == '\n' {
			failDNS()
		} else if c == '\\' {
			token.WriteRune(c)
			hasEscape = true
			c = t.getChar()
			if c == noChar || (c == '\n' && !t.quoting) {
				failDNS()
			}
		}
		token.WriteRune(c)
	}
	if token.Len() == 0 && ttype != tokQuoted {
		if t.multiline > 0 {
			failDNS()
		}
		ttype = tokEOF
	}
	return dnsToken{ttype: ttype, value: token.String(), hasEscape: hasEscape}
}

func (t *dnsTokenizer) unget(token dnsToken) {
	if t.ungottenToken != nil {
		failDNS()
	}
	t.ungottenToken = &token
}

// getInt is Tokenizer.get_int() (base 10) as a big integer.
func (t *dnsTokenizer) getInt() *big.Int {
	token := t.get(false).unescape()
	if token.ttype != tokIdentifier || !pystr.IsDigit(token.value) {
		failDNS()
	}
	value, err := pystr.Int(token.value, 10)
	if err != nil {
		failDNS()
	}
	return value
}

func (t *dnsTokenizer) getUint(maximum int64) {
	if value := t.getInt(); value.Sign() < 0 || value.Cmp(big.NewInt(maximum)) > 0 {
		failDNS()
	}
}

// getString is Tokenizer.get_string(max_length).
func (t *dnsTokenizer) getString(maxLength int) string {
	token := t.get(false).unescape()
	if token.ttype != tokIdentifier && token.ttype != tokQuoted {
		failDNS()
	}
	if maxLength > 0 && runeLen(token.value) > maxLength {
		failDNS()
	}
	return token.value
}

func (t *dnsTokenizer) getIdentifier() string {
	token := t.get(false).unescape()
	if token.ttype != tokIdentifier {
		failDNS()
	}
	return token.value
}

// getName is Tokenizer.get_name(origin, relativize=False).
func (t *dnsTokenizer) getName(origin *dnsName) {
	token := t.get(false)
	if token.ttype != tokIdentifier {
		failDNS()
	}
	dnsNameFromText(token.value, origin)
}

func (t *dnsTokenizer) getTTL() {
	token := t.get(false).unescape()
	if token.ttype != tokIdentifier {
		failDNS()
	}
	dnsTTLFromText(token.value)
}

func (t *dnsTokenizer) getEOL() {
	if !t.get(false).isEOLOrEOF() {
		failDNS()
	}
}

// getRemaining is Tokenizer.get_remaining().
func (t *dnsTokenizer) getRemaining() []dnsToken {
	var tokens []dnsToken
	for {
		token := t.get(false)
		if token.isEOLOrEOF() {
			t.unget(token)
			return tokens
		}
		tokens = append(tokens, token)
	}
}

// dnsName is dns.name.Name (labels in wire form).
type dnsName struct{ labels [][]byte }

func (n *dnsName) isAbsolute() bool {
	return len(n.labels) > 0 && len(n.labels[len(n.labels)-1]) == 0
}

// newDNSName is Name(labels): _validate_labels.
func newDNSName(labels [][]byte) *dnsName {
	total, first := 0, -1
	for j, label := range labels {
		total += len(label) + 1
		if len(label) > 63 {
			failDNS()
		}
		if first < 0 && len(label) == 0 {
			first = j
		}
	}
	if total > 255 {
		failDNS()
	}
	if first >= 0 && first != len(labels)-1 {
		failDNS()
	}
	return &dnsName{labels: labels}
}

func isDNSDot(c rune) bool { return c == '.' || c == 0x3002 || c == 0xff0e || c == 0xff61 }

// dnsNameFromText is dns.name.from_text(text, origin) (origin nil = None).
func dnsNameFromText(text string, origin *dnsName) *dnsName {
	ascii := true
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if !ascii {
		return dnsNameFromUnicode(text, origin)
	}
	var labels [][]byte
	var label []byte
	escaping, edigits, total := false, 0, 0
	if text == "@" {
		text = ""
	}
	if text != "" {
		if text == "." {
			return newDNSName([][]byte{{}})
		}
		for i := 0; i < len(text); i++ {
			c := text[i]
			if escaping {
				if edigits == 0 {
					if c >= '0' && c <= '9' {
						total = int(c - '0')
						edigits++
					} else {
						label = append(label, c)
						escaping = false
					}
				} else {
					if !(c >= '0' && c <= '9') {
						failDNS()
					}
					total = total*10 + int(c-'0')
					edigits++
					if edigits == 3 {
						escaping = false
						if total > 255 {
							raise(pyErr("error", "ubyte format requires 0 <= number <= 255"))
						}
						label = append(label, byte(total))
					}
				}
			} else if c == '.' {
				if len(label) == 0 {
					failDNS()
				}
				labels = append(labels, label)
				label = nil
			} else if c == '\\' {
				escaping, edigits, total = true, 0, 0
			} else {
				label = append(label, c)
			}
		}
		if escaping {
			failDNS()
		}
		if len(label) > 0 {
			labels = append(labels, label)
		} else {
			labels = append(labels, []byte{})
		}
	}
	return withOrigin(labels, origin)
}

func withOrigin(labels [][]byte, origin *dnsName) *dnsName {
	if (len(labels) == 0 || len(labels[len(labels)-1]) != 0) && origin != nil {
		labels = append(labels, origin.labels...)
	}
	return newDNSName(labels)
}

// dnsNameFromUnicode is dns.name.from_unicode(text, origin) with the IDNA
// 2003 codec.
func dnsNameFromUnicode(text string, origin *dnsName) *dnsName {
	var labels [][]byte
	var label []rune
	escaping, edigits, total := false, 0, 0
	encode := func(label []rune) []byte {
		if len(label) == 0 {
			return []byte{}
		}
		encoded, ok := idnaToASCII(label)
		if !ok {
			failDNS()
		}
		return encoded
	}
	if text == "@" {
		text = ""
	}
	if text != "" {
		if r := []rune(text); len(r) == 1 && isDNSDot(r[0]) {
			return newDNSName([][]byte{{}})
		}
		for _, c := range text {
			if escaping {
				if edigits == 0 {
					if pyIsDigitRune(c) {
						total = digitInt(c)
						edigits++
					} else {
						label = append(label, c)
						escaping = false
					}
				} else {
					if !pyIsDigitRune(c) {
						failDNS()
					}
					total = total*10 + digitInt(c)
					edigits++
					if edigits == 3 {
						escaping = false
						label = append(label, rune(total))
					}
				}
			} else if isDNSDot(c) {
				if len(label) == 0 {
					failDNS()
				}
				labels = append(labels, encode(label))
				label = nil
			} else if c == '\\' {
				escaping, edigits, total = true, 0, 0
			} else {
				label = append(label, c)
			}
		}
		if escaping {
			failDNS()
		}
		if len(label) > 0 {
			labels = append(labels, encode(label))
		} else {
			labels = append(labels, []byte{})
		}
	}
	return withOrigin(labels, origin)
}

var maxTTL = big.NewInt(4294967295)

// dnsTTLFromText is dns.ttl.from_text(text) (validity only).
func dnsTTLFromText(text string) {
	total := new(big.Int)
	if pystr.IsDigit(text) {
		value, err := pystr.Int(text, 10)
		if err != nil {
			failDNS()
		}
		total = value
	} else if text == "" {
		failDNS()
	} else {
		current := new(big.Int)
		needDigit := true
		ten := big.NewInt(10)
		for _, c := range text {
			if pyIsDigitRune(c) {
				current.Mul(current, ten)
				current.Add(current, big.NewInt(int64(digitInt(c))))
				needDigit = false
				continue
			}
			if needDigit {
				failDNS()
			}
			var unit int64
			switch pystr.Lower(string(c)) {
			case "w":
				unit = 604800
			case "d":
				unit = 86400
			case "h":
				unit = 3600
			case "m":
				unit = 60
			case "s":
				unit = 1
			default:
				failDNS()
			}
			total.Add(total, new(big.Int).Mul(current, big.NewInt(unit)))
			current = new(big.Int)
			needDigit = true
		}
		if current.Sign() != 0 {
			failDNS()
		}
	}
	if total.Sign() < 0 || total.Cmp(maxTTL) > 0 {
		failDNS()
	}
}

// ipv4ATON is dns.ipv4.inet_aton(text) (validity only).
func ipv4ATON(text []byte) []byte {
	parts := strings.Split(string(text), ".")
	if len(parts) != 4 {
		failDNS()
	}
	out := make([]byte, 4)
	for i, part := range parts {
		if part == "" {
			failDNS()
		}
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				failDNS()
			}
		}
		if len(part) > 1 && part[0] == '0' {
			failDNS()
		}
		if len(part) > 3 {
			failDNS()
		}
		value := 0
		for j := 0; j < len(part); j++ {
			value = value*10 + int(part[j]-'0')
		}
		if value > 255 {
			failDNS()
		}
		out[i] = byte(value)
	}
	return out
}

var (
	v4Ending       = regexp.MustCompile(`^([^\n]*):([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)\n?\z`)
	colonColonEnd  = regexp.MustCompile(`^[^\n]*::\n?\z`)
	hexDigitsBytes = regexp.MustCompile(`^[0-9A-Fa-f]*$`)
)

// ipv6ATON is dns.ipv6.inet_aton(text) (validity only).
func ipv6ATON(btext []byte) {
	text := string(btext)
	switch {
	case text == "":
		failDNS()
	case strings.HasSuffix(text, ":") && !strings.HasSuffix(text, "::"):
		failDNS()
	case strings.HasPrefix(text, ":") && !strings.HasPrefix(text, "::"):
		failDNS()
	case text == "::":
		text = "0::"
	}
	if m := v4Ending.FindStringSubmatch(text); m != nil {
		b := ipv4ATON([]byte(m[2]))
		text = m[1] + ":" + hex.EncodeToString(b[:2]) + ":" + hex.EncodeToString(b[2:])
	}
	if strings.HasPrefix(text, "::") {
		text = text[1:]
	} else if colonColonEnd.MatchString(text) {
		text = text[:len(text)-1]
	}
	chunks := strings.Split(text, ":")
	l := len(chunks)
	if l > 8 {
		failDNS()
	}
	seenEmpty := false
	var canonical strings.Builder
	for _, c := range chunks {
		if c == "" {
			if seenEmpty {
				failDNS()
			}
			seenEmpty = true
			for i := 0; i < 8-l+1; i++ {
				canonical.WriteString("0000")
			}
			continue
		}
		if len(c) > 4 {
			failDNS()
		}
		canonical.WriteString(strings.Repeat("0", 4-len(c)) + c)
	}
	if l < 8 && !seenEmpty {
		failDNS()
	}
	joined := canonical.String()
	if len(joined)%2 != 0 || !hexDigitsBytes.MatchString(joined) {
		failDNS()
	}
}

// rdataFromText is dns.rdata.from_text(IN, rdtype, text, origin=origin,
// relativize=False) for the types _zone_records_error passes (validity
// only). Every exception inside is dnspython's SyntaxError
// (ExceptionWrapper).
func rdataFromText(rdtype, text string, origin *dnsName) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(raised); ok {
				failDNS()
			}
			panic(r)
		}
	}()
	tok := newDNSTokenizer(text)
	if rdtype == "MINFO" {
		// No dnspython class: GenericRdata, whose text form starts with \#.
		token := tok.get(false)
		if token.ttype != tokIdentifier || token.value != `\#` {
			failDNS()
		}
	}
	first := tok.get(false)
	tok.unget(first)
	if first.ttype == tokIdentifier && first.value == `\#` {
		// Unreachable: the caller handles RFC 3597 data itself.
		failDNS()
	}
	switch rdtype {
	case "A":
		ipv4ATON([]byte(tok.getIdentifier()))
	case "AAAA":
		ipv6ATON([]byte(tok.getIdentifier()))
	case "CNAME", "NS", "PTR", "DNAME":
		tok.getName(origin)
	case "MX", "RT", "AFSDB":
		tok.getUint(65535)
		tok.getName(origin)
	case "RP":
		tok.getName(origin)
		tok.getName(origin)
	case "HINFO":
		for i := 0; i < 2; i++ {
			if len(tok.getString(255)) > 255 {
				failDNS()
			}
		}
	case "TXT", "SPF":
		strings := 0
		for _, token := range tok.getRemaining() {
			if len(token.unescapeToBytes()) > 255 {
				failDNS()
			}
			strings++
		}
		if strings == 0 {
			failDNS()
		}
	case "SOA":
		tok.getName(origin)
		tok.getName(origin)
		tok.getUint(4294967295)
		for i := 0; i < 4; i++ {
			tok.getTTL()
		}
	case "SRV":
		tok.getUint(65535)
		tok.getUint(65535)
		tok.getUint(65535)
		tok.getName(origin)
	case "NAPTR":
		tok.getUint(65535)
		tok.getUint(65535)
		for i := 0; i < 3; i++ {
			if len(tok.getString(0)) > 255 {
				failDNS()
			}
		}
		tok.getName(origin)
	case "CAA":
		tok.getUint(255)
		tag := tok.getString(0)
		tok.getString(0)
		if len(tag) > 255 || tag == "" {
			failDNS()
		}
		for i := 0; i < len(tag); i++ {
			c := tag[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				failDNS()
			}
		}
	default:
		failDNS()
	}
	tok.getEOL()
}
