package rules

// Rule readers consume captured model snapshots. The scan keeps small projections;
// the remaining Scope bridge is isolated at the end of this file.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/gateway"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/rulesetfmt"
	"configflow/internal/weburl"
)

type sourceOptions struct {
	host           string
	offline        bool
	connectTimeout time.Duration
	readTimeout    time.Duration
	boundDeadline  bool
}

func (s *Service) sourceContent(ctx context.Context, config *rulefetch.SourceConfig, item model.RuleConfig, library *model.RuleLibraryItem, options sourceOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	source := item
	if library != nil {
		source = shared.LibraryRule(*library)
	}
	cached, err := rulefetch.ReadCache(ctx, source, config)
	if err != nil {
		return "", err
	}
	if cached != nil {
		return sourceText(cached), nil
	}
	if sourceField(source.SourceType, "") == "content" {
		return sourceField(source.Content, ""), nil
	}
	if options.offline {
		return "", nil
	}
	address := sourceField(source.URL, "")
	if address == "" {
		return "", nil
	}
	if strings.HasPrefix(address, "/") {
		address = strings.TrimRight(config.Settings.ServerDomain, "/") + address
	}
	connect, read := options.connectTimeout, options.readTimeout
	if connect == 0 {
		connect = 30 * time.Second
	}
	if read == 0 {
		read = 30 * time.Second
	}
	fetch := s.fetchWithTimeouts(ctx, config, options.host, connect, read, options.boundDeadline)
	content, _, err := rulefetch.RefreshRule(ctx, source, config, rulefetch.CacheOptions{Client: s.client, Timeout: 30 * time.Second, Fetch: func(ctx context.Context, _ string) (rulefetch.Content, error) { return fetch(address) }})
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
		return s.sourceContent(ctx, config, mirror, nil, options)
	}
	return "", nil
}
func (s *Service) fetch(ctx context.Context, config *rulefetch.SourceConfig, host string, timeout time.Duration) func(string) (rulefetch.Content, error) {
	return s.fetchWithTimeouts(ctx, config, host, timeout, timeout, false)
}

func (s *Service) fetchWithTimeouts(ctx context.Context, config *rulefetch.SourceConfig, host string, connect, read time.Duration, boundDeadline bool) func(string) (rulefetch.Content, error) {
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
		response, err := s.client.Download(ctx, address, rulefetch.DownloadRequest{Config: config, HostURL: host, ConnectTimeout: connectTimeout, ReadTimeout: readTimeout})
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
func (s *Service) local(ctx context.Context, id, name, target, host string) (delivery, error) {
	config, err := scopedLocalConfig(ctx, id)
	if err != nil {
		return delivery{}, err
	}
	library := config.RuleLibrary
	var source *model.RuleLibraryItem
	for _, row := range library {
		if row.Name == name {
			value := row
			source = &value
			break
		}
	}
	cacheSource := model.RuleConfig{Name: name}
	if source != nil {
		cacheSource = shared.LibraryRule(*source)
	}
	filename, err := rulefetch.CacheReadPath(ctx, cacheSource, library)
	if err != nil {
		return delivery{}, &fault{500, msgLocalFailed}
	}
	var content []byte
	var stale error
	if source == nil {
		content, err = os.ReadFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			return delivery{}, missing(fmt.Sprintf(msgLocalMissing, name))
		}
	} else if sourceField(source.SourceType, "url") == "url" {
		content, stale, err = refresh.ReadRemote(ctx, *source, refresh.RuleReadOptions{Config: fetchConfig(config), Fetch: func(ctx context.Context, address string) (rulefetch.Content, error) {
			return s.fetch(ctx, fetchConfig(config), host, 2*time.Second)(address)
		}, Timeout: 2 * time.Second})
		if err != nil {
			if validation := remoteFault(err); validation != nil {
				validation.CacheRetained = boolPointer(false)
				return delivery{}, validation
			}
			return delivery{}, &sourceFailure{Status: 502, Message: weburl.SafeErrorDetails(err), CacheRetained: boolPointer(false)}
		}
	} else {
		content, err = rulefetch.ReadCache(ctx, shared.LibraryRule(*source), fetchConfig(config))
		if err == nil && content == nil {
			// The committed inline declaration remains authoritative if an edit
			// could not replace its stable cache. Never try the legacy cache again.
			content = []byte(sourceField(source.Content, ""))
			// This reader has no editor lease. Publishing its snapshot could race
			// a draft's DB commit/rename and overwrite the newly published cache.
			// Cold-cache population resolves the current DB declaration and the
			// shared writer checks it again under the repository lock.
			populateInlineCache(ctx, shared.LibraryRule(*source))
		}
	}
	if err != nil {
		return delivery{}, &fault{500, msgLocalFailed}
	}
	if source != nil && target == "mihomo" && !utf8.Valid(content) {
		logger.Warningf(msgLocalUTF8, localLogName(ctx, config, name))
	} else if source != nil && target == "mihomo" {
		format, err := rulesetfmt.RuleFormat(shared.LibraryRule(*source), config.RuleLibrary, "")
		if err != nil {
			return delivery{}, &fault{500, msgLocalFailed}
		}
		normalized, warnings, err := rulefetch.NormalizeRuleset(string(content), sourceField(source.Behavior, "domain"), format)
		if err != nil {
			return delivery{}, &fault{500, msgLocalFailed}
		}
		content = []byte(normalized)
		if len(warnings) > 0 {
			lines := make([]string, 0, min(10, len(warnings)))
			for _, warning := range warnings[:min(10, len(warnings))] {
				lines = append(lines, strconv.Itoa(warning.Line))
			}
			logger.Warningf(msgLocalReview, localLogName(ctx, config, name), len(warnings), strings.Join(lines, ", "))
		}
	}
	return delivery{Content: content, Stale: stale != nil}, nil
}

func populateInlineCache(ctx context.Context, source model.RuleConfig) {
	if ctx.Err() != nil {
		return
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return
	}
	catalog, err := r.Shared(ctx)
	if err != nil {
		return
	}
	for _, current := range catalog.RuleLibrary {
		if current.ID != source.ID {
			continue
		}
		if sourceField(current.SourceType, "url") == "content" && ctx.Err() == nil {
			_, _ = rulefetch.SaveSource(ctx, shared.LibraryRule(current), nil)
		}
		break
	}
}

func localLogName(ctx context.Context, config *model.Config, name string) string {
	secrets, err := redact.ExternalPayloadSecrets(ctx, config.SystemConfig)
	if err != nil {
		return redact.Redacted
	}
	var public struct {
		Name string `json:"name"`
	}
	if jsondecode.Unmarshal(redact.SanitizeCredentials(map[string]any{"name": name}, secrets), &public) != nil {
		return redact.Redacted
	}
	return logx.LogText(public.Name, 120)
}
func boolPointer(value bool) *bool { return &value }
func remoteFault(err error) *sourceFailure {
	var validation *rulefetch.RemoteRuleValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	raw, encodeErr := json.Marshal(validation.Errors)
	if encodeErr != nil {
		return nil
	}
	return &sourceFailure{Message: validation.Error(), Errors: raw}
}
func (s *Service) match(ctx context.Context, id, query, host string) (record, error) {
	config, err := resolvedConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	var invalidSource error
	matcher := shared.NewMatcher(config.Profile.RuleConfigs, config.RuleLibrary, func(item *model.RuleConfig, library *model.RuleLibraryItem) (string, error) {
		content, err := s.sourceContent(ctx, fetchConfig(config), *item, library, sourceOptions{host: host})
		// C14 accepts these source line boundaries and normalizes them for client
		// delivery. Feed the same records to C09's CR/LF text index, keeping the
		// common ASCII path allocation-free and leaving the raw cache untouched.
		if err == nil && strings.ContainsAny(content, "\v\f\x1c\x1d\x1e\u0085\u2028\u2029") {
			source := *item
			if library != nil {
				source = shared.LibraryRule(*library)
			}
			format, formatErr := rulesetfmt.RuleFormat(source, config.RuleLibrary, "")
			if formatErr != nil {
				err = formatErr
			} else if format == "text" {
				content = strings.Join(rulefetch.RuleLines(content), "\n")
			}
		}
		if err != nil && invalidSource == nil {
			invalidSource = err
		}
		return content, nil
	}, nil, nil)
	matched := matcher.Match(query)
	if invalidSource != nil {
		if validation := remoteFault(invalidSource); validation != nil {
			return nil, validation
		}
		return nil, &fault{500, msgMatchFailed}
	}
	if matched == nil {
		return nil, nil
	}
	return native(matched)
}
func (s *Service) scanRows(ctx context.Context, id, host string) ([]scanRow, func(int) (string, error), error) {
	config, err := resolvedConfig(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	items := config.Profile.RuleConfigs
	rows := make([]scanRow, len(items))
	library := map[string]*model.RuleLibraryItem{}
	for i := range config.RuleLibrary {
		row := &config.RuleLibrary[i]
		if _, exists := library[row.ID]; !exists {
			library[row.ID] = row
		}
	}
	for i, item := range items {
		raw, err := native(item)
		if err != nil {
			return nil, nil, err
		}
		policy, ok := raw["policy"]
		if !ok {
			policy = stringJSON("DIRECT")
		}
		rows[i] = scanRow{ID: item.ID, Name: item.Name, Kind: defaultText(raw, "itemType", "rule"), Type: item.RuleType, Value: item.Value, Policy: policy, Enabled: rawFlag(raw, "enabled", true) && rawFlag(raw, "library_enabled", true)}
		if _, ok := raw["name"]; !ok {
			rows[i].Name = msgRuleset
		}
	}
	return rows, func(index int) (string, error) {
		item := items[index]
		return s.sourceContent(ctx, fetchConfig(config), item, library[item.LibraryRuleID], sourceOptions{host: host})
	}, nil
}

func sourceText(content []byte) string {
	if !utf8.Valid(content) {
		return ""
	}
	return strings.TrimPrefix(string(content), "\ufeff")
}

// Wire installs C32's shared source readers for routers and gateway providers. The
// gateway passes its immutable publish snapshot; resolving it again could mix
// revisions. The source/cache owner retains shutdown cancellation.
func Wire() {
	s := &Service{client: rulefetch.DefaultClient}
	routers.RulesetContent = func(ctx context.Context, profile json.RawMessage, item routers.RuleSet, library *routers.LibraryRule) (string, error) {
		config, err := model.DecodeConfig(profile)
		if err != nil {
			return "", err
		}
		var row model.RuleConfig
		if err := json.Unmarshal(item.Raw, &row); err != nil {
			return "", err
		}
		var lib *model.RuleLibraryItem
		if library != nil {
			lib = &model.RuleLibraryItem{}
			if err := json.Unmarshal(library.Raw, lib); err != nil {
				return "", err
			}
		}
		return s.sourceContent(lifecycle.Detach(ctx), fetchConfig(config), row, lib, sourceOptions{offline: true})
	}
	gateway.SetRuleSourceReader(func(ctx context.Context, item model.RuleConfig, library *model.RuleLibraryItem, config *rulefetch.SourceConfig) (string, error) {
		ctx = lifecycle.Detach(ctx)
		if config == nil {
			var err error
			profile, getErr := resolvedConfig(ctx, "")
			err = getErr
			if err != nil {
				return "", err
			}
			config = fetchConfig(profile)
		}
		return s.sourceContent(ctx, config, item, library, sourceOptions{})
	})
}

func resolvedConfig(ctx context.Context, id string) (*model.Config, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	id, err = repo.ResolveProfileID(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.Config(ctx, id, nil)
}
func fetchConfig(config *model.Config) *rulefetch.SourceConfig {
	if config == nil {
		return nil
	}
	return &rulefetch.SourceConfig{Settings: config.SystemConfig, Library: config.RuleLibrary}
}
func sourceField(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

// scopedLocalConfig retains the admitted request's existing Scope snapshot until
// repo provides the equivalent scoped model read. No source/cache logic lives here.
func scopedLocalConfig(ctx context.Context, id string) (*model.Config, error) {
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return model.DecodeConfig(raw)
}
