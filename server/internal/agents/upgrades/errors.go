package upgrades

import (
	"errors"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// PyError is a Python exception raised by upgrades.py / address_follow.py:
// Class is its type name ("ValueError", "TypeError", "AttributeError",
// "KeyError", "StopIteration"), Message is str(exc).
type PyError struct {
	Class   string
	Message string
}

func (e *PyError) Error() string { return e.Message }

func valueError(message string) error { return &PyError{Class: "ValueError", Message: message} }

func typeError(message string) error { return &PyError{Class: "TypeError", Message: message} }

func keyError(key string) error { return &PyError{Class: "KeyError", Message: "'" + key + "'"} }

// noAttribute is the AttributeError of value.<name> on a non-dict.
func noAttribute(value any, name string) error {
	return &PyError{Class: "AttributeError", Message: "'" + typeName(value) + "' object has no attribute '" + name + "'"}
}

// IsValueError reports whether err is a Python ValueError: the module's own,
// AgentDeploymentConflict and the registry's or repository's ValueErrors.
func IsValueError(err error) bool {
	var py *PyError
	if errors.As(err, &py) {
		return py.Class == "ValueError"
	}
	var conflict *agents.DeploymentConflict
	var value *agents.ValueError
	return errors.As(err, &conflict) || errors.As(err, &value) || repo.IsValueError(err)
}

// ErrorClass is the Python class name of err ("" when it is not one of the
// module's Python exceptions).
func ErrorClass(err error) string {
	var py *PyError
	if errors.As(err, &py) {
		return py.Class
	}
	if IsValueError(err) {
		return "ValueError"
	}
	return ""
}

// typeName is type(value).__name__ for jsonx values.
func typeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case string:
		return "str"
	case bool:
		return "bool"
	case *jsonx.Object:
		if v == nil {
			return "NoneType"
		}
		return "dict"
	case []any:
		return "list"
	case jsonx.Number:
		if v.IsFloat() {
			return "float"
		}
		return "int"
	}
	return "object"
}

func unhashable(value any) bool {
	switch v := value.(type) {
	case []any:
		return true
	case *jsonx.Object:
		return v != nil
	}
	return false
}

// in is `value in set` for a set of strings: TypeError for list/dict values.
func in(value any, set map[string]bool) (bool, error) {
	if unhashable(value) {
		return false, typeError("unhashable type: '" + typeName(value) + "'")
	}
	text, ok := value.(string)
	return ok && set[text], nil
}

// get is value.get(key) for a dict, AttributeError otherwise (None too).
func get(value any, key string) (any, error) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, noAttribute(value, "get")
	}
	return object.Value(key), nil
}

// orDict is `value or {}`: nil for a falsy value, the dict, or the
// AttributeError a following .get raises for a truthy non-dict.
func orDict(value any) (*jsonx.Object, error) {
	if !jsonx.Truthy(value) {
		return jsonx.NewObject(), nil
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, noAttribute(value, "get")
	}
	return object, nil
}

// item is mapping[key]: KeyError when absent.
func item(object *jsonx.Object, key string) (any, error) {
	value, ok := object.Get(key)
	if !ok {
		return nil, keyError(key)
	}
	return value, nil
}

// truncate is text[:limit] in code points.
func truncate(text string, limit int) string {
	count := 0
	for i := range text {
		if count == limit {
			return text[:i]
		}
		count++
	}
	return text
}

// haActive is oxidns_ha_publish.active(agent), raising like Python for
// malformed records (a truthy non-dict job, an unhashable status).
func haActive(agent *jsonx.Object) (bool, error) {
	job, err := orDict(agent.Value(agents.HAField))
	if err != nil || job.Len() == 0 {
		return false, err
	}
	terminal, err := in(job.Value("status"), haTerminal)
	return !terminal, err
}

var haTerminal = map[string]bool{"succeeded": true, "failed": true, "rollback_failed": true}

// haEnabled is manager._oxidns_ha_enabled(agent).
func haEnabled(agent *jsonx.Object) (bool, error) {
	ha, err := orDict(agent.Value("oxidns_ha"))
	if err != nil {
		return false, err
	}
	return ha.Value("enabled") == true, nil
}

// haMember is `_oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent)`.
func haMember(agent *jsonx.Object) (bool, error) {
	enabled, err := haEnabled(agent)
	if err != nil || enabled {
		return enabled, err
	}
	return haActive(agent)
}

// findAgent is next(item for item in agents if item['id'] == agent_id).
func findAgent(list []any, agentID string) (*jsonx.Object, error) {
	for _, value := range list {
		object, ok := value.(*jsonx.Object)
		if !ok || object == nil {
			return nil, typeError("'" + typeName(value) + "' object is not subscriptable")
		}
		id, ok := object.Get("id")
		if !ok {
			return nil, keyError("id")
		}
		if jsonx.Equal(id, agentID) {
			return object, nil
		}
	}
	return nil, &PyError{Class: "StopIteration", Message: ""}
}
