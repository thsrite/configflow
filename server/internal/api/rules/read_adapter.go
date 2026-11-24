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

	"configflow/internal/jsondecode"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/rulesetfmt"
	"configflow/internal/rulesource"
	"configflow/internal/weburl"
)

type sourceOptions = rulesource.ReadOptions

func (s *Service) reader(ctx context.Context) *rulesource.Service {
	if s != nil && s.ruleSource != nil {
		return s.ruleSource
	}
	var r *repo.Repository
	var client *rulefetch.Client
	if s != nil {
		r = s.repo
		client = s.client
	}
	if r == nil {
		r, _ = repo.GetRepository(ctx)
	}
	return rulesource.New(rulesource.Options{Repo: r, Client: client})
}

func (s *Service) sourceContent(ctx context.Context, config *rulefetch.SourceConfig, item model.RuleConfig, library *model.RuleLibraryItem, options sourceOptions) (string, error) {
	return s.reader(ctx).Read(ctx, config, item, library, options)
}

func (s *Service) fetch(ctx context.Context, config *rulefetch.SourceConfig, host string, timeout time.Duration) func(string) (rulefetch.Content, error) {
	return s.reader(ctx).FetchWithTimeouts(ctx, config, host, timeout, timeout, false)
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
	r, err := s.repository(ctx)
	if err != nil {
		return delivery{}, &fault{500, msgLocalFailed}
	}
	filename, err := rulefetch.CacheReadPath(ctx, r, cacheSource, library)
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
		content, err = rulefetch.ReadCache(ctx, r, shared.LibraryRule(*source), fetchConfig(config))
		if err == nil && content == nil {
			// The committed inline declaration remains authoritative if an edit
			// could not replace its stable cache. Never try the legacy cache again.
			content = []byte(sourceField(source.Content, ""))
			// This reader has no editor lease. Publishing its snapshot could race
			// a draft's DB commit/rename and overwrite the newly published cache.
			// Cold-cache population resolves the current DB declaration and the
			// shared writer checks it again under the repository lock.
			s.populateInlineCache(ctx, shared.LibraryRule(*source))
		}
	}
	if err != nil {
		return delivery{}, &fault{500, msgLocalFailed}
	}
	if source != nil && target == "mihomo" && !utf8.Valid(content) {
		logger.Warningf(msgLocalUTF8, s.localLogName(ctx, config, name))
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
			logger.Warningf(msgLocalReview, s.localLogName(ctx, config, name), len(warnings), strings.Join(lines, ", "))
		}
	}
	return delivery{Content: content, Stale: stale != nil}, nil
}

func (s *Service) populateInlineCache(ctx context.Context, source model.RuleConfig) {
	if ctx.Err() != nil {
		return
	}
	r, err := s.repository(ctx)
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
			_, _ = rulefetch.SaveSource(ctx, r, shared.LibraryRule(current), nil)
		}
		break
	}
}

func (s *Service) localLogName(ctx context.Context, config *model.Config, name string) string {
	r, err := s.repository(ctx)
	if err != nil {
		return redact.Redacted
	}
	secrets, err := redact.ExternalPayloadSecrets(ctx, r, config.SystemConfig)
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
func (s *Service) match(ctx context.Context, id, query, host string) (*shared.Hit, error) {
	config, err := s.resolvedConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	var invalidSource error
	matcher := shared.NewMatcher(config.Profile.RuleConfigs, config.RuleLibrary, func(item *model.RuleConfig, library *model.RuleLibraryItem) (string, error) {
		content, err := s.sourceContent(ctx, fetchConfig(config), *item, library, sourceOptions{Host: host})
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
	return matched, nil
}
func (s *Service) scanRows(ctx context.Context, id, host string) ([]scanRow, func(int) (string, error), error) {
	config, err := s.resolvedConfig(ctx, id)
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
	for i := range items {
		item := &items[i]
		policy, present, err := model.EncodeMember(item, "policy")
		if err != nil {
			return nil, nil, err
		}
		if !present {
			policy = json.RawMessage(`"DIRECT"`)
		}
		kind := item.ItemType
		if kind == "" {
			kind = "rule"
		}
		rows[i] = scanRow{ID: item.ID, Name: item.Name, Kind: kind, Type: item.RuleType, Value: model.TextValue(item.Value), Policy: policy, Enabled: model.Flag(item.Enabled, true) && model.Flag(item.LibraryEnabled, true)}
		if item.Name == "" {
			rows[i].Name = msgRuleset
		}
	}
	return rows, func(index int) (string, error) {
		item := items[index]
		return s.sourceContent(ctx, fetchConfig(config), item, library[item.LibraryRuleID], sourceOptions{Host: host})
	}, nil
}

func (s *Service) resolvedConfig(ctx context.Context, id string) (*model.Config, error) {
	r, err := s.repository(ctx)
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

// scopedLocalConfig returns the admitted request's Scope snapshot.
func scopedLocalConfig(ctx context.Context, id string) (*model.Config, error) {
	return repo.Config(ctx, id)
}
