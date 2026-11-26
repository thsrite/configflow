package subs

import (
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// CompareHosts compares entry values without changing or normalizing stored metadata.
func CompareHosts(previous, current *HostsData) (HostChange, error) {
	oldEntries, newEntries := model.OrderedMap[model.Dynamic]{}, model.OrderedMap[model.Dynamic]{}
	if previous != nil {
		oldEntries = previous.Entries
	}
	if current != nil {
		newEntries = current.Entries
	}
	old := make(map[string]model.Dynamic, oldEntries.Len())
	for key, value := range oldEntries.All() {
		old[key] = value
	}
	change := HostChange{}
	for key, value := range newEntries.All() {
		prior, ok := old[key]
		if !ok {
			change.Added++
			continue
		}
		left, err := value.Node()
		if err != nil {
			return HostChange{}, err
		}
		right, err := prior.Node()
		if err != nil {
			return HostChange{}, err
		}
		if !yamlx.Equal(left, right) {
			change.Changed++
		}
		delete(old, key)
	}
	change.Removed = len(old)
	return change, nil
}
