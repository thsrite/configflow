package subs

import (
	"errors"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// ListenerReferenceError is mihomo_listeners.ListenerReferenceError, a
// StrategyReferenceError.
type ListenerReferenceError struct {
	Message string
	// Cause retains an editor or parser failure for errors.Is/errors.As.
	Cause error
}

func (e *ListenerReferenceError) Error() string { return e.Message }

// Unwrap exposes validation classification and the underlying parser/editor error.
func (e *ListenerReferenceError) Unwrap() []error {
	chain := []error{&rules.StrategyReferenceError{Message: e.Message}}
	if e.Cause != nil {
		chain = append(chain, e.Cause)
	}
	return chain
}

func lre(msg string) error { return &ListenerReferenceError{Message: msg} }

var listenerBuiltins = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "PASS-RULE", "COMPATIBLE", "GLOBAL"}

// listenersOf is _listeners: the document's listeners, validated.
func listenersOf(document *jsonx.Object) ([]any, error) {
	v := document.Value("listeners")
	if v == nil {
		return nil, nil
	}
	listeners, ok := v.([]any)
	if !ok {
		return nil, lre(msgListenerListRequired)
	}
	for i, l := range listeners {
		o, ok := l.(*jsonx.Object)
		if !ok {
			return nil, lre(fmt.Sprintf(msgListenerObjectRequired, i))
		}
		if t := o.Value("proxy"); t != nil && !isString(t) {
			return nil, lre(fmt.Sprintf(msgListenerTargetRequired, i))
		}
	}
	return listeners, nil
}

// RelinkGroupListeners is relink_group_listeners: it keeps listeners'
// explicit `proxy` references aligned with a managed group edit, rewriting
// profile["mihomo"]["custom_config"] with its comments preserved, and returns
// the changed paths. newName nil restores the listener's own rules.
func RelinkGroupListeners(profile *jsonx.Object, oldName string, newName *string) ([]string, error) {
	settings := profile.GetObject("mihomo")
	source, _ := settings.Value("custom_config").(string)
	if strings.TrimSpace(source) == "" || newName != nil && oldName == *newName {
		return nil, nil
	}
	var changed []string
	updated, err := yamlx.Edit([]byte(source), func(doc *yamlx.Node) error {
		if doc.Kind != yaml.MappingNode || doc.ShortTag() != "!!map" {
			return lre(msgListenerBaseObject)
		}
		listeners := yamlx.Resolve(yamlx.Get(doc, "listeners"))
		if listeners == nil || listeners.Kind == yaml.ScalarNode && listeners.ShortTag() == "!!null" {
			return nil
		}
		if listeners.Kind != yaml.SequenceNode || listeners.ShortTag() != "!!seq" {
			return lre(msgListenerListRequired)
		}
		var indexes []int
		for i, item := range listeners.Content {
			listener := yamlx.Resolve(item)
			if listener == nil || listener.Kind != yaml.MappingNode || listener.ShortTag() != "!!map" {
				return lre(fmt.Sprintf(msgListenerObjectRequired, i))
			}
			target := yamlx.Resolve(yamlx.Get(listener, "proxy"))
			if target == nil || target.Kind == yaml.ScalarNode && target.ShortTag() == "!!null" {
				continue
			}
			if target.Kind != yaml.ScalarNode || target.ShortTag() != "!!str" {
				return lre(fmt.Sprintf(msgListenerTargetRequired, i))
			}
			if strings.Contains(target.Value, "${") {
				return lre(fmt.Sprintf(msgListenerVariable, i))
			}
			if target.Value == oldName {
				indexes = append(indexes, i)
			}
		}
		if len(indexes) == 0 {
			return nil
		}
		// Ordinary unshared nodes keep their positions for a splice. Shared/merged
		// listeners are cloned so aliases elsewhere retain the original values.
		if !explicitListeners(doc) || sharedListenerNodes(listeners) {
			listeners = yamlx.Clone(listeners)
			yamlx.Set(doc, "listeners", listeners)
		}
		for _, i := range indexes {
			listener := yamlx.Resolve(listeners.Content[i])
			if newName == nil {
				yamlx.Delete(listener, "proxy")
			} else {
				target := yamlx.Resolve(yamlx.Get(listener, "proxy"))
				target.Value = *newName
			}
			changed = append(changed, fmt.Sprintf("listeners[%d].proxy", i))
		}
		return nil
	})
	if err != nil {
		var listenerErr *ListenerReferenceError
		if errors.As(err, &listenerErr) {
			return nil, err
		}
		if errors.Is(err, yamlx.ErrUnsafeEdit) {
			return nil, &ListenerReferenceError{Message: msgListenerUnsafeEdit, Cause: err}
		}
		return nil, &ListenerReferenceError{Message: fmt.Sprintf(msgListenerBaseParseDetail, err), Cause: err}
	}
	if len(changed) > 0 {
		if settings == nil {
			settings = jsonx.NewObject()
			profile.Set("mihomo", settings)
		}
		settings.Set("custom_config", string(updated))
	}
	return changed, nil
}

func explicitListeners(doc *yamlx.Node) bool {
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key := doc.Content[i]
		if key.Kind == yaml.ScalarNode && key.ShortTag() == "!!str" && key.Value == "listeners" {
			return true
		}
	}
	return false
}

func sharedListenerNodes(n *yamlx.Node) bool {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return true
	}
	for _, child := range n.Content {
		if sharedListenerNodes(child) {
			return true
		}
	}
	return false
}

// ValidateListenerPolicies is validate_listener_policies: every fixed
// listener target must exist, even without a dialer topology.
func ValidateListenerPolicies(document *jsonx.Object) error {
	available := map[string]bool{}
	for _, b := range listenerBuiltins {
		available[b] = true
	}
	for _, key := range []string{"proxies", "proxy-groups"} {
		items, _ := iterate(document.Value(key))
		for _, item := range items {
			if o, ok := item.(*jsonx.Object); ok {
				if n, ok := o.Value("name").(string); ok {
					available[n] = true
				}
			}
		}
	}
	listeners, err := listenersOf(document)
	if err != nil {
		return err
	}
	for i, l := range listeners {
		target := l.(*jsonx.Object).Value("proxy")
		if jsonx.Truthy(target) && !available[target.(string)] {
			return lre(fmt.Sprintf(msgListenerMissingPolicy, i, target.(string)))
		}
	}
	return nil
}
