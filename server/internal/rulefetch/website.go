package rulefetch

// Port of backend/utils/website_identity.py: offline service ownership and
// the page metadata an Agent returns.

import (
	"html"
	"regexp"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

// PageMarkers is PAGE_MARKERS (Go RE2 expressions the Agent runs; each
// capture at most 200 bytes), in Python's key order.
var PageMarkers = []struct{ Key, Pattern string }{
	{"title", `(?is)<title\b[^>]*>(.*?)</title>`},
	{"site", `(?is)<meta\b[^>]*(?:property|name)\s*=\s*["']og:site_name["'][^>]*>`},
	{"description", `(?is)<meta\b[^>]*(?:name|property)\s*=\s*["'](?:description|og:description)["'][^>]*>`},
	{"challenge", `(?i)cf-chl-|challenge-platform|just a moment|checking your browser|verify you are human|人机验证|安全验证`},
}

// WebsiteService is the part of region_check.SERVICES known_service reads.
// TODO(port): replace with C16 (backend/utils/region_check.py SERVICES).
type WebsiteService struct {
	ID, Name string
	Domains  []string
}

// Services is region_check.SERVICES (id, name, domains) in order.
var Services = []WebsiteService{
	{"openai", "ChatGPT", []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "sora.com"}},
	{"claude", "Claude", []string{"claude.ai", "claude.com", "anthropic.com"}},
	{"netflix", "Netflix", []string{"netflix.com", "netflix.net", "nflxext.com", "nflximg.com", "nflximg.net", "nflxso.net", "nflxvideo.net"}},
	{"youtube", "YouTube Premium", []string{"youtube.com", "googlevideo.com", "ytimg.com", "youtu.be", "youtube-nocookie.com"}},
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

// SiteTitle is site_title: drop page names and "Powered by" signatures.
func SiteTitle(title string) string {
	cleaned, previous, first := title, "", true
	for first || cleaned != previous {
		first = false
		previous = cleaned
		cleaned, _ = titleTail.SubTemplate(cleaned, "", 0)
		cleaned, _ = titleHead.SubTemplate(cleaned, "", 0)
	}
	if stripped := pyStrip(cleaned); stripped != "" {
		return stripped
	}
	return title
}

// CleanText is clean_text(value, limit): tags removed, entities decoded,
// whitespace collapsed, stripped, cut to limit characters.
func CleanText(value string, limit int) string {
	text := html.UnescapeString(tagPattern.ReplaceAllString(value, ""))
	var b strings.Builder
	space := false
	for _, r := range text {
		if rules.PyIsSpace(r) {
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

var (
	metaTag = regexp.MustCompile(`(?i)<([a-zA-Z][^\t\n\r\f />\x00]*)`)
	attr    = regexp.MustCompile(`((?:^|[\s/'"])[^\s/>][^\s/=>]*)(\s*=+\s*('[^']*'|"[^"]*"|[^>\s'"][^>\s]*|))?`)
)

// metaContent is _meta_content: the content attribute of the last <meta>
// start tag (html.parser.HTMLParser), cleaned. The start-tag scan follows
// HTMLParser's tolerant attribute grammar for the single tags the Agent
// returns.
func metaContent(value string) string {
	content := ""
	for _, loc := range metaTag.FindAllStringSubmatchIndex(value, -1) {
		if !strings.EqualFold(value[loc[2]:loc[3]], "meta") {
			continue
		}
		rest := value[loc[1]:]
		if end := strings.IndexByte(rest, '>'); end >= 0 {
			rest = rest[:end]
		}
		attrs := map[string]*string{}
		for _, m := range attr.FindAllStringSubmatch(" "+rest, -1) {
			name := strings.ToLower(strings.TrimLeft(m[1], " \t\n\r\f/'\""))
			if m[2] == "" {
				attrs[name] = nil
				continue
			}
			v := m[3]
			if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'' || v[0] == '"' && v[len(v)-1] == '"') {
				v = v[1 : len(v)-1]
			}
			if v != "" {
				v = html.UnescapeString(v)
			}
			attrs[name] = &v
		}
		content = ""
		if v := attrs["content"]; v != nil {
			content = *v
		}
	}
	return CleanText(content, 200)
}

// KnownService is known_service(host): the built-in service owning host at
// a domain boundary, or nil.
func KnownService(host string) *jsonx.Object {
	for _, service := range Services {
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
		if parsed, err := SplitURL(finalURL, ""); err == nil && parsed.Hostname() != "" {
			finalHost = parsed.Hostname()
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
