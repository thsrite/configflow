package oxidns

import (
	"configflow/internal/devicenotes"
	"configflow/internal/jsondecode"
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
func policyRowsNode(rows []devicenotes.Policy) *Node {
	result := L()
	for _, row := range rows {
		result.Content = append(result.Content, M("ip", row.IP, "name", row.Name, "mode", row.Mode))
	}
	return result
}
func notesState(p *Profile) (devicenotes.State, error) {
	if p.notesInvalid {
		return devicenotes.State{}, fail(devicenotes.NotesMessage)
	}
	notes, policies := p.get("device_notes"), get(p.Settings(), "device_policies")
	if !has(p.doc, "device_notes") {
		notes = M()
	}
	if !has(p.Settings(), "device_policies") {
		policies = L()
	}
	state, err := devicenotes.New(ToJSON(notes), ToJSON(policies))
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
	rawNotes, rawPolicies := p.get("device_notes"), get(p.Settings(), "device_policies")
	if !has(p.doc, "device_notes") {
		rawNotes = M()
	}
	if !has(p.Settings(), "device_policies") {
		rawPolicies = L()
	}
	notes, err := devicenotes.Set(ToJSON(rawNotes), ToJSON(rawPolicies), ip, name)
	if err != nil {
		return fail(err.Error())
	}
	p.setDeviceNotes(notesNode(notes))
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
func PreparePolicyNotes(p *Profile, rows *Node) (*Node, *Node, error) {
	parsed, err := devicenotes.ParsePolicies(ToJSON(rows))
	if err != nil {
		return nil, nil, fail(err.Error())
	}
	state, err := notesState(p)
	if err != nil {
		return nil, nil, err
	}
	prepared, notes, err := state.Prepare(parsed)
	if err != nil {
		return nil, nil, fail(err.Error())
	}
	return policyRowsNode(prepared), notesNode(notes), nil
}

// SavePolicyRows writes names separately. Name-only edits retain the exact
// settings object and never regenerate the native draft.
func SavePolicyRows(p *Profile, settings, rows *Node, baseURL string) (*Node, error) {
	prepared, notes, err := PreparePolicyNotes(p, rows)
	if err != nil {
		return nil, err
	}
	p.setDeviceNotes(notes)
	current := get(settings, "device_policies")
	if !has(settings, "device_policies") {
		current = L()
	}
	before, err := devicenotes.ParsePolicies(ToJSON(current))
	if err != nil {
		return nil, fail(err.Error())
	}
	var after []devicenotes.Policy
	if err := jsondecode.Unmarshal(ToJSON(prepared), &after); err != nil {
		return nil, err
	}
	if devicenotes.SameRoutes(before, after) {
		return settings, nil
	}
	return ApplyPolicies(settings, p, prepared, baseURL)
}
