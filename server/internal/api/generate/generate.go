// Package generate exposes manual configuration downloads and previews.
// Converter and archive work belongs to configgen; frozen artifact publication
// is wired to the Agent release owner separately.
package generate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/configgen"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// Register installs five generators and previews with their profile aliases.
func Register(r *httpx.Router) {
	registerArtifacts(r)
	for _, prefix := range []struct{ path, blueprint string }{{"/api/generate", "generate"}, {"/api/profiles/<profile_id>/generate", "profiles"}} {
		for _, engine := range []configgen.Engine{configgen.Mihomo, configgen.Surge, configgen.Loon, configgen.MosDNS, configgen.OxiDNS} {
			for _, preview := range []bool{false, true} {
				path := prefix.path + "/" + string(engine)
				name := prefix.blueprint + "."
				if prefix.blueprint == "profiles" {
					name += "profile_"
				}
				if preview {
					path += "/preview"
					name += "preview_"
				} else {
					name += "generate_"
				}
				name += string(engine)
				r.POST(path, name, auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) { return generate(c, engine, preview) }))
			}
		}
	}
}
func generate(c *httpx.Ctx, engine configgen.Engine, preview bool) (*httpx.Response, error) {
	var raw json.RawMessage
	if engine == configgen.OxiDNS {
		if !c.BindSilent(&raw) {
			raw = json.RawMessage(`{}`)
		}
	} else {
		if err := c.Bind(&raw); err != nil {
			return nil, err
		}
	}
	base, err := configgen.RequestBaseURL(raw)
	if err != nil {
		if errors.Is(err, configgen.ErrBaseURL) {
			return httpx.Message(400, false, err.Error()), nil
		}
		return httpx.Message(500, false, err.Error()), nil
	}

	if !preview {
		if engine == configgen.MosDNS {
			base = strings.TrimSpace(base)
			if base == "" {
				base = forwardedBase(c)
			}
		}
		if engine == configgen.OxiDNS {
			if base == "" {
				base = c.URLRoot()
			}
			base = strings.TrimRight(base, "/")
		}
		file, err := configgen.Build(c.Context(), c.ProfileID, engine, base)
		if err != nil {
			return failure(engine, err, preview)
		}
		return httpx.File(httpx.FileOptions{Path: file.Path, Data: file.Data, DownloadName: file.Name, AsAttachment: true, MimeType: file.MIME})
	}
	repository, err := repo.GetRepository(c.Context())
	if err != nil {
		return nil, err
	}
	profileID, err := repo.ResolveProfileID(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	config, err := repository.Config(c.Context(), profileID, nil)
	if err != nil {
		return nil, err
	}
	out, err := configgen.RenderConfig(c.Context(), config, engine, base)
	if err != nil {
		return failure(engine, err, preview)
	}
	if engine == configgen.OxiDNS {
		node, err := yamlx.Parse([]byte(out.Content))
		if err != nil {
			return nil, err
		}
		if err = yamlx.ProjectJSONKeys(node, yamlx.StringifyScalarKeys); err != nil {
			return nil, err
		}
		raw, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		rawSys, err := model.Marshal(config.SystemConfig)
		if err != nil {
			return nil, err
		}
		secrets, err := redact.ExternalPayloadSecrets(c.Context(), json.RawMessage(rawSys))
		if err != nil {
			return nil, err
		}
		sanitizedJSON := redact.SanitizeCredentials(json.RawMessage(raw), secrets)
		sanitized, err := yamlx.FromJSON(sanitizedJSON)
		if err != nil {
			return nil, err
		}
		text, err := yamlx.Marshal(sanitized)
		if err != nil {
			return nil, err
		}
		out.Content = string(text)
	}
	if engine == configgen.Mihomo {
		hosts := out.SubscriptionHosts
		if len(hosts) == 0 {
			hosts = json.RawMessage("null")
		}
		dialers := out.SubscriptionDialers
		if len(dialers) == 0 {
			dialers = json.RawMessage("null")
		}
		return httpx.JSON(struct {
			Content string          `json:"content"`
			Hosts   json.RawMessage `json:"subscription_hosts"`
			Dialers json.RawMessage `json:"subscription_dialers"`
		}{out.Content, hosts, dialers}), nil
	}
	return httpx.JSON(struct {
		Content string `json:"content"`
	}{out.Content}), nil
}
func forwardedBase(c *httpx.Ctx) string {
	return c.HeaderDefault("X-Forwarded-Proto", c.Scheme()) + "://" + c.HeaderDefault("X-Forwarded-Host", c.Host())
}
func failure(engine configgen.Engine, err error, preview bool) (*httpx.Response, error) {
	var reference *rules.StrategyReferenceError
	if errors.As(err, &reference) {
		return httpx.Message(400, false, err.Error()), nil
	}
	if engine == configgen.OxiDNS {
		if message, ok := oxidns.IsSettingsError(err); ok {
			return httpx.Message(400, false, message), nil
		}
	}
	if engine == configgen.Mihomo || engine == configgen.Surge || engine == configgen.Loon || engine == configgen.MosDNS && preview {
		return httpx.Message(500, false, err.Error()), nil
	}
	// DNS archive diagnostics retain the shared safe formatter.
	var archive *mosdns.ArchiveError
	if errors.As(err, &archive) {
		return httpx.Message(500, false, archive.Message), nil
	}
	var custom *convert.CustomConfigYAMLError
	if errors.As(err, &custom) {
		return httpx.Message(500, false, custom.Error()), nil
	}
	detail := weburl.SafeErrorDetails(err)
	if engine == configgen.MosDNS {
		detail = fmt.Sprintf(msgMosDNSFailure, detail)
	}
	if engine == configgen.OxiDNS && !preview {
		detail = fmt.Sprintf(msgOxiDNSFailure, detail)
	}
	return httpx.Message(500, false, detail), nil
}
