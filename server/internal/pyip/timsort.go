package pyip

// Sort is CPython 3.11's list.sort() (Objects/listobject.c: count_run,
// binarysort, powersort merge order, merge_lo/merge_hi with galloping) driven
// by less (Python's `<`). For a consistent ordering it equals any stable
// sort; Python's network ordering is not a strict weak order when scope ids
// are involved (same address, different scope id: neither is smaller), and
// then only the same comparison sequence gives the same result.
func Sort[T any](list []T, less func(a, b T) bool) {
	n := len(list)
	if n < 2 {
		return
	}
	ms := &mergeState[T]{list: list, less: less, minGallop: minGallop, listLen: n}
	minrun := computeMinrun(n)
	lo, remaining := 0, n
	for remaining > 0 {
		runLen, descending := ms.countRun(lo, lo+remaining)
		if descending {
			reverse(list[lo : lo+runLen])
		}
		if runLen < minrun {
			force := remaining
			if minrun < force {
				force = minrun
			}
			ms.binarySort(lo, lo+force, lo+runLen)
			runLen = force
		}
		ms.foundNewRun(runLen)
		ms.pending = append(ms.pending, run{base: lo, len: runLen})
		lo += runLen
		remaining -= runLen
	}
	ms.forceCollapse()
}

const minGallop = 7

type run struct {
	base, len, power int
}

type mergeState[T any] struct {
	list      []T
	less      func(a, b T) bool
	minGallop int
	listLen   int
	pending   []run
}

func computeMinrun(n int) int {
	r := 0
	for n >= 64 {
		r |= n & 1
		n >>= 1
	}
	return n + r
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func (ms *mergeState[T]) countRun(lo, hi int) (int, bool) {
	a := ms.list
	lo++
	if lo == hi {
		return 1, false
	}
	n := 2
	if ms.less(a[lo], a[lo-1]) {
		for lo = lo + 1; lo < hi; lo, n = lo+1, n+1 {
			if !ms.less(a[lo], a[lo-1]) {
				break
			}
		}
		return n, true
	}
	for lo = lo + 1; lo < hi; lo, n = lo+1, n+1 {
		if ms.less(a[lo], a[lo-1]) {
			break
		}
	}
	return n, false
}

func (ms *mergeState[T]) binarySort(lo, hi, start int) {
	a := ms.list
	if lo == start {
		start++
	}
	for ; start < hi; start++ {
		l, r := lo, start
		pivot := a[r]
		for l < r {
			p := l + ((r - l) >> 1)
			if ms.less(pivot, a[p]) {
				r = p
			} else {
				l = p + 1
			}
		}
		copy(a[l+1:start+1], a[l:start])
		a[l] = pivot
	}
}

func powerloop(s1, n1, n2, n int) int {
	result := 0
	a := 2*s1 + n1
	b := a + n1 + n2
	for {
		result++
		if a >= n {
			a -= n
			b -= n
		} else if b >= n {
			break
		}
		a <<= 1
		b <<= 1
	}
	return result
}

func (ms *mergeState[T]) foundNewRun(n2 int) {
	if len(ms.pending) == 0 {
		return
	}
	last := ms.pending[len(ms.pending)-1]
	power := powerloop(last.base, last.len, n2, ms.listLen)
	for len(ms.pending) > 1 && ms.pending[len(ms.pending)-2].power > power {
		ms.mergeAt(len(ms.pending) - 2)
	}
	ms.pending[len(ms.pending)-1].power = power
}

func (ms *mergeState[T]) forceCollapse() {
	for len(ms.pending) > 1 {
		p := ms.pending
		n := len(p) - 2
		if n > 0 && p[n-1].len < p[n+1].len {
			n--
		}
		ms.mergeAt(n)
	}
}

func (ms *mergeState[T]) mergeAt(i int) {
	p := ms.pending
	baseA, na := p[i].base, p[i].len
	baseB, nb := p[i+1].base, p[i+1].len
	p[i].len = na + nb
	if i == len(p)-3 {
		p[i+1] = p[i+2]
	}
	ms.pending = p[:len(p)-1]
	a := ms.list
	k := ms.gallopRight(a[baseB], a[baseA:baseA+na], 0)
	baseA += k
	na -= k
	if na == 0 {
		return
	}
	nb = ms.gallopLeft(a[baseA+na-1], a[baseB:baseB+nb], nb-1)
	if nb <= 0 {
		return
	}
	if na <= nb {
		ms.mergeLo(baseA, na, baseB, nb)
	} else {
		ms.mergeHi(baseA, na, baseB, nb)
	}
}

func (ms *mergeState[T]) gallopLeft(key T, a []T, hint int) int {
	n := len(a)
	lastofs, ofs := 0, 1
	if ms.less(a[hint], key) {
		maxofs := n - hint
		for ofs < maxofs {
			if ms.less(a[hint+ofs], key) {
				lastofs = ofs
				ofs = (ofs << 1) + 1
			} else {
				break
			}
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		lastofs += hint
		ofs += hint
	} else {
		maxofs := hint + 1
		for ofs < maxofs {
			if ms.less(a[hint-ofs], key) {
				break
			}
			lastofs = ofs
			ofs = (ofs << 1) + 1
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		k := lastofs
		lastofs = hint - ofs
		ofs = hint - k
	}
	lastofs++
	for lastofs < ofs {
		m := lastofs + ((ofs - lastofs) >> 1)
		if ms.less(a[m], key) {
			lastofs = m + 1
		} else {
			ofs = m
		}
	}
	return ofs
}

func (ms *mergeState[T]) gallopRight(key T, a []T, hint int) int {
	n := len(a)
	lastofs, ofs := 0, 1
	if ms.less(key, a[hint]) {
		maxofs := hint + 1
		for ofs < maxofs {
			if ms.less(key, a[hint-ofs]) {
				lastofs = ofs
				ofs = (ofs << 1) + 1
			} else {
				break
			}
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		k := lastofs
		lastofs = hint - ofs
		ofs = hint - k
	} else {
		maxofs := n - hint
		for ofs < maxofs {
			if ms.less(key, a[hint+ofs]) {
				break
			}
			lastofs = ofs
			ofs = (ofs << 1) + 1
		}
		if ofs > maxofs {
			ofs = maxofs
		}
		lastofs += hint
		ofs += hint
	}
	lastofs++
	for lastofs < ofs {
		m := lastofs + ((ofs - lastofs) >> 1)
		if ms.less(key, a[m]) {
			ofs = m
		} else {
			lastofs = m + 1
		}
	}
	return ofs
}

// mergeLo merges a[baseA:baseA+na] and the run right after it (na <= nb).
func (ms *mergeState[T]) mergeLo(baseA, na, baseB, nb int) {
	list := ms.list
	tmp := append([]T(nil), list[baseA:baseA+na]...)
	dest := baseA
	sa := 0 // index into tmp
	sb := baseB
	list[dest] = list[sb]
	dest++
	sb++
	nb--
	if nb == 0 {
		goto succeed
	}
	if na == 1 {
		goto copyB
	}
	for {
		acount, bcount := 0, 0
		for {
			if ms.less(list[sb], tmp[sa]) {
				list[dest] = list[sb]
				dest++
				sb++
				bcount++
				acount = 0
				nb--
				if nb == 0 {
					goto succeed
				}
				if bcount >= ms.minGallop {
					break
				}
			} else {
				list[dest] = tmp[sa]
				dest++
				sa++
				acount++
				bcount = 0
				na--
				if na == 1 {
					goto copyB
				}
				if acount >= ms.minGallop {
					break
				}
			}
		}
		ms.minGallop++
		for {
			if ms.minGallop > 1 {
				ms.minGallop--
			}
			k := ms.gallopRight(list[sb], tmp[sa:sa+na], 0)
			acount = k
			if k != 0 {
				copy(list[dest:dest+k], tmp[sa:sa+k])
				dest += k
				sa += k
				na -= k
				if na == 1 {
					goto copyB
				}
				if na == 0 {
					goto succeed
				}
			}
			list[dest] = list[sb]
			dest++
			sb++
			nb--
			if nb == 0 {
				goto succeed
			}
			k = ms.gallopLeft(tmp[sa], list[sb:sb+nb], 0)
			bcount = k
			if k != 0 {
				copy(list[dest:dest+k], list[sb:sb+k])
				dest += k
				sb += k
				nb -= k
				if nb == 0 {
					goto succeed
				}
			}
			list[dest] = tmp[sa]
			dest++
			sa++
			na--
			if na == 1 {
				goto copyB
			}
			if !(acount >= minGallop || bcount >= minGallop) {
				break
			}
		}
		ms.minGallop++
	}
succeed:
	if na > 0 {
		copy(list[dest:dest+na], tmp[sa:sa+na])
	}
	return
copyB:
	copy(list[dest:dest+nb], list[sb:sb+nb])
	list[dest+nb] = tmp[sa]
}

// mergeHi merges a[baseA:baseA+na] and the run right after it (na >= nb).
func (ms *mergeState[T]) mergeHi(baseA, na, baseB, nb int) {
	list := ms.list
	tmp := append([]T(nil), list[baseB:baseB+nb]...)
	dest := baseB + nb - 1
	sb := nb - 1 // index into tmp
	sa := baseA + na - 1
	list[dest] = list[sa]
	dest--
	sa--
	na--
	if na == 0 {
		goto succeed
	}
	if nb == 1 {
		goto copyA
	}
	for {
		acount, bcount := 0, 0
		for {
			if ms.less(tmp[sb], list[sa]) {
				list[dest] = list[sa]
				dest--
				sa--
				acount++
				bcount = 0
				na--
				if na == 0 {
					goto succeed
				}
				if acount >= ms.minGallop {
					break
				}
			} else {
				list[dest] = tmp[sb]
				dest--
				sb--
				bcount++
				acount = 0
				nb--
				if nb == 1 {
					goto copyA
				}
				if bcount >= ms.minGallop {
					break
				}
			}
		}
		ms.minGallop++
		for {
			if ms.minGallop > 1 {
				ms.minGallop--
			}
			k := ms.gallopRight(tmp[sb], list[baseA:baseA+na], na-1)
			k = na - k
			acount = k
			if k != 0 {
				dest -= k
				sa -= k
				copy(list[dest+1:dest+1+k], list[sa+1:sa+1+k])
				na -= k
				if na == 0 {
					goto succeed
				}
			}
			list[dest] = tmp[sb]
			dest--
			sb--
			nb--
			if nb == 1 {
				goto copyA
			}
			k = ms.gallopLeft(list[sa], tmp[:nb], nb-1)
			k = nb - k
			bcount = k
			if k != 0 {
				dest -= k
				sb -= k
				copy(list[dest+1:dest+1+k], tmp[sb+1:sb+1+k])
				nb -= k
				if nb == 1 {
					goto copyA
				}
				if nb == 0 {
					goto succeed
				}
			}
			list[dest] = list[sa]
			dest--
			sa--
			na--
			if na == 0 {
				goto succeed
			}
			if !(acount >= minGallop || bcount >= minGallop) {
				break
			}
		}
		ms.minGallop++
	}
succeed:
	if nb > 0 {
		copy(list[dest-(nb-1):dest+1], tmp[:nb])
	}
	return
copyA:
	dest -= na
	sa -= na
	copy(list[dest+1:dest+1+na], list[sa+1:sa+1+na])
	list[dest] = tmp[sb]
}
