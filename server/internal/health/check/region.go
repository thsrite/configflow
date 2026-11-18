package check

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonstore"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/weburl"
)

const (
	// RegionRetentionDays is the retention window in days for stored region checks.
	RegionRetentionDays = 30
	// MaxGroupTargets is the maximum number of strategy groups probed per check.
	MaxGroupTargets = 4
	// MaxRegionTargets is the maximum number of per-region nodes probed per check.
	MaxRegionTargets = 8
	// MaxDomainsPerCheck is the maximum number of custom domains per region check.
	MaxDomainsPerCheck = 10
	// CheckTimeoutMS is the per-request timeout in milliseconds for region checks.
	CheckTimeoutMS = 8000
)

// PageRequest describes a single HTTP page fetch and regex marker map for the Agent.
type PageRequest struct {
	ID      string            `json:"id"`
	URL     string            `json:"url"`
	Headers model.Map[string] `json:"headers,omitzero"`
	Markers model.Map[string] `json:"markers,omitzero"`
}

func stringMap(pairs ...string) model.Map[string] {
	var m model.Map[string]
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i], pairs[i+1])
	}
	return m
}

// DefaultExitRequest returns a fresh copy of the Cloudflare trace exit request.
func DefaultExitRequest() PageRequest {
	return PageRequest{
		ID:      "exit",
		URL:     "https://www.cloudflare.com/cdn-cgi/trace",
		Markers: stringMap("loc", `loc=([A-Z]{2})`, "ip", `ip=([0-9a-fA-F:.]+)`),
	}
}

// GroupTypes lists Mihomo strategy-group adapter types.
var GroupTypes = []string{"Selector", "URLTest", "Fallback", "LoadBalance", "Relay", "Smart"}

// NonNodeTypes lists Mihomo adapter types that are not individual proxy nodes.
var NonNodeTypes = append(append([]string{}, GroupTypes...), "Direct", "Reject", "RejectDrop", "Pass", "PassRule", "Compatible", "Dns")

// ExcludedTargets lists strategy-group names excluded from automatic region checks.
var ExcludedTargets = []string{"GLOBAL", "ConfigFlow-Region-Probe"}

// RegionRule maps a two-letter ISO region code and Chinese name to its name regex.
type RegionRule struct{ Code, Name, Pattern string }

// RegionRules lists supported region recognition patterns in priority order.
var RegionRules = []RegionRule{
	{"HK", regionNameHK, `港|(?<![A-Za-z])HK(?![A-Za-z])|Hong\s?Kong`},
	{"TW", regionNameTW, `台湾|台北|(?<![A-Za-z])TW(?![A-Za-z])|Taiwan`},
	{"JP", regionNameJP, `日本|东京|大阪|(?<![A-Za-z])JP(?![A-Za-z])|Japan|Tokyo`},
	{"SG", regionNameSG, `新加坡|狮城|(?<![A-Za-z])SG(?![A-Za-z])|Singapore`},
	{"US", regionNameUS, `美国|洛杉矶|硅谷|(?<![A-Za-z])US(?![A-Za-z])|United\s?States|America`},
	{"KR", regionNameKR, `韩国|首尔|(?<![A-Za-z])KR(?![A-Za-z])|Korea`},
	{"GB", regionNameGB, `英国|伦敦|(?<![A-Za-z])(UK|GB)(?![A-Za-z])|Britain|London`},
	{"DE", regionNameDE, `德国|法兰克福|(?<![A-Za-z])DE(?![A-Za-z])|Germany`},
	{"CA", regionNameCA, `加拿大|(?<![A-Za-z])CA(?![A-Za-z])|Canada`},
	{"AU", regionNameAU, `澳大利亚|澳洲|悉尼|(?<![A-Za-z])AU(?![A-Za-z])|Australia`},
	{"FR", regionNameFR, `法国|巴黎|(?<![A-Za-z])FR(?![A-Za-z])|France`},
	{"NL", regionNameNL, `荷兰|阿姆斯特丹|(?<![A-Za-z])NL(?![A-Za-z])|Netherlands`},
	{"IN", regionNameIN, `印度|(?<![A-Za-z])IN(?![A-Za-z])|India`},
	{"MY", regionNameMY, `马来|(?<![A-Za-z])MY(?![A-Za-z])|Malaysia`},
	{"TH", regionNameTH, `泰国|(?<![A-Za-z])TH(?![A-Za-z])|Thailand`},
	{"VN", regionNameVN, `越南|(?<![A-Za-z])VN(?![A-Za-z])|Vietnam`},
	{"PH", regionNamePH, `菲律宾|(?<![A-Za-z])PH(?![A-Za-z])|Philippines`},
	{"TR", regionNameTR, `土耳其|(?<![A-Za-z])TR(?![A-Za-z])|Turkey|Türkiye`},
	{"AR", regionNameAR, `阿根廷|(?<![A-Za-z])AR(?![A-Za-z])|Argentina`},
	{"BR", regionNameBR, `巴西|(?<![A-Za-z])BR(?![A-Za-z])|Brazil`},
}

func mustCompile(pattern string, ignoreCase bool) *rules.PyRegex {
	re, err := rules.CompilePyRegex(pattern, ignoreCase)
	if err != nil {
		panic(err)
	}
	return re
}

var (
	compileOnce    sync.Once
	regionPatterns []*rules.PyRegex
	netflixRegion  *rules.PyRegex
	regionURLHint  *rules.PyRegex
)

func compiled() {
	compileOnce.Do(func() {
		for _, rule := range RegionRules {
			regionPatterns = append(regionPatterns, mustCompile(rule.Pattern, true))
		}
		netflixRegion = mustCompile(`^/([a-z]{2})(?:-[a-z]{2})?/title/`, false)
		regionURLHint = mustCompile(`unavailable|region|country|geo-?block`, true)
	})
}

// RegionOfName extracts the two-letter region code from a node or group name,
// returning "" when no region pattern matches.
func RegionOfName(name string) string {
	compiled()
	if name == "" {
		return ""
	}
	for i, pattern := range regionPatterns {
		if pattern.Search(name) {
			return RegionRules[i].Code
		}
	}
	return ""
}

// RegionLabel returns the Chinese display label for code, code itself when
// unrecognized, or "未知" when code is empty.
func RegionLabel(code string) string {
	if code == "" {
		return labelUnknownRegion
	}
	for _, rule := range RegionRules {
		if rule.Code == code {
			return rule.Name
		}
	}
	return code
}

// RegionResult holds the Agent's response for one PageRequest.
type RegionResult struct {
	ID        string                  `json:"id"`
	Status    *model.Int              `json:"status"`
	FinalURL  string                  `json:"final_url"`
	Redirects []string                `json:"redirects"`
	Markers   model.Map[NullableText] `json:"markers"`
	Error     string                  `json:"error"`
	hasID     bool
	empty     bool
}

func isNullMarkersMap(m model.Map[NullableText]) bool {
	encoded, err := m.MarshalJSON()
	return err == nil && bytes.Equal(bytes.TrimSpace(encoded), []byte("null"))
}

// UnmarshalJSON decodes a RegionResult, enforcing that a present id is a valid
// textual or numeric identifier and applying R5 fallbacks to auxiliary fields.
func (r *RegionResult) UnmarshalJSON(data []byte) error {
	var rawObj rawJSONObject
	if err := json.Unmarshal(data, &rawObj); err != nil {
		return err
	}
	out := RegionResult{empty: rawObj.Fields.Len() == 0}
	if rawID, ok := rawObj.Fields.Get("id"); ok {
		out.hasID = true
		id, err := decodeNonNullText(bytes.TrimSpace(rawID))
		if err != nil {
			return valueError(fmt.Sprintf(msgRegionResultIDInvalid, compactRaw(rawID)))
		}
		out.ID = id
	}
	if rawStatus, ok := rawObj.Fields.Get("status"); ok && !bytes.Equal(bytes.TrimSpace(rawStatus), []byte("null")) {
		var code model.Int
		if err := json.Unmarshal(rawStatus, &code); err == nil {
			out.Status = &code
		}
	}
	if rawURL, ok := rawObj.Fields.Get("final_url"); ok {
		var finalURL Text
		if err := json.Unmarshal(rawURL, &finalURL); err == nil {
			out.FinalURL = string(finalURL)
		}
	}
	if rawRedirects, ok := rawObj.Fields.Get("redirects"); ok {
		var redirects []Text
		if err := json.Unmarshal(rawRedirects, &redirects); err == nil && redirects != nil {
			out.Redirects = make([]string, len(redirects))
			for i, item := range redirects {
				out.Redirects[i] = string(item)
			}
		}
	}
	if rawMarkers, ok := rawObj.Fields.Get("markers"); ok {
		var markers model.Map[NullableText]
		if err := json.Unmarshal(rawMarkers, &markers); err == nil && !isNullMarkersMap(markers) {
			out.Markers = markers
		}
	}
	if rawErr, ok := rawObj.Fields.Get("error"); ok {
		var errText StrictString
		if err := json.Unmarshal(rawErr, &errText); err == nil {
			out.Error = string(errText)
		}
	}
	*r = out
	return nil
}

// ServiceVerdict is the unlock verdict for one streaming/AI service.
type ServiceVerdict struct {
	Status   string       `json:"status"`
	Region   NullableText `json:"region"`
	Reason   string       `json:"reason"`
	Evidence []string     `json:"evidence"`
}

// MarshalJSON encodes the response DTO when used as an ordered map value.
func (v ServiceVerdict) MarshalJSON() ([]byte, error) {
	type wire ServiceVerdict
	return jsonresp.Encode(wire(v))
}

func makeVerdict(status string, region NullableText, reason string, evidence []string) ServiceVerdict {
	if evidence == nil {
		evidence = []string{}
	}
	return ServiceVerdict{
		Status:   status,
		Region:   region,
		Reason:   reason,
		Evidence: evidence,
	}
}

func formatStatus(code *model.Int) string {
	if code == nil {
		return "None"
	}
	return strconv.FormatInt(int64(*code), 10)
}

func formatEvidence(entry *RegionResult) string {
	if entry == nil || entry.empty {
		return msgNoResult
	}
	if entry.Error != "" {
		return entry.ID + "：" + entry.Error
	}
	text := entry.ID + "：HTTP " + formatStatus(entry.Status)
	if entry.FinalURL != "" && len(entry.Redirects) > 0 {
		text += " → " + entry.FinalURL
	}
	if entry.Markers.Len() > 0 {
		keys := entry.Markers.Keys()
		slices.Sort(keys)
		text += "，命中 " + strings.Join(keys, "、")
	}
	return text
}

func markerOf(entry *RegionResult, name string) NullableText {
	if entry == nil {
		return NullableText{}
	}
	value, ok := entry.Markers.Get(name)
	if !ok {
		return NullableText{}
	}
	return value
}

func markerTruthy(entry *RegionResult, name string) bool {
	m := markerOf(entry, name)
	return m.Valid && m.Value != ""
}

func trustedStatus(entry *RegionResult) (int64, bool) {
	if entry == nil || entry.empty || entry.Error != "" || entry.Status == nil {
		return 0, false
	}
	return int64(*entry.Status), true
}

func hasTrustedStatus(entry *RegionResult, want int64) bool {
	got, ok := trustedStatus(entry)
	return ok && got == want
}

func lookupResult(results map[string]RegionResult, id string) *RegionResult {
	entry, ok := results[id]
	if !ok {
		return nil
	}
	return &entry
}

// ---------- services ----------

func evaluateOpenAI(results map[string]RegionResult) (ServiceVerdict, error) {
	compliance := lookupResult(results, "openai-compliance")
	ios := lookupResult(results, "openai-ios")
	trace := lookupResult(results, "openai-trace")
	ev := []string{formatEvidence(compliance), formatEvidence(ios)}
	region := markerOf(trace, "loc")
	if markerTruthy(compliance, "unsupported") || markerTruthy(ios, "unsupported") {
		return makeVerdict("blocked", region, msgOpenAIUnsupported, ev), nil
	}
	if markerTruthy(ios, "vpn") {
		return makeVerdict("blocked", region, msgOpenAIVPN, ev), nil
	}
	if hasTrustedStatus(compliance, 200) && markerOf(compliance, "consent").Valid {
		reason := ""
		if markerTruthy(ios, "dc") {
			reason = msgOpenAIDatacenter
		}
		return makeVerdict("available", region, reason, ev), nil
	}
	return makeVerdict("unknown", region, msgResponseUnknown, ev), nil
}

func evaluateClaude(results map[string]RegionResult) (ServiceVerdict, error) {
	web := lookupResult(results, "claude-web")
	trace := lookupResult(results, "claude-trace")
	ev := []string{formatEvidence(web)}
	region := markerOf(trace, "loc")
	if web == nil || web.empty || web.Error != "" {
		return makeVerdict("unknown", region, msgRequestFailed, ev), nil
	}
	if strings.Contains(web.FinalURL, "app-unavailable-in-region") {
		return makeVerdict("blocked", region, msgClaudeBlocked, ev), nil
	}
	for _, redirect := range web.Redirects {
		if strings.Contains(redirect, "app-unavailable-in-region") {
			return makeVerdict("blocked", region, msgClaudeBlocked, ev), nil
		}
	}
	if hasTrustedStatus(web, 200) {
		return makeVerdict("available", region, "", ev), nil
	}
	if hasTrustedStatus(web, 403) {
		return makeVerdict("unknown", region, msgClaudeChallenge, ev), nil
	}
	return makeVerdict("unknown", region, msgResponseUnknown, ev), nil
}

func splitPath(rawURL string) (string, error) {
	parts, err := weburl.Parse(rawURL)
	if err != nil {
		return "", valueError(err.Error())
	}
	return weburl.RawPath(parts), nil
}

func netflixRegionOf(entry *RegionResult) (NullableText, error) {
	compiled()
	if entry == nil || entry.empty || entry.FinalURL == "" {
		return NullableText{}, nil
	}
	path, err := splitPath(entry.FinalURL)
	if err != nil {
		return NullableText{}, err
	}
	if match, err := netflixRegion.Match(path); err == nil && match != nil {
		code, _ := match.Group(1)
		return NullableText{Value: strings.ToUpper(code), Valid: true}, nil
	}
	if strings.HasPrefix(path, "/title/") && hasTrustedStatus(entry, 200) {
		return NullableText{Value: "US", Valid: true}, nil
	}
	return NullableText{}, nil
}

func evaluateNetflix(results map[string]RegionResult) (ServiceVerdict, error) {
	licensed := []*RegionResult{
		lookupResult(results, "netflix-licensed-1"),
		lookupResult(results, "netflix-licensed-2"),
	}
	original := lookupResult(results, "netflix-original")
	all := []*RegionResult{licensed[0], licensed[1], original}
	ev := make([]string, len(all))
	for i, entry := range all {
		ev[i] = formatEvidence(entry)
	}
	var region NullableText
	for _, entry := range all {
		detected, err := netflixRegionOf(entry)
		if err != nil {
			return ServiceVerdict{}, err
		}
		if detected.Valid && detected.Value != "" {
			region = detected
			break
		}
	}
	var statuses []int64
	for _, entry := range all {
		if code, ok := trustedStatus(entry); ok {
			statuses = append(statuses, code)
		}
	}
	for _, entry := range licensed {
		if hasTrustedStatus(entry, 200) {
			return makeVerdict("available", region, msgNetflixFull, ev), nil
		}
	}
	if hasTrustedStatus(original, 200) {
		return makeVerdict("partial", region, msgNetflixOriginal, ev), nil
	}
	every := func(want int64) bool {
		for _, code := range statuses {
			if code != want {
				return false
			}
		}
		return true
	}
	if len(statuses) > 0 && every(403) {
		return makeVerdict("blocked", region, msgNetflix403, ev), nil
	}
	if len(statuses) > 0 && every(404) {
		return makeVerdict("blocked", region, msgNetflix404, ev), nil
	}
	return makeVerdict("unknown", region, msgResponseUnknown, ev), nil
}

func evaluateYouTube(results map[string]RegionResult) (ServiceVerdict, error) {
	page := lookupResult(results, "youtube-premium")
	ev := []string{formatEvidence(page)}
	region := markerOf(page, "gl")
	if markerTruthy(page, "blocked") {
		return makeVerdict("blocked", region, msgYouTubeBlocked, ev), nil
	}
	if region.Valid && region.Value == "CN" {
		return makeVerdict("blocked", region, msgYouTubeCN, ev), nil
	}
	if region.Valid && region.Value != "" {
		return makeVerdict("available", region, "", ev), nil
	}
	return makeVerdict("unknown", NullableText{}, msgYouTubeNoRegion, ev), nil
}

// Service defines a built-in streaming/AI service checked during region probes.
type Service struct {
	ID, Name string
	Domains  []string
	requests func() []PageRequest
	evaluate func(map[string]RegionResult) (ServiceVerdict, error)
}

// Requests returns fresh PageRequest definitions for this service.
func (s Service) Requests() []PageRequest { return s.requests() }

// Evaluate computes the ServiceVerdict from request results keyed by request ID.
func (s Service) Evaluate(results map[string]RegionResult) (ServiceVerdict, error) {
	return s.evaluate(results)
}

// Services lists all built-in region-check services in display order.
var Services = []Service{
	{
		ID: "openai", Name: "ChatGPT",
		Domains: []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "sora.com"},
		requests: func() []PageRequest {
			return []PageRequest{
				{
					ID:      "openai-compliance",
					URL:     "https://api.openai.com/compliance/cookie_requirements",
					Headers: stringMap("Authorization", "Bearer null"),
					Markers: stringMap("unsupported", "unsupported_country", "consent", "cookie_consent_required"),
				},
				{
					ID:      "openai-ios",
					URL:     "https://ios.chat.openai.com/",
					Markers: stringMap("unsupported", "unsupported_country", "vpn", "VPN", "dc", `"type"\s*:\s*"dc"`),
				},
				{
					ID:      "openai-trace",
					URL:     "https://chatgpt.com/cdn-cgi/trace",
					Markers: stringMap("loc", `loc=([A-Z]{2})`),
				},
			}
		},
		evaluate: evaluateOpenAI,
	},
	{
		ID: "claude", Name: "Claude",
		Domains: []string{"claude.ai", "claude.com", "anthropic.com"},
		requests: func() []PageRequest {
			return []PageRequest{
				{ID: "claude-web", URL: "https://claude.ai/"},
				{
					ID:      "claude-trace",
					URL:     "https://claude.ai/cdn-cgi/trace",
					Markers: stringMap("loc", `loc=([A-Z]{2})`),
				},
			}
		},
		evaluate: evaluateClaude,
	},
	{
		ID: "netflix", Name: "Netflix",
		Domains: []string{
			"netflix.com", "netflix.net", "nflxext.com", "nflximg.com",
			"nflximg.net", "nflxso.net", "nflxvideo.net",
		},
		requests: func() []PageRequest {
			return []PageRequest{
				{ID: "netflix-licensed-1", URL: "https://www.netflix.com/title/81280792"},
				{ID: "netflix-licensed-2", URL: "https://www.netflix.com/title/70143836"},
				{ID: "netflix-original", URL: "https://www.netflix.com/title/80018499"},
			}
		},
		evaluate: evaluateNetflix,
	},
	{
		ID: "youtube", Name: "YouTube Premium",
		Domains: []string{"youtube.com", "googlevideo.com", "ytimg.com", "youtu.be", "youtube-nocookie.com"},
		requests: func() []PageRequest {
			return []PageRequest{
				{
					ID:      "youtube-premium",
					URL:     "https://www.youtube.com/premium",
					Headers: stringMap("Accept-Language", "en"),
					Markers: stringMap(
						"blocked", "Premium is not available in your country",
						"gl", `"INNERTUBE_CONTEXT_GL"\s*:\s*"([A-Z]{2})"`,
					),
				},
			}
		},
		evaluate: evaluateYouTube,
	},
}

// ServiceByID looks up a built-in Service by ID.
func ServiceByID(id string) (Service, bool) {
	for _, service := range Services {
		if service.ID == id {
			return service, true
		}
	}
	return Service{}, false
}

// BuildServiceRequests returns DefaultExitRequest followed by every Service's requests.
func BuildServiceRequests() []PageRequest {
	out := []PageRequest{DefaultExitRequest()}
	for _, service := range Services {
		out = append(out, service.Requests()...)
	}
	return out
}

func indexResultsByID(results []RegionResult) (map[string]RegionResult, error) {
	index := make(map[string]RegionResult, len(results))
	for _, entry := range results {
		if !entry.hasID && entry.ID == "" {
			return nil, valueError(msgRegionResultIDMissing)
		}
		index[entry.ID] = entry
	}
	return index, nil
}

// EvaluateServiceList evaluates every built-in Service against results.
func EvaluateServiceList(results []RegionResult) (model.Map[ServiceVerdict], error) {
	var out model.Map[ServiceVerdict]
	index, err := indexResultsByID(results)
	if err != nil {
		return out, err
	}
	for _, service := range Services {
		verdict, err := service.Evaluate(index)
		if err != nil {
			return out, err
		}
		out.Set(service.ID, verdict)
	}
	return out, nil
}

// ExitSummary holds exit IP location and error details extracted from trace results.
type ExitSummary struct {
	Loc   NullableText `json:"loc"`
	IP    NullableText `json:"ip"`
	Error NullableText `json:"error"`
}

// SummarizeExit extracts ExitSummary from the first result with ID "exit".
func SummarizeExit(results []RegionResult) (ExitSummary, error) {
	var exit *RegionResult
	for i := range results {
		if !results[i].hasID && results[i].ID == "" {
			return ExitSummary{}, valueError(msgRegionResultIDMissing)
		}
		if exit == nil && results[i].ID == "exit" {
			exit = &results[i]
		}
	}
	var errVal NullableText
	if exit != nil && exit.Error != "" {
		errVal = NullableText{Value: exit.Error, Valid: true}
	}
	return ExitSummary{
		Loc:   markerOf(exit, "loc"),
		IP:    markerOf(exit, "ip"),
		Error: errVal,
	}, nil
}

// PublicService is the public API representation of a built-in Service.
type PublicService struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

// PublicServiceList returns public metadata for all built-in Services.
func PublicServiceList() []PublicService {
	out := make([]PublicService, len(Services))
	for i, service := range Services {
		out[i] = PublicService{
			ID:      service.ID,
			Name:    service.Name,
			Domains: slices.Clone(service.Domains),
		}
	}
	return out
}

// ---------- arbitrary domains ----------

// BlockPattern is the page body regex matched by the Agent for geo-blocked domains.
const BlockPattern = `(?i)(not|isn't|is not) available in your (country|region|location)` +
	`|unavailable in your (country|region|location)` +
	`|not available in (this|the) (country|region)` +
	`|地区不可用|所在的?地区|所在国家`

// BuildDomainRequest builds a PageRequest for probing an arbitrary domain URL.
func BuildDomainRequest(requestID, url string) PageRequest {
	return PageRequest{
		ID:      requestID,
		URL:     url,
		Markers: stringMap("block", BlockPattern),
	}
}

// DomainClassification describes the region-restriction classification for one target.
type DomainClassification struct {
	State    string       `json:"state"`
	Detail   string       `json:"detail"`
	Status   *model.Int   `json:"status"`
	FinalURL NullableText `json:"final_url"`
}

// ClassifyDomain classifies a single target's RegionResult for originalURL into
// ok, restricted, denied, or error.
func ClassifyDomain(result *RegionResult, originalURL string) (DomainClassification, error) {
	compiled()
	if result == nil || result.empty || result.Error != "" {
		detail := msgNoResult
		if result != nil && !result.empty {
			detail = result.Error
		}
		return DomainClassification{
			State:  "error",
			Detail: detail,
		}, nil
	}
	detail := "HTTP " + formatStatus(result.Status)
	moved := false
	if result.FinalURL != "" {
		finalPath, err := splitPath(result.FinalURL)
		if err != nil {
			return DomainClassification{}, err
		}
		if regionURLHint.Search(finalPath) {
			origPath, err := splitPath(originalURL)
			if err != nil {
				return DomainClassification{}, err
			}
			moved = !regionURLHint.Search(origPath)
		}
	}
	hasCode := func(want int64) bool {
		return result.Status != nil && int64(*result.Status) == want
	}
	block := markerOf(result, "block")
	var state string
	switch {
	case hasCode(451):
		state, detail = "restricted", msgDomain451
	case block.Valid && block.Value != "" && (hasCode(200) || hasCode(403)):
		state, detail = "restricted", fmt.Sprintf(msgDomainBlockMarker, formatStatus(result.Status), block.Value)
	case moved:
		state, detail = "restricted", fmt.Sprintf(msgDomainMoved, result.FinalURL)
	case hasCode(403):
		state = "denied"
	case result.Status != nil && *result.Status != 0 && *result.Status < 500:
		state = "ok"
	default:
		state = "error"
	}
	return DomainClassification{
		State:    state,
		Detail:   detail,
		Status:   result.Status,
		FinalURL: NullableText{Value: result.FinalURL, Valid: true},
	}, nil
}

// TargetState holds the classified state of a domain on one target.
type TargetState struct {
	State  Text `json:"state"`
	Fields model.Fields
	raw    model.Raw
}

// UnmarshalJSON decodes TargetState under model R5 rules.
func (s *TargetState) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, s, &s.raw)
}

// MarshalJSON encodes TargetState preserving stored member order.
func (s TargetState) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&s, &s.raw)
}

// DomainSummary summarizes domain availability differences across targets.
type DomainSummary struct {
	Verdict    string   `json:"verdict"`
	Restricted []string `json:"restricted"`
	Usable     []string `json:"usable"`
}

// SummarizeTargets aggregates per-target classifications into a DomainSummary.
func SummarizeTargets(perTarget model.Map[TargetState]) DomainSummary {
	restricted := []string{}
	usable := []string{}
	total := 0
	for name, entry := range perTarget.All() {
		total++
		if entry.State == "restricted" {
			restricted = append(restricted, name)
		}
		if entry.State == "ok" {
			usable = append(usable, name)
		}
	}
	verdict := "unreachable"
	switch {
	case len(restricted) > 0 && len(usable) > 0:
		verdict = "suspected"
	case len(restricted) > 0 && len(restricted) == total:
		verdict = "all_restricted"
	case len(usable) > 0 && len(restricted) == 0:
		verdict = "no_difference"
	}
	return DomainSummary{
		Verdict:    verdict,
		Restricted: restricted,
		Usable:     usable,
	}
}

// ---------- target choice ----------

// AvailableTarget describes an available Mihomo adapter reported by an Agent.
type AvailableTarget struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Alive bool   `json:"alive"`
}

// UnmarshalJSON decodes AvailableTarget, requiring a non-null string or numeric
// name and applying R5 defaults for type ("") and alive (true).
func (t *AvailableTarget) UnmarshalJSON(data []byte) error {
	var rawObj rawJSONObject
	if err := json.Unmarshal(data, &rawObj); err != nil {
		return err
	}
	rawName, ok := rawObj.Fields.Get("name")
	if !ok {
		return valueError(msgTargetNameMissing)
	}
	name, err := decodeNonNullText(bytes.TrimSpace(rawName))
	if err != nil {
		return valueError(fmt.Sprintf(msgTargetNameInvalid, compactRaw(rawName)))
	}
	out := AvailableTarget{Name: name, Alive: true}
	if rawType, ok := rawObj.Fields.Get("type"); ok {
		var kind StrictString
		if err := json.Unmarshal(rawType, &kind); err == nil {
			out.Type = string(kind)
		}
	}
	if rawAlive, ok := rawObj.Fields.Get("alive"); ok {
		trimmed := bytes.TrimSpace(rawAlive)
		if bytes.Equal(trimmed, []byte("true")) {
			out.Alive = true
		} else if bytes.Equal(trimmed, []byte("false")) {
			out.Alive = false
		}
	}
	*t = out
	return nil
}

// RegionTarget is a selected target path for a region check.
type RegionTarget struct {
	Name       string       `json:"name"`
	Kind       string       `json:"kind"`
	RegionHint NullableText `json:"region_hint"`
}

func nullableRegion(code string) NullableText {
	if code == "" {
		return NullableText{}
	}
	return NullableText{Value: code, Valid: true}
}

// ChooseTargets selects DIRECT, up to MaxGroupTargets strategy groups (with
// preferredGroup first when non-empty), and one live node per region (up to
// MaxRegionTargets).
func ChooseTargets(targets []AvailableTarget, preferredGroup string) []RegionTarget {
	compiled()
	picked := []RegionTarget{}
	hasDirect := false
	for _, target := range targets {
		if target.Name == "DIRECT" {
			hasDirect = true
			break
		}
	}
	if hasDirect {
		picked = append(picked, RegionTarget{Name: "DIRECT", Kind: "direct"})
	}
	var groups []AvailableTarget
	for _, target := range targets {
		if slices.Contains(GroupTypes, target.Type) && !slices.Contains(ExcludedTargets, target.Name) {
			groups = append(groups, target)
		}
	}
	if preferredGroup != "" {
		sort.SliceStable(groups, func(i, j int) bool {
			return groups[i].Name == preferredGroup && groups[j].Name != preferredGroup
		})
	}
	if len(groups) > MaxGroupTargets {
		groups = groups[:MaxGroupTargets]
	}
	for _, target := range groups {
		picked = append(picked, RegionTarget{
			Name:       target.Name,
			Kind:       "group",
			RegionHint: nullableRegion(RegionOfName(target.Name)),
		})
	}
	byRegion := map[string]AvailableTarget{}
	for _, target := range targets {
		if slices.Contains(NonNodeTypes, target.Type) || !target.Alive {
			continue
		}
		if code := RegionOfName(target.Name); code != "" {
			if _, seen := byRegion[code]; !seen {
				byRegion[code] = target
			}
		}
	}
	count := 0
	for _, rule := range RegionRules {
		target, ok := byRegion[rule.Code]
		if !ok || count >= MaxRegionTargets {
			continue
		}
		count++
		picked = append(picked, RegionTarget{
			Name:       target.Name,
			Kind:       "node",
			RegionHint: NullableText{Value: rule.Code, Valid: true},
		})
	}
	return picked
}

// ---------- store ----------

// RegionData holds an Agent's stored service matrix, domain checks, and website identities.
type RegionData struct {
	Services json.RawMessage        `json:"services"`
	Domains  model.Map[TimedRecord] `json:"domains"`
	Websites model.Map[TimedRecord] `json:"websites"`
	Fields   model.Fields
	raw      model.Raw
}

// UnmarshalJSON decodes RegionData preserving extra members and order.
func (d *RegionData) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, d, &d.raw)
}

// MarshalJSON encodes RegionData preserving stored member order and raw bytes.
func (d RegionData) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&d, &d.raw)
}

func newEmptyRegionData() RegionData {
	var data RegionData
	_ = model.SetMemberJSON(&data, &data.raw, "services", json.RawMessage("null"))
	_ = model.SetMemberJSON(&data, &data.raw, "domains", json.RawMessage("{}"))
	_ = model.SetMemberJSON(&data, &data.raw, "websites", json.RawMessage("{}"))
	return data
}

var regionLock sync.Mutex

// RegionStore persists per-Agent region-check matrices and domain/website results.
type RegionStore struct{ files *jsonstore.Store }

// NewRegionStore creates a RegionStore rooted at dir.
func NewRegionStore(dir string) *RegionStore { return &RegionStore{files: jsonstore.New(dir)} }

// Path returns the JSON file path for agentID.
func (s *RegionStore) Path(agentID string) string { return s.files.Path(agentID) }

func (s *RegionStore) readData(agentID string) RegionData {
	var rawObj rawJSONObject
	if !s.files.Read(agentID, &rawObj) {
		return newEmptyRegionData()
	}
	if !rawObj.Fields.Has("services") {
		_ = rawObj.Fields.Set("services", json.RawMessage("null"))
	}
	hasDomains := rawObj.Fields.Has("domains")
	hasWebsites := rawObj.Fields.Has("websites")
	encoded, err := rawObj.MarshalJSON()
	if err != nil {
		return newEmptyRegionData()
	}
	var data RegionData
	if err := json.Unmarshal(encoded, &data); err != nil {
		return newEmptyRegionData()
	}
	if !hasDomains || hasRootIssue(data.raw.Issues(), "domains") || isNullTimedMap(data.Domains) {
		_ = model.SetMemberJSON(&data, &data.raw, "domains", json.RawMessage("{}"))
	}
	if !hasWebsites || hasRootIssue(data.raw.Issues(), "websites") || isNullTimedMap(data.Websites) {
		_ = model.SetMemberJSON(&data, &data.raw, "websites", json.RawMessage("{}"))
	}
	return data
}

// NaiveISOSeconds formats t's wall-clock fields as YYYY-MM-DDTHH:MM:SS.
func NaiveISOSeconds(t time.Time) string { return t.Format("2006-01-02T15:04:05") }

// oldest computes (now - days) in wall-clock time without DST shifts.
func oldest(now time.Time, days int) string {
	wall := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.UTC)
	return NaiveISOSeconds(wall.Add(-time.Duration(days) * 24 * time.Hour))
}

func hasEncodedMember(data *RegionData, name string) bool {
	encoded, err := data.MarshalJSON()
	if err != nil {
		return false
	}
	var rawObj rawJSONObject
	if err := json.Unmarshal(encoded, &rawObj); err != nil {
		return false
	}
	return rawObj.Fields.Has(name)
}

// UpdateData mutates agentID's stored RegionData, prunes entries older than
// RegionRetentionDays, bounds websites to the newest 1000, and writes atomically.
func (s *RegionStore) UpdateData(agentID string, mutate func(*RegionData) error, now time.Time) error {
	return s.UpdateDataContext(context.Background(), agentID, mutate, now)
}

// UpdateDataContext mutates region data while retaining the caller's owner
// lifetime through lock acquisition and the final file publication check.
func (s *RegionStore) UpdateDataContext(ctx context.Context, agentID string, mutate func(*RegionData) error, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now()
	}
	regionLock.Lock()
	defer regionLock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data := s.readData(agentID)
	if err := mutate(&data); err != nil {
		return err
	}
	encoded, err := model.Marshal(data)
	if err != nil {
		return err
	}
	var current RegionData
	if err := json.Unmarshal(encoded, &current); err != nil {
		return err
	}
	if hasRootIssue(model.Issues(current), "domains") || hasRootIssue(model.Issues(current), "websites") {
		return valueError(msgInvalidRegionStore)
	}

	if !validTimedRecords(data.Domains) {
		return valueError(msgInvalidRegionStore)
	}
	if !validTimedRecords(data.Websites) {
		return valueError(msgInvalidRegionStore)
	}
	for _, records := range []model.Map[TimedRecord]{data.Domains, data.Websites} {
		for _, record := range records.All() {
			if !validTimestamp(record) {
				return valueError(msgInvalidRegionStore)
			}
		}
	}

	bound := oldest(now, RegionRetentionDays)
	var domains model.Map[TimedRecord]
	for host, entry := range data.Domains.All() {
		if string(entry.CheckedAt) >= bound {
			domains.Set(host, entry)
		}
	}
	type websitePair struct {
		host  string
		entry TimedRecord
	}
	var entries []websitePair
	for host, entry := range data.Websites.All() {
		if string(entry.CheckedAt) >= bound {
			entries = append(entries, websitePair{host: host, entry: entry})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return string(entries[i].entry.CheckedAt) < string(entries[j].entry.CheckedAt)
	})
	if len(entries) > 1000 {
		entries = entries[len(entries)-1000:]
	}
	var websites model.Map[TimedRecord]
	for _, pair := range entries {
		websites.Set(pair.host, pair.entry)
	}
	if len(bytes.TrimSpace(data.Services)) == 0 {
		_ = model.SetMemberJSON(&data, &data.raw, "services", json.RawMessage("null"))
	} else if !hasEncodedMember(&data, "services") {
		_ = model.SetMemberJSON(&data, &data.raw, "services", data.Services)
	}
	domainsJSON, err := domains.MarshalJSON()
	if err != nil {
		return fmt.Errorf("encode region domains: %w", err)
	}
	websitesJSON, err := websites.MarshalJSON()
	if err != nil {
		return fmt.Errorf("encode region websites: %w", err)
	}
	if err := model.SetMemberJSON(&data, &data.raw, "domains", domainsJSON); err != nil {
		return fmt.Errorf("set region domains: %w", err)
	}
	if err := model.SetMemberJSON(&data, &data.raw, "websites", websitesJSON); err != nil {
		return fmt.Errorf("set region websites: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = s.files.Write(agentID, &data, false)
	return err
}

// LoadData returns agentID's stored RegionData with normalized defaults.
func (s *RegionStore) LoadData(agentID string) RegionData {
	regionLock.Lock()
	defer regionLock.Unlock()
	return s.readData(agentID)
}

// Delete removes the region store file for agentID.
func (s *RegionStore) Delete(agentID string) error {
	return s.DeleteContext(context.Background(), agentID)
}

// DeleteContext removes the region file only while its owner is active.
func (s *RegionStore) DeleteContext(ctx context.Context, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	regionLock.Lock()
	defer regionLock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.files.Remove(agentID)
}
