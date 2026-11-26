package subs

import (
	"runtime"
	"strings"
	"testing"
)

// legacyTrie is the pre-R13 layout (two separate tries, a map on every
// node), kept here as the reference for the shared trie.
type legacyTrie struct {
	matches map[string]*legacyTrie
	end     bool
}

func (t *legacyTrie) add(suffix string) {
	node := t
	labels := strings.Split(suffix, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		next := node.matches[labels[i]]
		if next == nil {
			next = &legacyTrie{matches: map[string]*legacyTrie{}}
			node.matches[labels[i]] = next
		}
		node = next
	}
	node.end = true
}

func legacySuffixIndex(root *legacyTrie, labels []string) int {
	node := root
	i := len(labels)
	j := i
	for k := len(labels) - 1; k >= 0; k-- {
		decoded := decodePunycode(labels[k])
		if next, ok := node.matches[decoded]; ok {
			j--
			node = next
			if node.end {
				i = j
			}
			continue
		}
		if _, wildcard := node.matches["*"]; wildcard {
			if _, exception := node.matches["!"+decoded]; exception {
				return j
			}
			return j - 1
		}
		break
	}
	return i
}

// suffixRules returns the public and private rules of the bundled snapshot.
func suffixRules(t testing.TB) (public, private []string) {
	t.Helper()
	pub, priv, _ := strings.Cut(tldSnapshot, "// ===BEGIN PRIVATE DOMAINS===")
	for _, m := range publicSuffixRE.FindAllStringSubmatch(pub, -1) {
		public = append(public, m[1])
	}
	for _, m := range publicSuffixRE.FindAllStringSubmatch(priv, -1) {
		private = append(private, m[1])
	}
	return public, private
}

func buildLegacy(public, private []string) (incl, excl *legacyTrie) {
	incl = &legacyTrie{matches: map[string]*legacyTrie{}}
	excl = &legacyTrie{matches: map[string]*legacyTrie{}}
	for _, s := range public {
		incl.add(s)
		excl.add(s)
	}
	for _, s := range private {
		incl.add(s)
	}
	return incl, excl
}

// TestSuffixIndexMatchesLegacyTries checks the shared trie against two
// separate tries on names derived from every rule of the snapshot (the rule,
// one and two labels below it, wildcards filled in, exceptions, parents),
// with and without the private section.
func TestSuffixIndexMatchesLegacyTries(t *testing.T) {
	suffixOnce.Do(loadSuffixTries)
	public, private := suffixRules(t)
	incl, excl := buildLegacy(public, private)
	var names []string
	for _, rule := range append(append([]string{}, public...), private...) {
		rule = strings.TrimLeft(rule, "!")
		filled := strings.ReplaceAll(rule, "*", "wild")
		names = append(names, rule, filled, "a."+filled, "b.a."+filled, "A."+strings.ToUpper(filled))
		if _, parent, ok := strings.Cut(filled, "."); ok {
			names = append(names, "x."+parent, parent)
		}
	}
	names = append(names, "", "example", "example.invalid", "192.0.2.1", "a..b", "xn--fiqs8s", "a.xn--fiqs8s",
		"a.b.c.d.e.example.com")
	tests := []struct {
		name           string
		includePrivate bool
		legacy         *legacyTrie
	}{
		{"with private section", true, incl},
		{"public only", false, excl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := 0
			for _, name := range names {
				labels := strings.Split(name, ".")
				got, want := suffixIndex(labels, tt.includePrivate), legacySuffixIndex(tt.legacy, labels)
				if got != want {
					diffs++
					if diffs <= 20 {
						t.Errorf("suffixIndex(%q) = %d, legacy tries %d", name, got, want)
					}
				}
			}
			t.Logf("%d names, %d differences", len(names), diffs)
		})
	}
}

// TestSuffixTrieMemory guards the R13 saving: the shared trie must stay well
// below the two legacy tries it replaces (it stays live once loaded).
func TestSuffixTrieMemory(t *testing.T) {
	public, private := suffixRules(t)
	heapBytes := func(build func() any) uint64 {
		t.Helper()
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		kept := build()
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(kept)
		return after.HeapAlloc - before.HeapAlloc
	}
	shared := heapBytes(func() any {
		root := &suffixTrie{}
		for _, s := range public {
			root.add(s, false)
		}
		for _, s := range private {
			root.add(s, true)
		}
		return root
	})
	legacy := heapBytes(func() any {
		incl, excl := buildLegacy(public, private)
		return [2]*legacyTrie{incl, excl}
	})
	t.Logf("shared trie %d KB, legacy tries %d KB", shared>>10, legacy>>10)
	if shared*2 > legacy {
		t.Errorf("shared trie %d bytes, want under half of the legacy %d bytes", shared, legacy)
	}
}
