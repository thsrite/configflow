package rules

import (
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// pyIDNAEncode is str.encode('idna') (CPython's encodings/idna.py, i.e. IDNA
// 2003 with nameprep) as far as domain_value_error needs it: the ASCII result
// or ok=false where Python raises UnicodeError. Tables are generated from
// CPython's own stringprep/unicodedata (see testdata/gen_idna.py).
func pyIDNAEncode(input string) (string, bool) {
	if input == "" {
		return "", true
	}
	if isASCII(input) {
		labels := strings.Split(input, ".")
		for _, label := range labels[:len(labels)-1] {
			if len(label) == 0 || len(label) >= 64 {
				return "", false
			}
		}
		if len(labels[len(labels)-1]) >= 64 {
			return "", false
		}
		return input, true
	}
	labels := splitIDNADots(input)
	trailing := ""
	if len(labels) > 0 && labels[len(labels)-1] == "" {
		trailing = "."
		labels = labels[:len(labels)-1]
	}
	var out strings.Builder
	for _, label := range labels {
		if out.Len() > 0 {
			out.WriteByte('.')
		}
		encoded, ok := idnaToASCII(label)
		if !ok {
			return "", false
		}
		out.WriteString(encoded)
	}
	return out.String() + trailing, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// splitIDNADots splits on the four IDNA label separators.
func splitIDNADots(s string) []string {
	var labels []string
	start := 0
	for i, r := range s {
		if r == '.' || r == 0x3002 || r == 0xff0e || r == 0xff61 {
			labels = append(labels, s[start:i])
			start = i + utf8.RuneLen(r)
		}
	}
	return append(labels, s[start:])
}

func idnaToASCII(label string) (string, bool) {
	if isASCII(label) {
		if len(label) > 0 && len(label) < 64 {
			return label, true
		}
		return "", false
	}
	label, ok := nameprep(label)
	if !ok {
		return "", false
	}
	if isASCII(label) {
		if len(label) > 0 && len(label) < 64 {
			return label, true
		}
		return "", false
	}
	if len(label) >= 4 && strings.EqualFold(label[:4], "xn--") {
		return "", false
	}
	encoded := "xn--" + punycodeEncode(label)
	if len(encoded) > 0 && len(encoded) < 64 {
		return encoded, true
	}
	return "", false
}

var (
	idnaOnce       sync.Once
	b1Set          map[rune]struct{}
	b2Map          map[rune]string
	nfkdMap        map[rune]string
	compositionMap map[[2]rune]rune
)

func idnaInit() {
	idnaOnce.Do(func() {
		b1Set = map[rune]struct{}{}
		for _, r := range idnaTableB1 {
			for c := r.lo; c <= r.hi; c++ {
				b1Set[c] = struct{}{}
			}
		}
		b2Map = make(map[rune]string, len(idnaMapB2))
		for _, m := range idnaMapB2 {
			b2Map[m.cp] = m.text
		}
		nfkdMap = make(map[rune]string, len(nfkdMappings))
		for _, m := range nfkdMappings {
			nfkdMap[m.cp] = m.text
		}
		compositionMap = make(map[[2]rune]rune, len(compositionPairs))
		for _, p := range compositionPairs {
			compositionMap[[2]rune{p.first, p.second}] = p.result
		}
	})
}

func inRanges(table []runeRange, r rune) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i].hi >= r })
	return i < len(table) && table[i].lo <= r
}

// nameprep is encodings.idna.nameprep.
func nameprep(label string) (string, bool) {
	idnaInit()
	var mapped strings.Builder
	for _, r := range label {
		if _, drop := b1Set[r]; drop {
			continue
		}
		if text, ok := b2Map[r]; ok {
			mapped.WriteString(text)
		} else {
			mapped.WriteRune(r)
		}
	}
	label = nfkc(mapped.String())
	for _, r := range label {
		if inRanges(idnaTableProhibited, r) {
			return "", false
		}
	}
	runes := []rune(label)
	randAL := make([]bool, len(runes))
	any := false
	for i, r := range runes {
		randAL[i] = inRanges(idnaTableD1, r)
		any = any || randAL[i]
	}
	if any {
		for _, r := range runes {
			if inRanges(idnaTableD2, r) {
				return "", false
			}
		}
		if !randAL[0] || !randAL[len(randAL)-1] {
			return "", false
		}
	}
	return label, true
}

func combiningOf(r rune) uint8 {
	if r < 0x300 {
		return 0
	}
	i := sort.Search(len(combiningClasses), func(i int) bool { return combiningClasses[i].hi >= r })
	if i < len(combiningClasses) && combiningClasses[i].lo <= r {
		return combiningClasses[i].class
	}
	return 0
}

const (
	hangulS    = 0xAC00
	hangulL    = 0x1100
	hangulV    = 0x1161
	hangulT    = 0x11A7
	hangulLCnt = 19
	hangulVCnt = 21
	hangulTCnt = 28
	hangulNCnt = hangulVCnt * hangulTCnt
	hangulSCnt = hangulLCnt * hangulNCnt
)

func decomposeInto(out []rune, r rune) []rune {
	if r >= hangulS && r < hangulS+hangulSCnt {
		s := r - hangulS
		out = append(out, hangulL+s/hangulNCnt, hangulV+(s%hangulNCnt)/hangulTCnt)
		if t := s % hangulTCnt; t != 0 {
			out = append(out, hangulT+t)
		}
		return out
	}
	if text, ok := nfkdMap[r]; ok {
		return append(out, []rune(text)...)
	}
	return append(out, r)
}

func composePair(a, b rune) (rune, bool) {
	if a >= hangulL && a < hangulL+hangulLCnt && b >= hangulV && b < hangulV+hangulVCnt {
		return hangulS + ((a-hangulL)*hangulVCnt+(b-hangulV))*hangulTCnt, true
	}
	if a >= hangulS && a < hangulS+hangulSCnt && (a-hangulS)%hangulTCnt == 0 && b > hangulT && b < hangulT+hangulTCnt {
		return a + (b - hangulT), true
	}
	r, ok := compositionMap[[2]rune{a, b}]
	return r, ok
}

// nfkc is unicodedata.normalize('NFKC', s) with CPython 3.11's data.
func nfkc(s string) string {
	idnaInit()
	if isASCII(s) {
		return s
	}
	var runes []rune
	for _, r := range s {
		runes = decomposeInto(runes, r)
	}
	// Canonical ordering: stable sort each run of non-starters by class.
	for i := 0; i < len(runes); {
		if combiningOf(runes[i]) == 0 {
			i++
			continue
		}
		j := i
		for j < len(runes) && combiningOf(runes[j]) != 0 {
			j++
		}
		run := runes[i:j]
		sort.SliceStable(run, func(a, b int) bool { return combiningOf(run[a]) < combiningOf(run[b]) })
		i = j
	}
	if len(runes) == 0 {
		return ""
	}
	out := []rune{runes[0]}
	starterPos := 0
	starter := runes[0]
	lastClass := int(combiningOf(starter))
	if lastClass != 0 {
		lastClass = 256
	}
	for _, r := range runes[1:] {
		class := int(combiningOf(r))
		if composed, ok := composePair(starter, r); ok && (lastClass < class || lastClass == 0) {
			out[starterPos] = composed
			starter = composed
			continue
		}
		if class == 0 {
			starterPos = len(out)
			starter = r
		}
		lastClass = class
		out = append(out, r)
	}
	return string(out)
}

// punycodeEncode is RFC 3492 as in CPython's encodings/punycode.py: basic code
// points first, then the delta-encoded rest.
func punycodeEncode(text string) string {
	const (
		base        = 36
		tMin        = 1
		tMax        = 26
		skew        = 38
		damp        = 700
		initialBias = 72
		initialN    = 128
	)
	runes := []rune(text)
	var basic []byte
	for _, r := range runes {
		if r < 128 {
			basic = append(basic, byte(r))
		}
	}
	out := append([]byte(nil), basic...)
	if len(basic) > 0 {
		out = append(out, '-')
	}
	digit := func(d int) byte {
		if d < 26 {
			return byte('a' + d)
		}
		return byte('0' + d - 26)
	}
	adapt := func(delta, points int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / points
		k := 0
		for delta > ((base-tMin)*tMax)/2 {
			delta /= base - tMin
			k += base
		}
		return k + (base-tMin+1)*delta/(delta+skew)
	}
	n, delta, bias := initialN, 0, initialBias
	handled := len(basic)
	for handled < len(runes) {
		m := int(^uint(0) >> 1)
		for _, r := range runes {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range runes {
			c := int(r)
			if c < n {
				delta++
			}
			if c == n {
				q := delta
				for k := base; ; k += base {
					t := k - bias
					if t < tMin {
						t = tMin
					} else if t > tMax {
						t = tMax
					}
					if q < t {
						break
					}
					out = append(out, digit(t+(q-t)%(base-t)))
					q = (q - t) / (base - t)
				}
				out = append(out, digit(q))
				bias = adapt(delta, handled+1, handled == len(basic))
				delta = 0
				handled++
			}
		}
		delta++
		n++
	}
	return string(out)
}
