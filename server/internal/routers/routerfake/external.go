package routerfake

import "slices"

// ExternalWrite simulates an external writer between apply and rollback. Counters
// restart for its new faults, while allocated IDs and the transcript are kept.
type ExternalWrite struct {
	Fault
	Faults []Fault `json:"faults,omitempty"`
}

// ApplyExternal applies test-only external changes without resetting device identity.
func (f *Fake) ApplyExternal(m ExternalWrite) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyMutation(m.Fault, true)
	f.faults = slices.Clone(m.Faults)
	f.counts = map[string]int{}
}
func (f *Fake) applyMutation(fault Fault, extra bool) {
	for collection, patches := range fault.PatchRows {
		for id, patch := range patches {
			if at := rowIndex(f.tables[collection], id); at >= 0 {
				for key, value := range patch {
					f.tables[collection][at][key] = value
				}
			}
		}
	}
	for collection, ids := range fault.RemoveIDs {
		f.tables[collection] = slices.DeleteFunc(f.tables[collection], func(row Row) bool { return slices.Contains(ids, text(row[".id"])) })
	}
	if extra {
		for collection, rows := range fault.ExtraRows {
			for _, row := range rows {
				f.tables[collection] = append(f.tables[collection], cloneRow(row))
			}
		}
	}
}
