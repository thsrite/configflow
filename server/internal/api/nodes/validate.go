package nodes

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// validateNodeLineFields rejects line-breaking and control characters in manual
// node fields spliced into Surge/Loon INI text configs (O9 round 2 L3).
//
// The identity fields (name, type, server, port) and URI share links are
// single-line by nature and reject every control character. Parameter values
// of structured and YAML/JSON nodes may legitimately span lines in Mihomo
// (ca-str, inline certificate or key PEM), so they keep TAB, LF and CR and
// reject only the other control and line-separator characters. Surge/Loon
// generation folds any remaining line break to a space, so a multi-line value
// can never add a line to those single-line formats (review batch-20).
func validateNodeLineFields(body record) error {
	for _, field := range []string{"name", "type", "server", "port"} {
		raw, exists := body[field]
		if !exists {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil && convert.HasClientControlChar(text) {
			return fmt.Errorf(msgNodeControlChar, field)
		}
	}
	if raw, exists := body["params"]; exists {
		if err := validateJSONControlChars(raw, "params", false); err != nil {
			return err
		}
	}
	if raw, exists := body["proxy_string"]; exists {
		var text string
		if json.Unmarshal(raw, &text) == nil && text != "" {
			if err := validateProxyStringFields(text); err != nil {
				return err
			}
		}
	}
	return nil
}

// paramControlRune is IsClientControlRune minus the whitespace a multi-line
// Mihomo parameter value may contain.
func paramControlRune(r rune) bool {
	return r != '\n' && r != '\r' && r != '\t' && convert.IsClientControlRune(r)
}

func hasControlOrEscaped(text string, unescape bool) bool {
	if convert.HasClientControlChar(text) {
		return true
	}
	if unescape {
		if decoded, err := url.PathUnescape(text); err == nil && convert.HasClientControlChar(decoded) {
			return true
		}
	}
	return false
}

// hasParamControl checks one parameter value: URI-sourced values (unescape)
// are single-line and strict; structured values may keep TAB, LF and CR.
func hasParamControl(text string, unescape bool) bool {
	if unescape {
		return hasControlOrEscaped(text, true)
	}
	return strings.IndexFunc(text, paramControlRune) >= 0
}

func validateJSONControlChars(raw json.RawMessage, prefix string, unescape bool) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if hasParamControl(text, unescape) {
			return fmt.Errorf(msgNodeControlChar, prefix)
		}
		return nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil && object != nil {
		keys := make([]string, 0, len(object))
		for k := range object {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if err := validateJSONControlChars(object[k], prefix+"."+k, unescape); err != nil {
				return err
			}
		}
		return nil
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for i, item := range list {
			if err := validateJSONControlChars(item, prefix+"["+strconv.Itoa(i)+"]", unescape); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProxyStringFields(text string) error {
	// Disallow non-whitespace control chars and Unicode line separators anywhere
	// in proxy_string, even inside multi-line YAML documents.
	if strings.IndexFunc(text, paramControlRune) >= 0 {
		return fmt.Errorf(msgNodeControlChar, "proxy_string")
	}
	trimmed := strings.TrimSpace(text)
	firstLine, _, hasNewline := strings.Cut(trimmed, "\n")
	isURI := strings.Contains(firstLine, "://") && !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "name:") && !strings.HasPrefix(trimmed, "type:")
	if isURI && (hasNewline || strings.ContainsRune(trimmed, '\r')) {
		return fmt.Errorf(msgNodeControlChar, "proxy_string")
	}
	for _, parsed := range subs.ParseList(text) {
		for _, item := range []struct{ field, value string }{
			{"proxy_string.name", parsed.Node.Name},
			{"proxy_string.type", parsed.Node.Type},
			{"proxy_string.server", parsed.Node.Server},
		} {
			if hasControlOrEscaped(item.value, isURI) {
				return fmt.Errorf(msgNodeControlChar, item.field)
			}
		}
		if params, err := parsed.Node.Params.Node(); err == nil && params != nil {
			if data, err := yamlx.ToJSON(params); err == nil {
				if err := validateJSONControlChars(data, "proxy_string.params", isURI); err != nil {
					return err
				}
			}
		}
		if parsed.Raw != nil {
			if data, err := yamlx.ToJSON(parsed.Raw); err == nil {
				if err := validateJSONControlChars(data, "proxy_string", false); err != nil {
					return err
				}
			}
		}
	}
	if isURI {
		if _, frag, ok := strings.Cut(trimmed, "#"); ok && hasControlOrEscaped(frag, true) {
			return fmt.Errorf(msgNodeControlChar, "proxy_string.name")
		}
	}
	return nil
}
