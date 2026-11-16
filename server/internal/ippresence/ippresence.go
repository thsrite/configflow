// Package ippresence is backend/common/rule_ip_presence.py (routers R02):
// whether a classical rule set really has IP lines.
//
// OxiDNS can only match IP rules against an answer, so every IP group makes
// each query that no domain rule claimed go to the local upstream first. A
// classical list such as TikTok.list has no IP lines at all and must not
// cause that.
//
// The answer comes from the content OxiDNS would download: the validated
// library cache for remote sources (never the network) or the inline
// content, split by the same function rule-proxy serves. Unknown content
// returns nil (None), and callers then keep the previous assumption that a
// classical set has IP lines.
//
// Nobody imports this package: internal/oxidns reaches ClassicalIPLines
// through oxidns.SetClassicalIPLines, registered by Wire (listed in
// server.wirePackages), which also registers routers R02's
// convert.MihomoHooks.IsFullTun.
package ippresence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/store"
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

// ClearMemo is rule_ip_presence._MEMO.clear() (tests).
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

// remember is _remember(key, value): the memo is cleared when full.
func remember(key memoKey, value bool) bool {
	memoMu.Lock()
	defer memoMu.Unlock()
	if len(memo) >= memoLimit {
		memo = map[memoKey]bool{}
	}
	memo[key] = value
	return value
}

// hasIPLines is _has_ip_lines(text). The error is the TypeError of
// convert_rule_content_for_mosdns (a scalar YAML payload).
func hasIPLines(text string) (bool, error) {
	converted, err := mosdns.ConvertRuleContentForMosDNS(text, mosdns.PartIP)
	if err != nil {
		return false, err
	}
	// An empty part comes back as a "# no ip rules" placeholder comment.
	for _, line := range convert.PySplitlines(converted) {
		if pystr.Strip(line) != "" && !strings.HasPrefix(strings.TrimLeftFunc(line, pystr.IsSpace), "#") {
			return true, nil
		}
	}
	return false, nil
}

// PyError is a Python exception rule_ip_presence.py does not catch.
type PyError struct{ Class, Message string }

func (e *PyError) Error() string       { return e.Message }
func (e *PyError) PythonClass() string { return e.Class }

func typeName(v any) string {
	switch value := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if value.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

func asDict(v any) (*jsonx.Object, error) {
	if o, ok := v.(*jsonx.Object); ok && o != nil {
		return o, nil
	}
	return nil, &PyError{"AttributeError", "'" + typeName(v) + "' object has no attribute 'get'"}
}

// declaration is _declaration(item, config): the library entry a profile
// rule set points at, as rule-proxy resolves it.
func declaration(item, config *jsonx.Object) (*jsonx.Object, error) {
	libraryID := item.Value("library_rule_id")
	if libraryID == nil {
		return item, nil
	}
	library := config.Value("rule_library")
	if !jsonx.Truthy(library) {
		return item, nil
	}
	var entries []any
	switch value := library.(type) {
	case []any:
		entries = value
	case *jsonx.Object:
		entries = jsonx.ListOf(value.Keys())
	case string:
		for _, r := range value {
			entries = append(entries, string(r))
		}
	default:
		return nil, &PyError{"TypeError", "'" + typeName(library) + "' object is not iterable"}
	}
	for _, v := range entries {
		if entry, ok := v.(*jsonx.Object); ok && jsonx.Equal(entry.Value("id"), libraryID) {
			return entry, nil
		}
	}
	return item, nil
}

// caught reports whether Python's `except (OSError, TypeError, ValueError)`
// around ruleset_format / rule_cache_read_path / os.stat catches err.
func caught(err error) bool {
	if errors.Is(err, store.ErrStorage) || repo.IsValueError(err) {
		return true
	}
	var classed interface{ PythonClass() string }
	if errors.As(err, &classed) {
		switch classed.PythonClass() {
		case "TypeError", "ValueError", "OSError", "FileNotFoundError", "PermissionError",
			"IsADirectoryError", "NotADirectoryError", "UnicodeDecodeError", "UnicodeEncodeError",
			"ProfileValidationError", "ProfileExists", "JSONDecodeError":
			return true
		}
	}
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}

// ClassicalIPLines is classical_ip_lines(item, config): true or false for a
// classical set whose content is known, else nil (None). item and config
// are dicts (*jsonx.Object); anything else is Python's AttributeError. The
// repository (rules directory, stored rule library) comes from ctx as in
// rulefetch.
func ClassicalIPLines(ctx context.Context, itemValue, configValue any) (any, error) {
	item, err := asDict(itemValue)
	if err != nil {
		return nil, err
	}
	library := item.Value("library_rule_id")
	var config *jsonx.Object
	if library != nil {
		if config, err = asDict(configValue); err != nil {
			return nil, err
		}
		if item, err = declaration(item, config); err != nil {
			return nil, err
		}
	}
	sourceType := item.GetOr("source_type", "url")
	if jsonx.Equal(sourceType, "content") {
		text, ok := item.Value("content").(string)
		if !ok {
			return nil, nil
		}
		sum := sha256.Sum256([]byte(text))
		key := memoKey{kind: "content", value: hex.EncodeToString(sum[:])}
		if value, ok := lookup(key); ok {
			return value, nil
		}
		has, err := hasIPLines(text)
		if err != nil {
			return nil, err
		}
		return remember(key, has), nil
	}
	if !jsonx.Equal(sourceType, "url") || !jsonx.Truthy(item.Value("url")) {
		return nil, nil
	}
	if config == nil {
		if config, err = asDict(configValue); err != nil {
			return nil, err
		}
	}
	format, err := convert.RulesetFormat(item, config, "")
	if err != nil {
		if caught(err) {
			return nil, nil
		}
		return nil, err
	}
	if format == "mrs" {
		return nil, nil
	}
	// rule_cache_read_path(item, config.get('rule_library')): None reads the
	// stored library. A non-empty string or dict already failed in
	// ruleset_format (AttributeError) and a number or bool there (caught
	// TypeError), so only empty ones arrive here.
	var rules []any
	switch value := config.Value("rule_library").(type) {
	case nil:
	case []any:
		rules = value
		if rules == nil {
			rules = []any{}
		}
	case string, *jsonx.Object:
		rules = []any{}
	default:
		return nil, nil
	}
	path, err := rulefetch.RuleCacheReadPath(ctx, item, rules)
	if err != nil {
		if caught(err) {
			return nil, nil
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	key := memoKey{kind: "cache", value: path, mtime: info.ModTime().UnixNano(), size: info.Size()}
	if value, ok := lookup(key); ok {
		return value, nil
	}
	data, err := rulefetch.ReadValidatedRuleCache(ctx, item, config)
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
	return remember(key, has), nil
}
