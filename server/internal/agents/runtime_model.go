package agents

import "configflow/internal/model"

// overlayModels operates only on owned records. Every applied runtime member
// is copied by the shared codec; nested metrics never alias the live store.
func (s *RuntimeState) overlayModels(records []model.Agent) []model.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range records {
		record := &records[i]
		entry, ok := s.newerModel(record)
		if !ok {
			continue
		}
		for i, key := range RuntimeFields {
			raw := entry.Fields[i]
			if raw != nil {
				_ = record.SetJSON(key, raw)
			}
		}
		record.LastHeartbeat = entry.LastHeartbeat
		record.Status = "online"
	}
	return records
}
