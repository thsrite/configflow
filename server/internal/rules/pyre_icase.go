package rules

import (
	"configflow/internal/pystr"
	"sort"
	"sync"
	"unicode"
)

// IGNORECASE with sre's semantics. Python does not use Unicode case folding:
// a cased literal c matches every character whose simple lowercase
// (_sre.unicode_tolower) is lower(c) or one of re._casefix._EXTRA_CASES of it,
// so "(?i)i" matches "İ" and "ı" while RE2's and regexp2's folding do not; a
// character set is tested with the lowercased character (_optimize_charset +
// IN_UNI_IGNORE). foldCase rewrites literals and sets under IGNORECASE into
// explicit case-sensitive sets once, after parsing, so neither engine applies
// its own folding (flagLetters never emits "i"); back references keep the
// flag (opGroupRef.icase) for the backtracking engine.

type caseTables struct {
	lower, upper []rune          // dense up to len; identity beyond
	inverse      map[rune][]rune // t → every c != t with lower(c) == t
	lowerKeys    []rune          // every c with lower(c) != c, ascending
	upperKeys    map[rune]bool   // every c with upper(c) != c
}

var (
	caseOnce sync.Once
	cases    caseTables
)

func loadCaseTables() *caseTables {
	caseOnce.Do(func() {
		dense := func(pairs []int32) []rune {
			size := rune(0)
			for i := 0; i < len(pairs); i += 2 {
				if pairs[i] >= size {
					size = pairs[i] + 1
				}
			}
			out := make([]rune, size)
			for i := range out {
				out[i] = rune(i)
			}
			for i := 0; i < len(pairs); i += 2 {
				out[pairs[i]] = pairs[i+1]
			}
			return out
		}
		cases.lower = dense(pyLowerPairs[:])
		cases.upper = dense(pyUpperPairs[:])
		cases.inverse = map[rune][]rune{}
		for i := 0; i < len(pyLowerPairs); i += 2 {
			c, t := pyLowerPairs[i], pyLowerPairs[i+1]
			cases.inverse[t] = append(cases.inverse[t], c)
			cases.lowerKeys = append(cases.lowerKeys, c)
		}
		cases.upperKeys = map[rune]bool{}
		for i := 0; i < len(pyUpperPairs); i += 2 {
			cases.upperKeys[pyUpperPairs[i]] = true
		}
	})
	return &cases
}

func (t *caseTables) lowerOf(r rune) rune {
	if r >= 0 && int(r) < len(t.lower) {
		return t.lower[r]
	}
	return r
}

func (t *caseTables) upperOf(r rune) rune {
	if r >= 0 && int(r) < len(t.upper) {
		return t.upper[r]
	}
	return r
}

// caseModel is the (iscased, tolower, fixes) triple _compile picks.
type caseModel struct {
	t     *caseTables
	ascii bool
}

func (m caseModel) lower(r rune) rune {
	if m.ascii {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	return m.t.lowerOf(r)
}

func (m caseModel) iscased(r rune) bool {
	if m.ascii {
		return r < 128 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}
	return m.t.lowerOf(r) != r || m.t.upperOf(r) != r
}

func (m caseModel) fixes(lo rune) []rune {
	if m.ascii {
		return nil
	}
	return pyExtraCases[lo]
}

// literalSet lists every character a cased literal c matches: those whose
// lowercase is lower(c) or one of its _EXTRA_CASES.
func (m caseModel) literalSet(c rune) []rune {
	lo := m.lower(c)
	targets := append([]rune{lo}, m.fixes(lo)...)
	inTargets := func(r rune) bool {
		for _, t := range targets {
			if r == t {
				return true
			}
		}
		return false
	}
	seen := map[rune]bool{}
	var out []rune
	add := func(r rune) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, target := range targets {
		if inTargets(m.lower(target)) {
			add(target)
		}
		if m.ascii {
			if target >= 'a' && target <= 'z' {
				add(target - 32)
			}
			continue
		}
		for _, r := range m.t.inverse[target] {
			add(r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type tailRange struct {
	lo, hi    rune
	uniIgnore bool
}

// categoryHas is the sre category test (Unicode or ASCII variant).
func categoryHas(cat catKind, r rune, ascii bool) bool {
	switch cat {
	case catDigit, catNotDigit:
		var is bool
		if ascii {
			is = r >= '0' && r <= '9'
		} else {
			is = unicode.Is(unicode.Nd, r)
		}
		return is == (cat == catDigit)
	case catWord, catNotWord:
		var is bool
		if ascii {
			is = r < 128 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_')
		} else {
			is = unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
		}
		return is == (cat == catWord)
	default:
		var is bool
		if ascii {
			is = r == ' ' || r >= '\t' && r <= '\r'
		} else {
			is = pystr.IsSpace(r)
		}
		return is == (cat == catSpace)
	}
}

// foldSet applies _optimize_charset with fixup: it returns nil when the set
// has no cased member (Python then tests the raw character with IN) and the
// explicit ranges of every matching character otherwise.
func (m caseModel) foldSet(items []setItem) []setItem {
	var bmp [0x10000 / 64]uint64
	mark := func(r rune) { bmp[r>>6] |= 1 << (uint(r) & 63) }
	var tailLiterals []rune
	var tailRanges []tailRange
	var cats []catKind
	negate, hascased := false, false
	for _, item := range items {
		switch item.kind {
		case setNegate:
			negate = true
		case setCategory:
			cats = append(cats, item.cat)
		case setLiteral:
			lo := m.lower(item.lo)
			if lo >= 0x10000 {
				hascased = true
				tailLiterals = append(tailLiterals, item.lo)
				continue
			}
			mark(lo)
			for _, k := range m.fixes(lo) {
				mark(k)
			}
			if !hascased && m.iscased(item.lo) {
				hascased = true
			}
		case setRange:
			overflow := false
			for r := item.lo; r <= item.hi; r++ {
				lo := m.lower(r)
				if lo >= 0x10000 {
					overflow = true
					break
				}
				mark(lo)
				for _, k := range m.fixes(lo) {
					mark(k)
				}
			}
			if overflow {
				hascased = true
				tailRanges = append(tailRanges, tailRange{item.lo, item.hi, true})
				continue
			}
			if !hascased {
				for r := item.lo; r <= item.hi; r++ {
					if m.iscased(r) {
						hascased = true
						break
					}
				}
			}
		}
	}
	if !hascased {
		return nil
	}
	// raw is the membership test without categories, applied by sre to the
	// lowercased character.
	raw := func(ch rune) bool {
		if ch < 0x10000 && bmp[ch>>6]&(1<<(uint(ch)&63)) != 0 {
			return true
		}
		for _, r := range tailLiterals {
			if ch == r {
				return true
			}
		}
		for _, r := range tailRanges {
			if r.lo <= ch && ch <= r.hi {
				return true
			}
			if up := m.t.upperOf(ch); r.uniIgnore && r.lo <= up && up <= r.hi {
				return true
			}
		}
		return false
	}
	anyCat := func(ch rune) bool {
		for _, cat := range cats {
			if categoryHas(cat, ch, m.ascii) {
				return true
			}
		}
		return false
	}
	// Python matches ch when raw(lower(ch)) || anyCat(lower(ch)). Outside
	// the lowercase table lower(ch) == ch, so the set is: the raw ranges,
	// corrected for the few characters whose lowercase differs (and, for
	// RANGE_UNI_IGNORE, whose uppercase differs), plus the categories kept as
	// categories, which is exact while a category agrees on ch and lower(ch)
	// for every corrected character it decides. Otherwise every code point is
	// tested.
	domain := m.lowerDomain()
	for _, ch := range domain {
		if lo := m.lower(ch); !raw(lo) && anyCat(ch) != anyCat(lo) {
			return m.bruteForce(raw, anyCat, negate)
		}
	}
	override := map[rune]bool{}
	for _, ch := range domain {
		override[ch] = raw(m.lower(ch))
	}
	for _, r := range tailRanges {
		if r.uniIgnore {
			for ch := range m.t.upperDomain() {
				if _, done := override[ch]; !done {
					override[ch] = raw(ch)
				}
			}
			break
		}
	}
	var spans []span
	for word := range bmp {
		for bits := bmp[word]; bits != 0; bits &= bits - 1 {
			ch := rune(word*64 + trailingZeros(bits))
			spans = append(spans, span{ch, ch})
		}
	}
	for _, r := range tailLiterals {
		spans = append(spans, span{r, r})
	}
	for _, r := range tailRanges {
		spans = append(spans, span{r.lo, r.hi})
	}
	spans = applyOverrides(mergeSpans(spans), override)
	var out []setItem
	if negate {
		out = append(out, setItem{kind: setNegate})
	}
	for _, sp := range spans {
		out = append(out, setItem{kind: setRange, lo: sp.lo, hi: sp.hi})
	}
	for _, cat := range cats {
		out = append(out, setItem{kind: setCategory, cat: cat})
	}
	if len(out) == 0 {
		// Matches nothing.
		return []setItem{{kind: setNegate}, {kind: setRange, lo: 0, hi: unicode.MaxRune}}
	}
	if negate && len(out) == 1 {
		// [^] of nothing matches everything.
		return []setItem{{kind: setRange, lo: 0, hi: unicode.MaxRune}}
	}
	return out
}

// bruteForce tests every code point (exact, slower).
func (m caseModel) bruteForce(raw, anyCat func(rune) bool, negate bool) []setItem {
	var out []setItem
	open := false
	for ch := rune(0); ch <= unicode.MaxRune; ch++ {
		if ch == 0xD800 {
			ch = 0xE000 // Go strings carry no surrogates
			open = false
		}
		lo := m.lower(ch)
		if (raw(lo) || anyCat(lo)) != negate {
			if open && out[len(out)-1].hi == ch-1 {
				out[len(out)-1].hi = ch
			} else {
				out = append(out, setItem{kind: setRange, lo: ch, hi: ch})
				open = true
			}
		}
	}
	if len(out) == 0 {
		return []setItem{{kind: setNegate}, {kind: setRange, lo: 0, hi: unicode.MaxRune}}
	}
	return out
}

// lowerDomain lists the characters whose lowercase differs from them.
func (m caseModel) lowerDomain() []rune {
	if m.ascii {
		out := make([]rune, 0, 26)
		for r := 'A'; r <= 'Z'; r++ {
			out = append(out, r)
		}
		return out
	}
	return m.t.lowerKeys
}

func (t *caseTables) upperDomain() map[rune]bool { return t.upperKeys }

func trailingZeros(x uint64) int {
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

func mergeSpans(spans []span) []span {
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	var out []span
	for _, sp := range spans {
		if n := len(out); n > 0 && sp.lo <= out[n-1].hi+1 {
			if sp.hi > out[n-1].hi {
				out[n-1].hi = sp.hi
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

// applyOverrides sets the membership of single characters in merged spans.
func applyOverrides(spans []span, override map[rune]bool) []span {
	contains := func(ch rune) bool {
		i := sort.Search(len(spans), func(i int) bool { return spans[i].hi >= ch })
		return i < len(spans) && spans[i].lo <= ch
	}
	var removes []rune
	var adds []span
	for ch, want := range override {
		has := contains(ch)
		switch {
		case want && !has:
			adds = append(adds, span{ch, ch})
		case !want && has:
			removes = append(removes, ch)
		}
	}
	sort.Slice(removes, func(i, j int) bool { return removes[i] < removes[j] })
	var out []span
	k := 0
	for _, sp := range spans {
		lo := sp.lo
		for k < len(removes) && removes[k] <= sp.hi {
			if removes[k] >= lo {
				if removes[k] > lo {
					out = append(out, span{lo, removes[k] - 1})
				}
				lo = removes[k] + 1
			}
			k++
		}
		if lo <= sp.hi {
			out = append(out, span{lo, sp.hi})
		}
	}
	return mergeSpans(append(out, adds...))
}

func literalItems(set []rune, negate bool) []setItem {
	var out []setItem
	if negate {
		out = append(out, setItem{kind: setNegate})
	}
	for _, r := range set {
		out = append(out, setItem{kind: setLiteral, lo: r})
	}
	return out
}

// combineFlags is _combine_flags.
func combineFlags(flags, add, del int) int {
	if add&typeFlags != 0 {
		flags &^= typeFlags
	}
	return (flags | add) &^ del
}

// foldCase rewrites nodes in place (see the file comment). flags are the
// flags in effect for nodes.
func foldCase(nodes []*reNode, flags int) {
	icase := flags&flagIgnoreCase != 0
	model := caseModel{t: loadCaseTables(), ascii: flags&flagUnicode == 0}
	for _, n := range nodes {
		switch n.op {
		case opLiteral, opNotLiteral:
			if icase && model.iscased(n.ch) {
				n.set = literalItems(model.literalSet(n.ch), n.op == opNotLiteral)
				n.op = opIn
			}
		case opIn:
			if icase {
				if folded := model.foldSet(n.set); folded != nil {
					n.set = folded
				}
			}
		case opGroupRef:
			n.icase = icase
		case opSubpattern:
			foldCase(n.body, combineFlags(flags, n.add, n.del))
		case opBranch:
			for _, alt := range n.alts {
				foldCase(alt, flags)
			}
		case opGroupRefExists:
			foldCase(n.yes, flags)
			foldCase(n.no, flags)
		default:
			foldCase(n.body, flags)
		}
	}
}
