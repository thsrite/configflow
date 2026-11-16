package oxidns

import (
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

// Port of backend/common/oxidns_references.py (module C22): conservative
// reference checks shared by both OxiDNS form editors.

var referenceFields = []string{"exec", "matches", "entry", "primary", "secondary", "executors", "provider"}

const (
	reasonEnv       = "未展开的环境变量引用"
	reasonReference = "仍有插件引用"
)

// NewReferenceError is OxidnsReferenceError(message, references): the
// message gets the first four reference locations appended in "（…）".
func NewReferenceError(message string, references []any) *Error {
	n := len(references)
	if n > 4 {
		n = 4
	}
	locations := make([]string, n)
	for i := 0; i < n; i++ {
		locations[i] = referencePath(asList(get(references[i], "path", nil)))
	}
	joined := strings.Join(locations, "、")
	if joined != "" {
		message += "（" + joined + "）"
	}
	return &Error{Class: ClassReference, Message: message, References: references}
}

// referencePath is _path(parts).
func referencePath(parts []any) string {
	var b strings.Builder
	for index, part := range parts {
		if n, ok := part.(jsonx.Number); ok && n.IsInt() {
			b.WriteString("[" + n.String() + "]")
			continue
		}
		if index > 0 {
			b.WriteString(".")
		}
		text, ok := part.(string)
		if !ok {
			raise(pyErr("TypeError", "can only concatenate str (not \""+typeName(part)+"\") to str"))
		}
		b.WriteString(text)
	}
	return b.String()
}

// ReferenceLocations is reference_locations(document, tags, dynamic=dynamic):
// the remaining edges ({path, tag, reason}; tag nil for an unexpanded
// environment reference) after the caller removed its precisely owned edges.
// Plugin tag/type declarations are not references; a dynamic plugin tag is
// reported when dynamic is set.
func ReferenceLocations(document any, tags []string, dynamic bool) (locations []any, err error) {
	defer catch(&err)
	return referenceLocations(document, tags, dynamic), nil
}

type tagPattern struct {
	tag     string
	pattern *rules.PyRegex
}

func referenceLocations(document any, tags []string, dynamic bool) []any {
	var patterns []tagPattern
	seen := map[string]bool{}
	for _, tag := range tags {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		pattern, err := rules.CompilePyRegex(`(?<![\w.\-])\$?`+convert.PyReEscape(tag)+`(?![\w.\-])`, false)
		must(err)
		patterns = append(patterns, tagPattern{tag, pattern})
	}
	result := []any{}
	var walk func(value any, path []any, reference bool)
	walk = func(value any, path []any, reference bool) {
		switch x := value.(type) {
		case *jsonx.Object:
			if x == nil {
				return
			}
			for key, element := range x.All() {
				definition := len(path) == 2 && path[0] == "plugins" && (key == "tag" || key == "type")
				if definition {
					if text, ok := element.(string); dynamic && key == "tag" && ok && envSearch(text) {
						result = append(result, jsonx.Obj("path", appendPath(path, key), "tag", nil, "reason", reasonEnv))
					}
					continue
				}
				_, isText := element.(string)
				_, isList := element.([]any)
				isReference := strIn(key, referenceFields...) || (key == "args" && (isText || isList))
				walk(element, appendPath(path, key), isReference)
			}
		case []any:
			for index, element := range x {
				walk(element, appendPath(path, jsonx.Int(index)), reference)
			}
		case string:
			if dynamic && reference && envSearch(x) {
				result = append(result, jsonx.Obj("path", appendPath(path), "tag", nil, "reason", reasonEnv))
			}
			for _, p := range patterns {
				if p.pattern.Search(x) {
					result = append(result, jsonx.Obj("path", appendPath(path), "tag", p.tag, "reason", reasonReference))
				}
			}
		}
	}
	walk(document, []any{}, false)
	return result
}

// DefaultReferencePrefix is reference_reason's default prefix.
const DefaultReferencePrefix = "功能仍被自定义执行链引用，请在完整参数中调整"

// ReferenceReason is reference_reason(references, prefix): prefix (Python's
// default: DefaultReferencePrefix) plus the first four distinct locations;
// an unexpanded environment reference switches to its own prefix.
func ReferenceReason(references []any, prefix string) (reason string, err error) {
	defer catch(&err)
	for _, reference := range references {
		if item(reference, "reason") == reasonEnv {
			prefix = "执行链含未展开的环境变量引用，请在完整参数中确认后调整"
			break
		}
	}
	n := len(references)
	if n > 4 {
		n = 4
	}
	locations := make([]string, 0, n)
	for i := 0; i < n; i++ {
		locations = append(locations, referencePath(asList(item(references[i], "path"))))
	}
	return prefix + "（" + strings.Join(dedupe(locations), "、") + "）", nil
}
