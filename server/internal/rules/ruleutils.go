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

// Port of the pure parts of backend/utils/rule_utils.py: the names of the rule
// cache files under DATA_DIR/shared/rules/{by-id,by-source,by-name}/ and the
// read-side path resolution. get_rules_dir() is the caller's `rulesDir`
// argument here; the repository-backed functions (get_rules_dir,
// read/write_validated_rule_cache, refresh_remote_rule,
// read_rule_source_content, save_rule_to_local) are in internal/rulefetch.

// SanitizeRuleName is sanitize_rule_name: a file-system safe name without
// extension.
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

// RuleCacheFilename is rule_cache_filename: the stable, resource-identity
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
		encoded, _ := jsonx.Encode([]any{text, rule.Value("url"), rule.GetOr("behavior", "domain"), format}, jsonx.Options{})
		text = string(encoded)
	}
	return namespace + "/" + sha256Hex(text) + ".list"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RuleCacheReadPath is rule_cache_read_path: new caches first, retaining
// unambiguous legacy caches offline. library is the library to disambiguate
// legacy names against (nil means "use stored", like Python's None); stored is
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

// TouchUnchangedCache is _touch_unchanged_cache: when the file and its .bak
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
