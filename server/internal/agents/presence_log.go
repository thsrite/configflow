package agents

// Agent presence is process-local: an Agent is followed after the first online
// scan, and a restart never logs every offline record as a new transition.
import (
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"encoding/json"
	"sync"

	"configflow/internal/logx"
	"configflow/internal/redact"
)

var presenceLogger = logx.Get("backend.agents.presence_log")

// PresenceObservation contains only the selected log-facing registry metadata.
// No metrics, history or captured configuration belongs in this DTO.
type PresenceObservation struct{ ID, Status, Name, LastHeartbeat string }
type presence struct{ status, name, heartbeat string }

var (
	presenceMu    sync.Mutex
	presenceKnown = map[string]presence{}
)

// ObserveAgentPresence logs transitions after sanitizing each display string.
// Only sanitized display values are retained in the process-local state.
func ObserveAgentPresence(records []PresenceObservation, secrets redact.Secrets) {
	presenceMu.Lock()
	defer presenceMu.Unlock()
	current := make(map[string]presence, len(records))
	order := make([]string, 0, len(records))
	for _, record := range records {
		if record.ID == "" {
			continue
		}
		status := "offline"
		if record.Status == "online" {
			status = "online"
		}
		if _, known := presenceKnown[record.ID]; status != "online" && !known {
			continue
		}
		name := record.Name
		if name == "" {
			name = record.ID
		}
		name = redact.DiagnosticText(name, secrets, 120)
		heartbeat := redact.DiagnosticText(record.LastHeartbeat, secrets, 32)
		if _, seen := current[record.ID]; !seen {
			order = append(order, record.ID)
		}
		current[record.ID] = presence{status, name, heartbeat}
	}
	for _, id := range order {
		item := current[id]
		previous, known := presenceKnown[id]
		if !known || previous.status == item.status {
			continue
		}
		if item.status == "online" {
			presenceLogger.Infof(msgPresenceOnline, logx.LogText(item.name, 120))
		} else {
			presenceLogger.Warningf(msgPresenceOffline, logx.LogText(item.name, 120), logx.LogText(item.heartbeat, 32))
		}
	}
	presenceKnown = current
}
func resetPresence() { presenceMu.Lock(); presenceKnown = map[string]presence{}; presenceMu.Unlock() }

// PresenceRecord holds the raw presence-relevant fields of one Agent projected
// without copying deployment, verification or metrics histories.
type PresenceRecord struct {
	ID           string
	ValidID      bool
	Status       string
	NameRaw      json.RawMessage
	NameErr      error
	HeartbeatRaw json.RawMessage
	HeartbeatErr error
}

// ObservePresenceRecords renders and records presence transitions from narrow
// PresenceRecord projections. Histories never enter this path.
func ObservePresenceRecords(records []PresenceRecord, secrets redact.Secrets) {
	observations := make([]PresenceObservation, 0, len(records))
	for i := range records {
		r := &records[i]
		if !r.ValidID {
			continue
		}
		observations = append(observations, PresenceObservation{
			ID:            r.ID,
			Status:        r.Status,
			Name:          registryDisplay(r.NameRaw, r.NameErr, secrets),
			LastHeartbeat: registryDisplay(r.HeartbeatRaw, r.HeartbeatErr, secrets),
		})
	}
	ObserveAgentPresence(observations, secrets)
}

// ObserveRegistryPresence projects only original log metadata and sanitizes rich
// imported values before rendering them. Histories never enter this projection.
func ObserveRegistryPresence(records []*model.Agent, secrets redact.Secrets) {
	projected := make([]PresenceRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		projected = append(projected, presenceRecordOf(record, "", false, false))
	}
	ObservePresenceRecords(projected, secrets)
}

func presenceRecordOf(record *model.Agent, liveHeartbeat string, hasLive, applyFreshness bool) PresenceRecord {
	id, valid := storedText(record.MemberJSON("id"))
	if !valid {
		return PresenceRecord{}
	}
	var status string
	var hbRaw json.RawMessage
	var hbErr error
	effectiveHeartbeat := record.LastHeartbeat
	if hasLive {
		status = "online"
		effectiveHeartbeat = liveHeartbeat
		hbRaw, hbErr = json.Marshal(liveHeartbeat)
	} else {
		status, _ = storedText(record.MemberJSON("status"))
		hbRaw, _, hbErr = record.MemberJSON("last_heartbeat")
	}
	if applyFreshness && !FreshHeartbeat(effectiveHeartbeat, nil) {
		status = "offline"
	}
	name, present, nameErr := record.MemberJSON("name")
	content, contentErr := model.JSONHasContent(name)
	if nameErr == nil && contentErr == nil && (!present || !content) {
		name, _, nameErr = record.MemberJSON("id")
	}
	return PresenceRecord{
		ID:           id,
		ValidID:      true,
		Status:       status,
		NameRaw:      name,
		NameErr:      nameErr,
		HeartbeatRaw: hbRaw,
		HeartbeatErr: hbErr,
	}
}
func registryDisplay(raw json.RawMessage, failure error, secrets redact.Secrets) string {
	if failure != nil {
		return redact.Redacted
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text *string
	if json.Unmarshal(raw, &text) == nil && text != nil {
		return *text
	}
	safe := redact.SanitizeCredentials(raw, secrets)
	data, err := jsonresp.EncodeUTF8(safe)
	if err != nil {
		return redact.Redacted
	}
	return string(data)
}
