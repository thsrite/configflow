package trafficstore

import (
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"math/big"
	"time"
)

// CleanReport transcodes dynamic input to the native exact-key decoder.
func CleanReport(value any) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	r, err := DecodeReport(raw)
	if err != nil || r == nil {
		return nil, err
	}
	return toObject(r)
}

// AddReport transcodes a cleaned report and native status response.
func (s *Store) AddReport(id string, report *jsonx.Object, at *time.Time) (*jsonx.Object, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	r, err := decodeCleanedReport(raw)
	if err != nil {
		return nil, err
	}
	status, err := s.Add(context.Background(), id, r, at)
	if err != nil {
		return nil, err
	}
	return toObject(status)
}

// SplitRouteKey adapts the typed route decoder.
func SplitRouteKey(key string) (*jsonx.Object, bool) {
	r, ok := ParseRouteKey(key)
	if !ok {
		return nil, false
	}
	out, _ := toObject(r)
	return out, true
}

// CachedStatus returns an independent compatibility cache snapshot.
func CachedStatus(path string) *jsonx.Object {
	mu.Lock()
	defer mu.Unlock()
	if e, ok := statusIndex[path]; ok {
		out, _ := toObject(e.Value.(*cacheEntry).status)
		return out
	}
	return nil
}

func decodeCleanedReport(raw json.RawMessage) (*Report, error) {
	var f model.Fields
	if err := jsondecode.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if !f.Has("schema") {
		if err := f.Set("schema", json.RawMessage("1")); err != nil {
			return nil, err
		}
	}
	dropped := member(f, "dropped")
	if len(dropped) == 0 {
		dropped = []byte("0")
	}
	n, ok := new(big.Int).SetString(string(dropped), 10)
	if !ok || n.Sign() < 0 {
		return nil, &TypeError{Message: "invalid cleaned dropped counter"}
	}
	if err := f.Set("dropped", json.RawMessage("0")); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	report, err := DecodeReport(raw)
	if report != nil {
		report.Dropped = n
	}
	return report, err
}
