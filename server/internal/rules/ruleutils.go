package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"configflow/internal/jsonx"
)

// Rule cache file names under DATA_DIR/shared/rules/{by-id,by-source,by-name}/
// (backend/utils/rule_utils.py) and the read-side path resolution. The names
// must stay those Python wrote, so existing caches are found. Fetching and
// writing caches is in internal/rulefetch.

// SanitizeRuleName is a file-system safe file name (without extension) for
// a rule name; legacy caches were named by it.
func SanitizeRuleName(name string) string {
	if name == "" {
		return "unnamed"
	}
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	safe = strings.Trim(safe, ". ")
	if safe == "" {
		return "unnamed"
	}
	if runes := []rune(safe); len(runes) > 200 {
		safe = string(runes[:200])
	}
	return safe
}

func sanitizeAny(name any) string {
	text, _ := name.(string)
	return SanitizeRuleName(text)
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// RuleCacheFilename is the stable, resource-identity
// based cache file name relative to the rules directory.
func RuleCacheFilename(rule *jsonx.Object) string {
	identity := rule.Value("library_rule_id")
	if !jsonx.Truthy(identity) {
		identity = rule.Value("id")
	}
	namespace := "by-id"
	var text string
	if identity != nil {
		text = jsonx.Str(identity)
	} else {
		namespace = "by-name"
		text = jsonx.Str(rule.GetOr("name", ""))
	}
	sourceType := rule.GetOr("source_type", "url")
	if sourceType == "url" && jsonx.Truthy(rule.Value("url")) {
		// Bind remote bytes to the declaration as well as the resource ID: a
		// delayed refresh of an old URL/type cannot replace the current cache.
		namespace = "by-source"
		format := rule.Value("format")
		if !jsonx.Truthy(format) {
			format = ""
		}
		// The JSON text (json.dumps spacing, non-ASCII kept) is part of the name.
		encoded, _ := jsonx.Encode([]any{text, rule.Value("url"), rule.GetOr("behavior", "domain"), format}, jsonx.Options{})
		text = string(encoded)
	}
	return namespace + "/" + sha256Hex(text) + ".list"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RuleCacheReadPath is the cache file to read: new caches first, retaining
// unambiguous legacy caches offline. library is the library to disambiguate
// legacy names against (nil: use stored); stored is
// the current shared rule_library from the repository.
func RuleCacheReadPath(rule *jsonx.Object, library, stored []any, rulesDir string) string {
	path := filepath.Join(rulesDir, RuleCacheFilename(rule))
	if fileExists(path) {
		return path
	}
	if library == nil {
		library = stored
	}
	// Migrate pre-validation identity caches only for the stored declaration;
	// proposed edits and stale publication snapshots cannot claim these bytes.
	var current *jsonx.Object
	for _, entry := range stored {
		if item, ok := entry.(*jsonx.Object); ok && jsonx.Equal(item.Value("id"), rule.Value("id")) {
			current = item
			break
		}
	}
	fields := []string{"url", "behavior", "format", "source_type"}
	same := func() bool {
		for _, key := range fields {
			if !jsonx.Equal(current.Value(key), rule.Value(key)) {
				return false
			}
		}
		return true
	}
	if current != nil && same() {
		identity := rule.Value("library_rule_id")
		if !jsonx.Truthy(identity) {
			identity = rule.Value("id")
		}
		old := filepath.Join(rulesDir, "by-id", sha256Hex(jsonx.Str(identity))+".list")
		if fileExists(old) {
			return old
		}
	}
	if rule.GetOr("source_type", "url") == "url" && jsonx.Truthy(rule.Value("url")) && (current == nil || !same()) {
		return path
	}
	name := rule.GetOr("name", "")
	legacyName := sanitizeAny(name)
	var matches []*jsonx.Object
	for _, entry := range library {
		if item, ok := entry.(*jsonx.Object); ok && sanitizeAny(item.GetOr("name", "")) == legacyName {
			matches = append(matches, item)
		}
	}
	// A legacy file has no identity receipt. Never attribute a shared filename
	// to either of two resources, or to an unknown spelling of a known name.
	if len(matches) > 1 || (len(matches) == 1 && !jsonx.Equal(matches[0].Value("name"), name)) {
		return path
	}
	legacy := filepath.Join(rulesDir, legacyName+".list")
	if fileExists(legacy) {
		return legacy
	}
	return path
}

// TouchUnchangedCache: when the file and its .bak
// copy already hold exactly content, only the modification time moves (the
// cache refresh time shown in the UI) and true is returned.
func TouchUnchangedCache(path string, content []byte) bool {
	for _, name := range []string{path, path + ".bak"} {
		existing, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(existing, content) {
			return false
		}
	}
	now := time.Now()
	return os.Chtimes(path, now, now) == nil
}
