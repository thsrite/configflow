package routerfake

// ReplaceTables models an external writer or table reorder without resetting
// the device's ID allocator, request counters, or recorded wire transcript.
func (f *Fake) ReplaceTables(tables map[string][]Row) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables = cloneTables(tables)
}
