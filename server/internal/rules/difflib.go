package rules

import (
	"sort"
)

// A port of difflib.SequenceMatcher (junk-free, autojunk on) and
// difflib.get_close_matches, which rule_syntax uses to suggest a rule type
// for a misspelled one. Scores must match Python's to the last bit because
// they are compared with a cutoff.

type sequenceMatcher struct {
	a, b []rune
	b2j  map[rune][]int
}

func newSequenceMatcher(b []rune) *sequenceMatcher {
	m := &sequenceMatcher{b: b, b2j: map[rune][]int{}}
	for i, elt := range b {
		m.b2j[elt] = append(m.b2j[elt], i)
	}
	if n := len(b); n >= 200 {
		ntest := n/100 + 1
		for elt, idxs := range m.b2j {
			if len(idxs) > ntest {
				delete(m.b2j, elt)
			}
		}
	}
	return m
}

type matchBlock struct{ i, j, size int }

func (m *sequenceMatcher) findLongestMatch(alo, ahi, blo, bhi int) matchBlock {
	besti, bestj, bestsize := alo, blo, 0
	j2len := map[int]int{}
	for i := alo; i < ahi; i++ {
		newj2len := map[int]int{}
		for _, j := range m.b2j[m.a[i]] {
			if j < blo {
				continue
			}
			if j >= bhi {
				break
			}
			k := j2len[j-1] + 1
			newj2len[j] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len = newj2len
	}
	for besti > alo && bestj > blo && m.a[besti-1] == m.b[bestj-1] && m.inB(m.b[bestj-1]) {
		besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
	}
	for besti+bestsize < ahi && bestj+bestsize < bhi && m.a[besti+bestsize] == m.b[bestj+bestsize] {
		bestsize++
	}
	return matchBlock{besti, bestj, bestsize}
}

// inB mirrors "not isbjunk(...)": with no junk set every element qualifies.
func (m *sequenceMatcher) inB(rune) bool { return true }

func (m *sequenceMatcher) matchingBlocks() []matchBlock {
	type window struct{ alo, ahi, blo, bhi int }
	queue := []window{{0, len(m.a), 0, len(m.b)}}
	var blocks []matchBlock
	for len(queue) > 0 {
		w := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		x := m.findLongestMatch(w.alo, w.ahi, w.blo, w.bhi)
		if x.size > 0 {
			blocks = append(blocks, x)
			if w.alo < x.i && w.blo < x.j {
				queue = append(queue, window{w.alo, x.i, w.blo, x.j})
			}
			if x.i+x.size < w.ahi && x.j+x.size < w.bhi {
				queue = append(queue, window{x.i + x.size, w.ahi, x.j + x.size, w.bhi})
			}
		}
	}
	sort.Slice(blocks, func(p, q int) bool {
		if blocks[p].i != blocks[q].i {
			return blocks[p].i < blocks[q].i
		}
		if blocks[p].j != blocks[q].j {
			return blocks[p].j < blocks[q].j
		}
		return blocks[p].size < blocks[q].size
	})
	return blocks
}

func calculateRatio(matches, length int) float64 {
	if length > 0 {
		return 2.0 * float64(matches) / float64(length)
	}
	return 1.0
}

func (m *sequenceMatcher) ratio() float64 {
	matches := 0
	for _, block := range m.matchingBlocks() {
		matches += block.size
	}
	return calculateRatio(matches, len(m.a)+len(m.b))
}

func (m *sequenceMatcher) realQuickRatio() float64 {
	la, lb := len(m.a), len(m.b)
	min := la
	if lb < min {
		min = lb
	}
	return calculateRatio(min, la+lb)
}

func (m *sequenceMatcher) quickRatio() float64 {
	full := map[rune]int{}
	for _, elt := range m.b {
		full[elt]++
	}
	avail := map[rune]int{}
	matches := 0
	for _, elt := range m.a {
		numb, ok := avail[elt]
		if !ok {
			numb = full[elt]
		}
		avail[elt] = numb - 1
		if numb > 0 {
			matches++
		}
	}
	return calculateRatio(matches, len(m.a)+len(m.b))
}

// getCloseMatch is get_close_matches(word, possibilities, n=1, cutoff): the
// best candidate (highest ratio, ties broken by the larger string) or "".
func getCloseMatch(word string, possibilities []string, cutoff float64) (string, bool) {
	m := newSequenceMatcher([]rune(word))
	best, bestScore, found := "", 0.0, false
	for _, x := range possibilities {
		m.a = []rune(x)
		if m.realQuickRatio() >= cutoff && m.quickRatio() >= cutoff {
			if score := m.ratio(); score >= cutoff {
				if !found || score > bestScore || (score == bestScore && x > best) {
					best, bestScore, found = x, score, true
				}
			}
		}
	}
	return best, found
}
