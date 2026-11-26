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

var (
	presenceMu sync.Mutex
	// presenceKnown maps each followed Agent to its last status ("online" or
	// "offline"). Display values are rendered only for a logged transition and
	// never retained.
	presenceKnown = map[string]string{}
)

// ObserveAgentPresence logs transitions after sanitizing the display strings of
// the transitioning records. Only statuses are retained in process-local state.
func ObserveAgentPresence(records []PresenceObservation, secrets redact.Secrets) {
	observePresence(len(records), func(i int) (string, string) { return records[i].ID, records[i].Status },
		func(i int) (string, string) {
			return presenceDisplay(records[i].ID, records[i].Name, records[i].LastHeartbeat, secrets)
		})
}

func presenceDisplay(id, name, heartbeat string, secrets redact.Secrets) (string, string) {
	if name == "" {
		name = id
	}
	return redact.DiagnosticText(name, secrets, 120), redact.DiagnosticText(heartbeat, secrets, 32)
}

// observePresence follows n records (identity and raw status from record) and
// logs each status change. render runs only for a record whose transition is
// logged: an idle scan compares statuses and renders nothing. A repeated ID
// keeps its last followed record, as before.
func observePresence(n int, record func(i int) (id, status string), render func(i int) (name, heartbeat string)) {
	presenceMu.Lock()
	defer presenceMu.Unlock()
	current := make(map[string]string, n)
	latest := make(map[string]int, n)
	order := make([]string, 0, n)
	for i := range n {
		id, status := record(i)
		if id == "" {
			continue
		}
		if status != "online" {
			status = "offline"
		}
		if _, known := presenceKnown[id]; status != "online" && !known {
			continue
		}
		if _, seen := current[id]; !seen {
			order = append(order, id)
		}
		current[id] = status
		latest[id] = i
	}
	for _, id := range order {
		status := current[id]
		previous, known := presenceKnown[id]
		if !known || previous == status {
			continue
		}
		name, heartbeat := render(latest[id])
		if status == "online" {
			presenceLogger.Infof(msgPresenceOnline, logx.LogText(name, 120))
		} else {
			presenceLogger.Warningf(msgPresenceOffline, logx.LogText(name, 120), logx.LogText(heartbeat, 32))
		}
	}
	presenceKnown = current
}
func resetPresence() { presenceMu.Lock(); presenceKnown = map[string]string{}; presenceMu.Unlock() }

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

// PresenceSecrets returns the credentials a presence line must mask. It is
// called at most once per observation, and only when a transition is logged.
// An error hides the name and heartbeat of every logged line.
type PresenceSecrets func() (redact.Secrets, error)

// ObservePresenceRecords renders and records presence transitions from narrow
// PresenceRecord projections. Histories never enter this path, and secrets is
// consulted only when a line is written.
func ObservePresenceRecords(records []PresenceRecord, secrets PresenceSecrets) {
	var (
		loaded bool
		values redact.Secrets
		failed bool
	)
	observePresence(len(records), func(i int) (string, string) {
		if !records[i].ValidID {
			return "", ""
		}
		return records[i].ID, records[i].Status
	}, func(i int) (string, string) {
		if !loaded {
			loaded = true
			var err error
			values, err = secrets()
			failed = err != nil
		}
		r := &records[i]
		if failed {
			return presenceDisplay(r.ID, redact.Redacted, redact.Redacted, nil)
		}
		return presenceDisplay(r.ID, registryDisplay(r.NameRaw, r.NameErr, values), registryDisplay(r.HeartbeatRaw, r.HeartbeatErr, values), values)
	})
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
	ObservePresenceRecords(projected, func() (redact.Secrets, error) { return secrets, nil })
}

func presenceRecordOf(record *model.Agent, liveHeartbeat string, hasLive, applyFreshness bool) PresenceRecord {
	id := record.ID
	if id == "" {
		return PresenceRecord{}
	}
	status := record.Status
	effectiveHeartbeat := record.LastHeartbeat
	if hasLive {
		status = "online"
		effectiveHeartbeat = liveHeartbeat
	}
	if applyFreshness && !FreshHeartbeat(effectiveHeartbeat, nil) {
		status = "offline"
	}
	hbRaw, hbErr := json.Marshal(effectiveHeartbeat)
	label := record.Name
	if label == "" {
		label = id
	}
	name, nameErr := json.Marshal(label)
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
