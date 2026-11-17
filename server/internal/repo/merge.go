package repo

import (
	"configflow/internal/jsonx"
)

// DeepMerge is config_repository._deep_merge: a deep copy of base with
// override's keys laid over it, objects merged recursively.
func DeepMerge(base, override *jsonx.Object) *jsonx.Object {
	result := base.DeepCopy()
	if result == nil {
		result = jsonx.NewObject()
	}
	for key, value := range override.All() {
		current, _ := jsonx.AsObject(result.Value(key))
		incoming, isObject := jsonx.AsObject(value)
		if current != nil && isObject {
			result.Set(key, DeepMerge(current, incoming))
		} else {
			result.Set(key, jsonx.DeepCopy(value))
		}
	}
	return result
}

// configDeepMerge is common/config.py::_deep_merge: base's key order, then
// override's new keys; values are shared, not copied.
func configDeepMerge(base, override *jsonx.Object) *jsonx.Object {
	result := jsonx.NewObject()
	for key, value := range base.All() {
		other, ok := override.Get(key)
		if !ok {
			result.Set(key, value)
			continue
		}
		left, leftObject := jsonx.AsObject(value)
		right, rightObject := jsonx.AsObject(other)
		if leftObject && rightObject {
			result.Set(key, configDeepMerge(left, right))
		} else {
			result.Set(key, other)
		}
	}
	for key, value := range override.All() {
		if !result.Has(key) {
			result.Set(key, value)
		}
	}
	return result
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
