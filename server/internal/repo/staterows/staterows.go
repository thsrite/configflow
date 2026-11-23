// Package staterows moves the Agent state members (model.AgentStateMembers)
// between the encoded system.agents row and agent_state rows
// (design/o17-runtime-state.md). It works on JSON text and copies member
// values byte for byte, so Split(Merge(x)) == x. Only the first entry
// carrying a given Agent id owns state rows; entries without an id, later
// entries with a repeated id and entries that are not objects keep every
// member in the configuration row. It does no I/O.
package staterows

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"configflow/internal/model"
	"configflow/internal/store"
)

// stateMember is one top-level state member name, or for a nested path the
// containing member and the nested name.
type stateMember struct{ container, name string }

var stateMembers = func() []stateMember {
	result := make([]stateMember, 0, len(model.AgentStateMembers))
	for _, path := range model.AgentStateMembers {
		if container, name, nested := strings.Cut(path, "."); nested {
			result = append(result, stateMember{container, name})
		} else {
			result = append(result, stateMember{name: path})
		}
	}
	return result
}()

func (m stateMember) path() string {
	if m.container == "" {
		return m.name
	}
	return m.container + "." + m.name
}

// jsonMember is one member of a JSON object: text is the original
// `"key":value` bytes and value the original value bytes.
type jsonMember struct {
	name        string
	text, value []byte
}

func (m jsonMember) keyText() []byte { return m.text[:len(m.text)-len(m.value)] }

// objectMembers splits a JSON object into its members, keeping their bytes.
func objectMembers(data []byte) ([]jsonMember, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if _, err := decoder.Token(); err != nil {
		return nil, false
	}
	var members []jsonMember
	for decoder.More() {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		name, ok := token.(string)
		if !ok {
			return nil, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		end := decoder.InputOffset()
		text := bytes.TrimLeft(trimmed[start:end], " \t\r\n,")
		members = append(members, jsonMember{name: name, text: text, value: text[len(text)-len(value):]})
	}
	if _, err := decoder.Token(); err != nil || decoder.InputOffset() != int64(len(trimmed)) {
		return nil, false
	}
	return members, true
}

// arrayElements splits a JSON array into its elements, keeping their bytes.
func arrayElements(data []byte) ([][]byte, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if _, err := decoder.Token(); err != nil {
		return nil, false
	}
	var elements [][]byte
	for decoder.More() {
		var element json.RawMessage
		if err := decoder.Decode(&element); err != nil {
			return nil, false
		}
		elements = append(elements, element)
	}
	if _, err := decoder.Token(); err != nil || decoder.InputOffset() != int64(len(trimmed)) {
		return nil, false
	}
	return elements, true
}

func joinJSON(open, close byte, items [][]byte) []byte {
	size := 2
	for _, item := range items {
		size += len(item) + 1
	}
	out := make([]byte, 0, size)
	out = append(out, open)
	for i, item := range items {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, item...)
	}
	return append(out, close)
}

func memberText(name string, value []byte) []byte {
	key, _ := json.Marshal(name) // a string always encodes
	return append(append(key, ':'), value...)
}

// agentID is the id the registry reads from an entry (the model's lenient
// stored rule), or "" when the entry has none.
func agentID(members []jsonMember) string {
	for _, member := range members {
		if member.name != "id" {
			continue
		}
		var record model.Agent
		if _, _, err := model.DecodeStoredMember(&record, "id", member.value, "agents.id"); err != nil {
			return ""
		}
		return record.ID
	}
	return ""
}

func (m stateMember) top() bool { return m.container == "" }

func topStateMember(name string) bool {
	return slices.ContainsFunc(stateMembers, func(m stateMember) bool { return m.top() && m.name == name })
}

func nestedStateMember(container, name string) bool {
	return container != "" && slices.ContainsFunc(stateMembers, func(m stateMember) bool { return m.container == container && m.name == name })
}

// isStateContainer reports a configuration member holding a nested state
// member; top-level state members have no container, so "" never is one.
func isStateContainer(name string) bool {
	return name != "" && slices.ContainsFunc(stateMembers, func(m stateMember) bool { return m.container == name })
}

// Split moves the state members of the encoded agents row into rows.
// It returns the row unchanged (same slice) when no entry has any.
func Split(row []byte) ([]byte, []store.StateRow) {
	elements, ok := arrayElements(row)
	if !ok {
		return row, nil
	}
	var rows []store.StateRow
	seen := map[string]bool{}
	changed := false
	for i, element := range elements {
		members, ok := objectMembers(element)
		if !ok {
			continue
		}
		id := agentID(members)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		kept := make([][]byte, 0, len(members))
		moved := false
		for _, member := range members {
			if topStateMember(member.name) {
				rows = append(rows, store.StateRow{Owner: id, Member: member.name, Value: slices.Clone(member.value)})
				moved = true
				continue
			}
			if isStateContainer(member.name) {
				if inner, ok := objectMembers(member.value); ok {
					innerKept := make([][]byte, 0, len(inner))
					innerMoved := false
					for _, nested := range inner {
						if nestedStateMember(member.name, nested.name) {
							rows = append(rows, store.StateRow{Owner: id, Member: member.name + "." + nested.name, Value: slices.Clone(nested.value)})
							innerMoved = true
							continue
						}
						innerKept = append(innerKept, nested.text)
					}
					if innerMoved {
						moved = true
						kept = append(kept, append(slices.Clone(member.keyText()), joinJSON('{', '}', innerKept)...))
						continue
					}
				}
			}
			kept = append(kept, member.text)
		}
		if moved {
			elements[i] = joinJSON('{', '}', kept)
			changed = true
		}
	}
	if !changed {
		return row, nil
	}
	return joinJSON('[', ']', elements), rows
}

// Merge puts state rows back into the configuration agents row. Rows
// of Agents the row does not hold are not part of the merged view (the next
// commit deletes them). A state member also present in the configuration row
// (only possible in a file edited by hand) is taken from the state row.
func Merge(row []byte, rows []store.StateRow) []byte {
	if len(rows) == 0 {
		return row
	}
	byOwner := map[string]map[string][]byte{}
	for _, state := range rows {
		if byOwner[state.Owner] == nil {
			byOwner[state.Owner] = map[string][]byte{}
		}
		byOwner[state.Owner][state.Member] = state.Value
	}
	elements, ok := arrayElements(row)
	if !ok {
		return row
	}
	seen := map[string]bool{}
	changed := false
	for i, element := range elements {
		members, ok := objectMembers(element)
		if !ok {
			continue
		}
		id := agentID(members)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		values := byOwner[id]
		if len(values) == 0 {
			continue
		}
		merged := make([][]byte, 0, len(members)+len(values))
		containers := map[string]bool{}
		for _, member := range members {
			if _, replaced := values[member.name]; replaced && topStateMember(member.name) {
				continue
			}
			if isStateContainer(member.name) {
				if inner, ok := objectMembers(member.value); ok {
					containers[member.name] = true
					innerMerged := make([][]byte, 0, len(inner)+1)
					for _, nested := range inner {
						if _, replaced := values[member.name+"."+nested.name]; replaced && nestedStateMember(member.name, nested.name) {
							continue
						}
						innerMerged = append(innerMerged, nested.text)
					}
					added := false
					for _, m := range stateMembers {
						if m.container != member.name {
							continue
						}
						if value, ok := values[m.path()]; ok {
							innerMerged = append(innerMerged, memberText(m.name, value))
							added = true
						}
					}
					if added {
						merged = append(merged, append(slices.Clone(member.keyText()), joinJSON('{', '}', innerMerged)...))
						continue
					}
				} else {
					containers[member.name] = true // damaged container: its state rows are not readable
				}
			}
			merged = append(merged, member.text)
		}
		for _, m := range stateMembers {
			value, ok := values[m.path()]
			if !ok {
				continue
			}
			if m.top() {
				merged = append(merged, memberText(m.name, value))
			} else if !containers[m.container] {
				merged = append(merged, memberText(m.container, joinJSON('{', '}', [][]byte{memberText(m.name, value)})))
				containers[m.container] = true
			}
		}
		elements[i] = joinJSON('{', '}', merged)
		changed = true
	}
	if !changed {
		return row
	}
	return joinJSON('[', ']', elements)
}
