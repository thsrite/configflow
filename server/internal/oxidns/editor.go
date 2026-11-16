package oxidns

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Parts of backend/common/oxidns_editor.py (module C26) that oxidns_common
// (C24) needs; C26 adds the rest of the module to this file.

// missingValue is the _MISSING sentinel of the OxiDNS form modules.
var missingValue any = &sentinel{"_MISSING"}

type sentinel struct{ name string }

// sameValue is oxidns_editor._same(left, right): equality that also
// compares types, so False is not 0 and 1 is not 1.0.
func sameValue(left, right any) bool {
	if left == missingValue || right == missingValue {
		return left == right
	}
	if typeName(left) != typeName(right) {
		return false
	}
	switch l := left.(type) {
	case *jsonx.Object:
		r, ok := right.(*jsonx.Object)
		if !ok || l.Len() != r.Len() {
			return false
		}
		for key := range l.All() {
			if !r.Has(key) {
				return false
			}
		}
		for key, value := range l.All() {
			if !sameValue(value, r.Value(key)) {
				return false
			}
		}
		return true
	case []any:
		r := right.([]any)
		if len(l) != len(r) {
			return false
		}
		for i := range l {
			if !sameValue(l[i], r[i]) {
				return false
			}
		}
		return true
	case yamlx.Timestamp:
		return l == right.(yamlx.Timestamp)
	case []byte:
		return string(l) == string(right.([]byte))
	}
	return jsonx.Equal(left, right)
}
