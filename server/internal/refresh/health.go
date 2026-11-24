package refresh

import (
	"bytes"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Constants of subscription_health.py.
const (
	HealthFile      = "subscription_health.json"
	HistorySize     = 24
	UserinfoTimeout = 6
	// UserinfoUserAgent: most airports send the traffic header only to
	// Clash-family user agents.
	UserinfoUserAgent = "clash-verge/v2.0.0"
)

var healthLog = logx.Get("backend.utils.subscription_health")

// isoMicro is datetime.isoformat() of an aware UTC time: microseconds only
// when non-zero.
func isoMicro(t time.Time) string {
	t = t.UTC()
	text := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		text += fmt.Sprintf(".%06d", us)
	}
	return text + "+00:00"
}

// healthNow is subscription_health._now().
func (s *Service) healthNow() string {
	return strings.Replace(isoMicro(s.currentEnv().Clock.Now()), "+00:00", "Z", 1)
}

// FetchTraffic reads provider quota headers; non-HTTP sources are ignored.
func (s *Service) FetchTraffic(ctx context.Context, address string) (*Traffic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lower := strings.ToLower(address)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return nil, nil
	}
	response, err := s.currentEnv().HTTP.Do(ctx, &substore.Request{Method: "GET", URL: address, Header: http.Header{"User-Agent": {UserinfoUserAgent}}, Timeout: substore.Seconds(UserinfoTimeout)})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var request *substore.RequestError
		if errors.As(err, &request) {
			healthLog.Infof(msgTrafficFailed, weburl.SafeErrorDetails(err))
			return nil, nil
		}
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, nil
	}
	return parseTraffic(response.Header.Get("subscription-userinfo"))
}

// FetchEvent is a bounded health-history point (subscription_health.record).
type FetchEvent struct {
	At     string     `json:"at"`
	Status string     `json:"status"`
	Count  *model.Int `json:"count"`
}

// HealthEntry is one subscription's history and latest traffic; traffic and
// traffic_at are written once traffic was reported.
type HealthEntry struct {
	History   []FetchEvent `json:"history"`
	Traffic   *Traffic     `json:"traffic,omitzero"`
	TrafficAt *string      `json:"traffic_at,omitzero"`
}

// HealthSnapshot keeps every subscription's entry as stored; only the entry
// that is recorded is rewritten.
type HealthSnapshot map[string]json.RawMessage

// Entry returns an independent typed entry. A member of the wrong type reads
// as its zero value; an entry that is not an object reads as empty.
func (s HealthSnapshot) Entry(id string) *HealthEntry {
	var entry HealthEntry
	if raw := bytes.TrimSpace(s[id]); len(raw) > 0 && raw[0] == '{' {
		_, _ = model.DecodeStored(raw, &entry, "")
	}
	return &entry
}

// ReadHealth reads health history without writing or creating a directory.
func (s *Service) ReadHealth(ctx context.Context) (HealthSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := s.repository()
	if err != nil {
		return HealthSnapshot{}, nil
	}
	data, err := readSharedData(ctx, r, HealthFile)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if isRepositoryOrFileError(err) {
			return HealthSnapshot{}, nil
		}
		return nil, err
	}
	var result HealthSnapshot
	if json.Unmarshal(data, &result) != nil || result == nil {
		return HealthSnapshot{}, nil
	}
	return result, nil
}
func (s *Service) saveHealthData(ctx context.Context, data HealthSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := s.repository()
	if err != nil {
		healthLog.Errorf(msgHealthWriteFailed, weburl.SafeErrorDetails(err))
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeSharedData(ctx, r, HealthFile, raw); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if isRepositoryOrFileError(err) {
			healthLog.Errorf(msgHealthWriteFailed, weburl.SafeErrorDetails(err))
			return nil
		}
		return err
	}
	return nil
}

// FetchEventInput is the outcome of one admitted source fetch.
type FetchEventInput struct {
	OK        bool
	Count     model.Int
	FromCache bool
	Traffic   *Traffic
}

// RecordHealth appends one event and commits under the originating generation.
func (s *Service) RecordHealth(ctx context.Context, id string, input FetchEventInput) (*HealthEntry, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		return &HealthEntry{}, nil
	}
	data, err := s.ReadHealth(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry := data.Entry(id)
	history := append([]FetchEvent(nil), entry.History...)
	if keep := HistorySize - 1; len(history) > keep {
		history = history[len(history)-keep:]
	}
	status := "fail"
	if input.FromCache {
		status = "cache"
	} else if input.OK {
		status = "ok"
	}
	count := input.Count
	entry.History = append(history, FetchEvent{At: s.healthNow(), Status: status, Count: &count})
	if !input.Traffic.empty() {
		entry.Traffic = input.Traffic
		at := s.healthNow()
		entry.TrafficAt = &at
	}
	raw, err := model.Marshal(entry)
	if err != nil {
		return nil, err
	}
	data[id] = raw
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.saveHealthData(ctx, data); err != nil {
		return nil, err
	}
	return entry, nil
}

// ForgetHealth deletes one entry; null/absent entries cause no write.
func (s *Service) ForgetHealth(ctx context.Context, id string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := s.ReadHealth(ctx)
	if err != nil {
		return err
	}
	raw, present := data[id]
	if !present || string(raw) == "null" {
		return nil
	}
	delete(data, id)
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.saveHealthData(ctx, data)
}
