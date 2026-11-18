package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"configflow/internal/model"
)

// Rule cache file names under DATA_DIR/shared/rules/{by-id,by-source,by-name}/
// (backend/utils/rule_utils.py) and the read-side path resolution. The names
// retain unambiguous identity/name cache fallbacks. Fetching and
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

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// CacheFilename binds remote bytes to both the resource and its declaration.
// D2 uses native JSON for the declaration digest; old identity caches remain
// readable only when the current stored declaration proves ownership.
func CacheFilename(rule model.RuleConfig) string {
	identity := rule.LibraryRuleID
	if identity == "" {
		identity = rule.ID
	}
	namespace, text := "by-id", identity
	if identity == "" {
		// An explicitly stored empty ID remains an identity; an absent or
		// null ID uses the name. Invalid typed IDs follow R5.
		data, _ := model.Marshal(rule)
		var members map[string]json.RawMessage
		_ = json.Unmarshal(data, &members)
		raw, present := members["id"]
		if !present || string(raw) == "null" || model.IssueUnder(model.Issues(rule), "id") {
			namespace, text = "by-name", rule.Name
		}
	}
	if valueOr(rule.SourceType, "url") == "url" && valueOr(rule.URL, "") != "" {
		namespace = "by-source"
		data, _ := json.Marshal([4]string{text, *rule.URL, valueOr(rule.Behavior, "domain"), valueOr(rule.Format, "")})
		text = string(data)
	}
	return namespace + "/" + sha256Hex(text) + ".list"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CacheReadPath is the cache file to read: new caches first, retaining
// unambiguous legacy caches offline. library is the library to disambiguate
// legacy names against (nil: use stored); stored is
// the current shared rule_library from the repository.
func CacheReadPath(rule model.RuleConfig, library, stored []model.RuleLibraryItem, rulesDir string) string {
	path := filepath.Join(rulesDir, CacheFilename(rule))
	// A handwritten edit marks its stable cache before committing the DB. A
	// failed replacement must not resurrect either the stable or legacy bytes.
	// Only a confirmed absence permits fallback. A permission/I/O failure while
	// inspecting the receipt cannot establish that the old bytes are current.
	if _, err := os.Stat(path + ".invalid"); !errors.Is(err, os.ErrNotExist) {
		return filepath.Join(path+".invalid", "unavailable")
	}
	if fileExists(path) {
		return path
	}
	if library == nil {
		library = stored
	}
	// Migrate pre-validation identity caches only for the stored declaration;
	// proposed edits and stale publication snapshots cannot claim these bytes.
	var current *model.RuleLibraryItem
	for i := range stored {
		if stored[i].ID == rule.ID {
			current = &stored[i]
			break
		}
	}
	same := func() bool {
		return sameOptional(current.URL, rule.URL) && sameOptional(current.Behavior, rule.Behavior) && sameOptional(current.Format, rule.Format) && sameOptional(current.SourceType, rule.SourceType)
	}
	if current != nil && same() {
		// This fixed-schema digest is read-only migration metadata, never a
		// writer's filename. Do not attribute an old declaration to a new one.
		if oldName, ok := LegacyCacheFilename(rule); ok {
			old := filepath.Join(rulesDir, oldName)
			if _, err := os.Stat(old + ".invalid"); !errors.Is(err, os.ErrNotExist) {
				return filepath.Join(old+".invalid", "unavailable")
			}
			if fileExists(old) {
				return old
			}
		}
		identity := rule.LibraryRuleID
		if identity == "" {
			identity = rule.ID
		}
		old := filepath.Join(rulesDir, "by-id", sha256Hex(identity)+".list")
		if fileExists(old) {
			return old
		}
	}
	if valueOr(rule.SourceType, "url") == "url" && valueOr(rule.URL, "") != "" && (current == nil || !same()) {
		return path
	}
	name := rule.Name
	legacyName := SanitizeRuleName(name)
	matches := 0
	matchedName := ""
	for _, item := range library {
		if SanitizeRuleName(item.Name) == legacyName {
			matches++
			matchedName = item.Name
		}
	}
	if matches > 1 || (matches == 1 && matchedName != name) {
		return path
	}
	legacy := filepath.Join(rulesDir, legacyName+".list")
	if fileExists(legacy) {
		return legacy
	}
	return path
}

func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// LibraryRule projects the declaration fields used by cache and fetch into a
// rule reference, retaining opaque extension fields without dynamic values.
func LibraryRule(item model.RuleLibraryItem) model.RuleConfig {
	return model.RuleConfig{ID: item.ID, Name: item.Name, Enabled: item.Enabled, SourceType: item.SourceType, Behavior: item.Behavior, Format: item.Format, URL: item.URL, Content: item.Content, Fields: item.Fields}
}

// LegacyCacheFilename recognizes the previous fixed-schema cache key solely
// for one-time migration. Writers always use CacheFilename. Stored invalid
// declaration types are never eligible to claim an older file.
func LegacyCacheFilename(rule model.RuleConfig) (string, bool) {
	for _, field := range []string{"id", "library_rule_id", "url", "behavior", "format", "source_type"} {
		if model.IssueUnder(model.Issues(rule), field) {
			return "", false
		}
	}
	if valueOr(rule.SourceType, "url") != "url" || valueOr(rule.URL, "") == "" {
		return "", false
	}
	data, err := model.Marshal(rule)
	if err != nil {
		return "", false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) != nil {
		return "", false
	}
	// Preserve explicit nulls in old declarations. Only the current stored
	// declaration may use this read-side address (see CacheReadPath).
	if raw, ok := members["source_type"]; ok && string(raw) != "\"url\"" {
		return "", false
	}
	id := rule.LibraryRuleID
	if id == "" {
		id = rule.ID
	}
	if id == "" {
		id = rule.Name
	}
	behavior := members["behavior"]
	if len(behavior) == 0 {
		behavior = json.RawMessage(`"domain"`)
	}
	format := valueOr(rule.Format, "")
	quote := func(value string) string {
		var b bytes.Buffer
		encoder := json.NewEncoder(&b)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(value)
		return strings.TrimSuffix(b.String(), "\n")
	}
	// Native scalar encoders plus the historical four-field separators; this
	// is not a general legacy JSON serializer and is never used for new writes.
	text := "[" + quote(id) + ", " + quote(*rule.URL) + ", " + string(behavior) + ", " + quote(format) + "]"
	return "by-source/" + sha256Hex(text) + ".list", true
}
