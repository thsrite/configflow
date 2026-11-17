// Package stats holds the route of backend/routes/stats.py (blueprint
// "stats", prefix /api/stats): the overview counts with their per-profile
// history.
package stats

import (
	"context"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

// Register adds the stats blueprint.
func Register(r *httpx.Router) {
	r.GET("/api/stats/overview", "stats.get_overview", getOverview)
}

// Constants of stats.py.
const (
	HistoryFile    = "stats_history.json"
	HistorySize    = 48
	SampleInterval = 30 * time.Minute
)

// Keys are STATS_KEYS.
var Keys = []string{"subscriptions", "nodes", "proxyGroups", "rules"}

// Now is the clock; tests replace it.
var Now = time.Now

// errPython marks a failure where Python raised an unexpected exception
// (TypeError, AttributeError) inside the handler's try block.
type errPython struct{ message string }

func (e *errPython) Error() string { return e.message }

func pyLen(v any) (int, error) {
	switch value := v.(type) {
	case []any:
		return len(value), nil
	case *jsonx.Object:
		return value.Len(), nil
	case string:
		return utf8.RuneCountInString(value), nil
	}
	return 0, &errPython{"object of type '" + httpx.PyTypeName(v) + "' has no len()"}
}

func getDataCount(ctx context.Context, dataType string) (int, error) {
	var config *jsonx.Object
	var err error
	if dataType == "subscriptions" || dataType == "nodes" {
		config, err = repo.GetSharedConfig(ctx)
	} else {
		config, err = repo.GetConfig(ctx, "")
	}
	if err != nil {
		return 0, err
	}
	field := map[string]string{"subscriptions": "subscriptions", "nodes": "nodes", "rules": "rule_configs",
		"proxy_groups": "proxy_groups"}[dataType]
	if field == "" {
		return 0, nil
	}
	return pyLen(config.GetOr(field, []any{}))
}

// isoformatZ is datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z').
func isoformatZ(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond()/1000 == 0 {
		return t.Format("2006-01-02T15:04:05") + "Z"
	}
	return t.Format("2006-01-02T15:04:05.000000") + "Z"
}

func pyInt(v any) (int64, error) {
	if !jsonx.Truthy(v) {
		return 0, nil
	}
	switch value := v.(type) {
	case bool:
		return 1, nil
	case jsonx.Number:
		if v, fits := value.Int64(); fits {
			return v, nil
		}
		f := value.Float64()
		n, _ := big.NewFloat(f).Int(nil)
		if n == nil {
			return 0, &errPython{"cannot convert float to integer"}
		}
		return n.Int64(), nil
	case string:
		n, ok := httpx.PyParseInt(value)
		if !ok {
			return 0, &errPython{"invalid literal for int() with base 10: " + jsonx.Repr(value)}
		}
		return n.Int64(), nil
	}
	return 0, &errPython{"int() argument must be a string, a bytes-like object or a real number, not '" + httpx.PyTypeName(v) + "'"}
}

var sampleLocks = lockx.NewLeafKeyed[string]("stats-history")

func recordHistory(ctx context.Context, counts *jsonx.Object, persist bool) (map[string][]any, error) {
	if persist {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	dir, err := repo.ResolveDataDir(repository.DataDir())
	if err != nil {
		return nil, err
	}
	_, release, err := sampleLocks.Lock(ctx, dir+"/"+profileID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var samples []any
	if data, err := repository.ReadProfileJSON(profileID, HistoryFile); err == nil {
		samples, _ = data.Value("samples").([]any)
	}
	now := Now().UTC()
	var lastAt *time.Time
	if len(samples) > 0 {
		last, ok := samples[len(samples)-1].(*jsonx.Object)
		if !ok {
			return nil, &errPython{httpx.NoAttributeGet(samples[len(samples)-1])}
		}
		if t, ok := taskstatus.FromISOFormat(replaceZ(jsonx.Str(last.GetOr("at", "")))); ok {
			lastAt = &t
		}
	}
	current := jsonx.Obj("at", isoformatZ(now))
	current.Update(counts)
	var series []any
	if lastAt == nil || now.Sub(*lastAt) >= SampleInterval {
		samples = append(append([]any{}, samples...), current)
		if len(samples) > HistorySize {
			samples = samples[len(samples)-HistorySize:]
		}
		// OSError and repository errors are ignored: the history is optional.
		if persist {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			_, _ = repository.WriteProfileJSON(ctx, profileID, HistoryFile, jsonx.Obj("samples", samples))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		series = samples
	} else {
		series = append(append([]any{}, samples[:len(samples)-1]...), current)
	}
	result := map[string][]any{}
	for _, key := range Keys {
		values := []any{}
		for _, sample := range series {
			object, ok := sample.(*jsonx.Object)
			if !ok {
				return nil, &errPython{httpx.NoAttributeGet(sample)}
			}
			n, err := pyInt(object.Value(key))
			if err != nil {
				return nil, err
			}
			values = append(values, n)
		}
		result[key] = values
	}
	return result, nil
}

func replaceZ(s string) string { return strings.ReplaceAll(s, "Z", "+00:00") }

func profileRevision(ctx context.Context) (any, any) {
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return 0, nil
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, nil
	}
	profiles, err := repository.ListProfiles(ctx)
	if err != nil {
		return 0, nil
	}
	for _, item := range profiles {
		metadata, _ := item.(*jsonx.Object)
		if metadata.Value("id") == profileID {
			if !jsonx.Truthy(metadata) {
				break
			}
			revision, err := repository.ConfigRevision(ctx, profileID)
			if err != nil {
				return 0, nil
			}
			return revision, metadata.Value("updated_at")
		}
	}
	return 0, nil
}

func getOverview(c *httpx.Ctx) (*httpx.Response, error) {
	// Python: except Exception as e → 500 {'success': False, 'error': str(e)}.
	response, err := overview(c.Context(), c.Request.Method != http.MethodHead)
	if err != nil {
		return httpx.JSONStatus(500, jsonx.Obj("success", false, "error", err.Error())), nil
	}
	return response, nil
}

func overview(ctx context.Context, persist bool) (*httpx.Response, error) {
	if persist {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	counts := jsonx.NewObject()
	for _, item := range [][2]string{{"subscriptions", "subscriptions"}, {"nodes", "nodes"},
		{"proxyGroups", "proxy_groups"}, {"rules", "rules"}} {
		n, err := getDataCount(ctx, item[1])
		if err != nil {
			return nil, err
		}
		counts.Set(item[0], n)
	}
	history, err := recordHistory(ctx, counts, persist)
	if err != nil {
		return nil, err
	}
	revision, updatedAt := profileRevision(ctx)
	data := jsonx.NewObject()
	for _, key := range Keys {
		data.Set(key, jsonx.Obj("total", counts.Value(key), "history", history[key]))
	}
	data.Set("profile", jsonx.Obj("revision", revision, "updated_at", updatedAt))
	return httpx.JSON(jsonx.Obj("success", true, "data", data)), nil
}
