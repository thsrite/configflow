package tasks

import (
	"configflow/internal/jsondecode"
	"encoding/json"
	"fmt"

	"configflow/internal/taskstatus"
)

// HistoryData is the displayed projection of one persisted receipt.
type HistoryData struct {
	LastRunAt           *string         `json:"last_run_at"`
	LastStatus          *string         `json:"last_status"`
	LastError           *string         `json:"last_error"`
	LastDurationMs      json.RawMessage `json:"last_duration_ms"`
	LastTrigger         *string         `json:"last_trigger"`
	LastSuccessAt       *string         `json:"last_success_at"`
	RunCount            json.Number     `json:"run_count"`
	FailureCount        json.Number     `json:"failure_count"`
	ConsecutiveFailures json.RawMessage `json:"consecutive_failures"`
	CacheRetained       *bool           `json:"cache_retained"`
	CacheUpdatedAt      *string         `json:"cache_updated_at"`
}

// ProjectHistory validates counters without losing arbitrary precision.
func ProjectHistory(entry *taskstatus.Receipt) (*HistoryData, error) {
	if entry == nil {
		entry = &taskstatus.Receipt{}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var result HistoryData
	// Counters can be decimal strings in old files; decode those separately.
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	delete(fields, "run_count")
	delete(fields, "failure_count")
	data, err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err = jsondecode.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidHistory, err)
	}
	run, err := taskstatus.Count(entry.RunCount)
	if err != nil {
		return nil, fmt.Errorf("%w: run_count: %w", ErrInvalidHistory, err)
	}
	failed, err := taskstatus.Count(entry.FailureCount)
	if err != nil {
		return nil, fmt.Errorf("%w: failure_count: %w", ErrInvalidHistory, err)
	}
	result.RunCount = json.Number(run.String())
	result.FailureCount = json.Number(failed.String())
	return &result, nil
}

// Task is one task row. Extra retains owner-specific response members while
// scheduled-task behavior uses only declared fields.
type Task struct {
	Key                  string          `json:"key"`
	Category             string          `json:"category"`
	Name                 string          `json:"name"`
	Detail               string          `json:"detail"`
	ResourceID           string          `json:"resource_id"`
	ProfileID            *string         `json:"profile_id"`
	ProfileName          *string         `json:"profile_name"`
	Enabled              bool            `json:"enabled"`
	IntervalSeconds      json.RawMessage `json:"interval_seconds"`
	ScheduleText         string          `json:"schedule_text"`
	Editable             bool            `json:"editable"`
	CanRun               bool            `json:"can_run"`
	RunHint              string          `json:"run_hint"`
	Running              bool            `json:"running"`
	NextRunAt            *string         `json:"next_run_at"`
	StateMessage         string          `json:"state_message"`
	Link                 *string         `json:"link"`
	NotificationsEnabled *bool           `json:"notifications_enabled,omitempty"`
	*HistoryData
	Extra map[string]json.RawMessage `json:"-"`
}

type taskWire Task

// UnmarshalJSON applies row defaults, preserving owner-specific members.
func (t *Task) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if object == nil {
		return fmt.Errorf("%s", msgRowShape)
	}
	result := taskWire{Enabled: true}
	if err := jsondecode.Unmarshal(data, &result); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &known); err != nil {
		return err
	}
	for key := range known {
		delete(fields, key)
	}
	result.Extra = fields
	*t = Task(result)
	return nil
}

// MarshalJSON combines the typed row with unowned response fields.
func (t Task) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(taskWire(t))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range t.Extra {
		if _, present := fields[key]; !present {
			fields[key] = value
		}
	}
	return json.Marshal(fields)
}
