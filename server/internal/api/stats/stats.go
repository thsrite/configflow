// Package stats exposes overview counts and optional per-profile history.
package stats

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/timefmt"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/jsonstore"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

// Handler serves the /api/stats routes backed by an explicit Repository.
type Handler struct {
	Repo      *repo.Repository
	Now       func() time.Time
	historyMu sync.Mutex
	history   *historyView
}

// NewHandler constructs a Handler for r.
func NewHandler(r *repo.Repository) *Handler {
	return &Handler{Repo: r, history: &historyView{series: make(map[string][]*json.Number)}}
}

func (h *Handler) repo(_ context.Context) (*repo.Repository, error) {
	if h != nil && h.Repo != nil {
		return h.Repo, nil
	}
	return nil, errors.New("api/stats: repository is nil")
}

func (h *Handler) resolveProfileID(ctx context.Context) (string, error) {
	r, err := h.repo(ctx)
	if err != nil {
		return "", err
	}
	return r.ResolveProfileID(ctx, "")
}

func (h *Handler) now() time.Time {
	if h != nil && h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Register adds the overview route to r.
func (h *Handler) Register(r *httpx.Router) {
	r.GET("/api/stats/overview", "stats.get_overview", h.getOverview)
}

// Sampling and retention constants.
const (
	HistoryFile    = "stats_history.json"
	HistorySize    = 48
	SampleInterval = 30 * time.Minute
)

// Keys is the order of overview series.
var Keys = []string{"subscriptions", "nodes", "proxyGroups", "rules"}

// Counts is the current typed catalog projection.
type Counts struct {
	Subscriptions int `json:"subscriptions"`
	Nodes         int `json:"nodes"`
	ProxyGroups   int `json:"proxyGroups"`
	Rules         int `json:"rules"`
}

// historyDocument is the overview history file: {"samples": [...]}.
type historyDocument struct {
	Samples []json.RawMessage `json:"samples"`
}

type historyWire historyDocument

// UnmarshalJSON reads the file leniently: samples that are not a list read
// as no samples (the next sample starts a new history).
func (d *historyDocument) UnmarshalJSON(data []byte) error {
	return model.DecodeLenient(data, (*historyWire)(d))
}

type sample struct {
	At            string          `json:"at"`
	Subscriptions json.RawMessage `json:"subscriptions"`
	Nodes         json.RawMessage `json:"nodes"`
	ProxyGroups   json.RawMessage `json:"proxyGroups"`
	Rules         json.RawMessage `json:"rules"`
}

type historyView struct {
	path    string
	sig     jsonstore.Signature
	counts  Counts
	sampled time.Time
	series  map[string][]*json.Number
	samples []json.RawMessage
}

func (h *Handler) recordHistory(ctx context.Context, profileID string, counts Counts, persist bool) (map[string][]*json.Number, error) {
	if persist {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repository, err := h.repo(ctx)
	if err != nil {
		return nil, err
	}
	// The repository retains profile-operation locking and replacement checks.
	path, err := repository.ProfilePath(profileID, HistoryFile)
	if err != nil {
		return nil, err
	}
	file := filepath.Join(filepath.Dir(path), HistoryFile)
	sig, statErr := jsonstore.FileSignature(file)
	h.historyMu.Lock()
	cached := h.history
	h.historyMu.Unlock()
	hit := statErr == nil && cached != nil && cached.path == file && cached.sig == sig
	var document historyDocument
	if hit {
		document.Samples = slices.Clip(cached.samples)
	} else {
		_ = jsonstore.New(filepath.Dir(path)).Read(strings.TrimSuffix(HistoryFile, ".json"), &document)
	}
	samples := document.Samples
	now := h.now().UTC()
	if hit && cached.counts == counts && now.Sub(cached.sampled) < SampleInterval {
		return cached.series, nil
	}
	var lastAt *time.Time
	if len(samples) > 0 {
		var last sample
		if err := jsondecode.Unmarshal(samples[len(samples)-1], &last); err != nil || string(samples[len(samples)-1]) == "null" {
			return nil, fmt.Errorf("%s", msgSample)
		}
		if stamp, ok := taskstatus.ParseTimestamp(last.At); ok {
			lastAt = &stamp
		}
	}
	current, err := json.Marshal(struct {
		At string `json:"at"`
		Counts
	}{timefmt.UTCZ(now), counts})
	if err != nil {
		return nil, err
	}
	var series []json.RawMessage
	if lastAt == nil || now.Sub(*lastAt) >= SampleInterval {
		samples = append(samples, current)
		if len(samples) > HistorySize {
			samples = samples[len(samples)-HistorySize:]
		}
		if persist {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			document.Samples = samples
			data, err := json.Marshal(document)
			if err != nil {
				return nil, err
			}
			_, _ = repository.WriteProfileText(ctx, profileID, HistoryFile, data)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		series = samples
	} else {
		series = append(append([]json.RawMessage{}, samples[:len(samples)-1]...), current)
	}
	result := map[string][]*json.Number{}
	for _, key := range Keys {
		result[key] = []*json.Number{}
	}
	for _, raw := range series {
		var row sample
		if err := jsondecode.Unmarshal(raw, &row); err != nil || string(raw) == "null" {
			return nil, fmt.Errorf("%s", msgSample)
		}
		values := []json.RawMessage{row.Subscriptions, row.Nodes, row.ProxyGroups, row.Rules}
		for i, key := range Keys {
			count, err := historyNumber(values[i])
			if err != nil {
				return nil, fmt.Errorf(msgCounter, err)
			}
			result[key] = append(result[key], count)
		}
	}
	if lastAt != nil && now.Sub(*lastAt) < SampleInterval && statErr == nil {
		if after, err := jsonstore.FileSignature(file); err == nil && sig == after {
			h.historyMu.Lock()
			h.history = &historyView{path: file, sig: sig, counts: counts, sampled: *lastAt, series: result, samples: samples}
			h.historyMu.Unlock()
		}
	}
	return result, nil
}
func (h *Handler) getOverview(c *httpx.Ctx) (*httpx.Response, error) {
	result, err := h.overview(c.Context(), c.Request.Method != http.MethodHead)
	if err != nil {
		return httpx.JSONStatus(500, struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}{false, err.Error()}), nil
	}
	return result, nil
}

type overviewSeries struct {
	Total   int            `json:"total"`
	History []*json.Number `json:"history"`
}
type overviewData struct {
	Subscriptions overviewSeries `json:"subscriptions"`
	Nodes         overviewSeries `json:"nodes"`
	ProxyGroups   overviewSeries `json:"proxyGroups"`
	Rules         overviewSeries `json:"rules"`
	Profile       struct {
		Revision  int64   `json:"revision"`
		UpdatedAt *string `json:"updated_at"`
	} `json:"profile"`
}

func (h *Handler) overview(ctx context.Context, persist bool) (*httpx.Response, error) {
	if persist {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := h.resolveProfileID(ctx)
	if err != nil {
		return nil, err
	}
	var counts Counts
	var revision int64
	var updated *string
	err = h.Repo.ViewProfile(ctx, id, func(profile *model.Profile, shared *model.Shared) error {
		counts = Counts{len(shared.Subscriptions), len(shared.Nodes), len(profile.ProxyGroups), len(profile.RuleConfigs)}
		revision = int64(profile.Revision) + int64(shared.Revision)
		if profile.UpdatedAt != "" {
			updated = new(profile.UpdatedAt)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	history, err := h.recordHistory(ctx, id, counts, persist)
	if err != nil {
		return nil, err
	}
	data := overviewData{
		Subscriptions: overviewSeries{counts.Subscriptions, history["subscriptions"]},
		Nodes:         overviewSeries{counts.Nodes, history["nodes"]},
		ProxyGroups:   overviewSeries{counts.ProxyGroups, history["proxyGroups"]},
		Rules:         overviewSeries{counts.Rules, history["rules"]},
	}
	data.Profile.Revision, data.Profile.UpdatedAt = revision, updated
	return httpx.JSON(struct {
		Success bool         `json:"success"`
		Data    overviewData `json:"data"`
	}{true, data}), nil
}

// historyNumber keeps display arithmetic inside the finite range. The original
// sample literal remains untouched in the file; null marks an unplottable point.
func historyNumber(raw json.RawMessage) (*json.Number, error) {
	count, err := taskstatus.Count(raw)
	if err != nil {
		text := bytes.TrimSpace(raw)
		if len(text) > 0 && (text[0] == '-' || (text[0] >= '0' && text[0] <= '9')) {
			var number json.Number
			if json.Unmarshal(text, &number) == nil {
				value, _ := number.Float64()
				if math.IsInf(value, 0) {
					return nil, nil
				}
			}
		}
		return nil, err
	}
	value, _ := new(big.Float).SetInt(count).Float64()
	if math.IsInf(value, 0) {
		return nil, nil
	}
	return new(json.Number(count.String())), nil
}
