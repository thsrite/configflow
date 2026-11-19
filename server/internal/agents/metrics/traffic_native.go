package metrics

import (
	"bytes"
	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"math/big"
	"strconv"
)

type trafficFields map[string]json.RawMessage

func integerJSON(raw json.RawMessage) (*big.Int, bool) {
	text := string(raw)
	if text == "" {
		return nil, false
	}
	for i, c := range text {
		if c < '0' || c > '9' {
			if i != 0 || c != '-' {
				return nil, false
			}
		}
	}
	return new(big.Int).SetString(text, 10)
}

// TrafficTotals are exact counters, including REAL values from existing SQLite files.
type TrafficTotals struct {
	Up    json.RawMessage `json:"up"`
	Down  json.RawMessage `json:"down"`
	Conns json.RawMessage `json:"conns"`
}

func trafficTotals(t triple) TrafficTotals {
	return TrafficTotals{t[0].json(), t[1].json(), t[2].json()}
}

// TrafficRow is an aggregate and its bounded detail rows.
type TrafficRow struct {
	Key string `json:"key"`
	TrafficTotals
	Parts     []TrafficRow `json:"parts,omitempty"`
	PartCount *int         `json:"part_count,omitempty"`
}

// MarshalJSON includes the empty detail array for top-level rows.
func (r TrafficRow) MarshalJSON() ([]byte, error) {
	type row TrafficRow
	if r.PartCount == nil {
		return json.Marshal(row(r))
	}
	return json.Marshal(struct {
		Key string `json:"key"`
		TrafficTotals
		Parts     []TrafficRow `json:"parts"`
		PartCount int          `json:"part_count"`
	}{r.Key, r.TrafficTotals, r.Parts, *r.PartCount})
}

// TrafficPoint is one UTC trend bucket.
type TrafficPoint struct {
	At   string          `json:"at"`
	Up   json.RawMessage `json:"up"`
	Down json.RawMessage `json:"down"`
}

// TrafficQuery is the native traffic history response.
type TrafficQuery struct {
	Range          string         `json:"range"`
	By             string         `json:"by"`
	Step           int64          `json:"step"`
	ChainSeparator string         `json:"chain_separator"`
	Start          string         `json:"start"`
	Rows           []TrafficRow   `json:"rows"`
	Omitted        int            `json:"omitted"`
	Total          TrafficTotals  `json:"total"`
	Outbounds      []TrafficRow   `json:"outbounds"`
	Trend          []TrafficPoint `json:"trend"`
}

// TrafficRetention reads the exact system_config member with the established default.
func TrafficRetention(system *model.System) (int64, error) {
	raw, err := json.Marshal(system)
	if err != nil {
		return 0, err
	}
	fields, err := jsondecode.Select(raw, "system_config")
	if err != nil {
		return 0, err
	}
	return retentionDaysJSON(fields["system_config"])
}

// SetTrafficRetention stores a validated retention choice without losing unknown settings.
func SetTrafficRetention(ctx context.Context, m *agents.Manager, days int64) (int64, error) {
	if !allowedRetention(days) {
		return 0, RetentionValueError
	}
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	err := m.Repository().UpdateSystem(ctx, func(ctx context.Context, s *model.System) error {
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		fields, err := jsondecode.Select(raw, "system_config")
		if err != nil {
			return err
		}
		member, exists := fields["system_config"]
		if exists && !bytes.Equal(bytes.TrimSpace(member), []byte("null")) && (len(bytes.TrimSpace(member)) == 0 || bytes.TrimSpace(member)[0] != '{') {
			return &AttributeError{Message: msgTrafficSettingsShape}
		}
		if exists && bytes.Equal(bytes.TrimSpace(member), []byte("null")) {
			return &AttributeError{Message: msgTrafficSettingsShape}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.SystemConfig.SetJSON(RetentionSetting, json.RawMessage(strconv.FormatInt(days, 10)))
	})
	return days, err
}
func allowedRetention(days int64) bool {
	for _, choice := range RetentionChoices {
		if days == choice {
			return true
		}
	}
	return false
}

// ValueError reports an invalid traffic setting.
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

// RetentionValueError is returned for an unsupported retention interval.
var RetentionValueError = &ValueError{Message: msgTrafficRetention}
