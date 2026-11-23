package oxidns

import (
	"bytes"

	"configflow/internal/devicenotes"
	"encoding/json"
	"maps"
	"slices"
)

// MaxNotes is the shared limit on stored display names.
const MaxNotes = devicenotes.MaxNotes

// NormalizeName validates a name through the shared notes package.
func NormalizeName(value *Node) (string, error) {
	if !isStr(value) {
		return "", fail(devicenotes.NameMessage)
	}
	name, err := devicenotes.NormalizeName(value.Value)
	if err != nil {
		return "", fail(err.Error())
	}
	return name, nil
}

func notesNode(notes devicenotes.Names) *Node {
	result := M()
	for _, ip := range slices.Sorted(maps.Keys(notes)) {
		set(result, ip, S(notes[ip]))
	}
	return result
}

// storedPolicies is the stored device_policies member ([] when absent).
func storedPolicies(settings *Node) json.RawMessage {
	if !has(settings, "device_policies") {
		return json.RawMessage("[]")
	}
	return ToJSON(get(settings, "device_policies"))
}
func policyRowsNode(rows []devicenotes.Policy) *Node {
	result := L()
	for _, row := range rows {
		result.Content = append(result.Content, M("ip", row.IP, "name", row.Name, "mode", row.Mode))
	}
	return result
}
func notesState(p *Profile) (devicenotes.State, error) {
	if p.notesNull {
		return devicenotes.State{}, fail(devicenotes.NotesMessage)
	}
	if p.notesNonText {
		return devicenotes.State{}, fail(devicenotes.NameMessage)
	}
	state, err := devicenotes.New(p.notes, storedPolicies(p.Settings()))
	if err != nil {
		return devicenotes.State{}, fail(err.Error())
	}
	return state, nil
}

// NormalizeNotes validates notes without keeping a second normalization policy.
func NormalizeNotes(value *Node) (*Node, error) {
	names, err := devicenotes.Parse(ToJSON(value))
	if err != nil {
		return nil, fail(err.Error())
	}
	return notesNode(names), nil
}

// StoredNames includes empty names that hide legacy policy-row names.
func StoredNames(p *Profile) (map[string]string, error) {
	state, err := notesState(p)
	if err != nil {
		return nil, err
	}
	return state.StoredNames(), nil
}

// ReadNotes returns visible names from notes and legacy policy rows.
func ReadNotes(p *Profile) (map[string]string, error) {
	state, err := notesState(p)
	if err != nil {
		return nil, err
	}
	return state.ReadNames(), nil
}

// SetNote changes only display names in the owning profile document.
func SetNote(p *Profile, ip, name string) error {
	notes, err := devicenotes.Set(p.notes, storedPolicies(p.Settings()), ip, name)
	if err != nil {
		return fail(err.Error())
	}
	p.setDeviceNotes(ToJSON(notesNode(notes)))
	return nil
}

// PolicyNamesRevision identifies names without changing policy settings.
func PolicyNamesRevision(p *Profile) (string, error) {
	state, err := notesState(p)
	if err != nil {
		return "", err
	}
	return state.Revision()
}

// PreparePolicyNotes separates policy edits from the display-name write plan.
func PreparePolicyNotes(p *Profile, rows []devicenotes.Policy) ([]devicenotes.Policy, devicenotes.Names, error) {
	state, err := notesState(p)
	if err != nil {
		return nil, nil, err
	}
	prepared, notes, err := state.Prepare(rows)
	if err != nil {
		return nil, nil, fail(err.Error())
	}
	return prepared, notes, nil
}

// SavePolicyRows writes names separately. Name-only edits retain the exact
// settings object and never regenerate the native draft.
func SavePolicyRows(p *Profile, settings *Node, rows []devicenotes.Policy, baseURL string) (*Node, error) {
	prepared, notes, err := PreparePolicyNotes(p, rows)
	if err != nil {
		return nil, err
	}
	p.setDeviceNotes(ToJSON(notesNode(notes)))
	before, err := devicenotes.ParsePolicies(storedPolicies(settings))
	if err != nil {
		return nil, fail(err.Error())
	}
	if devicenotes.SameRoutes(before, prepared) {
		return settings, nil
	}
	return ApplyPolicies(settings, p, prepared, baseURL)
}

// DeviceNotesJSON returns the native notes after SetNote or SavePolicyRows.
// The caller commits this value through its profile transaction.
func (p *Profile) DeviceNotesJSON() json.RawMessage { return bytes.Clone(p.notes) }
