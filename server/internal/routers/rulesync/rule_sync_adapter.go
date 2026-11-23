package rulesync

// Rule sync downloads and snapshot orchestration use the typed rule owner.
// Device writes remain in the sync service and guarded stores.
import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/weburl"
)

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
	var envErr error
	ctx, envErr = withSyncEnvironment(ctx, config)
	if envErr != nil {
		return "", envErr
	}
	var source struct {
		URL string `json:"url"`
	}
	if err := jsondecode.Unmarshal(raw, &source); err != nil {
		return "", err
	}
	environment := ctx.Value(syncEnvironmentKey{}).(*syncEnvironment)
	key := environment.transport + "\x00" + source.URL
	if value, ok := d.items[key]; ok {
		var src syncSource
		src.Library.Raw = raw
		if err := syncValidate(ctx, value, src, config); err != nil {
			return "", &syncDownloadError{device.Error(err.Error())}
		}
		return value, nil
	}
	cfg, err := syncEnvironmentConfig(ctx, config)
	if err != nil {
		return "", err
	}
	client := d.Client
	if client == nil {
		client = rulefetch.DefaultClient
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := client.Download(work, source.URL, rulefetch.DownloadRequest{Config: cfg, Stream: true, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second})
	// Only a body that outgrows the size or total time is the permanent limit
	// error (paused). The budget running out while connecting or waiting for
	// the response, including the client's retries, is a transient failure
	// retried at the next interval, as Python's connect/read timeouts are.
	responded := err == nil
	if err == nil {
		defer response.Close()
		err = response.RaiseForStatus()
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(response, syncMaxBytes+1))
	}
	if len(data) > syncMaxBytes || responded && errors.Is(work.Err(), context.DeadlineExceeded) {
		return "", device.Error(messageSyncDownloadLimit)
	}
	if err != nil {
		return "", &syncDownloadError{device.Error(messageSyncDownloadError + weburl.SafeErrorDetails(err))}
	}
	if !utf8.Valid(data) {
		return "", &syncDownloadError{device.Error(messageSyncDownloadUTF8)}
	}
	content := strings.TrimPrefix(string(data), "\ufeff")
	// Validate before caching, and the engine validates again on every declaration.
	var src syncSource
	src.Library.Raw = raw
	if err = syncValidate(ctx, content, src, config); err != nil {
		return "", &syncDownloadError{device.Error(err.Error())}
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
func loadSyncSources(ctx context.Context, config json.RawMessage, r *spec.Router, download RuleSyncDownload) (map[string]string, map[string]*RuleSyncRecord, error) {
	var envErr error
	ctx, envErr = withSyncEnvironment(ctx, config)
	if envErr != nil {
		return nil, nil, envErr
	}
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
				return nil, nil, device.Error(messageSyncTotalLarge)
			}
		}
		if source.Source.SourceType == "url" {
			if err = syncValidate(ctx, content, source, config); err != nil {

				return nil, nil, device.Error(fmt.Sprintf(messageSyncSourceError, device.OrText(source.Source.Name, source.Item.ID), err.Error()))
			}
		}
		record, err := ClassifySyncRules(content, source.Behavior)
		if err != nil {
			return nil, nil, device.Error(fmt.Sprintf(messageSyncSourceError, device.OrText(source.Source.Name, source.Item.ID), err.Error()))
		}
		record.Name = device.OrText(source.Source.Name, source.Item.ID)
		record.SourceType = source.Source.SourceType
		record.LibraryID = key
		contents[source.Item.ID] = content
		records[source.Item.ID] = record
		count += record.IPCount
		count6 += record.IP6Count
		if count > syncMaxIPs {
			return nil, nil, device.Error(messageSyncTotalIPs)
		}
		if count6 > syncMaxIPs {
			return nil, nil, device.Error(messageSyncTotalIPs6)
		}
	}
	return contents, records, nil
}
func syncConfig(ctx context.Context, db *repo.Repository, id string) (json.RawMessage, error) {
	config, err := db.ConfigWithOxiDNSAgents(ctx, id, repo.ResourceRoots{})
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}
