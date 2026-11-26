package repo

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"configflow/internal/yamlx"
)

// Three-way merge of one stored field (design n7-n8 2.5). A save that carries
// the state its caller read (the baseline) applies only the caller's
// baseline→proposed delta to the current value, so concurrent edits of other
// members or list items survive. Values are compared with yamlx.Equal: JSON
// types stay distinct (1, 1.0 and true are three values), unlike Python's
// equality. Object member order follows current; new members are appended.

// MergeJSON merges one field given as JSON values (current, baseline and
// proposed; nil means absent and reads as null). It returns the merged value
// and whether it differs from current; callers that keep current when nothing
// changed preserve its stored bytes (R2).
func MergeJSON(current, baseline, proposed []byte) (merged []byte, changed bool, err error) {
	nodes := [3]*yamlx.Node{}
	for index, data := range [][]byte{current, baseline, proposed} {
		if data == nil {
			nodes[index] = yamlx.Null()
			continue
		}
		if nodes[index], err = yamlx.FromJSON(data); err != nil {
			return nil, false, fmt.Errorf("merge field: %w", err)
		}
	}
	result := mergeNodes(nodes[0], nodes[1], nodes[2])
	if yamlx.Equal(result, nodes[0]) && current != nil {
		return current, false, nil
	}
	if merged, err = yamlx.ToJSON(result); err != nil {
		return nil, false, fmt.Errorf("merge field: %w", err)
	}
	return merged, true, nil
}

func isMapping(n *yamlx.Node) bool {
	n = yamlx.Resolve(n)
	return n != nil && n.Kind == yaml.MappingNode
}

func isSequence(n *yamlx.Node) bool {
	n = yamlx.Resolve(n)
	return n != nil && n.Kind == yaml.SequenceNode
}

// The trees come from yamlx.FromJSON (no aliases or merge keys) and are
// owned by one MergeJSON call. The result shares unchanged subtrees with its
// inputs; a mapping is copied (one level, copy-on-write) before it is changed,
// and no node of an input is ever modified.

func orNull(n *yamlx.Node) *yamlx.Node {
	if n == nil {
		return yamlx.Null()
	}
	return n
}

// shallowMapping copies a mapping's pair list so it can be changed.
func shallowMapping(n *yamlx.Node) *yamlx.Node {
	n = yamlx.Resolve(n)
	copied := *n
	copied.Content = slices.Clone(n.Content)
	return &copied
}

// deepMergeNodes returns a copy of base with override's members laid over it;
// objects present on both sides are merged recursively.
func deepMergeNodes(base, override *yamlx.Node) *yamlx.Node {
	result := shallowMapping(base)
	for key, value := range yamlx.Pairs(override) {
		if existing := yamlx.Get(result, key); isMapping(existing) && isMapping(value) {
			yamlx.Set(result, key, deepMergeNodes(existing, value))
		} else {
			yamlx.Set(result, key, value)
		}
	}
	return result
}

// itemKey identifies a list item across the three versions: an object with a
// string or integer id by that id, anything else by its whole value.
func itemKey(item *yamlx.Node) string {
	if isMapping(item) {
		if id := yamlx.Resolve(yamlx.Get(item, "id")); id != nil && id.Kind == yaml.ScalarNode {
			if tag := id.ShortTag(); tag == "!!str" || tag == "!!int" {
				hash := yamlx.ValueHash(id)
				return "id:" + string(hash[:])
			}
		}
	}
	hash := yamlx.ValueHash(item)
	return "value:" + string(hash[:])
}

// keyedItems is a list indexed by itemKey: first position, last value.
type keyedItems struct {
	keys   []string
	values map[string]*yamlx.Node
}

func indexItems(items []*yamlx.Node) *keyedItems {
	result := &keyedItems{values: map[string]*yamlx.Node{}}
	for _, item := range items {
		key := itemKey(item)
		if _, ok := result.values[key]; !ok {
			result.keys = append(result.keys, key)
		}
		result.values[key] = item
	}
	return result
}

func (k *keyedItems) has(key string) bool { _, ok := k.values[key]; return ok }

// mergeNodes applies the baseline→proposed delta to current and returns a new
// tree; none of its arguments is modified.
//
//   - proposed equal to baseline: current is kept.
//   - objects on all three sides: members the caller removed are removed when
//     current still holds the baseline value; members the caller added are
//     added (or deep-merged into a current object); members the caller changed
//     are merged recursively.
//   - lists on all three sides: items are matched by itemKey. Items the caller
//     removed are dropped, new items appended, changed items merged in place;
//     when the caller reordered retained items, proposed order wins and items
//     only current knows follow.
//   - anything else: proposed replaces current.
func mergeNodes(current, baseline, proposed *yamlx.Node) *yamlx.Node {
	if yamlx.Equal(proposed, baseline) {
		return orNull(current)
	}
	switch {
	case isMapping(current) && isMapping(baseline) && isMapping(proposed):
		return mergeMappings(current, baseline, proposed)
	case isSequence(current) && isSequence(baseline) && isSequence(proposed):
		return mergeSequences(current, baseline, proposed)
	}
	return orNull(proposed)
}

func mergeMappings(current, baseline, proposed *yamlx.Node) *yamlx.Node {
	result := shallowMapping(current)
	for key, old := range yamlx.Pairs(baseline) {
		if yamlx.Get(proposed, key) != nil {
			continue
		}
		if value := yamlx.Get(result, key); value != nil && yamlx.Equal(value, old) {
			yamlx.Delete(result, key)
		}
	}
	for key, value := range yamlx.Pairs(proposed) {
		old := yamlx.Get(baseline, key)
		existing := yamlx.Get(result, key)
		switch {
		case old == nil && existing == nil:
			yamlx.Set(result, key, value)
		case old == nil:
			if isMapping(existing) && isMapping(value) {
				yamlx.Set(result, key, deepMergeNodes(existing, value))
			}
		case !yamlx.Equal(value, old):
			yamlx.Set(result, key, mergeNodes(existing, old, value))
		}
	}
	return result
}

func mergeSequences(current, baseline, proposed *yamlx.Node) *yamlx.Node {
	current, baseline, proposed = yamlx.Resolve(current), yamlx.Resolve(baseline), yamlx.Resolve(proposed)
	baselineItems := indexItems(baseline.Content)
	proposedItems := indexItems(proposed.Content)
	proposedKeys := make([]string, 0, len(proposed.Content))
	for _, item := range proposed.Content {
		proposedKeys = append(proposedKeys, itemKey(item))
	}
	result := make([]*yamlx.Node, 0, len(current.Content))
	for _, item := range current.Content {
		key := itemKey(item)
		if baselineItems.has(key) && !proposedItems.has(key) {
			continue
		}
		result = append(result, item)
	}
	positions := map[string]int{}
	for index, item := range result {
		positions[itemKey(item)] = index
	}
	for _, key := range proposedKeys {
		item := proposedItems.values[key]
		index, inResult := positions[key]
		if !baselineItems.has(key) {
			if !inResult {
				result = append(result, item)
				positions[key] = len(result) - 1
			}
		} else if old := baselineItems.values[key]; inResult && !yamlx.Equal(item, old) {
			result[index] = mergeNodes(result[index], old, item)
		}
	}
	var retainedProposed, retainedBaseline []string
	for _, key := range proposedKeys {
		if baselineItems.has(key) {
			retainedProposed = append(retainedProposed, key)
		}
	}
	for _, item := range baseline.Content {
		if key := itemKey(item); proposedItems.has(key) {
			retainedBaseline = append(retainedBaseline, key)
		}
	}
	if !equalStrings(retainedProposed, retainedBaseline) {
		resultItems := indexItems(result)
		ordered := make([]*yamlx.Node, 0, len(resultItems.keys))
		seen := map[string]bool{}
		// A key the caller repeated is repeated, as the caller listed it.
		for _, key := range proposedKeys {
			if resultItems.has(key) {
				ordered = append(ordered, resultItems.values[key])
				seen[key] = true
			}
		}
		for _, key := range resultItems.keys {
			if !seen[key] {
				ordered = append(ordered, resultItems.values[key])
				seen[key] = true
			}
		}
		result = ordered
	}
	return yamlx.Seq(result...)
}
