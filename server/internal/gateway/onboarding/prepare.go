package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/gateway"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// CoreValidation keeps native Mihomo validation deferred until publication.
type CoreValidation struct {
	Valid             bool `json:"valid"`
	CoreCheckRequired bool `json:"core_check_required"`
}

// Validation contains both independently rendered services' diagnostics.
type Validation struct {
	Mihomo *CoreValidation    `json:"mihomo,omitempty"`
	OxiDNS *oxidns.Validation `json:"oxidns,omitempty"`
}

// PublishGuard pins a subsequent publication to the exact committed revision.
type PublishGuard struct {
	ProfileID   string `json:"expected_profile_id"`
	ServiceType string `json:"expected_service_type"`
	Revision    int64  `json:"expected_config_revision"`
}

// Report is the reviewable result of preparing both drafts without side effects.
type Report struct {
	Success                     bool                    `json:"success"`
	Ready                       bool                    `json:"ready"`
	Blockers                    []string                `json:"blockers"`
	Warnings                    []string                `json:"warnings"`
	Changes                     []string                `json:"changes"`
	Validation                  Validation              `json:"validation"`
	TemplateConversionAvailable bool                    `json:"template_conversion_available"`
	ProxyPrerequisites          *Prerequisites          `json:"proxy_prerequisites,omitempty"`
	ProfileID                   string                  `json:"profile_id,omitempty"`
	Revision                    *int64                  `json:"config_revision,omitempty"`
	Draft                       bool                    `json:"draft,omitempty"`
	PublishGuards               map[string]PublishGuard `json:"publish_guards,omitempty"`
	Message                     string                  `json:"message,omitempty"`
}

// Options supplies pure preparation dependencies and explicit takeover mode.
type Options struct {
	BaseURL        string
	Takeover       string
	DNSLoop        DNSLoopChecker
	LocalAddresses []netip.Addr
}

func oxidnsValues(config Document, baseURL string) (*yamlx.Node, *yamlx.Node, error) {
	p, err := oxidns.ProfileFromJSON(encode(config))
	if err != nil {
		return nil, nil, err
	}
	settings, err := oxidns.NormalizeSettings(p.Settings(), false)
	if err != nil {
		return nil, nil, err
	}
	if text(field(settings, "config_mode")) == "native" {
		view, err := oxidns.ReadCommonSettings(settings, p, baseURL)
		return settings, view.Values, err
	}
	return settings, settings, nil
}
func prepareMihomoDNS(config Document, options Options) error {
	mihomo := object(config["mihomo"])
	if mihomo == nil {
		mihomo = Document{}
	}
	raw, exists := mihomo["custom_config"]
	var source string
	if exists && json.Unmarshal(raw, &source) != nil || exists && string(raw) == "null" {
		return invalid(msgBaseText)
	}
	var document *yamlx.Node
	var err error
	if strings.TrimSpace(source) != "" {
		document, err = yamlx.Parse([]byte(source))
		if err != nil {
			return err
		}
	}
	check := options.DNSLoop
	if check == nil {
		check = CheckDNSLoop
	}
	document, err = PrepareMihomoDNS(document, options.LocalAddresses, check)
	if err != nil {
		return err
	}
	data, err := yamlx.Marshal(document)
	if err != nil {
		return err
	}
	mihomo["custom_config"] = encode(string(data))
	config["mihomo"] = encode(mihomo)
	return nil
}
func loopbackListen(listen string) bool {
	host := strings.TrimSpace(listen)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end >= 0 {
			host = host[1:end]
		}
	} else if strings.Count(host, ":") == 1 {
		host = strings.SplitN(host, ":", 2)[0]
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
func enableAPI(api *yamlx.Node, warnings *[]string) *yamlx.Node {
	api = editMapping(api)
	if api == nil || api.Kind != yaml.MappingNode {
		api = yamlx.Map()
	}
	if boolean(field(api, "enabled")) {
		return api
	}
	yamlx.Set(api, "enabled", yamlx.Bool(true))
	username := text(field(api, "username"))
	password := text(field(api, "password"))
	if (username != "") != (password != "") {
		yamlx.Set(api, "username", yamlx.Str(""))
		yamlx.Set(api, "password", yamlx.Str(""))
		username = ""
		*warnings = append(*warnings, msgHalfAccount)
	}
	ssl := field(api, "ssl")
	if (text(field(ssl, "cert")) != "") != (text(field(ssl, "key")) != "") {
		yamlx.Set(api, "ssl", yamlx.Map())
		*warnings = append(*warnings, msgHalfTLS)
	}
	listen := text(field(api, "listen"))
	if strings.TrimSpace(listen) == "" {
		yamlx.Set(api, "listen", yamlx.Str("127.0.0.1:9199"))
	} else if username == "" && !loopbackListen(listen) {
		yamlx.Set(api, "listen", yamlx.Str("127.0.0.1:9199"))
		*warnings = append(*warnings, fmt.Sprintf(fmtAPIListen, listen))
	}
	return api
}

// Prepare renders and validates a candidate without mutating config or writing
// files. Its candidate is private; only Report may be projected to a client.
func Prepare(ctx context.Context, config Document, form Request, options Options) (Document, Report, error) {
	candidate := clone(config)
	report := Report{Success: true, Blockers: []string{}, Warnings: []string{}, Changes: slices.Clone(gatewayChanges)}
	if options.Takeover == "full_tun" {
		report.Changes = slices.Clone(routerChanges)
	} else if options.Takeover == "legacy" {
		report.Warnings = append(report.Warnings, msgLegacy)
	}
	err := prepare(ctx, config, candidate, form, options, &report)
	if err != nil {
		if ctx.Err() != nil {
			return nil, report, ctx.Err()
		}
		// User-supplied YAML/settings errors become actionable blocked previews.
		// Infrastructure failures retain their error chain and abort the request.
		message, ok := validationMessage(err)
		if !ok {
			return nil, report, err
		}
		report.Blockers = append(report.Blockers, message)
	}
	return candidate, report, nil
}
func prepare(ctx context.Context, config, candidate Document, form Request, options Options, report *Report) error {
	if raw, present := config["mihomo"]; present && object(raw) == nil {
		return errors.New("onboarding: stored Mihomo settings must be an object")
	}

	settings, current, err := oxidnsValues(config, options.BaseURL)
	if err != nil {
		if _, ok := oxidns.IsSettingsError(err); !ok {
			return err
		}
		raw, decodeErr := yamlx.FromJSON(config["oxidns"])
		if decodeErr != nil {
			return decodeErr
		}
		if text(field(raw, "config_mode")) != "native" {
			return err
		}
		report.TemplateConversionAvailable = true
		if !form.ConfirmTemplateConversion {
			return invalid(msgReadNative + err.Error() + msgConfirmRead)
		}
		replacement := editMapping(raw)
		yamlx.Set(replacement, "config_mode", yamlx.Str("template"))
		yamlx.Set(replacement, "native_config", yamlx.Str(""))
		settings, err = oxidns.NormalizeSettings(replacement, true)
		if err != nil {
			return err
		}
		yamlx.Set(settings, "gateway_onboarding_native_backup", raw)
		current = settings
		report.Warnings = append(report.Warnings, msgConvertedRead)
	}
	previous := append(stringList(field(current, "direct_rulesets")), stringList(field(current, "proxy_rulesets"))...)
	choices, err := domainCandidates(config, previous)
	if err != nil {
		return err
	}
	byID := map[string]DomainCandidate{}
	for _, choice := range choices {
		byID[choice.ID] = choice
	}
	for _, id := range append(slices.Clone(form.DirectRuleSetIDs), form.ProxyRuleSetIDs...) {
		item, found := byID[id]
		if !found {
			return invalid(msgUnavailableChoice + msgMissingChoice)
		}
		if !item.Available && !slices.Contains(previous, id) {
			return invalid(msgUnavailableChoice + item.Reason)
		}
		if !item.Available {
			report.Warnings = append(report.Warnings, item.Name+"："+item.Reason)
		}
	}
	if options.Takeover == "full_tun" {
		err = fullTun(config, candidate, form.RuleSetIDs)
	} else {
		gw, e := gatewayConfig(candidate)
		if e != nil {
			return e
		}
		settings, e := gw.ValidateSelection(encode(gateway.Settings{Enabled: true, RuleSetIDs: form.RuleSetIDs}))
		if e != nil {
			return e
		}
		mihomo := object(candidate["mihomo"])
		if mihomo == nil {
			mihomo = Document{}
		}
		mihomo["linux_gateway"] = encode(settings)
		candidate["mihomo"] = encode(mihomo)
	}
	if err != nil {
		return err
	}
	if err = prepareMihomoDNS(candidate, options); err != nil {
		return err
	}
	changes, err := yamlx.FromJSON(encode(struct {
		Listen   string   `json:"listen"`
		TCP      bool     `json:"tcp_enabled"`
		IPv4     bool     `json:"prefer_ipv4"`
		Local    string   `json:"local_dns"`
		Remote   string   `json:"remote_dns"`
		Fallback string   `json:"fallback_dns"`
		Default  string   `json:"default_forward"`
		Direct   []string `json:"direct_rulesets"`
		Proxy    []string `json:"proxy_rulesets"`
	}{"0.0.0.0:53", true, true, form.LocalDNS, DNSTarget, DNSTarget, form.DefaultForward, form.DirectRuleSetIDs, form.ProxyRuleSetIDs}))
	if err != nil {
		return err
	}
	if text(field(settings, "config_mode")) == "native" {
		p, err := oxidns.ProfileFromJSON(encode(config))
		if err != nil {
			return err
		}
		view, err := oxidns.ReadCommonSettings(settings, p, options.BaseURL)
		if err != nil {
			return err
		}
		nativeChanges := editMapping(changes)
		for _, key := range []string{"local_dns", "remote_dns", "fallback_dns"} {
			upstreams, err := oxidns.ForwardUpstreams(text(field(changes, key)), nil)
			if err != nil {
				return err
			}
			data, err := yamlx.Marshal(upstreams)
			if err != nil {
				return err
			}
			yamlx.Set(nativeChanges, key, yamlx.Str(string(data)))
		}
		if !boolean(field(current, "api", "enabled")) && view.Capabilities["api.enabled"].Editable {
			yamlx.Set(nativeChanges, "api.enabled", yamlx.Bool(true))
		}
		if form.QueryRecorder != nil && boolean(field(current, "query_recorder", "enabled")) != *form.QueryRecorder {
			if view.Capabilities["query_recorder.enabled"].Editable {
				yamlx.Set(nativeChanges, "query_recorder.enabled", yamlx.Bool(*form.QueryRecorder))
			} else {
				report.Warnings = append(report.Warnings, msgQueryRecorder)
			}
		}
		update := func() error {
			if err := CheckNativeDNSPath(settings, true); err != nil {
				return err
			}
			restored, err := PrepareNativeFallback(settings)
			if err != nil {
				return err
			}
			p, err := oxidns.ProfileFromJSON(encode(candidate))
			if err != nil {
				return err
			}
			applied, err := oxidns.ApplyCommonSettings(restored, nativeChanges, p, options.BaseURL)
			if err != nil {
				return err
			}
			settings = applied.Settings
			return nil
		}
		if err = update(); err != nil {
			if _, ok := oxidns.IsSettingsError(err); !ok {
				return err
			}
			if !form.ConfirmTemplateConversion {
				report.TemplateConversionAvailable = true
				return invalid(msgUpdateNative + err.Error() + msgConfirmUpdate)
			}
			backup := settings
			settings = editMapping(settings)
			yamlx.Set(settings, "gateway_onboarding_native_backup", backup)
			yamlx.Set(settings, "config_mode", yamlx.Str("template"))
			yamlx.Set(settings, "native_config", yamlx.Str(""))
			for key, value := range yamlx.Pairs(changes) {
				yamlx.Set(settings, key, value)
			}
			report.Warnings = append(report.Warnings, msgConvertedUpdate)
		}
	} else {
		for key, value := range yamlx.Pairs(changes) {
			yamlx.Set(settings, key, value)
		}
	}
	if text(field(settings, "config_mode")) == "template" {
		yamlx.Set(settings, "api", enableAPI(field(settings, "api"), &report.Warnings))
		if form.QueryRecorder != nil {
			recorder := editMapping(field(settings, "query_recorder"))
			if recorder == nil {
				recorder = yamlx.Map()
			}
			yamlx.Set(recorder, "enabled", yamlx.Bool(*form.QueryRecorder))
			yamlx.Set(settings, "query_recorder", recorder)
		}
	}
	normalized, err := oxidns.NormalizeSettings(settings, true)
	if err != nil {
		return err
	}
	data, err := yamlx.ToJSON(normalized)
	if err != nil {
		return err
	}
	candidate["oxidns"] = data
	mihomo, err := generate(ctx, candidate, options.BaseURL)
	if err != nil {
		return err
	}
	p, err := oxidns.ProfileFromJSON(encode(candidate))
	if err != nil {
		return err
	}
	if _, err = oxidns.Generate(p, options.BaseURL); err != nil {
		return err
	}
	validation, err := oxidns.ValidateSettings(normalized, p, options.BaseURL)
	if err != nil {
		return err
	}
	report.Validation = Validation{Mihomo: &CoreValidation{true, true}, OxiDNS: &validation}
	if !validation.Valid {
		return invalid(msgValidation)
	}
	prerequisites, err := proxyPrerequisites(candidate, mihomo)
	if err != nil {
		return err
	}
	report.ProxyPrerequisites = &prerequisites
	report.Warnings = append(report.Warnings, prerequisites.Warnings...)
	if !prerequisites.Ready {
		return invalid(msgNoProxy)
	}
	selected := append(slices.Clone(form.RuleSetIDs), form.ProxyRuleSetIDs...)
	profile, _, err := readConfig(candidate)
	if err != nil {
		return err
	}
	takeover := msgIPCapture
	if options.Takeover == "full_tun" {
		takeover = msgRouterForward
	}
	for _, rule := range profile.RuleConfigs {
		if slices.Contains(selected, rule.ID) && !slices.Contains(prerequisites.Policies, policy(rule)) {
			name := rule.Name
			if _, present := object(encode(rule))["name"]; !present {
				name = rule.ID
			}
			p := policy(rule)
			if rule.Policy == nil {
				p = msgPolicyMissing
			}
			report.Warnings = append(report.Warnings, fmt.Sprintf(fmtPolicy, name, p, takeover))
		}
	}
	overlaps, err := overlapWarnings(ctx, candidate)
	if err != nil {
		return err
	}
	report.Warnings = append(report.Warnings, overlaps...)
	report.Ready = true
	return nil
}
