package config

import (
	"net/url"
	"strings"
	"unicode"

	"configflow/internal/auth"
	"configflow/internal/configgen"
	"configflow/internal/httpx"
	"configflow/internal/repo"
)

func registerLive(r *httpx.Router) {
	for _, engine := range []configgen.Engine{configgen.Mihomo, configgen.Surge, configgen.Loon, configgen.MosDNS, configgen.OxiDNS} {
		for _, scoped := range []bool{false, true} {
			path := "/api/config/"
			name := "config.get_"
			if scoped {
				path += "<profile_id>/"
				if engine != configgen.OxiDNS {
					name += "profile_"
				}
			}
			path += string(engine)
			name += string(engine) + "_config"
			r.GET(path, name, func(c *httpx.Ctx) (*httpx.Response, error) { return live(c, engine) })
		}
	}
	r.GET("/api/config/<profile_id>/loon/<filename>", "config.get_profile_loon_named_config", func(c *httpx.Ctx) (*httpx.Response, error) { return live(c, configgen.Loon) })
}
func live(c *httpx.Ctx, engine configgen.Engine) (*httpx.Response, error) {
	config, err := repo.GetConfig(c.Context(), c.ProfileID)
	if err != nil {
		return nil, err
	}
	result, err := auth.ValidateTokenOrJWT(c, config)
	if err != nil {
		return nil, err
	}
	if !result.Valid {
		return httpx.Message(401, false, result.Message), nil
	}
	base := c.HeaderDefault("X-Forwarded-Proto", c.Scheme()) + "://" + c.HeaderDefault("X-Forwarded-Host", c.Host())
	if engine == configgen.Mihomo {
		base, _ = c.Arg("base_url")
		if base == "" {
			var body struct {
				BaseURL string `json:"base_url"`
			}
			if c.BindSilent(&body) {
				base = body.BaseURL
			}
		}
	}
	out, err := configgen.Render(c.Context(), config, engine, base)
	if err != nil {
		return nil, err
	}
	disposition := "inline"
	if engine == configgen.Loon {
		name := loonName(config.GetString("name"), config.GetString("profile_id")) + ".lcf"
		disposition = "inline; filename*=UTF-8''" + strings.ReplaceAll(url.PathEscape(name), "+", "%2B")
	}
	return httpx.Text(200, out.Content).WithHeader("Content-Disposition", disposition), nil
}
func loonName(name, fallback string) string {
	var out strings.Builder
	separating := false
	for _, r := range name {
		invalid := r < 32 || strings.ContainsRune(`\/:*?"<>|`, r)
		if invalid {
			if !separating {
				out.WriteByte(' ')
			}
			separating = true
			continue
		}
		separating = false
		out.WriteRune(r)
	}
	name = strings.Trim(strings.TrimFunc(out.String(), unicode.IsSpace), ".")
	if name != "" {
		return name
	}
	if fallback != "" {
		return fallback
	}
	return "loon"
}
