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
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	shared "configflow/internal/rules"
	"configflow/internal/weburl"
)

func (s *Service) sourceContent(ctx context.Context, config, item, library *jsonx.Object, host string) (string, error) {
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
	address := source.GetString("url")
	if address == "" {
		return "", nil
	}
	if strings.HasPrefix(address, "/") {
		address = strings.TrimRight(config.GetObject("system_config").GetString("server_domain"), "/") + address
	}
	fetch := s.fetch(ctx, config, host, 30*time.Second)
	content, _, err := rulefetch.RefreshRemoteRule(ctx, source, config, rulefetch.RefreshOptions{Client: s.client, Timeout: 30 * time.Second, Fetch: func(string) (any, error) { return fetch(address) }})
	if err == nil {
		return sourceText(content), nil
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
		return s.sourceContent(ctx, config, mirror, nil, host)
	}
	return "", nil
}
func (s *Service) fetch(ctx context.Context, config *jsonx.Object, host string, timeout time.Duration) func(string) (any, error) {
	return func(address string) (any, error) {
		response, err := s.client.RequestRule(ctx, address, rulefetch.Request{Config: config, HostURL: host, ConnectTimeout: timeout, ReadTimeout: timeout})
		if err != nil {
			return nil, err
		}
		defer response.Close()
		if err = response.RaiseForStatus(); err != nil {
			return nil, err
		}
		return response.Content()
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
		content, err = os.ReadFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			_, err = rulefetch.SaveRuleToLocal(ctx, source, s.client)
			if err == nil {
				filename, err = rulefetch.RuleCacheReadPath(ctx, source, library)
			}
			if err == nil {
				content, err = os.ReadFile(filename)
			}
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
		content, err := s.sourceContent(ctx, config, item, library, host)
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
		return s.sourceContent(ctx, config, item, library[item.GetString("library_rule_id")], host)
	}, nil
}

func sourceText(content []byte) string {
	if !utf8.Valid(content) {
		return ""
	}
	return strings.TrimPrefix(string(content), "\ufeff")
}
