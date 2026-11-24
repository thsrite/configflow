// Package stats exposes overview counts and optional per-profile history.
package stats

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/timefmt"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"path/filepath"
	"strings"
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
	Repo *repo.Repository
	Now  func() time.Time
}

// NewHandler constructs a Handler for r (nil falls back to the context/default
// repository until W11 removes the locators).
func NewHandler(r *repo.Repository) *Handler { return &Handler{Repo: r} }

func (h *Handler) repo(ctx context.Context) (*repo.Repository, error) {
	if h != nil && h.Repo != nil {
		return h.Repo, nil
	}
	return repo.GetRepository(ctx)
}

func (h *Handler) resolveProfileID(ctx context.Context) (string, error) {
	if h != nil && h.Repo != nil {
		ctx = repo.WithRepository(ctx, h.Repo)
	}
	return repo.ResolveProfileID(ctx, "")
}

func (h *Handler) now() time.Time {
	if h != nil && h.Now != nil {
		return h.Now()
	}
	return Now()
}

// Register adds the overview route to r.
func (h *Handler) Register(r *httpx.Router) {
	r.GET("/api/stats/overview", "stats.get_overview", h.getOverview)
}

// Register adds the overview route.
func Register(r *httpx.Router) { NewHandler(nil).Register(r) }

// Sampling and retention constants.
const (
	HistoryFile    = "stats_history.json"
	HistorySize    = 48
	SampleInterval = 30 * time.Minute
)

// Keys is the order of overview series.
var Keys = []string{"subscriptions", "nodes", "proxyGroups", "rules"}

// Now is the sampling clock.
var Now = time.Now

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

func (h *Handler) currentCounts(ctx context.Context) (Counts, error) {
	repository, err := h.repo(ctx)
	if err != nil {
		return Counts{}, err
	}
	shared, err := repository.Shared(ctx)
	if err != nil {
		return Counts{}, err
	}
	profileID, err := h.resolveProfileID(ctx)
	if err != nil {
		return Counts{}, err
	}
	profile, err := repository.Profile(ctx, profileID)
	if err != nil {
		return Counts{}, err
	}
	return Counts{len(shared.Subscriptions), len(shared.Nodes), len(profile.ProxyGroups), len(profile.RuleConfigs)}, nil
}

func (h *Handler) recordHistory(ctx context.Context, counts Counts, persist bool) (map[string][]*json.Number, error) {
	if persist {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profileID, err := h.resolveProfileID(ctx)
	if err != nil {
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
	var document historyDocument
	_ = jsonstore.New(filepath.Dir(path)).Read(strings.TrimSuffix(HistoryFile, ".json"), &document)
	samples := document.Samples
	now := h.now().UTC()
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
	return result, nil
}
func (h *Handler) profileRevision(ctx context.Context) (int64, *string) {
	profileID, err := h.resolveProfileID(ctx)
	if err != nil {
		return 0, nil
	}
	repository, err := h.repo(ctx)
	if err != nil {
		return 0, nil
	}
	profile, err := repository.Profile(ctx, profileID)
	if err != nil {
		return 0, nil
	}
	revision, err := repository.ConfigRevision(ctx, profileID)
	if err != nil {
		return 0, nil
	}
	var updated *string
	if profile.UpdatedAt != "" {
		updated = new(profile.UpdatedAt)
	}
	return revision, updated
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
	counts, err := h.currentCounts(ctx)
	if err != nil {
		return nil, err
	}
	history, err := h.recordHistory(ctx, counts, persist)
	if err != nil {
		return nil, err
	}
	data := overviewData{
		Subscriptions: overviewSeries{counts.Subscriptions, history["subscriptions"]},
		Nodes:         overviewSeries{counts.Nodes, history["nodes"]},
		ProxyGroups:   overviewSeries{counts.ProxyGroups, history["proxyGroups"]},
		Rules:         overviewSeries{counts.Rules, history["rules"]},
	}
	data.Profile.Revision, data.Profile.UpdatedAt = h.profileRevision(ctx)
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
