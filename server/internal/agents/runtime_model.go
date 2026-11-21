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

// liveHeartbeats returns the live heartbeat timestamp for each stored record
// whose in-memory heartbeat is newer, acquiring the runtime mutex once.
func (s *RuntimeState) liveHeartbeats(records []model.Agent) map[int]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return nil
	}
	var newer map[int]string
	for i := range records {
		if entry, ok := s.newerModel(&records[i]); ok {
			if newer == nil {
				newer = make(map[int]string, len(records))
			}
			newer[i] = entry.LastHeartbeat
		}
	}
	return newer
}
