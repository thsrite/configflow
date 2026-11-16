package oxidns

import (
	"strings"

	"configflow/internal/jsonx"
)

// Parts of backend/common/oxidns_native_apply.py (module C29) that the
// common routing editor (C25) needs; C29 adds the rest of the module to this
// file.

// nativeWrappers is _WRAPPERS.
var nativeWrappers = []string{"china_dns", "direct_ip", "default_direct_ip"}

// managedTags is _MANAGED: every tag the common settings manage.
var managedTags = func() map[string]bool {
	out := map[string]bool{}
	for tag := range TagTypes {
		out[tag] = true
	}
	for _, tag := range nativeWrappers {
		out[tag] = true
	}
	for _, tag := range []string{"sequence_main", "remote_dns", "local_probe", "proxy_dns_seq", "ip_requery_proxy",
		"fallback_dns", "adguard_reject", "rules_refresh"} {
		out[tag] = true
	}
	return out
}()

// renameReferences is _rename(document, old, new): rewrites only explicit
// reference fields in place (never credentials or file paths) and returns
// the document. Lists are rebuilt, dicts are edited in place, as in Python.
func renameReferences(document any, old, new string) any {
	var walk func(value any, field string, hasField bool) any
	walk = func(value any, field string, hasField bool) any {
		switch x := value.(type) {
		case *jsonx.Object:
			if x == nil {
				return value
			}
			for _, key := range x.Keys() {
				if key != "tag" && key != "type" {
					x.Set(key, walk(x.Value(key), key, true))
				}
			}
		case []any:
			out := make([]any, len(x))
			for i, element := range x {
				out[i] = walk(element, field, hasField)
			}
			return out
		case string:
			if !hasField || !strIn(field, "exec", "matches", "entry", "primary", "secondary", "provider", "executors", "args") {
				return value
			}
			if strIn(field, "entry", "primary", "secondary", "provider") && x == old {
				return new
			}
			if field == "exec" && (x == "goto "+old || x == "jump "+old) {
				return strings.SplitN(x, " ", 2)[0] + " " + new
			}
			return replaceTagToken(x, old, new)
		}
		return value
	}
	return walk(document, "", false)
}

// replaceTagToken is re.sub(r'(?<![\w.\-])\$' + re.escape(old) +
// r'(?![\w.\-])', '$' + new, text).
func replaceTagToken(text, old, new string) string {
	t, g := []rune(text), []rune(old)
	var b strings.Builder
	for p := 0; p < len(t); {
		if t[p] == '$' && (p == 0 || !isTagRune(t[p-1])) && tagAt(t, p+1, g) {
			b.WriteString("$" + new)
			p += 1 + len(g)
			continue
		}
		b.WriteRune(t[p])
		p++
	}
	return b.String()
}
