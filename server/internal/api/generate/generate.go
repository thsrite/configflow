// Package generate exposes manual configuration downloads and previews.
// Converter and archive work belongs to configgen; frozen artifact publication
// is wired to the Agent release owner separately.
package generate

import (
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/configgen"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/weburl"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"fmt"
)

type request struct{ BaseURL string }

func (r *request) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	r.BaseURL = ""
	if data := fields["base_url"]; len(data) > 0 {
		return json.Unmarshal(data, &r.BaseURL)
	}
	return nil
}

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
	var body request
	if engine == configgen.OxiDNS {
		c.BindSilent(&body)
	} else {
		if err := c.Bind(&body); err != nil {
			return nil, err
		}
	}
	base := body.BaseURL
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
			return failure(engine, err)
		}
		return httpx.File(httpx.FileOptions{Path: file.Path, Data: file.Data, DownloadName: file.Name, AsAttachment: true, MimeType: file.MIME})
	}
	config, err := repo.GetConfig(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	out, err := configgen.Render(c.Context(), config, engine, base)
	if err != nil {
		return failure(engine, err)
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
		value, err := jsonx.Parse(raw)
		if err != nil {
			return nil, err
		}
		value, err = redact.SanitizeExternalPayload(c.Context(), value, config.GetObject("system_config"))
		if err != nil {
			return nil, err
		}
		sanitized, err := yamlbridge.Encode(value)
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
		return httpx.JSON(struct {
			Content string        `json:"content"`
			Hosts   *jsonx.Object `json:"subscription_hosts"`
			Dialers *jsonx.Object `json:"subscription_dialers"`
		}{out.Content, out.SubscriptionHosts, out.SubscriptionDialers}), nil
	}
	return httpx.JSON(struct {
		Content string `json:"content"`
	}{out.Content}), nil
}
func forwardedBase(c *httpx.Ctx) string {
	return c.HeaderDefault("X-Forwarded-Proto", c.Scheme()) + "://" + c.HeaderDefault("X-Forwarded-Host", c.Host())
}
func failure(engine configgen.Engine, err error) (*httpx.Response, error) {
	var reference *rules.StrategyReferenceError
	if errors.As(err, &reference) {
		return httpx.Message(400, false, err.Error()), nil
	}
	if engine == configgen.OxiDNS {
		if message, ok := oxidns.IsSettingsError(err); ok {
			return httpx.Message(400, false, message), nil
		}
	}
	// Converter/archive diagnostics can embed source URLs; use the shared safe
	// formatter rather than emitting raw request or subscription credentials.
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
	if engine == configgen.OxiDNS {
		detail = fmt.Sprintf(msgOxiDNSFailure, detail)
	}
	return httpx.Message(500, false, detail), nil
}
