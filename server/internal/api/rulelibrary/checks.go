package rulelibrary

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/weburl"
)

type checkResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
	Status    *int   `json:"status_code,omitempty"`
	Error     string `json:"error,omitempty"`
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

// Single checks the configured forwarding path with Python's five-second timeout.
func (s *Service) Single(ctx context.Context, body *singleRequest) (singleResult, error) {
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
	library := catalog.RuleLibrary
	system, err := systemConfig(ctx)
	if err != nil {
		return out, err
	}
	selected := []model.RuleLibraryItem{}
	for _, item := range library {
		if sourceTypeOf(item) == "url" && model.TextValue(item.URL) != "" {
			selected = append(selected, item)
		}
	}
	out.Total = len(selected)
	out.Skipped = len(library) - len(selected)
	out.Results, err = parallel(ctx, selected, func(ctx context.Context, item model.RuleLibraryItem) (checkResult, error) {
		url := model.TextValue(item.URL)
		result := checkResult{ID: item.ID, Name: item.Name, URL: url}
		status, err := s.requestStatus(ctx, url, system, true)
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
			next.Enabled = new(false)
			changes = append(changes, sourceChange{selected[i], next})
		}
	}
	_, err = editWithProfiles(ctx, changes, func(_ context.Context, current *model.Shared) error {
		items := slices.Clone(current.RuleLibrary)
		for _, change := range changes {
			i := find(items, change.Old.ID)
			if i >= 0 && same(items[i], change.Old) {
				items[i].Enabled = new(false)
			}
		}
		current.RuleLibrary = items
		return nil
	}, base)
	return out, err
}

// parallel uses a fixed worker count and joins all workers before returning.
// A child panic is contained, logged without source material, and returned.
func parallel[T any](ctx context.Context, items []model.RuleLibraryItem, run func(context.Context, model.RuleLibraryItem) (T, error)) ([]T, error) {
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
func runItem[T any](ctx context.Context, item model.RuleLibraryItem, run func(context.Context, model.RuleLibraryItem) (T, error)) (out T, err error) {
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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Success  bool   `json:"success"`
	Path     string `json:"local_path,omitempty"`
	Error    string `json:"error,omitempty"`
	Retained *bool  `json:"cache_retained,omitempty"`
}
type cacheResults struct {
	Success   bool          `json:"success"`
	Results   []cacheResult `json:"results"`
	Succeeded int           `json:"success_count"`
	Failed    int           `json:"failed_count"`
	Total     int           `json:"total_count"`
}

// Cache refreshes selected sources without changing preferences on failure.
func (s *Service) Cache(ctx context.Context, body *cacheRequest) (cacheResults, error) {
	ctx = lifecycle.Detach(ctx)
	out := cacheResults{Success: true, Results: []cacheResult{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !body.RuleIDs.OK || len(body.RuleIDs.Value) == 0 {
		return out, fail(400, msgCacheSelect)
	}
	chosen := map[string]bool{}
	for _, entry := range body.RuleIDs.Value {
		if entry.OK {
			chosen[entry.Value] = true
		}
	}
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return out, err
	}
	selected := []model.RuleLibraryItem{}
	for _, item := range catalog.RuleLibrary {
		if chosen[item.ID] {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return out, fail(404, msgCacheMissing)
	}
	out.Total = len(selected)
	out.Results, err = parallel(ctx, selected, func(ctx context.Context, item model.RuleLibraryItem) (cacheResult, error) {
		result := cacheResult{ID: item.ID, Name: item.Name}
		ctx, release, err := editLease(ctx, item.ID)
		if err != nil {
			return result, err
		}
		defer release()
		// A waiting manual refresh resolves the latest declaration after acquiring
		// the edit lease, so a slow previous request cannot republish old inline text.
		current, err := source(ctx, item.ID)
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
