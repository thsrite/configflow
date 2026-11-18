package subs

import (
	_ "embed"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// The public suffix list snapshot bundled with tldextract 5.1.2 (the
// version pinned in backend/requirements.txt), byte for byte. The Python code
// builds its extractors with suffix_list_urls=() and cache_dir=None, so it
// only ever uses this snapshot; using the same file keeps the results equal
// (golang.org/x/net/publicsuffix carries a newer list: 1171 of 27173 corpus
// names differed).
//
//go:embed data/tldextract-5.1.2-snapshot.dat
var tldSnapshot string

type suffixTrie struct {
	matches map[string]*suffixTrie
	end     bool
	private bool
}

func (t *suffixTrie) add(suffix string, private bool) {
	node := t
	labels := strings.Split(suffix, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		next := node.matches[labels[i]]
		if next == nil {
			next = &suffixTrie{matches: map[string]*suffixTrie{}}
			node.matches[labels[i]] = next
		}
		node = next
	}
	node.end = true
	node.private = private
}

// PUBLIC_SUFFIX_RE of tldextract.suffix_list (\w is Unicode in Python).
var publicSuffixRE = regexp.MustCompile(`(?m)^([.*!]*[\p{L}\p{N}_]\S*)`)

var (
	suffixOnce        sync.Once
	suffixTrieInclPSL *suffixTrie
	suffixTrieExclPSL *suffixTrie
)

func loadSuffixTries() {
	public, private, _ := strings.Cut(tldSnapshot, "// ===BEGIN PRIVATE DOMAINS===")
	find := func(text string) []string {
		var out []string
		for _, m := range publicSuffixRE.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1])
		}
		return out
	}
	publicTLDs, privateTLDs := find(public), find(private)
	suffixTrieInclPSL = &suffixTrie{matches: map[string]*suffixTrie{}}
	suffixTrieExclPSL = &suffixTrie{matches: map[string]*suffixTrie{}}
	for _, s := range publicTLDs {
		suffixTrieInclPSL.add(s, false)
		suffixTrieExclPSL.add(s, false)
	}
	for _, s := range privateTLDs {
		suffixTrieInclPSL.add(s, true)
	}
}

// decodePunycode is tldextract's _decode_punycode: the lowercase label, an
// xn-- label decoded. Python's idna.decode also validates the decoded label
// (IDNA 2008) and keeps the xn-- text when that fails; that check is left
// out: every rule of the list is a valid label, so an invalid decoded label
// matches no rule either way.
func decodePunycode(label string) string {
	lowered := strings.ToLower(label)
	if strings.HasPrefix(lowered, "xn--") {
		if u, ok := punycodeDecode(lowered[4:]); ok {
			return u
		}
	}
	return lowered
}

// punycodeDecode is the RFC 3492 decoder.
func punycodeDecode(s string) (string, bool) {
	const (
		base, tmin, tmax, skew, damp = 36, 1, 26, 38, 700
		initialBias, initialN        = 72, 128
	)
	var out []rune
	rest := s
	if i := strings.LastIndexByte(s, '-'); i >= 0 {
		for _, r := range s[:i] {
			if r >= utf8.RuneSelf {
				return "", false
			}
			out = append(out, r)
		}
		rest = s[i+1:]
	}
	if rest == "" {
		return "", false
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
	n, bias, i := initialN, initialBias, 0
	for pos := 0; pos < len(rest); {
		oldi, w := i, 1
		for k := base; ; k += base {
			if pos >= len(rest) {
				return "", false
			}
			c := rest[pos]
			pos++
			var digit int
			switch {
			case c >= '0' && c <= '9':
				digit = int(c-'0') + 26
			case c >= 'a' && c <= 'z':
				digit = int(c - 'a')
			case c >= 'A' && c <= 'Z':
				digit = int(c - 'A')
			default:
				return "", false
			}
			if digit > (1<<31-1-i)/w {
				return "", false
			}
			i += digit * w
			t := k - bias
			if t < tmin {
				t = tmin
			} else if t > tmax {
				t = tmax
			}
			if digit < t {
				break
			}
			if w > (1<<31-1)/(base-t) {
				return "", false
			}
			w *= base - t
		}
		bias = adapt(i-oldi, len(out)+1, oldi == 0)
		n += i / (len(out) + 1)
		i %= len(out) + 1
		if n > utf8.MaxRune || n >= 0xd800 && n <= 0xdfff {
			return "", false
		}
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = rune(n)
		i++
	}
	return string(out), true
}

// suffixIndex is _PublicSuffixListTLDExtractor.suffix_index.
func suffixIndex(labels []string, includePrivate bool) int {
	node := suffixTrieExclPSL
	if includePrivate {
		node = suffixTrieInclPSL
	}
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

var ipv4RE = regexp.MustCompile(`^(?:(?:[0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}(?:[0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`)

const schemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

// schemelessURL is tldextract.remote._schemeless_url.
func schemelessURL(url string) string {
	at := strings.Index(url, "//")
	if at == 0 {
		return url[2:]
	}
	if at < 2 || url[at-1] != ':' {
		return url
	}
	for _, r := range url[:at-1] {
		if !strings.ContainsRune(schemeChars, r) {
			return url
		}
	}
	return url[at+2:]
}

func partitionHead(s, sep string) string {
	head, _, _ := strings.Cut(s, sep)
	return head
}

// lenientNetloc is tldextract.remote.lenient_netloc.
func lenientNetloc(url string) string {
	after := partitionHead(partitionHead(partitionHead(schemelessURL(url), "/"), "?"), "#")
	if i := strings.LastIndex(after, "@"); i >= 0 {
		after = after[i+1:]
	}
	if strings.HasPrefix(after, "[") {
		if head, _, ok := strings.Cut(after, "]"); ok {
			return head + "]"
		}
	}
	host := strings.TrimSpace(partitionHead(after, ":"))
	return strings.TrimRight(host, ".。．｡")
}

// ExtractDomain is TLDExtract(suffix_list_urls=(), cache_dir=None,
// include_psl_private_domains=includePrivate)(url): the subdomain,
// registrable label and public suffix of a host or URL per tldextract
// 5.1.2's bundled list. Labels keep the case they were given.
func ExtractDomain(url string, includePrivate bool) (subdomain, domain, suffix string) {
	suffixOnce.Do(loadSuffixTries)
	netloc := lenientNetloc(url)
	netloc = strings.NewReplacer("。", ".", "．", ".", "｡", ".").Replace(netloc)
	if len([]rune(netloc)) >= 4 && strings.HasPrefix(netloc, "[") && strings.HasSuffix(netloc, "]") {
		if a, err := netip.ParseAddr(netloc[1 : len(netloc)-1]); err == nil && a.Is6() {
			return "", netloc, ""
		}
	}
	labels := strings.Split(netloc, ".")
	index := suffixIndex(labels, includePrivate)
	if index == len(labels) && len(labels) == 4 && netloc != "" && netloc[0] >= '0' && netloc[0] <= '9' && ipv4RE.MatchString(netloc) {
		return "", netloc, ""
	}
	if index != len(labels) {
		suffix = strings.Join(labels[index:], ".")
	}
	if index >= 2 {
		subdomain = strings.Join(labels[:index-1], ".")
	}
	if index > 0 {
		domain = labels[index-1]
	}
	return subdomain, domain, suffix
}
