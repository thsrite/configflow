package repo

// Stored-document constants and the jsonx input helpers that the old-format
// import conversion (importexport.go) and profile imports still use. Write
// validation is validation.go; load validation is load.go.

// SchemaVersion is ProfileRepository.SCHEMA_VERSION.
const SchemaVersion = 5

// DefaultProfileID is ProfileRepository.DEFAULT_PROFILE_ID.
const DefaultProfileID = "default"

var systemFields = map[string]bool{"system_config": true, "backup": true, "agents": true, "agent_registration_sessions": true, "_revision": true}

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
