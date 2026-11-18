package routers

// Legacy repository/rulefetch/task boundaries are isolated here; the sync
// engine holds native typed records and JSON, not dynamic document values.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/weburl"
)

func syncObject(raw json.RawMessage) (*jsonx.Object, error) {
	var out jsonx.Object
	err := json.Unmarshal(raw, &out)
	return &out, err
}
func syncValidate(ctx context.Context, content string, source syncSource, config json.RawMessage) error {
	rule, err := syncObject(source.Library.Raw)
	if err != nil {
		return err
	}
	cfg, err := syncObject(config)
	if err != nil {
		return err
	}
	return rulefetch.ValidateRemoteRuleContent(content, rule, cfg, "")
}
func syncCacheRead(ctx context.Context, source syncSource, config json.RawMessage) (string, error) {
	rule, err := syncObject(source.Library.Raw)
	if err != nil {
		return "", err
	}
	cfg, err := syncObject(config)
	if err != nil {
		return "", err
	}
	path, err := rulefetch.RuleCacheReadPath(ctx, rule, cfg.GetList("rule_library"))
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if source.Source.SourceType == "content" {
			return source.Source.Content, nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, syncMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > syncMaxBytes {
		return "", routerError(fmt.Sprintf(messageSyncSourceLarge, orText(source.Source.Name, source.Item.ID)))
	}
	if !utf8.Valid(raw) {
		return "", routerError(fmt.Sprintf(messageSyncUTF8, orText(source.Source.Name, source.Item.ID)))
	}
	return strings.TrimPrefix(string(raw), "\ufeff"), nil
}
func syncCacheWrite(ctx context.Context, source syncSource, content string, config json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rule, err := syncObject(source.Library.Raw)
	if err != nil {
		return err
	}
	cfg, err := syncObject(config)
	if err != nil {
		return err
	}
	_, err = rulefetch.WriteValidatedRuleCache(ctx, rule, content, cfg)
	return err
}

// RuleSyncDownload fetches one declaration; each consumer independently validates its content.
type RuleSyncDownload func(context.Context, json.RawMessage, json.RawMessage) (string, error)

type syncDownloadError struct{ cause error }

func (e *syncDownloadError) Error() string { return e.cause.Error() }
func (e *syncDownloadError) Unwrap() error { return e.cause }

// RuleSyncDownloads shares a bounded FIFO transport cache within a single round.
// It is used sequentially by the scheduler, never across rounds or goroutines.
type RuleSyncDownloads struct {
	Client *rulefetch.Client
	items  map[string]string
	order  []string
	size   int
}

// Download applies the production 5/10 second I/O and 30 second total limits.
func (d *RuleSyncDownloads) Download(ctx context.Context, raw, config json.RawMessage) (string, error) {
	var source struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", err
	}
	var cfgFields map[string]json.RawMessage
	_ = json.Unmarshal(config, &cfgFields)
	key := routeDigest([]json.RawMessage{mustPolicyJSON(source.URL), cfgFields["system_config"]})
	if value, ok := d.items[key]; ok {
		return value, nil
	}
	cfg, err := syncObject(config)
	if err != nil {
		return "", err
	}
	client := d.Client
	if client == nil {
		client = rulefetch.DefaultClient
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := client.RequestRule(work, source.URL, rulefetch.Request{Config: cfg, Stream: true, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second})
	if err == nil {
		defer response.Close()
		err = response.RaiseForStatus()
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(response, syncMaxBytes+1))
	}
	if len(data) > syncMaxBytes || errors.Is(work.Err(), context.DeadlineExceeded) {
		return "", routerError(messageSyncDownloadLimit)
	}
	if err != nil {
		return "", &syncDownloadError{routerError(messageSyncDownloadError + weburl.SafeErrorDetails(err))}
	}
	if !utf8.Valid(data) {
		return "", &syncDownloadError{routerError(messageSyncDownloadError + "UnicodeDecodeError")}
	}
	content := strings.TrimPrefix(string(data), "\ufeff")
	// Validate before caching, and the engine validates again on every declaration.
	var src syncSource
	src.Library.Raw = raw
	if err = syncValidate(ctx, content, src, config); err != nil {
		return "", &syncDownloadError{routerError(err.Error())}
	}
	if d.items == nil {
		d.items = map[string]string{}
	}
	for len(d.order) > 0 && d.size+len(content) > 16*1024*1024 {
		old := d.order[0]
		d.order = d.order[1:]
		d.size -= len(d.items[old])
		delete(d.items, old)
	}
	d.items[key] = content
	d.order = append(d.order, key)
	d.size += len(content)
	return content, nil
}
func loadSyncSources(ctx context.Context, config json.RawMessage, r *Router, download RuleSyncDownload) (map[string]string, map[string]*RuleSyncRecord, error) {
	sources, err := selectedSyncSources(config, r)
	if err != nil {
		return nil, nil, err
	}
	contents, records, fetched := map[string]string{}, map[string]*RuleSyncRecord{}, map[string]string{}
	size, count, count6 := 0, 0, 0
	for _, source := range sources {
		if err = ctx.Err(); err != nil {
			return nil, nil, err
		}
		key := source.Source.ID
		content, ok := fetched[key]
		if !ok {
			if download != nil && source.Source.SourceType == "url" {
				content, err = download(ctx, source.Library.Raw, config)
			} else {
				content, err = syncCacheRead(ctx, source, config)
			}
			if err != nil {
				return nil, nil, err
			}
			fetched[key] = content
			size += len(content)
			if size > 16*1024*1024 {
				return nil, nil, routerError(messageSyncTotalLarge)
			}
		}
		if source.Source.SourceType == "url" {
			if err = syncValidate(ctx, content, source, config); err != nil {
				var transient *syncDownloadError
				if download != nil {
					transient = &syncDownloadError{routerError(err.Error())}
					return nil, nil, transient
				}
				return nil, nil, routerError(fmt.Sprintf(messageSyncSourceError, orText(source.Source.Name, source.Item.ID), err.Error()))
			}
		}
		record, err := ClassifySyncRules(content, source.Behavior)
		if err != nil {
			return nil, nil, routerError(fmt.Sprintf(messageSyncSourceError, orText(source.Source.Name, source.Item.ID), err.Error()))
		}
		record.Name = orText(source.Source.Name, source.Item.ID)
		record.SourceType = source.Source.SourceType
		record.LibraryID = key
		contents[source.Item.ID] = content
		records[source.Item.ID] = record
		count += record.IPCount
		count6 += record.IP6Count
		if count > syncMaxIPs {
			return nil, nil, routerError(messageSyncTotalIPs)
		}
		if count6 > syncMaxIPs {
			return nil, nil, routerError(messageSyncTotalIPs6)
		}
	}
	return contents, records, nil
}
func syncConfig(ctx context.Context, db *repo.Repository, id string) (json.RawMessage, error) {
	config, err := db.GetCompatConfig(ctx, id, repo.ResourceRoots{})
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}
func findSyncRouter(config json.RawMessage, id string) *Router {
	for _, r := range routersOf(config) {
		if r.ID == id {
			return r
		}
	}
	return nil
}
