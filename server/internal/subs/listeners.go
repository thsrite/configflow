package subs

import (
	"errors"
	"fmt"
	"strings"

	"configflow/internal/model"
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

// listenerNodes validates the open engine document without decoding a config.
func listenerNodes(document *yamlx.Node) ([]*yamlx.Node, error) {
	list := yamlx.Resolve(yamlx.Get(document, "listeners"))
	if list == nil || list.Kind == yaml.ScalarNode && list.ShortTag() == "!!null" {
		return nil, nil
	}
	if list.Kind != yaml.SequenceNode {
		return nil, lre(msgListenerListRequired)
	}
	for i, item := range list.Content {
		item = yamlx.Resolve(item)
		if item == nil || item.Kind != yaml.MappingNode {
			return nil, lre(fmt.Sprintf(msgListenerObjectRequired, i))
		}
		target := yamlx.Resolve(yamlx.Get(item, "proxy"))
		if target != nil && target.ShortTag() != "!!null" && (target.Kind != yaml.ScalarNode || target.ShortTag() != "!!str") {
			return nil, lre(fmt.Sprintf(msgListenerTargetRequired, i))
		}
	}
	return list.Content, nil
}

// RelinkListeners keeps it keeps listeners'
// explicit `proxy` references aligned with a managed group edit, rewriting
// profile["mihomo"]["custom_config"] with its comments preserved, and returns
// the changed paths. newName nil restores the listener's own rules.
func RelinkListeners(profile *model.Profile, oldName string, newName *string) ([]string, error) {
	if profile == nil {
		return nil, nil
	}
	settings := &profile.Mihomo
	source := settings.CustomConfig
	if source == model.DamagedText {
		return nil, lre(msgCustomConfigNotText)
	}
	if strings.TrimSpace(source) == "" || newName != nil && oldName == *newName {
		return nil, nil
	}
	var changed []string
	updated, err := yamlx.Edit([]byte(source), func(doc *yamlx.Node) error {
		if doc.Kind != yaml.MappingNode || doc.ShortTag() != "!!map" {
			return lre(msgListenerBaseObject)
		}
		listenersItem := yamlx.Get(doc, "listeners")
		listeners := yamlx.Resolve(listenersItem)
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
		// Ordinary unshared nodes keep their positions for a splice. With
		// aliases or anchors, only containers reachable from elsewhere are
		// copied (one level, children stay shared and unexpanded, like Python's
		// deepcopy), so other consumers keep the original values and alias
		// chains are never expanded (review N7-2 F1).
		shared := !explicitListeners(doc) || sharedListenerNodes(listeners)
		detached := !explicitListeners(doc) || listenersItem != listeners || listeners.Anchor != ""
		if detached {
			listeners = detachListenerNode(listenersItem)
			yamlx.Set(doc, "listeners", listeners)
		}
		for _, i := range indexes {
			item := listeners.Content[i]
			listener := yamlx.Resolve(item)
			// Items of a copied sequence are still the original's items.
			if detached || item != listener || listener.Anchor != "" {
				listener = detachListenerNode(item)
				listeners.Content[i] = listener
			}
			switch {
			case newName == nil:
				yamlx.Delete(listener, "proxy")
			case shared:
				yamlx.Set(listener, "proxy", renamedListenerTarget(listener, *newName))
			default:
				yamlx.Resolve(yamlx.Get(listener, "proxy")).Value = *newName
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
		settings.CustomConfig = string(updated)
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

// detachListenerNode copies a container that an alias or anchor makes
// reachable from elsewhere. An alias keeps its own comments at this position.
func detachListenerNode(item *yamlx.Node) *yamlx.Node {
	n := yamlx.Resolve(item)
	c := *n
	c.Anchor = ""
	c.Content = append([]*yamlx.Node(nil), n.Content...)
	if item != n {
		c.HeadComment, c.LineComment, c.FootComment = item.HeadComment, item.LineComment, item.FootComment
		if c.Style&yaml.FlowStyle == 0 {
			// yaml.v3 does not write a block collection's line comment.
			c.HeadComment, c.LineComment = joinComments(c.HeadComment, c.LineComment), ""
		}
	}
	return &c
}

func joinComments(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + "\n" + b
}

// renamedListenerTarget is a new proxy value for a listener whose nodes may be
// shared. An explicit value keeps its comments; an inherited one is overridden.
func renamedListenerTarget(listener *yamlx.Node, name string) *yamlx.Node {
	for i := 0; i+1 < len(listener.Content); i += 2 {
		key := listener.Content[i]
		if key.Kind == yaml.ScalarNode && key.ShortTag() == "!!str" && key.Value == "proxy" {
			item := listener.Content[i+1]
			value := *yamlx.Resolve(item)
			value.Anchor, value.Value = "", name
			if item.Kind == yaml.AliasNode {
				value.HeadComment, value.LineComment, value.FootComment = item.HeadComment, item.LineComment, item.FootComment
			}
			return &value
		}
	}
	return yamlx.Str(name)
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

// ValidateListenerNode checks every fixed target, including documents without dialer topology.
func ValidateListenerNode(document *yamlx.Node) error {
	available := map[string]bool{}
	for _, name := range listenerBuiltins {
		available[name] = true
	}
	for _, key := range []string{"proxies", "proxy-groups"} {
		list := yamlx.Resolve(yamlx.Get(document, key))
		if list == nil || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range list.Content {
			name := yamlx.Resolve(yamlx.Get(item, "name"))
			if name != nil && name.Kind == yaml.ScalarNode && name.ShortTag() == "!!str" {
				available[name.Value] = true
			}
		}
	}
	listeners, err := listenerNodes(document)
	if err != nil {
		return err
	}
	for i, item := range listeners {
		target := yamlx.Resolve(yamlx.Get(item, "proxy"))
		if target != nil && target.ShortTag() == "!!str" && target.Value != "" && !available[target.Value] {
			return lre(fmt.Sprintf(msgListenerMissingPolicy, i, target.Value))
		}
	}
	return nil
}
