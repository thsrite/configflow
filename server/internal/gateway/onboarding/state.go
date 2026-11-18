package onboarding

import (
	"context"
	"net/url"
	"strings"

	"configflow/internal/gateway"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

// FormState is the saved wizard selection. Omitted query_recorder writes keep it unchanged.
type FormState struct {
	RuleSetIDs       []string `json:"rule_set_ids"`
	DirectRuleSetIDs []string `json:"direct_rule_set_ids"`
	ProxyRuleSetIDs  []string `json:"proxy_rule_set_ids"`
	LocalDNS         string   `json:"local_dns"`
	DefaultForward   *string  `json:"default_forward"`
	QueryRecorder    bool     `json:"query_recorder"`
}

// State is the read-only wizard view.
type State struct {
	ProfileID                   string              `json:"profile_id"`
	Revision                    int64               `json:"config_revision"`
	ServerURL                   string              `json:"server_url"`
	Settings                    FormState           `json:"settings"`
	GatewayEnabled              bool                `json:"gateway_enabled"`
	IPCandidates                []gateway.Candidate `json:"ip_candidates"`
	Takeover                    string              `json:"takeover"`
	DomainCandidates            []DomainCandidate   `json:"domain_candidates"`
	FakeIPRange                 string              `json:"fake_ip_range"`
	OxiDNSMode                  string              `json:"oxidns_mode"`
	Warnings                    []string            `json:"warnings"`
	FieldWarnings               map[string]string   `json:"field_warnings"`
	Blockers                    []string            `json:"blockers"`
	CanPrepare                  bool                `json:"can_prepare"`
	TemplateConversionAvailable bool                `json:"template_conversion_available"`
	ProxyPrerequisites          Prerequisites       `json:"proxy_prerequisites"`
}

// ReadState inspects existing drafts without generation, mutation or network I/O.
func ReadState(config Document, revision int64, options Options) (State, error) {
	out := State{ProfileID: stringValue(config["profile_id"]), Revision: revision, Takeover: options.Takeover, Blockers: []string{}, Warnings: []string{}, FieldWarnings: map[string]string{}}
	if out.Takeover == "" {
		out.Takeover = "selective"
	}
	view := config
	if options.Takeover == "full_tun" {
		view = clone(config)
		mihomo := object(view["mihomo"])
		if mihomo == nil {
			mihomo = Document{}
		}
		ids, err := routeIDs(config)
		if err != nil {
			return out, err
		}
		mihomo["linux_gateway"] = encode(gateway.Settings{Enabled: false, RuleSetIDs: ids})
		view["mihomo"] = encode(mihomo)
	}
	gw, err := gatewayConfig(view)
	if err != nil {
		return out, err
	}
	editor, err := gw.EditorState(false)
	if err != nil {
		return out, err
	}
	out.Warnings = append(out.Warnings, editor.Warnings...)
	out.IPCandidates = editor.Candidates
	out.FakeIPRange = editor.FakeIPRange
	out.GatewayEnabled = editor.Settings.Enabled && options.Takeover != "full_tun"
	settings, values, err := oxidnsValues(config, options.BaseURL)
	if err != nil {
		if _, ok := oxidns.IsSettingsError(err); !ok {
			return out, err
		}
		message := err.Error()
		settings, err = yamlx.FromJSON(config["oxidns"])
		if err != nil {
			return out, err
		}
		values = settings
		out.Blockers = append(out.Blockers, msgReadNative+message)
		out.TemplateConversionAvailable = text(field(settings, "config_mode")) == "native"
	}
	direct := stringList(field(values, "direct_rulesets"))
	proxy := stringList(field(values, "proxy_rulesets"))
	local := text(field(values, "local_dns"))
	if local == "" {
		local = DefaultLocalDNS
	}
	defaultForward := "forward_remote"
	var defaultPointer *string
	if value := field(values, "default_forward"); value == nil || value.ShortTag() != "!!null" {
		if value != nil {
			defaultForward = text(value)
		}
		defaultPointer = &defaultForward
	}
	out.Settings = FormState{editor.Settings.RuleSetIDs, direct, proxy, local, defaultPointer, boolean(field(values, "query_recorder", "enabled"))}
	system := object(config["system_config"])
	out.ServerURL = stringValue(system["server_domain"])
	u, e := url.Parse(out.ServerURL)
	if e == nil {
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || host == "::1" || host == "0.0.0.0" || host == "::" || strings.HasPrefix(host, "127.") {
			out.Warnings = append(out.Warnings, msgServerLoopback)
		}
	}
	if _, err := LocalUpstreams(local); err != nil {
		out.FieldWarnings["local_dns"] = msgLocalWarning
	}
	out.OxiDNSMode = text(field(settings, "config_mode"))
	if out.OxiDNSMode == "" {
		out.OxiDNSMode = "template"
	}
	if out.OxiDNSMode == "native" && len(out.Blockers) == 0 {
		check := func() error {
			if err := CheckNativeDNSPath(settings, true); err != nil {
				return err
			}
			if _, err := PrepareNativeFallback(settings); err != nil {
				return err
			}
			p, err := oxidns.ProfileFromJSON(encode(config))
			if err != nil {
				return err
			}
			common, err := oxidns.ReadCommonSettings(settings, p, options.BaseURL)
			if err != nil {
				return err
			}
			missing := []string{}
			for _, key := range []string{"listen", "tcp_enabled", "prefer_ipv4", "local_dns", "remote_dns", "fallback_dns", "default_forward", "direct_rulesets", "proxy_rulesets"} {
				if !common.Capabilities[key].Editable {
					missing = append(missing, key)
				}
			}
			if len(missing) > 0 {
				return invalid(msgUpdateNative + strings.Join(missing, "、") + msgConfirmState)
			}
			return nil
		}
		if err := check(); err != nil {
			out.Blockers = append(out.Blockers, err.Error())
			out.TemplateConversionAvailable = true
		}
	}
	out.ProxyPrerequisites, err = proxyPrerequisites(config, nil)
	if err != nil {
		return out, err
	}
	if !out.ProxyPrerequisites.Ready {
		out.Warnings = append(out.Warnings, msgPrepareProxy)
	}
	out.DomainCandidates, err = domainCandidates(config, append(append([]string{}, direct...), proxy...))
	if err != nil {
		return out, err
	}
	out.CanPrepare = len(out.Blockers) == 0
	return out, nil
}

// Readiness is the installation precondition of the already saved drafts.
type Readiness struct {
	Ready              bool          `json:"ready"`
	ProxyPrerequisites Prerequisites `json:"proxy_prerequisites"`
	Validation         Validation    `json:"validation"`
}

// AssertReady rejects incomplete saved drafts without repairing or writing them.
// A22 passes the target host's addresses through Options.LocalAddresses.
func AssertReady(ctx context.Context, config Document, options Options) (Readiness, error) {
	out := Readiness{}
	gw, err := gatewayConfig(config)
	if err != nil {
		return out, err
	}
	state, err := gw.EditorState(true)
	if err != nil {
		return out, err
	}
	marker, err := markerState(config)
	if err != nil {
		return out, err
	}
	if marker == "valid" {
		if state.Settings.Enabled {
			return out, invalid(msgReadyGateway)
		}
		document, err := yamlx.Parse([]byte(stringValue(object(config["mihomo"])["custom_config"])))
		if err != nil || !fullTunDocument(document) {
			return out, invalid(msgReadyTUN)
		}
	} else if !state.Settings.Enabled {
		return out, invalid(msgReadySave)
	}
	settings, values, err := oxidnsValues(config, options.BaseURL)
	if err != nil {
		return out, err
	}
	if text(field(values, "listen")) != "0.0.0.0:53" || !boolean(field(values, "tcp_enabled")) || !boolean(field(values, "prefer_ipv4")) {
		return out, invalid(msgReadyListen)
	}
	defaultForward := text(field(values, "default_forward"))
	if defaultForward != "forward_local" && defaultForward != "forward_remote" {
		return out, invalid(msgReadyDefault)
	}
	if _, err = LocalUpstreams(text(field(values, "local_dns"))); err != nil {
		return out, err
	}
	for _, key := range []string{"remote_dns", "fallback_dns"} {
		upstreams, err := oxidns.ForwardUpstreams(text(field(values, key)), nil)
		if err != nil {
			return out, err
		}
		if len(upstreams.Content) == 0 {
			return out, invalid(msgReadyUpstreams)
		}
		for _, item := range upstreams.Content {
			addr := text(field(item, "addr"))
			if addr != DNSTarget && addr != "udp://"+DNSTarget || nonempty(field(item, "socks5")) || nonempty(field(item, "outbound")) {
				return out, invalid(msgReadyUpstreams)
			}
		}
	}
	p, err := oxidns.ProfileFromJSON(encode(config))
	if err != nil {
		return out, err
	}
	if text(field(settings, "config_mode")) == "native" {
		if err = CheckNativeDNSPath(settings, false); err != nil {
			return out, err
		}
		common, err := oxidns.ReadCommonSettings(settings, p, options.BaseURL)
		if err != nil {
			return out, err
		}
		for _, key := range []string{"default_forward", "direct_rulesets", "proxy_rulesets"} {
			if !common.Capabilities[key].Editable {
				return out, invalid(msgReadyRouting)
			}
		}
	}
	validation, err := oxidns.ValidateSettings(settings, p, options.BaseURL)
	if err != nil {
		return out, err
	}
	if !validation.Valid {
		return out, invalid(msgReadyValidation)
	}
	if err = prepareMihomoDNS(clone(config), options); err != nil {
		return out, err
	}
	document, err := generate(ctx, config, options.BaseURL)
	if err != nil {
		return out, err
	}
	if !nonempty(field(document, "dns", "nameserver")) {
		return out, invalid(msgReadyNameserver)
	}
	prerequisites, err := proxyPrerequisites(config, document)
	if err != nil {
		return out, err
	}
	if !prerequisites.Ready {
		return out, invalid(msgReadyProxy)
	}
	out.Ready = true
	out.ProxyPrerequisites = prerequisites
	out.Validation = Validation{OxiDNS: &validation}
	return out, nil
}
