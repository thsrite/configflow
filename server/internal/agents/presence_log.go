package agents

// Agent presence is process-local: an Agent is followed after the first online
// scan, and a restart never logs every offline record as a new transition.
import (
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
