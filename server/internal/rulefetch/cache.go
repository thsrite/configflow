package rulefetch

// The repository-backed functions of backend/utils/rule_utils.py
// (get_rules_dir, read/write_validated_rule_cache, refresh_remote_rule,
// read_rule_source_content, save_rule_to_local). The pure name/path helpers
// are in internal/rules (SanitizeRuleName, RuleCacheFilename,
// RuleCacheReadPath, TouchUnchangedCache).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"time"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

var utilsLogger = logx.Get("backend.utils.rule_utils")

// GetRulesDir is get_rules_dir: DATA_DIR/shared/rules.
func GetRulesDir(ctx context.Context) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	return r.SharedRulesDir()
}

// RuleCacheReadPath is rule_cache_read_path(rule, library) with the stored
// shared rule library; library nil is None.
func RuleCacheReadPath(ctx context.Context, rule *jsonx.Object, library []any) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		return "", err
	}
	// Python returns an existing current cache before it reads the shared
	// document (a repository failure then does not matter).
	current := filepath.Join(root, rules.RuleCacheFilename(rule))
	_, receiptErr := os.Stat(current + ".invalid")
	if fileExists(current) || !errors.Is(receiptErr, os.ErrNotExist) {
		return rules.RuleCacheReadPath(rule, library, nil, root), nil
	}
	shared, err := r.GetShared(ctx)
	if err != nil {
		return "", err
	}
	stored := shared.GetList("rule_library")
	if stored == nil {
		stored = []any{}
	}
	return rules.RuleCacheReadPath(rule, library, stored, root), nil
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func contentBytes(content any) []byte {
	switch v := content.(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	}
	return nil
}

// ReadValidatedRuleCache is read_validated_rule_cache: only cache bytes that
// satisfy the current remote declaration, else nil. config nil is None.
func ReadValidatedRuleCache(ctx context.Context, rule, config *jsonx.Object) ([]byte, error) {
	var library []any
	if config != nil {
		library = config.GetList("rule_library")
		if library == nil {
			library = []any{}
		}
	}
	cachePath, err := RuleCacheReadPath(ctx, rule, library)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil
	}
	if rule.GetOr("source_type", "url") == "url" {
		if err := ValidateRemoteRuleContent(content, rule, config, ""); err != nil {
			var remote *RemoteRuleValidationError
			if errors.As(err, &remote) {
				return nil, nil
			}
			return nil, err
		}
	}
	return content, nil
}

// WriteValidatedRuleCache is write_validated_rule_cache: validate the
// complete response, then atomically replace the raw bytes (identical
// bytes only get a new modification time). It returns the cache file name
// relative to the rules directory.
func WriteValidatedRuleCache(ctx context.Context, rule *jsonx.Object, content any, config *jsonx.Object) (string, error) {
	if rule.GetOr("source_type", "url") == "url" {
		if err := ValidateRemoteRuleContent(content, rule, config, ""); err != nil {
			return "", err
		}
	}
	filename := rules.RuleCacheFilename(rule)
	relative := path.Join("rules", filename)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	target, err := r.SharedPath(relative)
	if err != nil {
		return "", err
	}
	data := contentBytes(content)
	write := func(locked context.Context) error {
		if !rules.TouchUnchangedCache(target, data) {
			_, err := r.WriteSharedText(locked, relative, data)
			return err
		}
		return nil
	}
	if rule.GetOr("source_type", "url") == "content" {
		err = writeCurrentInline(ctx, r, rule, target, data, write)
	} else {
		err = write(ctx)
	}
	if err != nil {
		return "", err
	}
	return filename, nil
}

var errInlineCacheOnly = errors.New("inline cache write without document commit")

// Every inline writer (including discovery and Agent tools) must serialize its
// declaration check with DB edits. A stale caller may finish its request, but
// must never replace the cache of a newer declaration or an active editor.
func writeCurrentInline(ctx context.Context, r *repo.Repository, rule *jsonx.Object, target string, data []byte, write func(context.Context) error) error {
	// A C33 publisher may hold L3 while an independently admitted reader
	// finishes. Its receipt already forbids this write, so do not wait on L3.
	if _, err := os.Stat(target + ".invalid"); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	id := rule.GetString("library_rule_id")
	if id == "" {
		id = rule.GetString("id")
	}
	if id == "" {
		return write(ctx)
	}
	check := func(tx context.Context, catalog *jsonx.Object) error {
		if err := tx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(target + ".invalid"); !errors.Is(err, os.ErrNotExist) {
			return nil
		}
		for _, value := range catalog.GetList("rule_library") {
			current, ok := value.(*jsonx.Object)
			if !ok || current.GetString("id") != id {
				continue
			}
			if current.GetOr("source_type", "url") == "content" && bytes.Equal(data, contentBytes(current.GetOr("content", ""))) {
				return write(tx)
			}
			break
		}
		return nil
	}
	_, err := r.UpdateSharedTransaction(ctx, func(tx context.Context, catalog *jsonx.Object) (*jsonx.Object, error) {
		if err := check(tx, catalog); err != nil {
			return nil, err
		}
		// This existing transaction only serializes cache I/O. Abort its
		// unchanged candidate so no revision, SQLite write or hook is produced.
		return nil, errInlineCacheOnly
	})
	if errors.Is(err, errInlineCacheOnly) {
		return nil
	}
	return err
}

// RefreshOptions are refresh_remote_rule's keyword arguments.
type RefreshOptions struct {
	// Timeout is timeout= (10 s when 0).
	Timeout time.Duration
	// Fetch replaces the download (fetch=); it returns string or []byte.
	Fetch func(url string) (any, error)
	// AllowStale serves validated previous bytes when the refresh fails.
	AllowStale bool
	Client     *Client // nil: DefaultClient
}

// RefreshRemoteRule is refresh_remote_rule: the raw bytes, plus the refresh
// error when validated cache bytes are served instead (AllowStale).
func RefreshRemoteRule(ctx context.Context, rule, config *jsonx.Object, opts RefreshOptions) ([]byte, error, error) {
	content, err := func() (any, error) {
		url := jsonx.Str(rule.GetOr("url", ""))
		if opts.Fetch != nil {
			return opts.Fetch(url)
		}
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 10 * time.Second
		}
		client := opts.Client
		if client == nil {
			client = DefaultClient
		}
		response, err := client.RequestRule(ctx, url, Request{Config: config, ConnectTimeout: timeout, ReadTimeout: timeout})
		if err != nil {
			return nil, err
		}
		if err := response.RaiseForStatus(); err != nil {
			return nil, err
		}
		return response.Content()
	}()
	if err == nil {
		if _, err = WriteValidatedRuleCache(ctx, rule, content, config); err == nil {
			return contentBytes(content), nil, nil
		}
	}
	if opts.AllowStale {
		cached, readErr := ReadValidatedRuleCache(ctx, rule, config)
		if readErr != nil {
			return nil, nil, readErr
		}
		if cached != nil {
			detail := safeExceptionDetails(err)
			var remote *RemoteRuleValidationError
			if errors.As(err, &remote) {
				detail = err.Error()
			}
			utilsLogger.Warningf("规则 %s 更新失败，继续使用上一次有效缓存：%s", jsonx.Str(rule.GetOr("name", "")), detail)
			return cached, err, nil
		}
	}
	return nil, nil, err
}

// ReadRuleSourceContent is read_rule_source_content: validate a source
// without publishing its catalog entry or cache. The result is the
// handwritten content (string), the downloaded text (string) or MRS bytes
// ([]byte), or nil (None) when there is no source to read. Transport
// failures become a RequestException without details.
func ReadRuleSourceContent(ctx context.Context, rule *jsonx.Object, client *Client) (any, error) {
	sourceType := rule.GetOr("source_type", "url")
	if sourceType == "content" {
		return rule.GetOr("content", ""), nil
	}
	if sourceType != "url" || !jsonx.Truthy(rule.Value("url")) {
		return nil, nil
	}
	if client == nil {
		client = DefaultClient
	}
	content, err := func() (any, error) {
		response, err := client.RequestRule(ctx, jsonx.Str(rule.Value("url")), Request{ConnectTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second})
		if err != nil {
			return nil, err
		}
		if err := response.RaiseForStatus(); err != nil {
			return nil, err
		}
		data, err := response.Content()
		if err != nil {
			return nil, err
		}
		if err := ValidateRemoteRuleContent(data, rule, nil, ""); err != nil {
			return nil, err
		}
		format, err := convert.RulesetFormat(rule, jsonx.NewObject(), "")
		if err != nil {
			return nil, err
		}
		if format == "mrs" {
			return data, nil
		}
		return string(data), nil
	}()
	if err == nil {
		return content, nil
	}
	var remote *RemoteRuleValidationError
	if errors.As(err, &remote) || !(IsRequestException(err) || isValueError(err)) {
		// RemoteRuleValidationError is re-raised; anything that is neither a
		// RequestException nor a ValueError (storage errors) propagates.
		return nil, err
	}
	// Never expose proxy credentials through transport error messages.
	return nil, &FetchError{Class: "RequestException", Message: "Rule download failed (" + PythonClass(err) + ")"}
}

// SaveRuleToLocal is save_rule_to_local: refresh a source; a failed
// validation or download never changes its cache.
func SaveRuleToLocal(ctx context.Context, rule *jsonx.Object, client *Client) (string, error) {
	content, err := ReadRuleSourceContent(ctx, rule, client)
	if err != nil {
		return "", err
	}
	if content == nil {
		return "", errors.New("请提供有效的规则来源和订阅地址")
	}
	return WriteValidatedRuleCache(ctx, rule, content, nil)
}
