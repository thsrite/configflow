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

func validateJSONControlChars(raw json.RawMessage, prefix string, unescape bool) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if hasControlOrEscaped(text, unescape) {
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
	if strings.IndexFunc(text, func(r rune) bool {
		return r != '\n' && r != '\r' && r != '\t' && convert.IsClientControlRune(r)
	}) >= 0 {
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
