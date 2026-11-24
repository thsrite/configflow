// Package mosdns exposes the MosDNS editor and authenticated rule resources.
package mosdns

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/repo"
)

// Deps supplies the collaborators for the MosDNS HTTP Handler.
type Deps struct {
	Repo *repo.Repository
}

// Handler serves MosDNS editor and authenticated rule-proxy HTTP routes.
type Handler struct {
	repo *repo.Repository
}

// New constructs a MosDNS Handler with explicit dependencies. Nil fields fall
// back to transitional process defaults until W11.
func New(deps Deps) *Handler { return &Handler{repo: deps.Repo} }

func (h *Handler) repository(ctx context.Context) (*repo.Repository, error) {
	if h != nil && h.repo != nil {
		return h.repo, nil
	}
	return repo.GetRepository(ctx)
}

// Register adds the MosDNS routes.
func Register(r *httpx.Router) { New(Deps{}).Register(r) }

// Register mounts the MosDNS routes on r.
func (h *Handler) Register(r *httpx.Router) {
	r.POST("/api/mosdns/settings", "mosdns.save_mosdns_settings", auth.Require(h.saveSettings))
	for _, s := range sections {
		path := s.path
		r.Handle([]string{"GET", "POST"}, "/api/mosdns/"+path, "mosdns."+s.endpoint, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
			return h.handleSection(c, path)
		}))
	}
	// Public: the rule proxy checks its own token or JWT.
	r.GET("/api/mosdns/rule-proxy", "mosdns.mosdns_rule_proxy", h.ruleProxy)
	r.GET("/api/profiles/<profile_id>/mosdns/rule-proxy", "profiles.profile_mosdns_rule_proxy", h.ruleProxy)
}

type section struct{ path, endpoint string }

var sections = []section{
	{"rulesets", "handle_mosdns_rulesets"},
	{"custom-matches", "handle_mosdns_custom_matches"},
	{"dns-servers", "handle_mosdns_dns_servers"},
	{"log-settings", "handle_mosdns_log_settings"},
	{"api-settings", "handle_mosdns_api_settings"},
	{"cache-settings", "handle_mosdns_cache_settings"},
}

func pointer[T any](value T) *T { return &value }
func or[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
func array[T any](value []T) []T {
	if value == nil {
		return []T{}
	}
	return value
}

type rulesetDTO struct {
	DirectSets []string `json:"direct_rulesets"`
	ProxySets  []string `json:"proxy_rulesets"`
	Direct     []string `json:"direct_rules"`
	Proxy      []string `json:"proxy_rules"`
}
type matchesDTO struct {
	Matches  []model.MosDNSMatch `json:"custom_matches"`
	Position string              `json:"position"`
}
type serversDTO struct {
	Local    string `json:"local_dns"`
	Remote   string `json:"remote_dns"`
	Fallback string `json:"fallback_dns"`
	Default  string `json:"default_forward"`
	Hosts    string `json:"custom_hosts"`
}
type logDTO struct {
	Enabled bool   `json:"log_enabled"`
	Level   string `json:"log_level"`
	File    string `json:"log_file"`
}
type apiDTO struct {
	Enabled bool   `json:"api_enabled"`
	Address string `json:"api_address"`
}
type cacheDTO struct {
	Enabled  bool      `json:"cache_enabled"`
	Size     model.Int `json:"cache_size"`
	TTL      model.Int `json:"cache_lazy_ttl"`
	Dump     bool      `json:"cache_dump_enabled"`
	File     string    `json:"cache_dump_file"`
	Interval model.Int `json:"cache_dump_interval"`
}
type successDTO struct {
	Success bool                 `json:"success"`
	Guard   *mosdns.PublishGuard `json:"publish_guard,omitempty"`
}

func sectionResponse(name string, s model.MosDNSSettings) *httpx.Response {
	switch name {
	case "rulesets":
		return httpx.JSON(rulesetDTO{array(s.DirectRulesets), array(s.ProxyRulesets), array(s.DirectRules), array(s.ProxyRules)})
	case "custom-matches":
		return httpx.JSON(matchesDTO{array(s.CustomMatches), or(s.CustomMatchPosition, "tail")})
	case "dns-servers":
		return httpx.JSON(serversDTO{s.LocalDNS, s.RemoteDNS, s.FallbackDNS, or(s.DefaultForward, "forward_remote"), s.CustomHosts})
	case "log-settings":
		return httpx.JSON(logDTO{or(s.LogEnabled, true), or(s.LogLevel, "info"), or(s.LogFile, "")})
	case "api-settings":
		return httpx.JSON(apiDTO{or(s.APIEnabled, true), or(s.APIAddress, or(s.APIAddr, ":8080"))})
	default:
		return httpx.JSON(cacheDTO{or(s.CacheEnabled, true), or(s.CacheSize, 10240), or(s.CacheLazyTTL, 21600), or(s.CacheDumpEnabled, true), or(s.CacheDumpFile, "./cache.dump"), or(s.CacheDumpInterval, 300)})
	}
}
func requestSettings(c *httpx.Ctx, name string) (model.MosDNSSettings, error) {
	var value model.MosDNSSettings
	data, err := c.Data()
	if err != nil {
		return value, err
	}
	if !c.IsJSON() {
		return value, repo.NewValidationError(SettingsObjectMessage)
	}
	if name == "cache-settings" && bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		data = []byte("{}")
	}
	var fields model.Fields
	if err = json.Unmarshal(data, &fields); err != nil {
		return value, repo.NewValidationError(SettingsObjectMessage)
	}
	if name == "" || name == "cache-settings" {
		for _, key := range []string{"cache_enabled", "cache_dump_enabled"} {
			if raw, present := fields.Get(key); present {
				token := bytes.TrimSpace(raw)
				if !bytes.Equal(token, []byte("true")) && !bytes.Equal(token, []byte("false")) {
					return value, repo.NewValidationError(SettingsFieldTypeMessage)
				}
			}
		}
	}
	if name == "custom-matches" {
		if raw, ok := fields.Get("position"); ok {
			if err = fields.Set("custom_match_position", raw); err != nil {
				return value, err
			}
		}
	}
	data, err = fields.MarshalJSON()
	if err != nil {
		return value, err
	}
	// The settings are a plain record (O18 T1c): input is decoded strictly,
	// so a member of the wrong type is refused instead of read as absent.
	var profile model.Profile
	if err = model.DecodeInputMember("profile", &profile, "mosdns", data); err != nil {
		return value, repo.NewValidationError(SettingsFieldTypeMessage)
	}
	return profile.MosDNS, nil
}
func (h *Handler) handleSection(c *httpx.Ctx, path string) (*httpx.Response, error) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		r, err := h.repository(c.Context())
		if err != nil {
			return nil, err
		}
		id, err := repo.ResolveProfileID(c.Context(), "")
		if err != nil {
			return nil, err
		}
		profile, err := r.Profile(c.Context(), id)
		if err != nil {
			return nil, err
		}
		if c.Request.Method == http.MethodHead {
			return nil, nil
		}
		return sectionResponse(path, profile.MosDNS), nil
	}
	value, err := requestSettings(c, path)
	if err != nil {
		return nil, err
	}
	guard, err := h.save(c.Context(), path, value)
	if err != nil {
		return nil, err
	}
	result := successDTO{Success: true}
	if path == "cache-settings" {
		result.Guard = &guard
	}
	return httpx.JSON(result), nil
}
func (h *Handler) saveSettings(c *httpx.Ctx) (*httpx.Response, error) {
	value, err := requestSettings(c, "")
	if err != nil {
		return nil, err
	}
	guard, err := h.save(c.Context(), "", value)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(successDTO{Success: true, Guard: &guard}), nil
}

// save is the sole write owner. Request cancellation is detached; Runtime
// shutdown remains visible before the transaction and its final assignment.
func (h *Handler) save(ctx context.Context, section string, in model.MosDNSSettings) (mosdns.PublishGuard, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return mosdns.PublishGuard{}, err
	}
	r, err := h.repository(ctx)
	if err != nil {
		return mosdns.PublishGuard{}, err
	}
	id, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return mosdns.PublishGuard{}, err
	}
	if err = ctx.Err(); err != nil {
		return mosdns.PublishGuard{}, err
	}
	revision, err := r.UpdateProfile(ctx, id, func(ctx context.Context, p *model.Profile) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		s := p.MosDNS
		if section == "" {
			s.CustomConfig = in.CustomConfig
		}
		if section == "" || section == "rulesets" {
			s.DirectRulesets = array(in.DirectRulesets)
			s.ProxyRulesets = array(in.ProxyRulesets)
			s.DirectRules = array(in.DirectRules)
			s.ProxyRules = array(in.ProxyRules)
		}
		if section == "" || section == "custom-matches" {
			s.CustomMatches = array(in.CustomMatches)
			s.CustomMatchPosition = pointer(or(in.CustomMatchPosition, "tail"))
		}
		if section == "" || section == "dns-servers" {
			s.LocalDNS = in.LocalDNS
			s.RemoteDNS = in.RemoteDNS
			s.FallbackDNS = in.FallbackDNS
			s.DefaultForward = pointer(or(in.DefaultForward, "forward_remote"))
			s.CustomHosts = in.CustomHosts
		}
		if section == "" || section == "log-settings" {
			s.LogEnabled = pointer(or(in.LogEnabled, true))
			s.LogLevel = pointer(or(in.LogLevel, "info"))
			s.LogFile = pointer(or(in.LogFile, ""))
		}
		if section == "" || section == "api-settings" {
			s.APIEnabled = pointer(or(in.APIEnabled, true))
			s.APIAddress = pointer(or(in.APIAddress, ":8080"))
			if err := s.Clear("api_addr"); err != nil {
				return err
			}
		}
		if section == "" || section == "cache-settings" {
			s.CacheEnabled = pointer(or(in.CacheEnabled, true))
			s.CacheDumpEnabled = pointer(or(in.CacheDumpEnabled, true))
			s.CacheSize = pointer(or(in.CacheSize, 10240))
			s.CacheLazyTTL = pointer(or(in.CacheLazyTTL, 21600))
			interval := or(in.CacheDumpInterval, 300)
			if interval <= 0 {
				interval = 300
			}
			s.CacheDumpInterval = pointer(interval)
			s.CacheDumpFile = pointer(or(in.CacheDumpFile, "./cache.dump"))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		p.MosDNS = s
		return nil
	})
	if err != nil {
		return mosdns.PublishGuard{}, err
	}
	repo.ResetConfigContext(ctx)
	return mosdns.PublishGuard{ProfileID: id, ServiceType: "mosdns", ConfigRevision: revision}, nil
}

func (h *Handler) ruleProxy(c *httpx.Ctx) (*httpx.Response, error) {
	header, _ := c.Header("Authorization")
	result := mosdns.RuleProxy(c.Context(), mosdns.RuleProxyRequest{
		Auth: mosdns.AuthRequest{Internal: c.IsInternalCall(), Authorization: header, Token: c.ArgDefault("token", "")},
		URL:  c.ArgDefault("url", ""), Part: c.ArgDefault("part", ""), SourceID: c.ArgDefault("source_id", ""),
		HostURL: c.HostURL(),
	})
	response := httpx.Text(result.Status, result.Text)
	if result.JSON != nil {
		response = httpx.JSONStatus(result.Status, result.JSON)
	}
	if result.NoStore {
		response.NoStore()
	}
	if result.Stale {
		response.WithHeader("X-ConfigFlow-Rule-Cache", "stale")
	}
	return response, nil
}
