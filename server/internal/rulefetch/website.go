package rulefetch

// Port of backend/utils/website_identity.py: offline service ownership and
// the page metadata an Agent returns.

import (
	"regexp"
	"strings"

	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/rules"
	"configflow/internal/weburl"
)

// PageMarkers is PAGE_MARKERS (Go RE2 expressions the Agent runs; each
// capture at most 200 bytes), in Python's key order.
var PageMarkers = []struct{ Key, Pattern string }{
	{"title", `(?is)<title\b[^>]*>(.*?)</title>`},
	{"site", `(?is)<meta\b[^>]*(?:property|name)\s*=\s*["']og:site_name["'][^>]*>`},
	{"description", `(?is)<meta\b[^>]*(?:name|property)\s*=\s*["'](?:description|og:description)["'][^>]*>`},
	{"challenge", `(?i)cf-chl-|challenge-platform|just a moment|checking your browser|verify you are human|人机验证|安全验证`},
}

const (
	titleSeparator = `(?:\s*::\s*|\s+[|\-–—·]\s+)`
	titleNoise     = `(?:用户登录|会员登录|登录|登入|注册|首页|主页|log\s*in|sign\s*in|home|powered\s+by\s+[^|:]+)`
)

var (
	titleTail  = mustPyRegex(`(?i)`+titleSeparator+titleNoise+`\s*$`, false)
	titleHead  = mustPyRegex(`(?i)^`+titleNoise+titleSeparator, false)
	errorTitle = mustPyRegex(`(?i)^(?:access denied|forbidden|not found|error|404|403|502|503)\b`, false)
	tagPattern = regexp.MustCompile(`<[^>]*>`)
)

func dropMatch(*rules.Match) (string, error) { return "", nil }

// SiteTitle is site_title: drop page names and "Powered by" signatures.
func SiteTitle(title string) string {
	cleaned, previous, first := title, "", true
	for first || cleaned != previous {
		first = false
		previous = cleaned
		cleaned, _ = titleTail.Sub(cleaned, dropMatch, 0)
		cleaned, _ = titleHead.Sub(cleaned, dropMatch, 0)
	}
	if stripped := pyStrip(cleaned); stripped != "" {
		return stripped
	}
	return title
}

// CleanText is clean_text(value, limit): tags removed, entities decoded,
// whitespace collapsed, stripped, cut to limit characters.
func CleanText(value string, limit int) string {
	text := pyUnescape(tagPattern.ReplaceAllString(value, ""))
	var b strings.Builder
	space := false
	for _, r := range text {
		if pystr.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	text = pyStrip(b.String())
	if runes := []rune(text); len(runes) > limit {
		text = string(runes[:limit])
	}
	return text
}

// KnownService is known_service(host): the built-in service owning host at
// a domain boundary, or nil.
func KnownService(host string) *jsonx.Object {
	for _, service := range check.Services { // region_check.SERVICES
		for _, suffix := range service.Domains {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				name := service.Name
				if service.ID == "youtube" {
					name = "YouTube"
				}
				category := "视频服务"
				if service.ID == "openai" || service.ID == "claude" {
					category = "AI 服务"
				}
				return jsonx.Obj("name", name, "source", "catalog", "category", category, "evidence", "内置服务域名："+suffix)
			}
		}
	}
	return nil
}

// PageIdentity is page_identity(result, host, checked_at): page content is
// self-reported, never verified ownership. result nil is None.
func PageIdentity(result *jsonx.Object, host string, checkedAt any) *jsonx.Object {
	if result == nil {
		result = jsonx.NewObject()
	}
	markers := result.GetObject("markers")
	status := result.Value("status")
	identity := jsonx.Obj("source", "page", "host", host, "checked_at", checkedAt,
		"name", "", "description", "", "reason", "", "status", status)
	if failure := result.Value("error"); jsonx.Truthy(failure) {
		identity.Set("reason", "访问失败："+jsonx.Str(failure))
		return identity
	}
	code := 0.0
	if n, ok := status.(jsonx.Number); ok {
		code = n.Float64()
	} else if b, ok := status.(bool); ok && b {
		code = 1
	}
	if !(code >= 200 && code < 300) {
		shown := any("未知")
		if jsonx.Truthy(status) {
			shown = status
		}
		identity.Set("reason", "页面未成功返回（HTTP "+jsonx.Str(shown)+"）")
		return identity
	}
	if jsonx.Truthy(markers.Value("challenge")) {
		identity.Set("reason", "页面要求人机验证，无法识别")
		return identity
	}
	title := CleanText(stringValue(markers.Value("title")), 200)
	if errorTitle.Search(title) {
		identity.Set("reason", "返回错误页，无法识别")
		return identity
	}
	name := metaContent(stringValue(markers.Value("site")))
	if name == "" {
		name = SiteTitle(title)
	}
	identity.Set("name", name)
	identity.Set("description", metaContent(stringValue(markers.Value("description"))))
	finalHost := host
	if finalURL, ok := result.Value("final_url").(string); ok {
		if parsed, err := weburl.Parse(finalURL); err == nil && weburl.Hostname(parsed) != "" {
			finalHost = weburl.Hostname(parsed)
		}
	}
	identity.Set("final_host", finalHost)
	if name == "" {
		identity.Set("reason", "未找到网站名称，可能是 API 或资源域名")
	}
	return identity
}

func stringValue(v any) string {
	text, _ := v.(string)
	return text
}
