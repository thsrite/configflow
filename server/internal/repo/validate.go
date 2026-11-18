package repo

// Stored-document constants and the jsonx input helpers that the old-format
// import conversion (importexport.go) and profile imports still use. Write
// validation is validation.go; load validation is load.go.

import (
	"configflow/internal/jsonx"
)

// SchemaVersion is ProfileRepository.SCHEMA_VERSION.
const SchemaVersion = 5

// DefaultProfileID is ProfileRepository.DEFAULT_PROFILE_ID.
const DefaultProfileID = "default"

var systemFields = map[string]bool{"system_config": true, "backup": true, "agents": true, "_revision": true}

// isSchemaVersion is value == SCHEMA_VERSION (5 and 5.0, nothing else).
func isSchemaVersion(value any) bool {
	number, ok := value.(jsonx.Number)
	return ok && jsonx.Equal(number, jsonx.Int(SchemaVersion))
}

// enabledOr is obj.get(key, True) as a truth value.
func truthyOr(obj *jsonx.Object, key string, fallback bool) bool {
	value, ok := obj.Get(key)
	if !ok {
		return fallback
	}
	return jsonx.Truthy(value)
}

// itemsByID is _items_by_id: a list of objects with unique non-empty string ids.
func itemsByID(items any, label string) (*objectMap, error) {
	values, ok := jsonx.AsList(items)
	if !ok {
		return nil, validationError(label + " 必须是列表")
	}
	result := newObjectMap()
	for _, item := range values {
		object, _ := jsonx.AsObject(item)
		id, isString := object.Value("id").(string)
		if object == nil || !isString || id == "" {
			return nil, validationError(label + " 中每一项必须有 ID")
		}
		if _, duplicate := result.values[id]; duplicate {
			return nil, validationError(label + " 存在重复 ID：" + id)
		}
		result.set(id, object)
	}
	return result, nil
}

func (m *objectMap) has(key any) bool {
	text, ok := key.(string)
	if !ok {
		return false
	}
	_, found := m.values[text]
	return found
}

// idList is _id_list: a list of distinct strings.
func idList(values any, label string) ([]string, error) {
	items, ok := jsonx.AsList(values)
	if !ok {
		return nil, validationError(label + " 只能包含资源 ID，不能设置资源覆盖")
	}
	result := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, validationError(label + " 只能包含资源 ID，不能设置资源覆盖")
		}
		result = append(result, text)
		seen[text] = true
	}
	if len(seen) != len(result) {
		return nil, validationError(label + " 含有重复引用")
	}
	return result, nil
}

// orderedSet is a set that remembers insertion order, so the first error is
// deterministic where Python iterates a set.
type orderedSet struct {
	keys []string
	set  map[string]bool
}

func newOrderedSet() *orderedSet { return &orderedSet{set: map[string]bool{}} }

func (s *orderedSet) add(key string) {
	if !s.set[key] {
		s.set[key] = true
		s.keys = append(s.keys, key)
	}
}

func (s *orderedSet) has(key string) bool { return s != nil && s.set[key] }

// ResourceRoots names shared resources to resolve in addition to those a
// profile's composition selects: kind → ids, a nil slice meaning every
// resource of that kind (Python: {kind: None}).
type ResourceRoots map[string][]string

var groupResourceFields = [][2]string{{"subscriptions", "subscriptions"}, {"manual_nodes", "nodes"},
	{"aggregations", "subscription_aggregations"}}

var sourceKinds = map[string]string{"node": "nodes", "subscription": "subscriptions", "aggregation": "subscription_aggregations"}

func sourceKind(value any) string {
	text, _ := value.(string)
	return sourceKinds[text]
}

// pruneRuleDNS is _prune_rule_dns: drop per-rule OxiDNS DNS entries of rules
// the profile no longer has. Rule ids and list entries are matched by their
// JSON value (a non-string id such as 5 keeps an equal entry).
func pruneRuleDNS(profile *jsonx.Object) error {
	oxidns, ok := jsonx.AsObject(profile.Value("oxidns"))
	rules, isList := jsonx.AsList(profile.Value("rule_configs"))
	if !ok || !isList {
		return nil
	}
	key := func(value any) string {
		text, err := jsonx.Encode(value, jsonx.Options{SortKeys: true})
		if err != nil {
			return ""
		}
		return string(text)
	}
	ids := map[string]bool{}
	for _, rule := range rules {
		if object, ok := jsonx.AsObject(rule); ok {
			ids[key(object.Value("id"))] = true
		}
	}
	for _, field := range RuleDNSFields {
		switch value := oxidns.Value(field).(type) {
		case *jsonx.Object:
			kept := jsonx.NewObject()
			for identity, item := range value.All() {
				if ids[key(identity)] {
					kept.Set(identity, item)
				}
			}
			oxidns.Set(field, kept)
		case []any:
			kept := []any{}
			for _, identity := range value {
				if ids[key(identity)] {
					kept = append(kept, identity)
				}
			}
			oxidns.Set(field, kept)
		}
	}
	return nil
}

// objectMap is a dict of resources keyed by id: insertion-ordered, a later
// duplicate replaces the value and keeps the first position.
type objectMap struct {
	keys   []string
	values map[string]*jsonx.Object
}

func newObjectMap() *objectMap { return &objectMap{values: map[string]*jsonx.Object{}} }

func (m *objectMap) set(key string, value *jsonx.Object) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}
