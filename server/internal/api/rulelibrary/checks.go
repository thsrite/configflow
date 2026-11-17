package rulelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/weburl"
)

type checkResult struct {
	ID        json.RawMessage `json:"id"`
	Name      json.RawMessage `json:"name"`
	URL       json.RawMessage `json:"url"`
	Available bool            `json:"available"`
	Status    *int            `json:"status_code,omitempty"`
	Error     string          `json:"error,omitempty"`
}
type singleResult struct {
	Success   bool `json:"success"`
	Status    int  `json:"status_code"`
	Available bool `json:"available"`
}
type checksResult struct {
	Success bool          `json:"success"`
	Results []checkResult `json:"results"`
	Failed  int           `json:"failed_count"`
	Total   int           `json:"total_count"`
	Skipped int           `json:"skipped_count"`
}

func defaultField(item record, key string) json.RawMessage {
	if item.Has(key) {
		return field(item, key)
	}
	return json.RawMessage(`""`)
}

// Single checks the configured forwarding path with Python's five-second timeout.
func (s *Service) Single(ctx context.Context, body record) (singleResult, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return singleResult{}, err
	}
	address, err := requestURL(body)
	if err != nil {
		return singleResult{}, err
	}
	system, err := systemConfig(ctx)
	if err != nil {
		return singleResult{}, err
	}
	status, err := s.requestStatus(ctx, address, system, false)
	if err != nil {
		return singleResult{}, fail(500, weburl.SafeErrorDetails(err))
	}
	return singleResult{true, status, status == 200}, nil
}

// Test probes at most ten sources concurrently, then disables only declarations
// that still match the tested snapshot. It never saves that old shared snapshot.
func (s *Service) Test(ctx context.Context, base string) (checksResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := checksResult{Success: true, Results: []checkResult{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return out, err
	}
	library, err := rows(catalog, "rule_library")
	if err != nil {
		return out, err
	}
	system, err := systemConfig(ctx)
	if err != nil {
		return out, err
	}
	selected := []record{}
	for _, item := range library {
		if sourceType(item) == "url" && text(item, "url") != "" {
			selected = append(selected, item)
		}
	}
	out.Total = len(selected)
	out.Skipped = len(library) - len(selected)
	out.Results, err = parallel(ctx, selected, func(ctx context.Context, item record) (checkResult, error) {
		result := checkResult{ID: defaultField(item, "id"), Name: defaultField(item, "name"), URL: defaultField(item, "url")}
		status, err := s.requestStatus(ctx, text(item, "url"), system, true)
		if err != nil {
			result.Error = weburl.SafeErrorDetails(err)
		} else {
			result.Status = &status
			result.Available = status < 400
		}
		return result, nil
	})
	if err != nil {
		return out, err
	}
	changes := []sourceChange{}
	for i, result := range out.Results {
		if !result.Available {
			out.Failed++
			next := selected[i]
			if err = put(&next, "enabled", false); err != nil {
				return out, err
			}
			changes = append(changes, sourceChange{selected[i], next})
		}
	}
	_, err = editWithProfiles(ctx, changes, func(_ context.Context, current *record) error {
		items, err := rows(*current, "rule_library")
		if err != nil {
			return err
		}
		for _, change := range changes {
			i := find(items, text(change.Old, "id"))
			if i >= 0 && same(items[i], change.Old) {
				if err = put(&items[i], "enabled", false); err != nil {
					return err
				}
			}
		}
		return put(current, "rule_library", items)
	}, base)
	return out, err
}

// parallel uses a fixed worker count and joins all workers before returning.
// A child panic is contained, logged without source material, and returned.
func parallel[T any](ctx context.Context, items []record, run func(context.Context, record) (T, error)) ([]T, error) {
	out := make([]T, len(items))
	failures := make([]error, len(items))
	var next atomic.Int64
	var workers sync.WaitGroup
	for range min(10, len(items)) {
		workers.Go(func() {
			for {
				index := int(next.Add(1) - 1)
				if index >= len(items) {
					return
				}
				out[index], failures[index] = runItem(ctx, items[index], run)
			}
		})
	}
	workers.Wait()
	for _, err := range failures {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func runItem[T any](ctx context.Context, item record, run func(context.Context, record) (T, error)) (out T, err error) {
	defer func() {
		if recover() != nil {
			logx.Get("api.rulelibrary").Error("rule library worker panicked")
			err = errors.New("rule library worker failed")
		}
	}()
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return run(ctx, item)
}

type cacheResult struct {
	ID       json.RawMessage `json:"id"`
	Name     json.RawMessage `json:"name"`
	Success  bool            `json:"success"`
	Path     string          `json:"local_path,omitempty"`
	Error    string          `json:"error,omitempty"`
	Retained *bool           `json:"cache_retained,omitempty"`
}
type cacheResults struct {
	Success   bool          `json:"success"`
	Results   []cacheResult `json:"results"`
	Succeeded int           `json:"success_count"`
	Failed    int           `json:"failed_count"`
	Total     int           `json:"total_count"`
}

// Cache refreshes selected sources without changing preferences on failure.
func (s *Service) Cache(ctx context.Context, body record) (cacheResults, error) {
	ctx = lifecycle.Detach(ctx)
	out := cacheResults{Success: true, Results: []cacheResult{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var selectedIDs []json.RawMessage
	input, present := body.Get("rule_ids")
	if !present || json.Unmarshal(input, &selectedIDs) != nil || len(selectedIDs) == 0 {
		return out, fail(400, msgCacheSelect)
	}
	chosen := map[string]bool{}
	for _, raw := range selectedIDs {
		var id string
		if string(raw) != "null" && json.Unmarshal(raw, &id) == nil {
			chosen[id] = true
		}
	}
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return out, err
	}
	library, err := rows(catalog, "rule_library")
	if err != nil {
		return out, err
	}
	selected := []record{}
	for _, item := range library {
		if chosen[text(item, "id")] {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return out, fail(404, msgCacheMissing)
	}
	out.Total = len(selected)
	out.Results, err = parallel(ctx, selected, func(ctx context.Context, item record) (cacheResult, error) {
		result := cacheResult{ID: defaultField(item, "id"), Name: defaultField(item, "name")}
		ctx, release, err := editLease(ctx, item)
		if err != nil {
			return result, err
		}
		defer release()
		// A waiting manual refresh resolves the latest declaration after acquiring
		// the edit lease, so a slow previous request cannot republish old inline text.
		current, err := source(ctx, text(item, "id"))
		if err != nil {
			result.Error = weburl.SafeErrorDetails(err)
			retained := false
			result.Retained = &retained
			return result, nil
		}
		result.Path, err = s.cacheOne(ctx, current)
		if err == nil {
			result.Success = true
			return result, nil
		}
		result.Path = ""
		result.Error = describeFailure(err)
		cached, readErr := readCache(ctx, current)
		if readErr != nil {
			return result, readErr
		}
		retained := cached != nil
		result.Retained = &retained
		return result, nil
	})
	if err != nil {
		return out, err
	}
	for _, item := range out.Results {
		if item.Success {
			out.Succeeded++
		} else {
			out.Failed++
		}
	}
	return out, nil
}
