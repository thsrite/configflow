package oxidns

import (
	"sort"
	"strings"
)

// IDNA 2003 as dnspython's default codec uses it for non-ASCII DNS names
// (dns.name.IDNA_2003 → encodings.idna.ToASCII of CPython 3.11). Tables:
// unicode_c23_tables.go (testdata/gen_unicode_c23.py).

func inRuneRanges(table [][2]rune, r rune) bool {
	i := sort.Search(len(table), func(i int) bool { return table[i][1] >= r })
	return i < len(table) && table[i][0] <= r
}

// pyIsAlnum is str.isalnum() for one character (CPython 3.11).
func pyIsAlnum(r rune) bool {
	if r < 128 {
		return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
	}
	return inRuneRanges(alnumRanges, r)
}

// isWordRune is re's \w for str patterns: isalnum() or "_".
func isWordRune(r rune) bool { return r == '_' || pyIsAlnum(r) }

const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// nfkc32 is unicodedata.ucd_3_2_0.normalize('NFKC', s) as CPython's
// nfd_nfkd/nfc_nfkc compute it (3.2 decompositions, the running database's
// combining classes and composites).
func nfkc32(s []rune) []rune {
	var d []rune
	for _, r := range s {
		if r >= hangulSBase && r < hangulSBase+hangulSCount {
			index := r - hangulSBase
			d = append(d, hangulLBase+index/hangulNCount, hangulVBase+(index%hangulNCount)/hangulTCount)
			if t := index % hangulTCount; t != 0 {
				d = append(d, hangulTBase+t)
			}
			continue
		}
		if mapped, ok := nfkd32[r]; ok {
			d = append(d, []rune(mapped)...)
			continue
		}
		d = append(d, r)
	}
	// Canonical ordering (CPython's insertion sort).
	if len(d) > 0 {
		prev := combiningClass[d[0]]
		for i := 1; i < len(d); i++ {
			cur := combiningClass[d[i]]
			if prev == 0 || cur == 0 || prev <= cur {
				prev = cur
				continue
			}
			o := i - 1
			for {
				d[o], d[o+1] = d[o+1], d[o]
				o--
				if o < 0 {
					break
				}
				prev = combiningClass[d[o]]
				if prev == 0 || prev <= cur {
					break
				}
			}
			prev = combiningClass[d[i]]
		}
	}
	return composeNFC(d)
}

var compositeFirsts = func() map[rune]bool {
	m := map[rune]bool{}
	for pair := range primaryComposites {
		m[pair[0]] = true
	}
	return m
}()

var compositeLasts = func() map[rune]bool {
	m := map[rune]bool{}
	for pair := range primaryComposites {
		m[pair[1]] = true
	}
	return m
}()

// composeNFC is the composition loop of CPython's nfc_nfkc.
func composeNFC(input []rune) []rune {
	output := make([]rune, 0, len(input))
	var skipped []int
	i := 0
	for i < len(input) {
		isSkipped := false
		for k, s := range skipped {
			if s == i {
				skipped[k] = skipped[len(skipped)-1]
				skipped = skipped[:len(skipped)-1]
				isSkipped = true
				break
			}
		}
		if isSkipped {
			i++
			continue
		}
		code := input[i]
		if code >= hangulLBase && code < hangulLBase+hangulLCount && i+1 < len(input) &&
			input[i+1] >= hangulVBase && input[i+1] < hangulVBase+hangulVCount {
			lIndex := code - hangulLBase
			vIndex := input[i+1] - hangulVBase
			code = hangulSBase + (lIndex*hangulVCount+vIndex)*hangulTCount
			i += 2
			if i < len(input) && hangulTBase < input[i] && input[i] < hangulTBase+hangulTCount {
				code += input[i] - hangulTBase
				i++
			}
			output = append(output, code)
			continue
		}
		if !compositeFirsts[code] {
			output = append(output, code)
			i++
			continue
		}
		o := len(output)
		output = append(output, code)
		i1 := i + 1
		var comb uint8
		for i1 < len(input) {
			code1 := input[i1]
			comb1 := combiningClass[code1]
			if comb != 0 {
				if comb1 == 0 {
					break
				}
				if comb >= comb1 {
					i1++
					continue
				}
			}
			composite, ok := rune(0), false
			if compositeLasts[code1] {
				composite, ok = primaryComposites[[2]rune{output[o], code1}]
			}
			if !ok {
				if comb1 == 0 {
					break
				}
				comb = comb1
				i1++
				continue
			}
			output[o] = composite
			skipped = append(skipped, i1)
			i1++
			if !compositeFirsts[output[o]] {
				break
			}
		}
		i++
	}
	return output
}

// nameprep is encodings.idna.nameprep(label); ok=false is its UnicodeError.
func nameprep(label []rune) ([]rune, bool) {
	var mapped []rune
	for _, c := range label {
		if inRuneRanges(nameprepB1, c) {
			continue
		}
		if m, ok := nameprepB2[c]; ok {
			mapped = append(mapped, []rune(m)...)
		} else {
			mapped = append(mapped, c)
		}
	}
	result := nfkc32(mapped)
	for _, c := range result {
		if inRuneRanges(nameprepProhibited, c) {
			return nil, false
		}
	}
	randAL := false
	for _, c := range result {
		if inRuneRanges(nameprepRandAL, c) {
			randAL = true
			break
		}
	}
	if randAL {
		for _, c := range result {
			if inRuneRanges(nameprepL, c) {
				return nil, false
			}
		}
		if !inRuneRanges(nameprepRandAL, result[0]) || !inRuneRanges(nameprepRandAL, result[len(result)-1]) {
			return nil, false
		}
	}
	return result, true
}

func allASCII(label []rune) bool {
	for _, c := range label {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// idnaToASCII is encodings.idna.ToASCII(label); ok=false is its
// UnicodeError (dnspython's LabelTooLong).
func idnaToASCII(label []rune) ([]byte, bool) {
	if allASCII(label) {
		if 0 < len(label) && len(label) < 64 {
			return []byte(string(label)), true
		}
		return nil, false
	}
	label, ok := nameprep(label)
	if !ok {
		return nil, false
	}
	if allASCII(label) {
		if 0 < len(label) && len(label) < 64 {
			return []byte(string(label)), true
		}
		return nil, false
	}
	if strings.HasPrefix(string(label), "xn--") {
		return nil, false
	}
	encoded := "xn--" + punycodeEncode(label)
	if 0 < len(encoded) && len(encoded) < 64 {
		return []byte(encoded), true
	}
	return nil, false
}

// punycodeEncode is str.encode('punycode') (RFC 3492).
func punycodeEncode(input []rune) string {
	const (
		base        = 36
		tmin        = 1
		tmax        = 26
		skew        = 38
		damp        = 700
		initialBias = 72
		initialN    = 128
	)
	var out []byte
	for _, c := range input {
		if c < 0x80 {
			out = append(out, byte(c))
		}
	}
	b := len(out)
	if b > 0 {
		out = append(out, '-')
	}
	digit := func(d int) byte {
		if d < 26 {
			return byte('a' + d)
		}
		return byte('0' + d - 26)
	}
	adapt := func(delta, numPoints int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / numPoints
		k := 0
		for delta > ((base-tmin)*tmax)/2 {
			delta /= base - tmin
			k += base
		}
		return k + (base-tmin+1)*delta/(delta+skew)
	}
	n, delta, bias, h := initialN, 0, initialBias, b
	for h < len(input) {
		m := int(^uint(0) >> 1)
		for _, c := range input {
			if int(c) >= n && int(c) < m {
				m = int(c)
			}
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, c := range input {
			if int(c) < n {
				delta++
			}
			if int(c) == n {
				q := delta
				for k := base; ; k += base {
					t := k - bias
					if t < tmin {
						t = tmin
					} else if t > tmax {
						t = tmax
					}
					if q < t {
						break
					}
					out = append(out, digit(t+(q-t)%(base-t)))
					q = (q - t) / (base - t)
				}
				out = append(out, digit(q))
				bias = adapt(delta, h+1, h == b)
				delta = 0
				h++
			}
		}
		delta++
		n++
	}
	return string(out)
}
