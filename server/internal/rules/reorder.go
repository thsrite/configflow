package rules

// Reorder moves records identified by ids to the requested end without
// replacing their stored fields. A duplicate stored ID retains the last row.
// Missing IDs leave the original collection untouched.
func Reorder[T any](items []T, ids []string, position string, idOf func(T) string) (ordered []T, missing []string) {
	byID := make(map[string]T, len(items))
	for _, item := range items {
		byID[idOf(item)] = item
	}
	selected := make(map[string]bool, len(ids))
	moved := make([]T, 0, len(ids))
	for _, id := range ids {
		item, ok := byID[id]
		if !ok {
			missing = append(missing, id)
		}
		selected[id] = true
		moved = append(moved, item)
	}
	if len(missing) > 0 {
		return items, missing
	}
	rest := make([]T, 0, len(items))
	for _, item := range items {
		if !selected[idOf(item)] {
			rest = append(rest, item)
		}
	}
	if position == "top" {
		return append(moved, rest...), nil
	}
	return append(rest, moved...), nil
}
