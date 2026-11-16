package refresh

// Port of backend/utils/subscription_health.py: one record per subscription
// fetch (success / failure / fallback to the cache), the last HistorySize
// kept for the health bar, and the traffic of the provider's
// `subscription-userinfo` header.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/substore"
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
func healthNow() string {
	return strings.Replace(isoMicro(currentEnv().Clock.Now()), "+00:00", "Z", 1)
}

// ParseUserinfo is parse_userinfo (ported with the Sub-Store client, which
// needs it).
func ParseUserinfo(header string) (*jsonx.Object, error) { return substore.ParseUserinfo(header) }

// FetchUserinfo is fetch_userinfo(url): only the response headers matter;
// any request failure is nil (None). A value that is not a string or not an
// http(s) address is nil without a request. The Go client reads the body
// too (requests streamed it and closed without reading).
func FetchUserinfo(ctx context.Context, url any) (*jsonx.Object, error) {
	text, ok := url.(string)
	lower := pystr.Lower(text)
	if !ok || !(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
		return nil, nil
	}
	resp, err := currentEnv().HTTP.Do(ctx, &substore.Request{Method: "GET", URL: text,
		Header: http.Header{"User-Agent": {UserinfoUserAgent}}, Timeout: substore.Seconds(UserinfoTimeout)})
	if err != nil {
		var request *substore.RequestError
		if errors.As(err, &request) {
			healthLog.Infof("读取订阅流量信息失败: %s", pythonClass(request))
			return nil, nil
		}
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, nil
	}
	return substore.ParseUserinfo(resp.HeaderValue("subscription-userinfo"))
}

// LoadHealth is load_health(): the stored records, {} when the file is
// missing, unreadable or not an object.
func LoadHealth(ctx context.Context) (*jsonx.Object, error) {
	ctx = detached(ctx)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	data, err := r.ReadSharedJSON(ctx, HealthFile)
	if err != nil {
		if isRepositoryOrOSError(err) {
			return jsonx.NewObject(), nil
		}
		return nil, err
	}
	return data, nil
}

// saveHealth is _save_health: a write failure is only logged.
func saveHealth(ctx context.Context, data *jsonx.Object) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if _, err := r.WriteSharedJSON(ctx, HealthFile, data); err != nil {
		if isRepositoryOrOSError(err) {
			healthLog.Errorf("写入订阅健康记录失败: %s", errorText(err))
			return nil
		}
		return err
	}
	return nil
}

// FetchRecord is the keyword arguments of record_fetch.
type FetchRecord struct {
	OK        bool
	Count     any // int(count or 0); nil is 0
	FromCache bool
	Traffic   *jsonx.Object
}

// pyList is list(value) for a value read from the file (`… or []` already
// applied): a list, the characters of a string, the keys of an object.
func pyList(value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return append([]any(nil), v...), nil
	case string:
		out := make([]any, 0, utf8.RuneCountInString(v))
		for _, r := range v {
			out = append(out, string(r))
		}
		return out, nil
	case *jsonx.Object:
		out := make([]any, 0, v.Len())
		for _, key := range v.Keys() {
			out = append(out, key)
		}
		return out, nil
	}
	return nil, pyErr("TypeError", "'%s' object is not iterable", pyTypeName(value))
}

// RecordFetch is record_fetch(sub_id, ok=, count=, from_cache=, traffic=):
// append one fetch record (status ok / cache / fail) and, with traffic,
// update the traffic. It returns the subscription's latest health entry
// ({} without an id).
func RecordFetch(ctx context.Context, subID string, rec FetchRecord) (*jsonx.Object, error) {
	ctx = detached(ctx)
	if subID == "" {
		return jsonx.NewObject(), nil
	}
	data, err := LoadHealth(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := data.Value(subID).(*jsonx.Object)
	if !ok {
		entry = jsonx.NewObject()
	}
	history, err := pyList(orDefault(entry.Value("history"), []any{}))
	if err != nil {
		return nil, err
	}
	if keep := HistorySize - 1; len(history) > keep {
		history = history[len(history)-keep:]
	}
	status := "fail"
	switch {
	case rec.OK && !rec.FromCache:
		status = "ok"
	case rec.FromCache:
		status = "cache"
	}
	count, err := pyIntValue(orDefault(rec.Count, jsonx.Int(0)))
	if err != nil {
		return nil, err
	}
	history = append(history, jsonx.Obj("at", healthNow(), "status", status, "count", jsonx.BigInt(count)))
	entry.Set("history", history)
	if rec.Traffic != nil && rec.Traffic.Len() > 0 {
		entry.Set("traffic", rec.Traffic)
		entry.Set("traffic_at", healthNow())
	}
	data.Set(subID, entry)
	if err := saveHealth(ctx, data); err != nil {
		return nil, err
	}
	return entry, nil
}

// Forget is forget(sub_id): drop the subscription's records (saved only
// when there was a non-null entry).
func Forget(ctx context.Context, subID string) error {
	ctx = detached(ctx)
	data, err := LoadHealth(ctx)
	if err != nil {
		return err
	}
	value, present := data.Get(subID)
	data.Delete(subID)
	if present && value != nil {
		return saveHealth(ctx, data)
	}
	return nil
}
