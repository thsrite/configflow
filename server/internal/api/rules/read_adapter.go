package rules

// Repository and legacy rule-engine adaptation stays at this boundary. The scan
// keeps only small projections; hydrated source content is never JSON-copied.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/weburl"
)

type sourceOptions struct {
	host    string
	offline bool
}

func (s *Service) sourceContent(ctx context.Context, config, item, library *jsonx.Object, options sourceOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	source := library
	if source == nil {
		source = item
	}
	cached, err := rulefetch.ReadValidatedRuleCache(ctx, source, config)
	if err != nil {
		return "", err
	}
	if cached != nil {
		return sourceText(cached), nil
	}
	if source.GetString("source_type") == "content" {
		return source.GetString("content"), nil
	}
	if options.offline {
		return "", nil
	}
	address := source.GetString("url")
	if address == "" {
		return "", nil
	}
	if strings.HasPrefix(address, "/") {
		address = strings.TrimRight(config.GetObject("system_config").GetString("server_domain"), "/") + address
	}
	fetch := s.fetch(ctx, config, options.host, 30*time.Second)
	content, _, err := rulefetch.RefreshRemoteRule(ctx, source, config, rulefetch.RefreshOptions{Client: s.client, Timeout: 30 * time.Second, Fetch: func(string) (any, error) { return fetch(address) }})
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
	if alternate := item.GetString("url"); library != nil && alternate != "" && alternate != library.GetString("url") {
		mirror := library.Clone()
		mirror.Set("url", alternate)
		format, err := convert.RulesetFormat(library, config, "")
		if err != nil {
			return "", err
		}
		mirror.Set("format", format)
		return s.sourceContent(ctx, config, mirror, nil, options)
	}
	return "", nil
}
func (s *Service) fetch(ctx context.Context, config *jsonx.Object, host string, timeout time.Duration) func(string) (any, error) {
	return func(address string) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := s.client.RequestRule(ctx, address, rulefetch.Request{Config: config, HostURL: host, ConnectTimeout: timeout, ReadTimeout: timeout})
		if err != nil {
			return nil, err
		}
		defer response.Close()
		if err = response.RaiseForStatus(); err != nil {
			return nil, err
		}
		content, err := response.Content()
		if err == nil {
			err = ctx.Err()
		}
		return content, err
	}
}
func (s *Service) local(ctx context.Context, id, name, target, host string) (delivery, error) {
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return delivery{}, err
	}
	library := config.GetList("rule_library")
	var source *jsonx.Object
	for _, value := range library {
		if row, ok := value.(*jsonx.Object); ok && row.GetString("name") == name {
			source = row
			break
		}
	}
	cacheSource := source
	if cacheSource == nil {
		cacheSource = jsonx.Obj("name", name)
	}
	filename, err := rulefetch.RuleCacheReadPath(ctx, cacheSource, library)
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
	} else if source.GetOr("source_type", "url") == "url" {
		content, stale, err = refresh.ReadRuleSource(ctx, source, refresh.RuleOptions{Config: config, Fetch: s.fetch(ctx, config, host, 2*time.Second), Timeout: 2 * time.Second})
		if err != nil {
			if validation := remoteFault(err); validation != nil {
				validation.CacheRetained = boolPointer(false)
				return delivery{}, validation
			}
			return delivery{}, &sourceFailure{Status: 502, Message: weburl.SafeErrorDetails(err), CacheRetained: boolPointer(false)}
		}
	} else {
		content, err = rulefetch.ReadValidatedRuleCache(ctx, source, config)
		if err == nil && content == nil {
			// The committed inline declaration remains authoritative if an edit
			// could not replace its stable cache. Never try the legacy cache again.
			content = []byte(source.GetString("content"))
			// Cache population is best effort here: delivery already has the
			// committed bytes and does not need to read another file afterwards.
			_, _ = rulefetch.SaveRuleToLocal(ctx, source, s.client)
		}
	}
	if err != nil {
		return delivery{}, &fault{500, msgLocalFailed}
	}
	if source != nil && target == "mihomo" && utf8.Valid(content) {
		format, err := convert.RulesetFormat(source, config, "")
		if err != nil {
			return delivery{}, &fault{500, msgLocalFailed}
		}
		normalized, _, err := rulefetch.NormalizeMihomoRuleset(string(content), source.GetOr("behavior", "domain"), format)
		if err != nil {
			return delivery{}, &fault{500, msgLocalFailed}
		}
		content = []byte(normalized)
	}
	return delivery{Content: content, Stale: stale != nil}, nil
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
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	var invalidSource error
	matcher := shared.NewRuleConfigMatcher(config.GetList("rule_configs"), config.GetList("rule_library"), func(item, library *jsonx.Object) (string, error) {
		content, err := s.sourceContent(ctx, config, item, library, sourceOptions{host: host})
		// C14 accepts these source line boundaries and normalizes them for client
		// delivery. Feed the same records to C09's CR/LF text index, keeping the
		// common ASCII path allocation-free and leaving the raw cache untouched.
		if err == nil && strings.ContainsAny(content, "\v\f\x1c\x1d\x1e\u0085\u2028\u2029") {
			source := library
			if source == nil {
				source = item
			}
			format, formatErr := convert.RulesetFormat(source, config, "")
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
	config, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	items := config.GetList("rule_configs")
	rows := make([]scanRow, len(items))
	library := map[string]*jsonx.Object{}
	for _, value := range config.GetList("rule_library") {
		if row, ok := value.(*jsonx.Object); ok {
			if _, exists := library[row.GetString("id")]; !exists {
				library[row.GetString("id")] = row
			}
		}
	}
	for i, value := range items {
		item, ok := value.(*jsonx.Object)
		if !ok {
			return nil, nil, errors.New("rule record is not an object")
		}
		policy, err := json.Marshal(item.GetOr("policy", "DIRECT"))
		if err != nil {
			return nil, nil, err
		}
		rows[i] = scanRow{ID: item.GetString("id"), Name: item.GetString("name"), Kind: item.GetString("itemType"), Type: item.GetString("rule_type"), Value: item.GetString("value"), Policy: policy, Enabled: jsonx.Truthy(item.GetOr("enabled", true)) && jsonx.Truthy(item.GetOr("library_enabled", true))}
		if _, ok := item.Get("name"); !ok {
			rows[i].Name = msgRuleset
		}
		if _, ok := item.Get("itemType"); !ok {
			rows[i].Kind = "rule"
		}
	}
	return rows, func(index int) (string, error) {
		item := items[index].(*jsonx.Object)
		return s.sourceContent(ctx, config, item, library[item.GetString("library_rule_id")], sourceOptions{host: host})
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
		config, err := sourceObject(profile)
		if err != nil {
			return "", err
		}
		row, err := sourceObject(item.Raw)
		if err != nil {
			return "", err
		}
		var lib *jsonx.Object
		if library != nil {
			lib, err = sourceObject(library.Raw)
			if err != nil {
				return "", err
			}
		}
		return s.sourceContent(lifecycle.Detach(ctx), config, row, lib, sourceOptions{offline: true})
	}

	gateway.SetRulesetContent(func(ctx context.Context, item, library, config *jsonx.Object) (string, error) {
		ctx = lifecycle.Detach(ctx)
		if config == nil {
			var err error
			config, err = repo.GetConfig(ctx, "")
			if err != nil {
				return "", err
			}
		}
		return s.sourceContent(ctx, config, item, library, sourceOptions{})
	})
}

func sourceObject(raw json.RawMessage) (*jsonx.Object, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("decode rule source snapshot: %w", err)
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, errors.New("rule source snapshot must be an object")
	}
	return object, nil
}
