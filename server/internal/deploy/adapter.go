package deploy

// All legacy document boundaries live here; the rest of deploy only sees typed
// values, yamlx nodes, opaque records or raw JSON.
import (
	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/provider"
	"configflow/internal/pyyaml"
	"configflow/internal/refresh"
	"configflow/internal/rulefetch"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Source is an immutable profile snapshot until the typed repository lands.
type Source struct {
	legacy   *jsonx.Object
	revision *int64
}

// NewSource captures a legacy profile and its optional revision.
func NewSource(config *jsonx.Object, revision *int64) *Source {
	s := &Source{}
	if config != nil {
		s.legacy = config.DeepCopy()
	}
	if revision != nil {
		r := *revision
		s.revision = &r
	}
	return s
}

// ProfileID returns the snapshot's resolved profile identifier.
func (s *Source) ProfileID() string {
	if s == nil {
		return ""
	}
	return s.legacy.GetString("profile_id")
}
func (s *Source) gateway() *gateway.Config {
	if s == nil {
		return gateway.NewConfig(nil)
	}
	return gateway.NewConfig(s.legacy)
}

// NewTarget captures the Agent fields needed by the publishing chain.
func NewTarget(record *jsonx.Object) Target {
	if record == nil {
		return Target{}
	}
	r := record.DeepCopy()
	port, _ := json.Marshal(r.Value("port"))
	return Target{ID: r.GetString("id"), Name: r.GetString("name"), ProfileID: r.GetString("profile_id"), ServiceType: r.GetString("service_type"), DeploymentMethod: r.GetString("deployment_method"), Version: r.GetString("version"), Host: r.GetString("host"), Port: port, Token: r.GetString("token"), DomainDiscovery: r.GetBool("domain_discovery_enabled"), Record: r}
}

func (s *Source) config() *jsonx.Object {
	if s == nil || s.legacy == nil {
		return jsonx.NewObject()
	}
	return s.legacy
}
func (s *Source) serverDomain(fallback string) string {
	domain := s.config().GetObject("system_config").GetString("server_domain")
	if domain == "" {
		return fallback
	}
	return domain
}
func (s *Source) internal(rawURL, baseURL string) bool {
	cfg := s.config().DeepCopy()
	settings := cfg.GetObject("system_config")
	if settings == nil {
		settings = jsonx.NewObject()
		cfg.Set("system_config", settings)
	}
	settings.Set("server_domain", s.serverDomain(baseURL))
	return rulefetch.IsInternalRuleURL(rawURL, cfg, "")
}
func (s *Source) profilePath(suffix string) string {
	if id := s.ProfileID(); id != "" {
		return "/api/profiles/" + id + suffix
	}
	return suffix
}
func (s *Source) library() []RuleRef {
	out := []RuleRef{}
	for _, value := range s.config().GetList("rule_library") {
		item, ok := value.(*jsonx.Object)
		if !ok {
			continue
		}
		out = append(out, RuleRef{ID: item.GetString("id"), Name: item.GetString("name"), URL: item.GetString("url"), SourceType: item.GetString("source_type"), Content: item.GetString("content"), Behavior: item.GetString("behavior"), Format: item.GetString("format")})
	}
	return out
}
func (s *Source) requestRule(ctx context.Context, rawURL string) (*rulefetch.Response, error) {
	return rulefetch.RequestRule(ctx, rawURL, rulefetch.Request{Config: s.config(), MaxBytes: MaxFileBytes, Stream: true, ConnectTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second})
}
func (s *Source) readRule(ctx context.Context, rawURL, sourceID string, declared *RuleRef, fetch func(context.Context, string) ([]byte, error)) ([]byte, error, error, bool) {
	cfg := s.config()
	rule, err := rulefetch.ResolveRemoteRuleSource(rawURL, cfg, sourceID)
	if err != nil {
		return nil, nil, err, true
	}
	if rule == nil && declared != nil {
		rule = jsonx.Obj("name", declared.Name, "behavior", declared.Behavior, "format", declared.Format, "source_type", "url", "url", rawURL)
	}
	if rule == nil || rule.GetOr("source_type", "url") != "url" {
		return nil, nil, nil, false
	}
	value, stale, err := refresh.ReadRuleSource(ctx, rule, refresh.RuleOptions{Config: cfg, Fetch: func(u string) (any, error) { return fetch(ctx, u) }})
	return value, stale, err, true
}

// SourceError bridges the legacy provider parser's marks; all new deploy YAML
// readers use yamlx.Parse. Delete this bridge when N7-3 migrates provider.
func (Locations) SourceError(message, text string, line, column *int, cause *pyyaml.Error, sourceLabel string) string {
	var diagnostic error
	if cause != nil {
		if line == nil && cause.ProblemMark != nil {
			l, c := cause.ProblemMark.Line+1, cause.ProblemMark.Column+1
			line, column = &l, &c
		}
		parts := []string{}
		if cause.Context != "" {
			parts = append(parts, cause.Context)
		}
		if cause.Problem != "" {
			parts = append(parts, cause.Problem)
		}
		if len(parts) > 0 {
			diagnostic = errors.New(strings.Join(parts, ": "))
		} else {
			diagnostic = cause
		}
	}
	return SourceError(message, text, Position{Line: line, Column: column}, diagnostic, sourceLabel).Error()
}
func downloadRef(d convert.Download) (RuleRef, string, error) {
	u, ok := d.URL.(string)
	if !ok {
		return RuleRef{}, "", preparation("Required rule has no source URL")
	}
	p, ok := d.LocalPath.(string)
	if !ok {
		return RuleRef{}, "", preparation("Invalid deployment file path")
	}
	return RuleRef{Name: d.Name, URL: u, Behavior: d.Behavior, Format: d.Format}, p, nil
}

// Wire installs deployment preparation diagnostics. A14 adds publication hooks.
func Wire() { provider.SetLocations(Locations{}) }
