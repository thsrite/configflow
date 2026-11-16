package oxidns

import (
	"math/big"
	"strings"

	"configflow/internal/pystr"
)

// zoneRecordsError is oxidns_plugin_constraints._zone_records_error(raw,
// single): zone text parsed without opening $INCLUDE files ("" = None).
func zoneRecordsError(value any, single bool) (message string) {
	raw, ok := value.(string)
	if !ok || deferredText(raw) {
		return ""
	}
	if single && (strings.Count(raw, "{qname}") > 1 || strings.Count(raw, "{qclass}") > 1) {
		return "每条响应记录中的 {qname} 和 {qclass} 各只能出现一次"
	}
	source := raw
	if single {
		source = strings.ReplaceAll(strings.ReplaceAll(raw, "{qname}", "configflow-placeholder.invalid."), "{qclass}", "CH")
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(dnsFail); ok {
				message = "DNS 记录的名称、TTL、类型或 RDATA 格式无效"
				return
			}
			panic(r)
		}
	}()
	return zoneRecords(raw, source, single)
}

var (
	zoneAllowedText   = map[string]bool{}
	zoneNamePositions = map[string][]int{
		"MX": {1}, "RT": {1}, "AFSDB": {1}, "RP": {0, 1}, "MINFO": {0, 1}, "SOA": {0, 1}, "SRV": {3}, "NAPTR": {5},
	}
	maxInt64 = big.NewInt(9223372036854775807)
)

func init() {
	for _, name := range strings.Fields("A AAAA CNAME NS PTR DNAME ANAME MD MF MB MG MR NSAP-PTR NSAPPTR MX RT AFSDB RP MINFO HINFO TXT SPF AVC RESINFO SOA SRV NAPTR CAA") {
		zoneAllowedText[name] = true
	}
	for _, name := range strings.Fields("CNAME NS PTR DNAME ANAME MD MF MB MG MR NSAP-PTR NSAPPTR") {
		zoneNamePositions[name] = []int{0}
	}
}

// generateRange is re.fullmatch(r'(\d+)-(\d+)(?:/(\d+))?', value).
func generateRange(value string) (start, stop, step string, ok bool) {
	first, rest, found := strings.Cut(value, "-")
	if !found || !decimalDigits(first) {
		return "", "", "", false
	}
	second, third, hasStep := strings.Cut(rest, "/")
	if !decimalDigits(second) || (hasStep && !decimalDigits(third)) {
		return "", "", "", false
	}
	if !hasStep {
		third = ""
	}
	return first, second, third, true
}

// quotedOrWord is re.findall(r'"(?:\\.|[^"\\])*"|\S+', raw).
func quotedOrWord(raw string) []string {
	runes := []rune(raw)
	var out []string
	i := 0
	for i < len(runes) {
		if runes[i] == '"' {
			j := i + 1
			for j < len(runes) {
				if runes[j] == '\\' {
					if j+1 < len(runes) && runes[j+1] != '\n' {
						j += 2
						continue
					}
					break
				}
				if runes[j] == '"' {
					break
				}
				j++
			}
			if j < len(runes) && runes[j] == '"' {
				out = append(out, string(runes[i:j+1]))
				i = j + 1
				continue
			}
		}
		if pystr.IsSpace(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && !pystr.IsSpace(runes[j]) {
			j++
		}
		out = append(out, string(runes[i:j]))
		i = j
	}
	return out
}

func zoneRecords(raw, source string, single bool) string {
	tokenizer := newDNSTokenizer(source)
	var origin, currentOwner *dnsName
	recordCount := new(big.Int)
	unresolvedInclude := false
	for {
		first := tokenizer.get(true)
		leading := first.ttype == tokWhitespace
		if leading {
			first = tokenizer.get(false)
		}
		if first.ttype == tokEOF {
			break
		}
		if first.ttype == tokEOL {
			continue
		}
		tokens := []dnsToken{first}
		for {
			token := tokenizer.get(false)
			if token.isEOLOrEOF() {
				break
			}
			tokens = append(tokens, token)
		}
		values := make([]string, len(tokens))
		for i, token := range tokens {
			values[i] = token.value
		}
		if strings.HasPrefix(values[0], "$") {
			directive := pystr.Upper(values[0])
			if directive == "$ORIGIN" && len(values) == 2 {
				origin = dnsNameFromText(values[1], origin)
				if !origin.isAbsolute() {
					return "$ORIGIN 必须能解析为绝对域名"
				}
				currentOwner = nil
				continue
			}
			if directive == "$TTL" && len(values) == 2 {
				dnsTTLFromText(values[1])
				continue
			}
			if directive == "$INCLUDE" {
				if len(values) != 2 && len(values) != 3 {
					return "$INCLUDE 需要文件路径和可选 origin"
				}
				if !strings.HasPrefix(values[1], "/") {
					return "内联 zone 中的 $INCLUDE 必须使用绝对路径"
				}
				unresolvedInclude = true
				continue
			}
			if directive == "$GENERATE" {
				if len(values) < 5 {
					return "$GENERATE 需要范围、名称、类型和 RDATA"
				}
				a, b, c, ok := generateRange(values[1])
				if c == "" {
					c = "1"
				}
				if !ok || constraintUint(a, maxInt64) == nil || constraintUint(b, maxInt64) == nil || constraintUint(c, maxInt64) == nil ||
					constraintUint(a, bigU64).Cmp(constraintUint(b, bigU64)) > 0 || constraintUint(c, bigU64).Sign() == 0 {
					return "$GENERATE 范围或步长无效"
				}
				span := new(big.Int).Sub(constraintUint(b, bigU64), constraintUint(a, bigU64))
				span.Div(span, constraintUint(c, bigU64))
				recordCount.Add(recordCount, span.Add(span, big.NewInt(1)))
				if single && recordCount.Cmp(big.NewInt(1)) > 0 {
					return "响应记录条目不能包含多条生成记录"
				}
				continue
			}
			return "不支持的 zone 指令或指令参数不完整"
		}
		offset := 0
		if !leading {
			if tokens[0].ttype == tokQuoted {
				return "DNS 记录的域名不能放在引号内"
			}
			currentOwner = dnsNameFromText(values[0], origin)
			offset = 1
		}
		if currentOwner == nil {
			return "省略记录名称前必须先定义名称"
		}
		if !currentOwner.isAbsolute() {
			return "相对域名需要 $ORIGIN；否则名称必须以 . 结尾"
		}
		rtype := ""
		typeIndex := -1
		for offset < len(values) {
			candidate := pystr.Upper(values[offset])
			isTypeN := strings.HasPrefix(candidate, "TYPE") && decimalDigits(candidate[4:]) && constraintUint(candidate[4:], big.NewInt(65535)) != nil
			if isTypeN || (!pystr.IsDigit(candidate) && dnsCodeValid("qtype", candidate)) {
				rtype, typeIndex = candidate, offset
				offset++
				break
			}
			if !(dnsCodeValid("qclass", candidate) && !pystr.IsDigit(candidate)) {
				dnsTTLFromText(values[offset])
			}
			offset++
		}
		if rtype == "" {
			return "DNS 记录缺少类型和 RDATA"
		}
		if single {
			original := quotedOrWord(raw)
			if strings.Contains(raw, "{qname}") && (len(original) == 0 || original[0] != "{qname}") {
				return "{qname} 只能作为未加引号的记录名称"
			}
			if strings.Contains(raw, "{qclass}") {
				found := false
				end := typeIndex
				if end > len(original) {
					end = len(original)
				}
				for k := 1; k < end; k++ {
					if original[k] == "{qclass}" {
						found = true
					}
				}
				if !found {
					return "{qclass} 只能作为类型前的未加引号的记录类别"
				}
			}
		}
		dataTokens := tokens[offset:]
		dataValues := values[offset:]
		if len(dataValues) == 0 {
			return "DNS 记录缺少 RDATA"
		}
		if dataValues[0] == `\#` || dataValues[0] == "#" {
			if len(dataValues) < 2 || !pystr.IsDigit(dataValues[1]) {
				return "RFC3597 记录需要长度和十六进制数据"
			}
			hexdata := strings.Join(dataValues[2:], "")
			length := constraintUint(dataValues[1], big.NewInt(65535))
			if length == nil || int64(runeLen(hexdata)) != length.Int64()*2 || !isHexText(hexdata) {
				return "RFC3597 数据长度或十六进制格式无效"
			}
		} else {
			if !zoneAllowedText[rtype] {
				return "该记录类型在 OxiDNS 中需要 RFC3597 格式：\\# 长度 十六进制数据"
			}
			for _, position := range zoneNamePositions[rtype] {
				if position >= len(dataValues) {
					return "DNS 记录的 RDATA 参数不足"
				}
				if !dnsNameFromText(dataValues[position], origin).isAbsolute() {
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
			parts := make([]string, len(dataTokens))
			for i, token := range dataTokens {
				if token.ttype == tokQuoted {
					parts[i] = `"` + token.value + `"`
				} else {
					parts[i] = token.value
				}
			}
			rdataFromText(parsed, strings.Join(parts, " "), origin)
		}
		recordCount.Add(recordCount, big.NewInt(1))
	}
	if single && (recordCount.Cmp(big.NewInt(1)) > 0 || (recordCount.Cmp(big.NewInt(1)) != 0 && !unresolvedInclude)) {
		return "每个响应记录条目必须恰好包含一条 DNS 记录"
	}
	return ""
}

func isHexText(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
