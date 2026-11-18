package rulefetch

// The repository-backed functions of backend/utils/rule_utils.py
// (get_rules_dir, read/write_validated_rule_cache, refresh_remote_rule,
// read_rule_source_content, save_rule_to_local). The pure name/path helpers
// are in internal/rules (SanitizeRuleName, RuleCacheFilename,
// CacheReadPath).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/rulesetfmt"
	"configflow/internal/weburl"
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

// CacheReadPath is rule_cache_read_path(rule, library) with the stored
// shared rule library; library nil is None.
func CacheReadPath(ctx context.Context, rule model.RuleConfig, library []model.RuleLibraryItem) (string, error) {
	return cacheReadPath(ctx, rule, func() []model.RuleLibraryItem { return library })
}
func cacheReadPath(ctx context.Context, rule model.RuleConfig, library func() []model.RuleLibraryItem) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		return "", err
	}
	current := filepath.Join(root, rules.CacheFilename(rule))
	_, receiptErr := os.Stat(current + ".invalid")
	if fileExists(current) || !errors.Is(receiptErr, os.ErrNotExist) {
		return rules.CacheReadPath(rule, nil, nil, root), nil
	}
	// View is read-only. Copying Shared here would marshal and decode every
	// unrelated node/subscription for each ruleset miss.
	var result string
	err = r.View(ctx, func(document *model.Document) error {
		result = rules.CacheReadPath(rule, library(), document.Shared.RuleLibrary, root)
		return nil
	})
	return result, err
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// Content is explicitly text or binary. An MRS source must be binary.
type Content struct {
	Data []byte
	Text bool
}

// ReadCache is read_validated_rule_cache: only cache bytes that
// satisfy the current remote declaration, else nil. config nil is None.
func ReadCache(ctx context.Context, rule model.RuleConfig, config *SourceConfig) ([]byte, error) {
	return ReadCacheWithFormat(ctx, rule, config, "")
}

// ReadCacheWithFormat validates a cached source with a caller-known format.
// A nonempty format never runs inference or decodes a rule-library payload.
func ReadCacheWithFormat(ctx context.Context, rule model.RuleConfig, config *SourceConfig, format string) ([]byte, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cachePath, err := cacheReadPath(ctx, rule, func() []model.RuleLibraryItem {
		if config == nil {
			return nil
		}
		if config.cacheLibrary != nil {
			return config.cacheLibrary()
		}
		return config.Library
	})
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil
	}
	if textOr(rule.SourceType, "url") == "url" {
		if err := validateBytes(Content{Data: content}, rule, config, format); err != nil {
			var remote *RemoteRuleValidationError
			if errors.As(err, &remote) {
				return nil, nil
			}
			return nil, err
		}
	}
	// Migrate an old name only after byte validation and another declaration
	// check under the repository transaction. Keep the old file for rollback.
	root, err := GetRulesDir(ctx)
	if err != nil {
		return nil, err
	}
	if cachePath != filepath.Join(root, rules.CacheFilename(rule)) {
		if err := migrateCache(ctx, rule, content, cachePath); err != nil {
			return nil, err
		}
	}
	return content, nil
}

// WriteCache is write_validated_rule_cache: validate the
// complete response, then atomically replace the raw bytes (identical
// bytes only get a new modification time). It returns the cache file name
// relative to the rules directory.
func WriteCache(ctx context.Context, rule model.RuleConfig, content Content, config *SourceConfig) (string, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if textOr(rule.SourceType, "url") == "url" {
		if err := validateBytes(content, rule, config, ""); err != nil {
			return "", err
		}
	}
	filename := rules.CacheFilename(rule)
	relative := path.Join("rules", filename)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	target, err := r.SharedPath(relative)
	if err != nil {
		return "", err
	}
	data := content.Data
	write := func(locked context.Context) error {
		if err := locked.Err(); err != nil {
			return err
		}
		_, err := r.WriteSharedTextIfChanged(locked, relative, data)
		return err
	}
	if textOr(rule.SourceType, "url") == "content" {
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
func writeCurrentInline(ctx context.Context, r *repo.Repository, rule model.RuleConfig, target string, data []byte, write func(context.Context) error) error {
	// A C33 publisher may hold L3 while an independently admitted reader
	// finishes. Its receipt already forbids this write, so do not wait on L3.
	if _, err := os.Stat(target + ".invalid"); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	id := rule.LibraryRuleID
	if id == "" {
		id = rule.ID
	}
	if id == "" {
		return write(ctx)
	}
	err := r.UpdateShared(ctx, func(tx context.Context, catalog *model.Shared) error {
		if err := tx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(target + ".invalid"); !errors.Is(err, os.ErrNotExist) {
			return errInlineCacheOnly
		}
		for _, current := range catalog.RuleLibrary {
			if current.ID != id {
				continue
			}
			if textOr(current.SourceType, "url") == "content" && bytes.Equal(data, []byte(textOr(current.Content, ""))) {
				if err := tx.Err(); err != nil {
					return err
				}
				if err := write(tx); err != nil {
					return err
				}
			}
			break
		}
		return errInlineCacheOnly
	})
	if errors.Is(err, errInlineCacheOnly) {
		return nil
	}
	return err
}

// CacheOptions are refresh_remote_rule's keyword arguments.
type CacheOptions struct {
	// Timeout is timeout= (10 s when 0).
	Timeout time.Duration
	// Fetch replaces the download (fetch=); it returns string or []byte.
	Fetch func(context.Context, string) (Content, error)
	// AllowStale serves validated previous bytes when the refresh fails.
	AllowStale bool
	Client     *Client // nil: DefaultClient
}

// RefreshRule is refresh_remote_rule: the raw bytes, plus the refresh
// error when validated cache bytes are served instead (AllowStale).
func RefreshRule(ctx context.Context, rule model.RuleConfig, config *SourceConfig, opts CacheOptions) ([]byte, error, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	content, err := func() (Content, error) {
		address := textOr(rule.URL, "")
		if opts.Fetch != nil {
			return opts.Fetch(ctx, address)
		}
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 10 * time.Second
		}
		client := opts.Client
		if client == nil {
			client = DefaultClient
		}
		return downloadContent(ctx, client, address, config, timeout)
	}()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	if err == nil {
		if _, err = WriteCache(ctx, rule, content, config); err == nil {
			return content.Data, nil, nil
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil, err
	}
	if opts.AllowStale {
		cached, readErr := ReadCache(ctx, rule, config)
		if readErr != nil {
			return nil, nil, readErr
		}
		if cached != nil {
			utilsLogger.Warningf(msgStaleCache, logx.LogText(rule.Name, 120), weburl.SafeErrorDetails(err))
			return cached, err, nil
		}
	}
	return nil, nil, err
}

// ReadSource is read_rule_source_content: validate a source
// without publishing its catalog entry or cache. The result is the
// handwritten content (string), the downloaded text (string) or MRS bytes
// ([]byte), or nil (None) when there is no source to read. Transport
// failures become a RequestException without details.
func ReadSource(ctx context.Context, rule model.RuleConfig, client *Client) (*Content, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source := textOr(rule.SourceType, "url")
	if source == "content" {
		return &Content{Data: []byte(textOr(rule.Content, "")), Text: true}, nil
	}
	if source != "url" || textOr(rule.URL, "") == "" {
		return nil, nil
	}
	if client == nil {
		client = DefaultClient
	}
	content, err := downloadContent(ctx, client, *rule.URL, nil, 10*time.Second)
	if err != nil {
		return nil, sourceDownloadError(err)
	}
	if err := validateBytes(content, rule, nil, ""); err != nil {
		return nil, err
	}
	format, err := rulesetfmt.RuleFormat(rule, nil, "")
	if err != nil {
		return nil, err
	}
	content.Text = format != "mrs"
	return &content, nil
}

// SaveSource validates and publishes a complete source; a failure keeps its cache.
func SaveSource(ctx context.Context, rule model.RuleConfig, client *Client) (string, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := ReadSource(ctx, rule, client)
	if err != nil {
		return "", err
	}
	if content == nil {
		return "", errors.New(msgMissingSource)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return WriteCache(ctx, rule, *content, nil)
}
func textOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func migrateCache(ctx context.Context, rule model.RuleConfig, data []byte, oldPath string) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		return err
	}
	returnErr := r.UpdateShared(ctx, func(tx context.Context, shared *model.Shared) error {
		if err := tx.Err(); err != nil {
			return err
		}
		// Resolve with the current catalog a second time; an edit/receipt that
		// occurred after the read forbids publishing those old bytes.
		if rules.CacheReadPath(rule, nil, shared.RuleLibrary, root) != oldPath {
			return errInlineCacheOnly
		}
		found := false
		for _, item := range shared.RuleLibrary {
			if item.ID == rule.ID {
				found = sameDeclaration(rule, rules.LibraryRule(item))
				break
			}
		}
		if !found {
			return errInlineCacheOnly
		}
		if err := tx.Err(); err != nil {
			return err
		}
		_, err := r.WriteSharedTextIfChanged(tx, path.Join("rules", rules.CacheFilename(rule)), data)
		if err != nil {
			return err
		}
		return errInlineCacheOnly
	})
	if errors.Is(returnErr, errInlineCacheOnly) {
		return nil
	}
	return returnErr
}
func sameDeclaration(a, b model.RuleConfig) bool {
	return textOr(a.URL, "") == textOr(b.URL, "") && textOr(a.SourceType, "url") == textOr(b.SourceType, "url") && textOr(a.Behavior, "domain") == textOr(b.Behavior, "domain") && textOr(a.Format, "") == textOr(b.Format, "")
}
