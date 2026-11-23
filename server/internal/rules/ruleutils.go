package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"configflow/internal/digest"
	"configflow/internal/model"
	"configflow/internal/pathx"
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
		raw, present, _ := model.EncodeMember(&rule, "id")
		if !present || string(raw) == "null" {
			namespace, text = "by-name", rule.Name
		}
	}
	if valueOr(rule.SourceType, "url") == "url" && valueOr(rule.URL, "") != "" {
		namespace = "by-source"
		data, _ := json.Marshal([4]string{text, *rule.URL, valueOr(rule.Behavior, "domain"), valueOr(rule.Format, "")})
		text = string(data)
	}
	return namespace + "/" + digest.SHA256HexString(text) + ".list"
}

// CacheReadPath is the cache file to read: new caches first, retaining
// unambiguous legacy caches offline. library is the library to disambiguate
// legacy names against (nil: use stored); stored is
// the current shared rule_library from the repository.
// An empty result means a receipt or path boundary forbids reading the cache.
func CacheReadPath(rule model.RuleConfig, library, stored []model.RuleLibraryItem, rulesDir string) string {
	return cacheReadPath(rule, library, stored, rulesDir, func() bool { return cacheSourceOwned(rule, stored, nil) })
}

// StoredCacheReadPath recognizes legacy cache ownership from the current stored
// library and profile-local URL declarations, never a caller's proposed snapshot.
func StoredCacheReadPath(rule model.RuleConfig, library []model.RuleLibraryItem, document *model.Document, rulesDir string) string {
	if document == nil {
		return CacheReadPath(rule, library, nil, rulesDir)
	}
	return cacheReadPath(rule, library, document.Shared.RuleLibrary, rulesDir, func() bool { return OwnsCacheSource(rule, document) })
}

// OwnsCacheSource reports whether the current stored document owns the exact
// source declaration. It does not confer permission to migrate or write a cache.
func OwnsCacheSource(rule model.RuleConfig, document *model.Document) bool {
	return document != nil && cacheSourceOwned(rule, document.Shared.RuleLibrary, document.Profiles)
}

func cacheSourceOwned(rule model.RuleConfig, stored []model.RuleLibraryItem, profiles []*model.Profile) bool {
	for _, item := range stored {
		if item.ID == rule.ID && originalMemberEqual(LibraryRule(item), rule, "id") {
			if SameSourceDeclaration(LibraryRule(item), rule) {
				return true
			}
			break
		}
	}
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		records := profile.RuleConfigs
		if len(records) == 0 {
			records = nil
			// Old profiles can store URL rules under either legacy collection. Decode
			// only those members, once per legacy miss, not the complete configuration.
			for _, name := range []string{"rules", "rule_sets"} {
				if raw, ok := profile.LegacyRuleList(name); ok {
					var entries []model.RuleConfig
					if json.Unmarshal(raw, &entries) == nil {
						records = append(records, entries...)
					}
				}
			}
		}
		for _, item := range records {
			if item.LibraryRuleID != "" || (item.ItemType != "" && item.ItemType != "ruleset") || valueOr(item.SourceType, "url") != "url" || valueOr(item.URL, "") == "" {
				continue
			}
			if !originalMemberEqual(item, rule, "id") || (item.ID == "" && !originalMemberEqual(item, rule, "name")) {
				continue
			}
			if SameSourceDeclaration(item, rule) {
				return true
			}
		}
	}
	return false
}

func cacheReadPath(rule model.RuleConfig, library, stored []model.RuleLibraryItem, rulesDir string, owns func() bool) (selected string) {
	defer func() {
		// Names are sanitized/hashes, but an existing cache or namespace can
		// still be a symlink. Never lend bytes from outside the rules root to
		// a matcher or to the one-time migration writer.
		resolved, err := filepath.EvalSymlinks(selected)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				selected = ""
			}
			return
		}
		root, err := filepath.EvalSymlinks(rulesDir)
		if err != nil {
			selected = ""
			return
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || !filepath.IsLocal(relative) {
			selected = ""
		}
	}()
	path := filepath.Join(rulesDir, CacheFilename(rule))
	// A handwritten edit marks its stable cache before committing the DB. A
	// failed replacement must not resurrect either the stable or legacy bytes.
	// Only a confirmed absence permits fallback. A permission/I/O failure while
	// inspecting the receipt cannot establish that the old bytes are current.
	if _, err := os.Lstat(path + ".invalid"); !errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if pathx.Accessible(path) {
		return path
	}
	if library == nil {
		library = stored
	}
	// Legacy identity/source caches require a current stored owner. A
	// captured snapshot or a proposed edit is not evidence of ownership.
	claimed := owns()
	if claimed {
		// This fixed-schema digest is read-only migration metadata, never a
		// writer's filename. Do not attribute an old declaration to a new one.
		if oldName, ok := LegacyCacheFilename(rule); ok {
			old := filepath.Join(rulesDir, oldName)
			if _, err := os.Lstat(old + ".invalid"); !errors.Is(err, os.ErrNotExist) {
				return ""
			}
			if pathx.Accessible(old) {
				return old
			}
		}
		identity := rule.LibraryRuleID
		if identity == "" {
			identity = rule.ID
		}
		old := filepath.Join(rulesDir, "by-id", digest.SHA256HexString(identity)+".list")
		if _, err := os.Lstat(old + ".invalid"); !errors.Is(err, os.ErrNotExist) {
			return ""
		}
		if pathx.Accessible(old) {
			return old
		}
	}
	if valueOr(rule.SourceType, "url") == "url" && valueOr(rule.URL, "") != "" && !claimed {
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
	if _, err := os.Lstat(legacy + ".invalid"); !errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if pathx.Accessible(legacy) {
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
	// This is a record-sized native projection, not a resolved Config decode.
	// Preserve original declaration evidence for cache ownership/type checks.
	data, _ := model.Marshal(item)
	var rule model.RuleConfig
	_ = json.Unmarshal(data, &rule)
	return rule
}

// LegacyCacheFilename recognizes the previous fixed-schema cache key solely
// for one-time migration. Writers always use CacheFilename. Stored invalid
// declaration types are never eligible to claim an older file.
func LegacyCacheFilename(rule model.RuleConfig) (string, bool) {
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
	for _, field := range []string{"id", "library_rule_id", "url", "behavior", "format", "source_type"} {
		raw, present := members[field]
		if !present || string(raw) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return "", false
		}
	}
	// Preserve explicit nulls in old declarations. Only the current stored
	// declaration may use this read-side address (see CacheReadPath).
	if raw, ok := members["source_type"]; ok {
		var source string
		if json.Unmarshal(raw, &source) != nil || source != "url" {
			return "", false
		}
	}
	id := rule.LibraryRuleID
	if id == "" {
		if raw, present := members["id"]; present && string(raw) != "null" {
			id = rule.ID
		} else {
			id = rule.Name
		}
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
		encoded := strings.TrimSuffix(b.String(), "\n")
		// Historical cache keys used literal UTF-8 line separators. This
		// read-only four-scalar address must not retain JSON's optional JS
		// safety escapes; escaped backslashes remain literal backslashes.
		var out strings.Builder
		for i := 0; i < len(encoded); i++ {
			if encoded[i] == '\\' && i+1 < len(encoded) {
				if encoded[i+1] == '\\' {
					out.WriteString(encoded[i : i+2])
					i++
					continue
				}
				if strings.HasPrefix(encoded[i:], `\u2028`) {
					out.WriteRune('\u2028')
					i += 5
					continue
				}
				if strings.HasPrefix(encoded[i:], `\u2029`) {
					out.WriteRune('\u2029')
					i += 5
					continue
				}
			}
			out.WriteByte(encoded[i])
		}
		return out.String()
	}
	if string(behavior) != "null" {
		var value string
		if json.Unmarshal(behavior, &value) != nil {
			return "", false
		}
		behavior = json.RawMessage(quote(value))
	}
	// Native scalar encoders plus the historical four-field separators; this
	// is not a general legacy JSON serializer and is never used for new writes.
	text := "[" + quote(id) + ", " + quote(*rule.URL) + ", " + string(behavior) + ", " + quote(format) + "]"
	return "by-source/" + digest.SHA256HexString(text) + ".list", true
}

// SameSourceDeclaration compares original declaration evidence for migration.
// R5 text coercion is not proof that an old identity cache belongs to a source.
func SameSourceDeclaration(a, b model.RuleConfig) bool {
	for _, field := range []string{"url", "behavior", "format", "source_type"} {
		if !originalMemberEqual(a, b, field) {
			return false
		}
	}
	return true
}
func originalMemberEqual(a, b model.RuleConfig, field string) bool {
	member := func(rule model.RuleConfig) json.RawMessage {
		value, _, _ := model.EncodeMember(&rule, field)
		if string(value) == "null" {
			return nil
		}
		return value
	}
	left, right := member(a), member(b)
	if len(left) > 0 && len(right) > 0 && left[0] == '"' && right[0] == '"' {
		var x, y string
		return json.Unmarshal(left, &x) == nil && json.Unmarshal(right, &y) == nil && x == y
	}
	return bytes.Equal(left, right)
}
