package oxidns

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// Zone text checks of the arbitrary/response plugins: the subset of
// dnspython 2.7 (zone tokenizer, names, TTLs, text RDATA of the types OxiDNS
// reads in text form) that oxidns_plugin_constraints used. Parsing failures
// unwind with panic(zoneFail{}) and become one generic message.

type zoneFail struct{}

func zfail() { panic(zoneFail{}) }

const (
	tkEOF = iota
	tkEOL
	tkSpace
	tkIdent
	tkQuoted
	tkDelim
)

type ztoken struct {
	kind   int
	value  string
	escape bool
}

func (t ztoken) end() bool { return t.kind == tkEOL || t.kind == tkEOF }

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

// unescape is Token.unescape (\DDD and \X); bytes gives the escaped bytes.
func (t ztoken) unescape() []byte {
	if !t.escape {
		return []byte(t.value)
	}
	r := []rune(t.value)
	var out []byte
	for i := 0; i < len(r); i++ {
		c := r[i]
		if c == '\\' {
			if i++; i >= len(r) {
				zfail()
			}
			c = r[i]
			if isDigit(c) {
				if i+2 >= len(r) || !isDigit(r[i+1]) || !isDigit(r[i+2]) {
					zfail()
				}
				code := int(c-'0')*100 + int(r[i+1]-'0')*10 + int(r[i+2]-'0')
				if code > 255 {
					zfail()
				}
				out = append(out, byte(code))
				i += 2
				continue
			}
		}
		out = append(out, string(c)...)
	}
	return out
}

type tokenizer struct {
	in        []rune
	pos       int
	back      []rune
	token     *ztoken
	multiline int
	quoting   bool
}

func (t *tokenizer) char() rune {
	if n := len(t.back); n > 0 {
		c := t.back[n-1]
		t.back = t.back[:n-1]
		return c
	}
	if t.pos >= len(t.in) {
		t.pos = len(t.in) + 1
		return -1
	}
	t.pos++
	return t.in[t.pos-1]
}

func (t *tokenizer) skip() int {
	n := 0
	for {
		c := t.char()
		if c != ' ' && c != '\t' && (c != '\n' || t.multiline == 0) {
			t.back = append(t.back, c)
			return n
		}
		n++
	}
}

func (t *tokenizer) delimiter(c rune) bool {
	if t.quoting {
		return c == '"'
	}
	return strings.ContainsRune(" \t\n;()\"", c)
}

// get is Tokenizer.get(want_leading).
func (t *tokenizer) get(leading bool) ztoken {
	if u := t.token; u != nil {
		t.token = nil
		if u.kind != tkSpace || leading {
			return *u
		}
	}
	if t.skip() > 0 && leading {
		return ztoken{kind: tkSpace, value: " "}
	}
	var b strings.Builder
	kind, escape := tkIdent, false
	for {
		c := t.char()
		if c == -1 || t.delimiter(c) {
			if c == -1 && t.quoting {
				zfail()
			}
			if b.Len() == 0 && kind != tkQuoted {
				switch c {
				case '(':
					t.multiline++
					t.skip()
					continue
				case ')':
					if t.multiline <= 0 {
						zfail()
					}
					t.multiline--
					t.skip()
					continue
				case '"':
					t.quoting = !t.quoting
					if t.quoting {
						kind = tkQuoted
					} else {
						t.skip()
					}
					continue
				case '\n':
					return ztoken{kind: tkEOL, value: "\n"}
				case ';':
					for c = t.char(); c != '\n' && c != -1; c = t.char() {
					}
					if c == -1 {
						if t.multiline > 0 {
							zfail()
						}
						return ztoken{kind: tkEOF}
					}
					if t.multiline > 0 {
						t.skip()
						continue
					}
					return ztoken{kind: tkEOL, value: "\n"}
				case -1:
				default:
					b.WriteRune(c)
					kind = tkDelim
				}
			} else {
				t.back = append(t.back, c)
			}
			break
		} else if t.quoting && c == '\n' {
			zfail()
		} else if c == '\\' {
			b.WriteRune(c)
			escape = true
			if c = t.char(); c == -1 || c == '\n' && !t.quoting {
				zfail()
			}
		}
		b.WriteRune(c)
	}
	if b.Len() == 0 && kind != tkQuoted {
		if t.multiline > 0 {
			zfail()
		}
		kind = tkEOF
	}
	return ztoken{kind: kind, value: b.String(), escape: escape}
}

func (t *tokenizer) unget(tok ztoken) {
	if t.token != nil {
		zfail()
	}
	t.token = &tok
}

func (t *tokenizer) uint(maximum int64) {
	tok := t.get(false)
	text := string(tok.unescape())
	if _, ok := boundedUint(text, big.NewInt(maximum), false); tok.kind != tkIdent || !ok {
		zfail()
	}
}

func (t *tokenizer) str() string {
	tok := t.get(false)
	if tok.kind != tkIdent && tok.kind != tkQuoted {
		zfail()
	}
	return string(tok.unescape())
}

func (t *tokenizer) name(origin *dnsName) {
	tok := t.get(false)
	if tok.kind != tkIdent {
		zfail()
	}
	nameFromText(tok.value, origin)
}

// dnsName is a name in wire labels; absolute when it ends with the root.
type dnsName struct{ labels [][]byte }

func (n *dnsName) absolute() bool { return len(n.labels) > 0 && len(n.labels[len(n.labels)-1]) == 0 }

func dnsNewName(labels [][]byte, origin *dnsName) *dnsName {
	if (len(labels) == 0 || len(labels[len(labels)-1]) != 0) && origin != nil {
		labels = append(labels, origin.labels...)
	}
	total, empty := 0, -1
	for i, l := range labels {
		total += len(l) + 1
		if len(l) > 63 {
			zfail()
		}
		if empty < 0 && len(l) == 0 {
			empty = i
		}
	}
	if total > 255 || empty >= 0 && empty != len(labels)-1 {
		zfail()
	}
	return &dnsName{labels}
}

var idna2003 = idna.New(idna.MapForLookup(), idna.Transitional(true), idna.StrictDomainName(false))

// nameFromText is dns.name.from_text: escapes, "@", IDNA for non-ASCII labels.
func nameFromText(text string, origin *dnsName) *dnsName {
	if text == "@" {
		text = ""
	}
	if text == "" {
		return dnsNewName(nil, origin)
	}
	isDot := func(c rune) bool { return c == '.' || c == 0x3002 || c == 0xff0e || c == 0xff61 }
	if r := []rune(text); len(r) == 1 && isDot(r[0]) {
		return dnsNewName([][]byte{{}}, nil)
	}
	ascii := strings.IndexFunc(text, func(r rune) bool { return r >= 0x80 }) < 0
	var labels [][]byte
	var label []rune
	flush := func() {
		if ascii {
			b := make([]byte, len(label))
			for i, r := range label {
				b[i] = byte(r)
			}
			labels = append(labels, b)
		} else if len(label) == 0 {
			labels = append(labels, []byte{})
		} else {
			encoded, err := idna2003.ToASCII(string(label))
			if err != nil {
				zfail()
			}
			labels = append(labels, []byte(encoded))
		}
		label = nil
	}
	r := []rune(text)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\':
			if i+1 >= len(r) {
				zfail()
			}
			if !isDigit(r[i+1]) {
				label = append(label, r[i+1])
				i++
				continue
			}
			if i+3 >= len(r) || !isDigit(r[i+2]) || !isDigit(r[i+3]) {
				zfail()
			}
			code := int(r[i+1]-'0')*100 + int(r[i+2]-'0')*10 + int(r[i+3]-'0')
			if ascii && code > 255 {
				zfail()
			}
			label = append(label, rune(code))
			i += 3
		case (ascii && c == '.') || (!ascii && isDot(c)):
			if len(label) == 0 {
				zfail()
			}
			flush()
		default:
			label = append(label, c)
		}
	}
	if len(label) > 0 {
		flush()
	} else {
		labels = append(labels, []byte{})
	}
	return dnsNewName(labels, origin)
}

// ttlFromText is dns.ttl.from_text: seconds or 1w2d3h4m5s, at most 2^32-1.
func ttlFromText(text string) *big.Int {
	total := new(big.Int)
	if text == "" {
		zfail()
	}
	if strings.Trim(text, "0123456789") == "" {
		total.SetString(text, 10)
	} else {
		current, digit := new(big.Int), false
		for _, c := range text {
			if isDigit(c) {
				current.Mul(current, big.NewInt(10)).Add(current, big.NewInt(int64(c-'0')))
				digit = true
				continue
			}
			unit := map[rune]int64{'w': 604800, 'd': 86400, 'h': 3600, 'm': 60, 's': 1}[c|0x20]
			if !digit || unit == 0 {
				zfail()
			}
			total.Add(total, current.Mul(current, big.NewInt(unit)))
			current, digit = new(big.Int), false
		}
		if current.Sign() != 0 {
			zfail()
		}
	}
	if total.Cmp(big.NewInt(4294967295)) > 0 {
		zfail()
	}
	return total
}

func ipv4Text(text string) []byte {
	parts := strings.Split(text, ".")
	if len(parts) != 4 {
		zfail()
	}
	out := make([]byte, 4)
	for i, part := range parts {
		v, err := strconv.Atoi(part)
		if part == "" || strings.Trim(part, "0123456789") != "" || len(part) > 3 || len(part) > 1 && part[0] == '0' || err != nil || v > 255 {
			zfail()
		}
		out[i] = byte(v)
	}
	return out
}

var (
	v6Tail = regexp.MustCompile(`^([^\n]*):([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)\n?\z`)
	v6Hex  = regexp.MustCompile(`^[0-9A-Fa-f]*$`)
)

// ipv6Text is dns.ipv6.inet_aton (validity).
func ipv6Text(text string) {
	switch {
	case text == "",
		strings.HasSuffix(text, ":") && !strings.HasSuffix(text, "::"),
		strings.HasPrefix(text, ":") && !strings.HasPrefix(text, "::"):
		zfail()
	case text == "::":
		text = "0::"
	}
	if m := v6Tail.FindStringSubmatch(text); m != nil {
		b := ipv4Text(m[2])
		text = m[1] + ":" + strconv.FormatUint(uint64(b[0])<<8|uint64(b[1]), 16) + ":" + strconv.FormatUint(uint64(b[2])<<8|uint64(b[3]), 16)
	}
	if strings.HasPrefix(text, "::") {
		text = text[1:]
	} else if strings.HasSuffix(text, "::") {
		text = text[:len(text)-1]
	}
	chunks := strings.Split(text, ":")
	if len(chunks) > 8 {
		zfail()
	}
	empty := false
	for _, c := range chunks {
		if c == "" {
			if empty {
				zfail()
			}
			empty = true
		} else if len(c) > 4 || !v6Hex.MatchString(c) {
			zfail()
		}
	}
	if len(chunks) < 8 && !empty {
		zfail()
	}
}

// rdataFromText is dns.rdata.from_text(IN, type, text, origin) (validity).
func rdataFromText(rdtype, text string, origin *dnsName) {
	t := &tokenizer{in: []rune(text)}
	first := t.get(false)
	if rdtype == "MINFO" || first.kind == tkIdent && first.value == `\#` {
		zfail() // GenericRdata text form; the caller handles RFC 3597 itself
	}
	t.unget(first)
	ident := func() string {
		tok := t.get(false)
		if tok.kind != tkIdent {
			zfail()
		}
		return string(tok.unescape())
	}
	switch rdtype {
	case "A":
		ipv4Text(ident())
	case "AAAA":
		ipv6Text(ident())
	case "CNAME", "NS", "PTR", "DNAME":
		t.name(origin)
	case "MX", "RT", "AFSDB":
		t.uint(65535)
		t.name(origin)
	case "RP":
		t.name(origin)
		t.name(origin)
	case "HINFO":
		for i := 0; i < 2; i++ {
			if len(t.str()) > 255 {
				zfail()
			}
		}
	case "TXT", "SPF":
		count := 0
		for tok := t.get(false); !tok.end(); tok = t.get(false) {
			if len(tok.unescape()) > 255 {
				zfail()
			}
			count++
		}
		if count == 0 {
			zfail()
		}
		return
	case "SOA":
		t.name(origin)
		t.name(origin)
		t.uint(4294967295)
		for i := 0; i < 4; i++ {
			tok := t.get(false)
			if tok.kind != tkIdent {
				zfail()
			}
			ttlFromText(string(tok.unescape()))
		}
	case "SRV":
		t.uint(65535)
		t.uint(65535)
		t.uint(65535)
		t.name(origin)
	case "NAPTR":
		t.uint(65535)
		t.uint(65535)
		for i := 0; i < 3; i++ {
			if len(t.str()) > 255 {
				zfail()
			}
		}
		t.name(origin)
	case "CAA":
		t.uint(255)
		tag := t.str()
		t.str()
		if tag == "" || len(tag) > 255 || strings.Trim(tag, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			zfail()
		}
	default:
		zfail()
	}
	if !t.get(false).end() {
		zfail()
	}
}

var (
	allowedText   = strings.Fields("A AAAA CNAME NS PTR DNAME ANAME MD MF MB MG MR NSAP-PTR NSAPPTR MX RT AFSDB RP MINFO HINFO TXT SPF AVC RESINFO SOA SRV NAPTR CAA")
	namePositions = func() map[string][]int {
		m := map[string][]int{"MX": {1}, "RT": {1}, "AFSDB": {1}, "RP": {0, 1}, "MINFO": {0, 1}, "SOA": {0, 1}, "SRV": {3}, "NAPTR": {5}}
		for _, t := range strings.Fields("CNAME NS PTR DNAME ANAME MD MF MB MG MR NSAP-PTR NSAPPTR") {
			m[t] = []int{0}
		}
		return m
	}()
	typeNumber  = regexp.MustCompile(`^TYPE[0-9]+$`)
	generateRE  = regexp.MustCompile(`^([0-9]+)-([0-9]+)(?:/([0-9]+))?$`)
	rawTokenRE  = regexp.MustCompile(`"(?:\\.|[^"\\])*"|\S+`)
	maxInt63    = big.NewInt(9223372036854775807)
	zoneMessage = "DNS 记录的名称、TTL、类型或 RDATA 格式无效"
)

// zoneRecordsError is _zone_records_error: "" when the zone text is valid.
// single is one response record ({qname}/{qclass} placeholders allowed).
func zoneRecordsError(raw *Node, single bool) (message string) {
	if !isStr(raw) || strings.Contains(raw.Value, "${") {
		return ""
	}
	text := raw.Value
	if single && (strings.Count(text, "{qname}") > 1 || strings.Count(text, "{qclass}") > 1) {
		return "每条响应记录中的 {qname} 和 {qclass} 各只能出现一次"
	}
	source := text
	if single {
		source = strings.NewReplacer("{qname}", "configflow-placeholder.invalid.", "{qclass}", "CH").Replace(text)
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(zoneFail); !ok {
				panic(r)
			}
			message = zoneMessage
		}
	}()
	t := &tokenizer{in: []rune(source)}
	var origin, owner *dnsName
	class, ttl := "IN", big.NewInt(3600)
	records := new(big.Int)
	include := false
	for {
		first := t.get(true)
		leading := first.kind == tkSpace
		if leading {
			first = t.get(false)
		}
		if first.kind == tkEOF {
			break
		}
		if first.kind == tkEOL {
			continue
		}
		tokens := []ztoken{first}
		for tok := t.get(false); !tok.end(); tok = t.get(false) {
			tokens = append(tokens, tok)
		}
		values := make([]string, len(tokens))
		for i, tok := range tokens {
			values[i] = tok.value
		}
		if strings.HasPrefix(values[0], "$") {
			switch directive := strings.ToUpper(values[0]); {
			case directive == "$ORIGIN" && len(values) == 2:
				origin = nameFromText(values[1], origin)
				if !origin.absolute() {
					return "$ORIGIN 必须能解析为绝对域名"
				}
				owner = nil
			case directive == "$TTL" && len(values) == 2:
				ttl = ttlFromText(values[1])
			case directive == "$INCLUDE":
				if len(values) != 2 && len(values) != 3 {
					return "$INCLUDE 需要文件路径和可选 origin"
				}
				if !strings.HasPrefix(values[1], "/") {
					return "内联 zone 中的 $INCLUDE 必须使用绝对路径"
				}
				include = true
			case directive == "$GENERATE":
				if len(values) < 5 {
					return "$GENERATE 需要范围、名称、类型和 RDATA"
				}
				m := generateRE.FindStringSubmatch(values[1])
				if m == nil {
					return "$GENERATE 范围或步长无效"
				}
				if m[3] == "" {
					m[3] = "1"
				}
				var n [3]*big.Int
				for i := range n {
					v, ok := boundedUint(m[i+1], maxInt63, false)
					if !ok {
						return "$GENERATE 范围或步长无效"
					}
					n[i] = v
				}
				if n[0].Cmp(n[1]) > 0 || n[2].Sign() == 0 {
					return "$GENERATE 范围或步长无效"
				}
				span := new(big.Int).Sub(n[1], n[0])
				records.Add(records, span.Div(span, n[2]).Add(span, big.NewInt(1)))
				if single && records.Cmp(big.NewInt(1)) > 0 {
					return "响应记录条目不能包含多条生成记录"
				}
			default:
				return "不支持的 zone 指令或指令参数不完整"
			}
			continue
		}
		offset := 0
		if !leading {
			if tokens[0].kind == tkQuoted {
				return "DNS 记录的域名不能放在引号内"
			}
			owner = nameFromText(values[0], origin)
			offset = 1
		}
		if owner == nil {
			return "省略记录名称前必须先定义名称"
		}
		if !owner.absolute() {
			return "相对域名需要 $ORIGIN；否则名称必须以 . 结尾"
		}
		rtype, klass, recordTTL, typeIndex := "", class, ttl, 0
		for ; offset < len(values); offset++ {
			candidate := strings.ToUpper(values[offset])
			digits := strings.Trim(candidate, "0123456789") == "" && candidate != ""
			if typeNumber.MatchString(candidate) && uintMax(candidate[4:], 65535) || !digits && DNSCodeValid("qtype", S(candidate)) {
				rtype, typeIndex = candidate, offset
				offset++
				break
			}
			if DNSCodeValid("qclass", S(candidate)) && !digits {
				klass = candidate
			} else {
				recordTTL = ttlFromText(values[offset])
			}
		}
		if rtype == "" {
			return "DNS 记录缺少类型和 RDATA"
		}
		if single {
			original := rawTokenRE.FindAllString(text, -1)
			if strings.Contains(text, "{qname}") && (len(original) == 0 || original[0] != "{qname}") {
				return "{qname} 只能作为未加引号的记录名称"
			}
			if strings.Contains(text, "{qclass}") {
				end := typeIndex
				if end > len(original) {
					end = len(original)
				}
				found := false
				for i := 1; i < end; i++ {
					found = found || original[i] == "{qclass}"
				}
				if !found {
					return "{qclass} 只能作为类型前的未加引号的记录类别"
				}
			}
		}
		data, dataTokens := values[offset:], tokens[offset:]
		if len(data) == 0 {
			return "DNS 记录缺少 RDATA"
		}
		if data[0] == `\#` || data[0] == "#" {
			if len(data) < 2 || strings.Trim(data[1], "0123456789") != "" || data[1] == "" {
				return "RFC3597 记录需要长度和十六进制数据"
			}
			hexdata := strings.Join(data[2:], "")
			size, ok := boundedUint(data[1], big.NewInt(65535), false)
			if !ok || int64(len(hexdata)) != size.Int64()*2 || !v6Hex.MatchString(hexdata) {
				return "RFC3597 数据长度或十六进制格式无效"
			}
		} else {
			if !containsString(allowedText, rtype) {
				return `该记录类型在 OxiDNS 中需要 RFC3597 格式：\# 长度 十六进制数据`
			}
			for _, position := range namePositions[rtype] {
				if position >= len(data) {
					return "DNS 记录的 RDATA 参数不足"
				}
				if !nameFromText(data[position], origin).absolute() {
					return "RDATA 中的相对域名需要 $ORIGIN；否则必须以 . 结尾"
				}
			}
			parsed := rtype
			switch rtype {
			case "ANAME", "MD", "MF", "MB", "MG", "MR", "NSAPPTR", "NSAP-PTR":
				parsed = "CNAME"
			case "AVC", "RESINFO":
				parsed = "TXT"
			}
			encoded := make([]string, len(dataTokens))
			for i, tok := range dataTokens {
				encoded[i] = tok.value
				if tok.kind == tkQuoted {
					encoded[i] = `"` + tok.value + `"`
				}
			}
			rdataFromText(parsed, strings.Join(encoded, " "), origin)
		}
		class, ttl = klass, recordTTL
		records.Add(records, big.NewInt(1))
	}
	if single && (records.Cmp(big.NewInt(1)) > 0 || records.Cmp(big.NewInt(1)) != 0 && !include) {
		return "每个响应记录条目必须恰好包含一条 DNS 记录"
	}
	return ""
}
