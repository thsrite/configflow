// Package yamlbridge builds validated native YAML graphs from Go values and YAML nodes.
// It owns no document schema.
package yamlbridge

import (
	"encoding/json"
	"fmt"
	"iter"
	"math"
	"reflect"
	"sort"
	"strconv"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

type orderedObject interface {
	All() iter.Seq2[string, any]
}

func isNilContainer(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// Encode constructs one validated physical graph without expanding aliases.
// Native subtrees are copied; orphan anchors are defined at their first use and
// colliding source anchor names are made unique. Source graphs are not mutated.
func Encode(value any) (*yamlx.Node, error) {
	type listID struct {
		first  *any
		length int
	}
	type mapID struct {
		ptr uintptr
	}
	seen := map[any]*yamlx.Node{}
	anchors := 0
	names := map[string]*yamlx.Node{}
	anchor := func(n *yamlx.Node, requested string) string {
		if requested != "" && (names[requested] == nil || names[requested] == n) {
			n.Anchor = requested
			names[requested] = n
			return requested
		}
		for {
			anchors++
			name := fmt.Sprintf("id%03d", anchors)
			if names[name] == nil {
				names[name] = n
				n.Anchor = name
				return name
			}
		}
	}
	alias := func(n *yamlx.Node) *yamlx.Node {
		if n.Anchor == "" {
			anchor(n, "")
		}
		return &yamlx.Node{Kind: yaml.AliasNode, Value: n.Anchor, Alias: n}
	}
	native := map[*yamlx.Node]*yamlx.Node{}
	openAliases := map[*yamlx.Node]bool{}
	var copyNode func(*yamlx.Node) (*yamlx.Node, error)
	copyNode = func(n *yamlx.Node) (*yamlx.Node, error) {
		if n == nil {
			return yamlx.Null(), nil
		}
		if n.Kind == yaml.AliasNode {
			if n.Alias == nil || openAliases[n] {
				// Let the shared facade produce its typed, Chinese diagnostic.
				return yamlx.Encode(n)
			}
			openAliases[n] = true
			defer delete(openAliases, n)
			if target := native[n.Alias]; target != nil {
				a := *n
				a.Value = alias(target).Value
				a.Alias = target
				return &a, nil
			}
			// A selected subtree can refer to an anchor defined outside it.
			// Promote that target to a definition, retaining occurrence notes.
			target, err := copyNode(n.Alias)
			if err != nil {
				return nil, err
			}
			target.HeadComment = joinComments(target.HeadComment, n.HeadComment)
			target.LineComment = joinComments(target.LineComment, n.LineComment)
			target.FootComment = joinComments(target.FootComment, n.FootComment)
			return target, nil
		}
		if previous := native[n]; previous != nil {
			return alias(previous), nil
		}
		c := *n
		c.Content, c.Alias, c.Anchor = nil, nil, ""
		native[n] = &c
		if n.Anchor != "" {
			anchor(&c, n.Anchor)
		}
		for _, child := range n.Content {
			value, err := copyNode(child)
			if err != nil {
				return nil, err
			}
			c.Content = append(c.Content, value)
		}
		return &c, nil
	}

	var build func(any) (*yamlx.Node, error)
	build = func(value any) (*yamlx.Node, error) {
		switch v := value.(type) {
		case *yamlx.Node:
			return copyNode(v)
		case nil:
			return yamlx.Null(), nil
		case string:
			return yamlx.Str(v), nil
		case bool:
			return yamlx.Bool(v), nil
		case orderedObject:
			if isNilContainer(v) {
				return yamlx.Null(), nil
			}
			if n := seen[v]; n != nil {
				return alias(n), nil
			}
			n := yamlx.Map()
			seen[v] = n
			for key, item := range v.All() {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, yamlx.Str(key), child)
			}
			return n, nil
		case map[string]any:
			if v == nil {
				return yamlx.Null(), nil
			}
			id := mapID{reflect.ValueOf(v).Pointer()}
			if n := seen[id]; n != nil {
				return alias(n), nil
			}
			n := yamlx.Map()
			seen[id] = n
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child, err := build(v[key])
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, yamlx.Str(key), child)
			}
			return n, nil
		case []any:
			n := yamlx.Seq()
			if len(v) > 0 {
				id := listID{&v[0], len(v)}
				if previous := seen[id]; previous != nil {
					return alias(previous), nil
				}
				seen[id] = n
			}
			for _, item := range v {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, child)
			}
			return n, nil
		case json.Number:
			switch string(v) {
			case "NaN":
				return yamlx.Float(math.NaN()), nil
			case "Infinity":
				return yamlx.Float(math.Inf(1)), nil
			case "-Infinity":
				return yamlx.Float(math.Inf(-1)), nil
			}
			return yamlx.FromJSON([]byte(string(v)))
		case int:
			return yamlx.FromJSON([]byte(strconv.Itoa(v)))
		case int8:
			return yamlx.FromJSON([]byte(strconv.FormatInt(int64(v), 10)))
		case int16:
			return yamlx.FromJSON([]byte(strconv.FormatInt(int64(v), 10)))
		case int32:
			return yamlx.FromJSON([]byte(strconv.FormatInt(int64(v), 10)))
		case int64:
			return yamlx.FromJSON([]byte(strconv.FormatInt(v, 10)))
		case uint:
			return yamlx.FromJSON([]byte(strconv.FormatUint(uint64(v), 10)))
		case uint8:
			return yamlx.FromJSON([]byte(strconv.FormatUint(uint64(v), 10)))
		case uint16:
			return yamlx.FromJSON([]byte(strconv.FormatUint(uint64(v), 10)))
		case uint32:
			return yamlx.FromJSON([]byte(strconv.FormatUint(uint64(v), 10)))
		case uint64:
			return yamlx.FromJSON([]byte(strconv.FormatUint(v, 10)))
		case float32:
			return yamlx.Float(float64(v)), nil
		case float64:
			return yamlx.Float(v), nil
		}
		return nil, fmt.Errorf("cannot represent an object: %T", value)
	}
	n, err := build(value)
	if err != nil {
		return nil, err
	}
	return yamlx.Encode(n)
}

func joinComments(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n" + b
}
