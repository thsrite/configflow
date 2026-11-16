package oxidns

import (
	"strings"
	"unicode/utf8"
)

// Conservative reference checks shared by the OxiDNS form editors: unknown
// literal references and unexpanded expressions prevent automatic deletion.

const (
	// DefaultReferencePrefix is reference_reason's default prefix.
	DefaultReferencePrefix = "功能仍被自定义执行链引用，请在完整参数中调整"
	envReason              = "未展开的环境变量引用"
)

var referenceFields = []string{"exec", "matches", "entry", "primary", "secondary", "executors", "provider"}

// Reference is one remaining edge; Tag "" with the env reason is an
// unexpanded ${...} reference (Python's tag None).
type Reference struct {
	Path   Path
	Tag    string
	Reason string
}

func isTagRune(r rune) bool { return isWordRune(r) || r == '.' || r == '-' }

// mentions is (?<![\w.\-])\$?<tag>(?![\w.\-]) searched in s.
func mentions(s, tag string) bool {
	for from := 0; from <= len(s); {
		i := strings.Index(s[from:], tag)
		if i < 0 {
			return false
		}
		i += from
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[i+len(tag):])
		if (i == 0 || !isTagRune(before)) && (i+len(tag) == len(s) || !isTagRune(after)) {
			return true
		}
		_, width := utf8.DecodeRuneInString(s[i:])
		from = i + max(width, 1)
	}
	return false
}

// ReferenceLocations finds the edges left after the caller removed the
// ones it owns. Plugin tag/type declarations are not references; dynamic
// also reports unexpanded ${...} references in reference fields.
func ReferenceLocations(document *Node, tags []string, dynamic bool) []Reference {
	var result []Reference
	var walk func(value *Node, at Path, reference bool)
	walk = func(value *Node, at Path, reference bool) {
		switch {
		case isMap(value):
			pairs(value, func(key string, item *Node) bool {
				if len(at) == 2 && at[0] == (Elem{Key: "plugins"}) && (key == "tag" || key == "type") {
					if dynamic && key == "tag" && isStr(item) && envRef(item.Value) {
						result = append(result, Reference{Path: at.with(key), Reason: envReason})
					}
					return true
				}
				ref := containsString(referenceFields, key) || key == "args" && (isStr(item) || isSeq(item))
				walk(item, at.with(key), ref)
				return true
			})
		case isSeq(value):
			for i, item := range value.Content {
				walk(item, at.with(i), reference)
			}
		case isStr(value):
			if dynamic && reference && envRef(value.Value) {
				result = append(result, Reference{Path: at, Reason: envReason})
			}
			seen := map[string]bool{}
			for _, tag := range tags {
				if !seen[tag] && mentions(value.Value, tag) {
					result = append(result, Reference{Path: at, Tag: tag, Reason: "仍有插件引用"})
				}
				seen[tag] = true
			}
		}
	}
	walk(document, nil, false)
	return result
}

// ReferenceReason explains references (the first four distinct locations).
func ReferenceReason(references []Reference, prefix string) string {
	var locations []string
	for i, item := range references {
		if item.Reason == envReason {
			prefix = "执行链含未展开的环境变量引用，请在完整参数中确认后调整"
		}
		if i < 4 && !containsString(locations, item.Path.dotted()) {
			locations = append(locations, item.Path.dotted())
		}
	}
	return prefix + "（" + strings.Join(locations, "、") + "）"
}
