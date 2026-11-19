package upgrades

import (
	"errors"
	"strconv"

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
		return py.Class == "ValueError" || py.Class == "UnicodeEncodeError"
	}
	// Compatibility classification remains in the deferred upgrades owner.
	if errors.Is(err, agents.ErrHeaderEncoding) {
		return true
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
	if errors.Is(err, agents.ErrHeaderEncoding) {
		return "UnicodeEncodeError"
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

// pyDict is dict(value) for the truthy stored upgrade record poll copies: a
// dict is copied; a list is read as key/value pairs (two-element lists,
// two-character strings, two-key dicts); anything else raises Python's
// ValueError or TypeError. Pairs with a non-string key give a TypeError
// here: Python builds the dict, but every later step of poll on it fails
// (KeyError on 'status', or jsonify cannot sort mixed keys; both 500).
func pyDict(value any) (*jsonx.Object, error) {
	switch v := value.(type) {
	case *jsonx.Object:
		return v.Clone(), nil
	case string:
		// Every element of a str is a one-character str.
		return nil, valueError("dictionary update sequence element #0 has length 1; 2 is required")
	case []any:
		out := jsonx.NewObject()
		var nonString error
		for i, element := range v {
			var pair []any
			switch e := element.(type) {
			case []any:
				pair = e
			case string:
				for _, r := range e {
					pair = append(pair, string(r))
				}
			case *jsonx.Object:
				if e == nil {
					return nil, typeError("cannot convert dictionary update sequence element #" + strconv.Itoa(i) + " to a sequence")
				}
				for _, key := range e.Keys() {
					pair = append(pair, key)
				}
			default:
				return nil, typeError("cannot convert dictionary update sequence element #" + strconv.Itoa(i) + " to a sequence")
			}
			if len(pair) != 2 {
				return nil, valueError("dictionary update sequence element #" + strconv.Itoa(i) + " has length " +
					strconv.Itoa(len(pair)) + "; 2 is required")
			}
			if unhashable(pair[0]) {
				return nil, typeError("unhashable type: '" + typeName(pair[0]) + "'")
			}
			key, ok := pair[0].(string)
			if !ok {
				if nonString == nil {
					nonString = typeError("upgrade record with a non-string key " + jsonx.Repr(pair[0]))
				}
				continue
			}
			out.Set(key, pair[1])
		}
		if nonString != nil {
			return nil, nonString
		}
		return out, nil
	}
	return nil, typeError("'" + typeName(value) + "' object is not iterable")
}
