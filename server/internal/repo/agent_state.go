package repo

// Agent state rows (design/o17-runtime-state.md): the repository works on one
// merged document whose system.agents holds every member of every Agent;
// storage keeps the state members in agent_state (package staterows). An
// unchanged registry commits nothing because the split of a merge is the
// identity.

import (
	"bytes"
	"slices"

	"configflow/internal/model"

	"configflow/internal/repo/staterows"
	"configflow/internal/store"
)

const agentsField = "agents"

func splitAgents(row []byte) ([]byte, []store.StateRow) { return staterows.Split(row) }

func mergeAgents(row []byte, rows []store.StateRow) []byte { return staterows.Merge(row, rows) }

// mergeRows returns doc with its agent_state rows merged into system.agents
// (the repository's view); doc itself is not modified.
func mergeRows(doc *store.Document) *store.Document {
	if doc == nil || len(doc.State) == 0 {
		return doc
	}
	result := *doc
	result.State = nil
	if doc.System == nil {
		return &result
	}
	fields := slices.Clone(doc.System.Fields)
	for i := range fields {
		if fields[i].Name == agentsField {
			fields[i].Value = mergeAgents(fields[i].Value, doc.State)
		}
	}
	result.System = &store.Section{Fields: fields}
	return &result
}

// splitRows moves the state members of system.agents into doc.State (in
// place; doc is the commit's own encoding).
func splitRows(doc *store.Document) {
	if doc == nil || doc.System == nil {
		return
	}
	doc.State = nil
	for i := range doc.System.Fields {
		if doc.System.Fields[i].Name == agentsField {
			doc.System.Fields[i].Value, doc.State = splitAgents(doc.System.Fields[i].Value)
		}
	}
}

// agentStateOnly reports whether after differs from before in Agent state
// members only (at least one, and nothing else of the system section).
func agentStateOnly(before, after *model.System) (bool, error) {
	encode := func(system *model.System) (map[string][]byte, []store.StateRow, error) {
		members, err := model.EncodeMembers(system)
		if err != nil {
			return nil, nil, err
		}
		values := make(map[string][]byte, len(members))
		var state []store.StateRow
		for _, member := range members {
			value := []byte(member.Value)
			if member.Name == agentsField {
				value, state = splitAgents(value)
			}
			values[member.Name] = value
		}
		delete(values, "_revision")
		return values, state, nil
	}
	old, oldState, err := encode(before)
	if err != nil {
		return false, err
	}
	next, nextState, err := encode(after)
	if err != nil {
		return false, err
	}
	if len(old) != len(next) {
		return false, nil
	}
	for name, value := range old {
		if other, ok := next[name]; !ok || !bytes.Equal(value, other) {
			return false, nil
		}
	}
	if len(oldState) != len(nextState) {
		return true, nil
	}
	for i := range oldState {
		if oldState[i].Owner != nextState[i].Owner || oldState[i].Member != nextState[i].Member || !bytes.Equal(oldState[i].Value, nextState[i].Value) {
			return true, nil
		}
	}
	return false, nil
}

// markRestoredAgents marks every imported Agent whose record carries no state
// member: a backup made since storage version 2 holds configuration only, so
// what its device runs is unknown until the next publication
// (design/o17-runtime-state.md 2.5.1). Records from an older backup that
// carry their state are imported with it and stay unmarked.
func markRestoredAgents(system *model.System, at string) error {
	raw, present, err := model.EncodeMember(system, agentsField)
	if err != nil || !present {
		return err
	}
	_, rows := splitAgents(raw)
	owners := map[string]bool{}
	for _, row := range rows {
		owners[row.Owner] = true
	}
	seen := map[string]bool{}
	for i := range system.Agents {
		agent := &system.Agents[i]
		if agent.ID == "" || seen[agent.ID] {
			continue
		}
		seen[agent.ID] = true
		if !owners[agent.ID] {
			agent.StateRestoredAt = new(at)
		}
	}
	return nil
}
