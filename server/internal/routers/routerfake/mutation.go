package routerfake

// Mutation models an external writer immediately before a read or after a write.
// Count is the request count for the exact Method/Path, independently of faults.
type Mutation struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Count  int    `json:"count"`
	Table  string `json:"table"`
	ID     string `json:"id,omitempty"`
	Set    Row    `json:"set,omitempty"`
	Append Row    `json:"append,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

func (f *Fake) mutateLocked(method, path string) {
	for _, m := range f.mutations {
		if m.Method != method || m.Path != path || m.Count != f.counts[method+" "+path] {
			continue
		}
		rows := f.tables[m.Table]
		for i := len(rows) - 1; i >= 0; i-- {
			if text(rows[i][".id"]) == m.ID {
				if m.Remove {
					rows = append(rows[:i], rows[i+1:]...)
				} else {
					for k, v := range m.Set {
						rows[i][k] = v
					}
				}
			}
		}
		if m.Append != nil {
			rows = append(rows, cloneRow(m.Append))
		}
		f.tables[m.Table] = rows
	}
}
