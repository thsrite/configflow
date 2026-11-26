package repo

// Agent state rows (design/o17-runtime-state.md): the repository works on one
// merged document whose system.agents holds every member of every Agent;
// storage keeps the state members in agent_state (package staterows). An
// unchanged registry commits nothing because the split of a merge is the
// identity.

import (
	"bytes"
	"encoding/json"
	"slices"

	"configflow/internal/model"

	"configflow/internal/repo/staterows"
	"configflow/internal/store"
)

const (
	agentsField      = "agents"
	routersField     = "routers"
	oxidnsHAStateKey = "oxidns_ha_state"
)

func splitAgents(row []byte) ([]byte, []store.StateRow) { return staterows.Split(row) }

func mergeAgents(row []byte, rows []store.StateRow) []byte { return staterows.Merge(row, rows) }

func splitRouters(profileID string, row []byte) ([]byte, []store.StateRow) {
	return staterows.SplitRouters(profileID, row)
}

func mergeRouters(profileID string, row []byte, rows []store.StateRow) []byte {
	return staterows.MergeRouters(profileID, row, rows)
}

// mergeRows returns doc with its agent_state rows merged into system.agents
// and each profile's routers field (the repository's view); doc itself is not
// modified.
func mergeRows(doc *store.Document) *store.Document {
	if doc == nil || len(doc.State) == 0 {
		return doc
	}
	result := *doc
	result.State = nil
	if doc.System != nil {
		fields := slices.Clone(doc.System.Fields)
		for i := range fields {
			if fields[i].Name == agentsField {
				fields[i].Value = mergeAgents(fields[i].Value, doc.State)
			}
		}
		result.System = &store.Section{Fields: fields}
	}
	if len(doc.Profiles) > 0 {
		profiles := make([]store.ProfileRows, len(doc.Profiles))
		for i, profile := range doc.Profiles {
			fields := slices.Clone(profile.Fields)
			for j := range fields {
				if fields[j].Name == routersField {
					fields[j].Value = mergeRouters(profile.ID, fields[j].Value, doc.State)
				}
			}
			profiles[i] = store.ProfileRows{ID: profile.ID, Fields: fields}
		}
		result.Profiles = profiles
	}
	return &result
}

// splitRows moves the state members of system.agents and each profile's
// routers into doc.State (in place; doc is the commit's own encoding).
func splitRows(doc *store.Document) {
	if doc == nil {
		return
	}
	doc.State = nil
	if doc.System != nil {
		for i := range doc.System.Fields {
			if doc.System.Fields[i].Name == agentsField {
				var rows []store.StateRow
				doc.System.Fields[i].Value, rows = splitAgents(doc.System.Fields[i].Value)
				doc.State = append(doc.State, rows...)
			}
		}
	}
	for i := range doc.Profiles {
		for j := range doc.Profiles[i].Fields {
			if doc.Profiles[i].Fields[j].Name == routersField {
				var rows []store.StateRow
				doc.Profiles[i].Fields[j].Value, rows = splitRouters(doc.Profiles[i].ID, doc.Profiles[i].Fields[j].Value)
				doc.State = append(doc.State, rows...)
			}
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

// exportableHALeftoverRows returns the minimal oxidns_ha_state rows that full
// exports and WebDAV backups keep for Agents left with keepalived/VIP on the
// device after an HA enable failed and rollback also failed (M2). Only the
// failed status and member list are kept; no error text, timestamp or other
// runtime state is exported.
func exportableHALeftoverRows(agents []model.Agent) ([]store.StateRow, error) {
	seen := map[string]bool{}
	var rows []store.StateRow
	for i := range agents {
		agent := &agents[i]
		if agent.ID == "" || seen[agent.ID] {
			continue
		}
		seen[agent.ID] = true
		if agent.OxiDNSHA != nil && agent.OxiDNSHA.Enabled != nil && *agent.OxiDNSHA.Enabled {
			continue
		}
		if agent.OxiDNSHAState == nil || agent.OxiDNSHAState.Status != "failed" || len(agent.OxiDNSHAState.Members) == 0 {
			continue
		}
		raw, err := model.Marshal(model.HAState{Status: "failed", Members: slices.Clone(agent.OxiDNSHAState.Members)})
		if err != nil {
			return nil, err
		}
		rows = append(rows, store.StateRow{Owner: agent.ID, Member: oxidnsHAStateKey, Value: raw})
	}
	return rows, nil
}

// isExportedHALeftover reports whether rows hold only the minimal HA leftover
// marker kept in configuration-only exports (no other state, no error or
// timestamp). Such an Agent was restored from a configuration-only backup and
// still needs the restore marker.
func isExportedHALeftover(rows []store.StateRow) bool {
	if len(rows) != 1 || rows[0].Member != oxidnsHAStateKey {
		return false
	}
	var state model.HAState
	if err := json.Unmarshal(rows[0].Value, &state); err != nil {
		return false
	}
	return state.Status == "failed" && len(state.Members) > 0 && state.Error == nil && state.UpdatedAt == 0
}

// markRestoredAgents marks every imported Agent whose record carries no state
// member (or only the exported HA leftover marker): a backup made since
// storage version 2 holds configuration only, so what its device runs is
// unknown until the next publication (design/o17-runtime-state.md 2.5.1).
// Records from an older backup that carry their state are imported with it
// and stay unmarked.
func markRestoredAgents(system *model.System, at string) error {
	raw, present, err := model.EncodeMember(system, agentsField)
	if err != nil || !present {
		return err
	}
	_, rows := splitAgents(raw)
	byOwner := map[string][]store.StateRow{}
	for _, row := range rows {
		byOwner[row.Owner] = append(byOwner[row.Owner], row)
	}
	seen := map[string]bool{}
	for i := range system.Agents {
		agent := &system.Agents[i]
		if agent.ID == "" || seen[agent.ID] {
			continue
		}
		seen[agent.ID] = true
		ownerRows := byOwner[agent.ID]
		if len(ownerRows) == 0 || isExportedHALeftover(ownerRows) {
			agent.StateRestoredAt = new(at)
		}
	}
	return nil
}

// routerStateOnly reports whether after differs from before in router state
// members (model.RouterStateMembers) only (at least one, and nothing else of
// the profile section).
func routerStateOnly(profileID string, before, after *model.Profile) (bool, error) {
	encode := func(profile *model.Profile) (map[string][]byte, []store.StateRow, error) {
		members, err := model.EncodeMembers(profile)
		if err != nil {
			return nil, nil, err
		}
		values := make(map[string][]byte, len(members))
		var state []store.StateRow
		for _, member := range members {
			value := []byte(member.Value)
			if member.Name == routersField {
				value, state = splitRouters(profileID, value)
			}
			values[member.Name] = value
		}
		delete(values, "_revision")
		delete(values, "updated_at")
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

// markRestoredRouters marks every imported router whose record carries no
// state member (last_release): a configuration-only backup holds no
// publication record, so rule sync stays fail closed until the router is
// published again. Records from an older backup carrying last_release stay
// unmarked.
func markRestoredRouters(document *model.Document, at string) error {
	for _, profile := range document.Profiles {
		profileID := profile.Key()
		raw, present, err := model.EncodeMember(profile, routersField)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		_, rows := splitRouters(profileID, raw)
		owners := map[string]bool{}
		for _, row := range rows {
			owners[row.Owner] = true
		}
		seen := map[string]bool{}
		for i := range profile.Routers.Items {
			router := &profile.Routers.Items[i]
			if router.ID == "" || seen[router.ID] {
				continue
			}
			seen[router.ID] = true
			if !owners[staterows.RouterStateOwner(profileID, router.ID)] {
				router.StateRestoredAt = new(at)
			}
		}
	}
	return nil
}
