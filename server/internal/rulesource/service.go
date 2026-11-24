package rulesource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/rulesetfmt"
)

// Options configures a rulesource Service instance.
type Options struct {
	Repo   *repo.Repository
	Client *rulefetch.Client
}

// ReadOptions controls rule-source content reads.
type ReadOptions struct {
	Host           string
	Offline        bool
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	BoundDeadline  bool
}

// ContentOptions controls an explicit JSON-snapshot source read. Zero timeouts
// retain the 30-second defaults; a context deadline additionally bounds both.
type ContentOptions struct {
	AllowNetwork   bool
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

// Service owns rule-set content reading backed by a rulefetch.Client.
type Service struct {
	repo   *repo.Repository
	client *rulefetch.Client
}

// New constructs a rulesource Service. A nil Client resolves
// rulefetch.DefaultClient dynamically on each call.
func New(opts Options) *Service {
	return &Service{repo: opts.Repo, client: opts.Client}
}

func (s *Service) repository(ctx context.Context) (*repo.Repository, error) {
	if s != nil && s.repo != nil {
		return s.repo, nil
	}
	return repo.GetRepository(ctx)
}

var (
	defaultSvcMu   sync.RWMutex
	defaultService = New(Options{})
)

// Default returns the process default rulesource Service.
func Default() *Service {
	defaultSvcMu.RLock()
	defer defaultSvcMu.RUnlock()
	return defaultService
}

// UseService installs svc as the process default rulesource Service.
func UseService(svc *Service) {
	if svc == nil {
		return
	}
	defaultSvcMu.Lock()
	defaultService = svc
	defaultSvcMu.Unlock()
}

// FetchConfig builds the minimal rulefetch.SourceConfig needed for cache and
// download resolution from a model.Config.
func FetchConfig(config *model.Config) *rulefetch.SourceConfig {
	if config == nil {
		return nil
	}
	return &rulefetch.SourceConfig{Settings: config.SystemConfig, Library: config.RuleLibrary}
}

// Read reads rule-set content from cache, inline content, or remote refresh
// (when Offline is false), falling back to an alternate item URL when the
// primary library URL fails with a non-validation error.
func (s *Service) Read(ctx context.Context, config *rulefetch.SourceConfig, item model.RuleConfig, library *model.RuleLibraryItem, options ReadOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r, err := s.repository(ctx)
	if err != nil {
		return "", err
	}
	source := item
	if library != nil {
		source = shared.LibraryRule(*library)
	}
	cached, err := rulefetch.ReadCache(ctx, r, source, config)
	if err != nil {
		return "", err
	}
	if cached != nil {
		return sourceText(cached), nil
	}
	if sourceField(source.SourceType, "") == "content" {
		return sourceField(source.Content, ""), nil
	}
	if options.Offline {
		return "", nil
	}
	address := sourceField(source.URL, "")
	if address == "" {
		return "", nil
	}
	if strings.HasPrefix(address, "/") {
		address = strings.TrimRight(config.Settings.ServerDomain, "/") + address
	}
	connect, read := options.ConnectTimeout, options.ReadTimeout
	if connect == 0 {
		connect = 30 * time.Second
	}
	if read == 0 {
		read = 30 * time.Second
	}
	fetch := s.FetchWithTimeouts(ctx, config, options.Host, connect, read, options.BoundDeadline)
	content, _, err := rulefetch.RefreshRule(ctx, r, source, config, rulefetch.CacheOptions{
		Client:  s.clientOrDefault(),
		Timeout: 30 * time.Second,
		Fetch:   func(ctx context.Context, _ string) (rulefetch.Content, error) { return fetch(address) },
	})
	if err == nil {
		return sourceText(content), nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var validation *rulefetch.RemoteRuleValidationError
	if errors.As(err, &validation) {
		return "", err
	}
	// Download diagnostics are already logged by the fetch owner with redaction.
	if alternate := sourceField(item.URL, ""); library != nil && alternate != "" && alternate != sourceField(library.URL, "") {
		mirror := shared.LibraryRule(*library)
		mirror.URL = &alternate
		format, err := rulesetfmt.RuleFormat(shared.LibraryRule(*library), config.Library, "")
		if err != nil {
			return "", err
		}
		mirror.Format = &format
		return s.Read(ctx, config, mirror, nil, options)
	}
	return "", nil
}

// FetchWithTimeouts returns a single-URL download callback respecting connect,
// read, and optional context-deadline bounds.
func (s *Service) FetchWithTimeouts(ctx context.Context, config *rulefetch.SourceConfig, host string, connect, read time.Duration, boundDeadline bool) func(string) (rulefetch.Content, error) {
	client := s.clientOrDefault()
	return func(address string) (rulefetch.Content, error) {
		if err := ctx.Err(); err != nil {
			return rulefetch.Content{}, err
		}
		connectTimeout, readTimeout := connect, read
		if deadline, ok := ctx.Deadline(); boundDeadline && ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return rulefetch.Content{}, context.DeadlineExceeded
			}
			connectTimeout, readTimeout = min(connect, remaining), min(read, remaining)
		}
		response, err := client.Download(ctx, address, rulefetch.DownloadRequest{
			Config:         config,
			HostURL:        host,
			ConnectTimeout: connectTimeout,
			ReadTimeout:    readTimeout,
		})
		if err != nil {
			return rulefetch.Content{}, err
		}
		defer response.Close()
		if err = response.RaiseForStatus(); err != nil {
			return rulefetch.Content{}, err
		}
		content, err := response.Content()
		if err == nil {
			err = ctx.Err()
		}
		return rulefetch.Content{Data: content}, err
	}
}

// ReadJSON decodes raw JSON config/item/library snapshots and reads rule-set
// content. An explicit caller deadline is preserved rather than detached.
func (s *Service) ReadJSON(ctx context.Context, config, item, library json.RawMessage, options ContentOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if options.ConnectTimeout < 0 || options.ReadTimeout < 0 {
		return "", errors.New("rule content timeouts must not be negative")
	}
	profile, err := model.DecodeConfig(config)
	if err != nil {
		return "", err
	}
	var row model.RuleConfig
	if err := json.Unmarshal(item, &row); err != nil {
		return "", err
	}
	var lib *model.RuleLibraryItem
	if trimmed := bytes.TrimSpace(library); len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null")) {
		lib = &model.RuleLibraryItem{}
		if err := json.Unmarshal(trimmed, lib); err != nil {
			return "", err
		}
	}
	return s.Read(ctx, FetchConfig(profile), row, lib, ReadOptions{
		Offline:        !options.AllowNetwork,
		ConnectTimeout: options.ConnectTimeout,
		ReadTimeout:    options.ReadTimeout,
		BoundDeadline:  true,
	})
}

// ReadOfflineJSON reads cached/inline rule-set content from raw JSON snapshots
// with request cancellation detached (retaining shutdown cancellation).
func (s *Service) ReadOfflineJSON(ctx context.Context, profile, item, library json.RawMessage) (string, error) {
	config, err := model.DecodeConfig(profile)
	if err != nil {
		return "", err
	}
	var row model.RuleConfig
	if err := json.Unmarshal(item, &row); err != nil {
		return "", err
	}
	var lib *model.RuleLibraryItem
	if len(library) > 0 {
		lib = &model.RuleLibraryItem{}
		if err := json.Unmarshal(library, lib); err != nil {
			return "", err
		}
	}
	return s.Read(lifecycle.Detach(ctx), FetchConfig(config), row, lib, ReadOptions{Offline: true})
}

// ReadGateway satisfies gateway.RuleSourceReader: reads rule-set content for
// gateway provider generation with request cancellation detached.
func (s *Service) ReadGateway(ctx context.Context, item model.RuleConfig, library *model.RuleLibraryItem, config *rulefetch.SourceConfig) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if config == nil {
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return "", err
		}
		id, err := repo.ResolveProfileID(ctx, "")
		if err != nil {
			return "", err
		}
		profile, err := r.Config(ctx, id, nil)
		if err != nil {
			return "", err
		}
		config = FetchConfig(profile)
	}
	return s.Read(ctx, config, item, library, ReadOptions{})
}

func (s *Service) clientOrDefault() *rulefetch.Client {
	if s != nil && s.client != nil {
		return s.client
	}
	return rulefetch.DefaultClient
}

func sourceText(content []byte) string {
	if !utf8.Valid(content) {
		return ""
	}
	return strings.TrimPrefix(string(content), "\ufeff")
}

func sourceField(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
