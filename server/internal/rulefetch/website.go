package rulefetch

// Port of backend/utils/website_identity.py: offline service ownership and
// the page metadata an Agent returns.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"configflow/internal/health/check"
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
		if ruleSpace(r) {
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

// ServiceIdentity describes an offline catalog match.
type ServiceIdentity struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Category string `json:"category"`
	Evidence string `json:"evidence"`
}

// LookupService identifies a host at a DNS label boundary without network I/O.
func LookupService(host string) *ServiceIdentity {
	for _, service := range check.Services {
		for _, suffix := range service.Domains {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				name := service.Name
				if service.ID == "youtube" {
					name = "YouTube"
				}
				category := msgVideoService
				if service.ID == "openai" || service.ID == "claude" {
					category = msgAIService
				}
				return &ServiceIdentity{Name: name, Source: "catalog", Category: category, Evidence: msgServiceDomain + suffix}
			}
		}
	}
	return nil
}

// PageProbe carries the Agent's website markers. Status remains an opaque
// response member; only a JSON number authorizes the successful-page branch.
type PageProbe struct {
	Status   json.RawMessage            `json:"status"`
	Error    string                     `json:"error"`
	Markers  map[string]json.RawMessage `json:"markers"`
	FinalURL string                     `json:"final_url"`
}

// PageInfo is unverified, self-reported website identity.
type PageInfo struct {
	Source      string          `json:"source"`
	Host        string          `json:"host"`
	CheckedAt   string          `json:"checked_at"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Reason      string          `json:"reason"`
	Status      json.RawMessage `json:"status"`
	FinalHost   *string         `json:"final_host,omitempty"`
}

// IdentifyPage derives display metadata from a typed probe; it does not prove
// ownership or perform requests.
func IdentifyPage(result PageProbe, host, checkedAt string) PageInfo {
	identity := PageInfo{Source: "page", Host: host, CheckedAt: checkedAt, Status: bytes.Clone(result.Status)}
	if result.Error != "" {
		identity.Reason = msgPageAccess + result.Error
		return identity
	}
	var code float64
	if json.Unmarshal(result.Status, &code) != nil || !(code >= 200 && code < 300) {
		shown := msgUnknownStatus
		var text string
		if len(result.Status) > 0 && string(result.Status) != "null" && string(result.Status) != "false" && string(result.Status) != "0" {
			if json.Unmarshal(result.Status, &text) == nil {
				if text != "" {
					shown = text
				}
			} else {
				shown = string(result.Status)
			}
		}
		identity.Reason = fmt.Sprintf(msgPageStatus, shown)
		return identity
	}
	var challenge bool
	var challengeText string
	raw := result.Markers["challenge"]
	_ = json.Unmarshal(raw, &challenge)
	_ = json.Unmarshal(raw, &challengeText)
	if challenge || challengeText != "" {
		identity.Reason = msgPageChallenge
		return identity
	}
	text := func(key string) string {
		var value string
		_ = json.Unmarshal(result.Markers[key], &value)
		return value
	}
	title := CleanText(text("title"), 200)
	if errorTitle.Search(title) {
		identity.Reason = msgPageError
		return identity
	}
	identity.Name = metaContent(text("site"))
	if identity.Name == "" {
		identity.Name = SiteTitle(title)
	}
	identity.Description = metaContent(text("description"))
	finalHost := host
	if parsed, err := weburl.Parse(result.FinalURL); err == nil && weburl.Hostname(parsed) != "" {
		finalHost = weburl.Hostname(parsed)
	}
	identity.FinalHost = &finalHost
	if identity.Name == "" {
		identity.Reason = msgPageNoName
	}
	return identity
}
