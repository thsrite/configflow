// Package ippresence determines whether a classical rule set really has IP
// lines (routers R02).
//
// OxiDNS can only match IP rules against an answer, so every IP group makes
// each query that no domain rule claimed go to the local upstream first. A
// classical list such as TikTok.list has no IP lines at all and must not
// cause that.
//
// The answer comes from the content OxiDNS would download: the validated
// library cache for remote sources (never the network) or the inline
// content, split by the same function rule-proxy serves. Unknown content
// returns nil, and callers then keep the previous assumption that a
// classical set has IP lines.
//
// internal/oxidns imports this package and calls PeekTypedClassicalIPLines
// directly (O17 W0); routers R02's convert.MihomoIntegrations.FullTun is
// installed by internal/gateway.
package ippresence

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/mosdns"
)

const memoLimit = 1024

type memoKey struct {
	kind  string // "content" or "cache"
	value string // sha256 hex or path
	mtime int64
	size  int64
}

var (
	memoMu sync.Mutex
	memo   = map[memoKey]bool{}
)

// ClearMemo clears the in-process IP presence memo cache (used by tests).
func ClearMemo() {
	memoMu.Lock()
	memo = map[memoKey]bool{}
	memoMu.Unlock()
}

func lookup(key memoKey) (bool, bool) {
	memoMu.Lock()
	defer memoMu.Unlock()
	value, ok := memo[key]
	return value, ok
}

// remember caches a presence decision and resets the map when full.
func remember(key memoKey, value bool) bool {
	memoMu.Lock()
	defer memoMu.Unlock()
	if len(memo) >= memoLimit {
		memo = map[memoKey]bool{}
	}
	memo[key] = value
	return value
}

func hasIPLines(text string) (bool, error) {
	converted, err := mosdns.ConvertRuleContentForMosDNS(text, mosdns.PartIP)
	if err != nil {
		return false, err
	}
	// An empty part comes back as a "# no ip rules" placeholder comment.
	for _, line := range convert.RuleLines(converted) {
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
		if strings.TrimSpace(trimmed) != "" && !strings.HasPrefix(trimmed, "#") {
			return true, nil
		}
	}
	return false, nil
}

func inlineIPLines(text string) (*bool, error) {
	sum := sha256.Sum256([]byte(text))
	key := memoKey{kind: "content", value: hex.EncodeToString(sum[:])}
	if value, ok := lookup(key); ok {
		return &value, nil
	}
	has, err := hasIPLines(text)
	if err != nil {
		return nil, err
	}
	remember(key, has)
	return &has, nil
}

func cachedIPLines(path string, read func() ([]byte, error)) (*bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	key := memoKey{kind: "cache", value: path, mtime: info.ModTime().UnixNano(), size: info.Size()}
	if value, ok := lookup(key); ok {
		return &value, nil
	}
	data, err := read()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	if !utf8.Valid(data) {
		return nil, nil
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	has, err := hasIPLines(text)
	if err != nil {
		return nil, err
	}
	remember(key, has)
	return &has, nil
}
