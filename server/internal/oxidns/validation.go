package oxidns

import (
	"embed"
	"encoding/json"
	"fmt"
	"math/big"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"configflow/internal/netipx"
	"configflow/internal/weburl"
)

// Validation follows the pinned OxiDNS 1.6.1 schema embedded in schemas/ (checked against backend/schemas/oxidns).
// It never changes the document; the Agent runs the core's own check before
// activation.

// MaxIssues bounds every issue list.
const MaxIssues = 200

var u64 = func() *big.Int { v, _ := new(big.Int).SetString("18446744073709551615", 10); return v }()

// Schema is the subset of JSON Schema the catalog uses.
type Schema struct {
	Type             typeList           `json:"type"`
	Enum             []json.RawMessage  `json:"enum"`
	Properties       map[string]*Schema `json:"properties"`
	Items            *Schema            `json:"items"`
	Additional       json.RawMessage    `json:"additionalProperties"`
	Required         []string           `json:"required"`
	Minimum          json.Number        `json:"minimum"`
	Maximum          json.Number        `json:"maximum"`
	ExclusiveMinimum json.Number        `json:"exclusiveMinimum"`
	MinLength        int                `json:"minLength"`
	MinItems         *int               `json:"minItems"`
	MaxItems         *int               `json:"maxItems"`
	Format           string             `json:"format"`
	AnyOf            []*Schema          `json:"anyOf"`
	IgnoredArgs      bool               `json:"x-ignored-args"`
	ArgsOptional     bool               `json:"x-args-optional"`
	Kind             string             `json:"x-kind"`

	enum       []*Node
	additional *Schema
	closed     bool
}

type typeList []string

func (t *typeList) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		err := json.Unmarshal(data, &s)
		*t = typeList{s}
		return err
	}
	return json.Unmarshal(data, (*[]string)(t))
}

// prepare decodes enum values and additionalProperties once.
func (s *Schema) prepare() error {
	if s == nil {
		return nil
	}
	for _, raw := range s.Enum {
		n, err := FromJSON(raw)
		if err != nil {
			return err
		}
		s.enum = append(s.enum, n)
	}
	switch a := strings.TrimSpace(string(s.Additional)); {
	case a == "false":
		s.closed = true
	case strings.HasPrefix(a, "{"):
		s.additional = &Schema{}
		if err := json.Unmarshal(s.Additional, s.additional); err != nil {
			return err
		}
	}
	for _, child := range append(append([]*Schema{s.Items, s.additional}, s.AnyOf...), values(s.Properties)...) {
		if err := child.prepare(); err != nil {
			return err
		}
	}
	return nil
}

func values(m map[string]*Schema) []*Schema {
	out := make([]*Schema, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

//go:embed schemas/*.json
var schemaFS embed.FS

// Embedded schemas are validated at process startup, before requests can arrive.
func init() { schemas() }

var catalog struct {
	once    sync.Once
	global  *Schema
	plugins map[string]*Schema
	codes   map[string][]string
}

func schemaFile(name string, out any) {
	data, err := schemaFS.ReadFile("schemas/" + name)
	if err == nil {
		err = json.Unmarshal(data, out)
	}
	if err != nil {
		panic("oxidns: schema " + name + ": " + err.Error())
	}
}

// schemas returns the global schema and the plugin catalog (shared, read-only).
func schemas() (*Schema, map[string]*Schema) {
	catalog.once.Do(func() {
		var global struct {
			Global  *Schema            `json:"global"`
			Plugins map[string]*Schema `json:"plugins"`
		}
		var plugins struct {
			Plugins map[string]*Schema `json:"plugins"`
		}
		schemaFile("global.json", &global)
		schemaFile("plugins.json", &plugins)
		schemaFile("dns-codes.json", &catalog.codes)
		catalog.global, catalog.plugins = global.Global, global.Plugins
		for k, v := range plugins.Plugins {
			catalog.plugins[k] = v
		}
		for _, s := range append(values(catalog.plugins), catalog.global) {
			if err := s.prepare(); err != nil {
				panic(err)
			}
		}
	})
	return catalog.global, catalog.plugins
}

func pluginKind(kind string) string {
	_, plugins := schemas()
	if s := plugins[kind]; s != nil {
		return s.Kind
	}
	return ""
}

// spaces matches one Unicode whitespace character (regexp's \s is ASCII).
const spaces = `[\s\x{1c}-\x{1f}\x{85}\p{Z}]`

var digitsRE = regexp.MustCompile(`^\+?[0-9]+$`)

// boundedUint is _uint of the validation modules: decimal text (optional
// "+" when plus) not above maximum.
func boundedUint(raw string, maximum *big.Int, plus bool) (*big.Int, bool) {
	if plus {
		if !digitsRE.MatchString(raw) {
			return nil, false
		}
		raw = strings.TrimPrefix(raw, "+")
	} else if raw == "" || strings.Trim(raw, "0123456789") != "" {
		return nil, false
	}
	v, _ := new(big.Int).SetString(raw, 10)
	return v, v.Cmp(maximum) <= 0
}

func uintMax(raw string, maximum int64) bool {
	_, ok := boundedUint(raw, big.NewInt(maximum), true)
	return ok
}

// DNSCodeValid is dns_code_valid: a DNS type/class/response code name or
// number within range.
func DNSCodeValid(kind string, token *Node) bool {
	if !isStr(token) && !isInt(token) {
		return false
	}
	raw := token.Value
	if digitsRE.MatchString(raw) {
		limit := int64(65535)
		if kind == "base-rcode" || kind == "dns-base-rcode" {
			limit = 15
		}
		return uintMax(raw, limit)
	}
	schemas()
	key := strings.TrimPrefix(kind, "dns-")
	names := catalog.codes[key]
	if key == "base-rcode" {
		names = catalog.codes["rcode"][:11]
	}
	return containsString(names, strings.ToUpper(raw))
}

// envRef is (?<!\$)\$\{[^}]+\}: an unexpanded ${NAME} reference.
func envRef(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '$' && s[i+1] == '{' && (i == 0 || s[i-1] != '$') && s[i+2] != '}' && strings.IndexByte(s[i+2:], '}') > 0 {
			return true
		}
	}
	return false
}

// anyEnv is \$\{[^}]+\} without the $$ escape.
func anyEnv(s string) bool {
	for i := strings.Index(s, "${"); i >= 0; {
		if j := strings.IndexByte(s[i+2:], '}'); j > 0 {
			return true
		}
		next := strings.Index(s[i+1:], "${")
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return false
}

func endpointValid(value *Node, listen, socks bool) bool {
	if !isStr(value) {
		return false
	}
	raw := value.Value
	if socks {
		raw = strings.TrimSpace(raw)
		if i := strings.LastIndexByte(raw, '@'); i >= 0 {
			if !strings.Contains(raw[:i], ":") {
				return false
			}
			raw = raw[i+1:]
		}
	}
	i := strings.LastIndexByte(raw, ':')
	if i < 0 || !uintMax(raw[i+1:], 65535) {
		return false
	}
	host := raw[:i]
	bracketed := strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && len(host) >= 2
	if socks {
		if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
			if !bracketed {
				return false
			}
			host = host[1 : len(host)-1]
		}
		return host != ""
	}
	if host == "" {
		return listen
	}
	inner := host
	if bracketed {
		inner = host[1 : len(host)-1]
	}
	addr, err := netip.ParseAddr(inner)
	return err == nil && !strings.Contains(host, "%") && (addr.Is4() || strings.HasPrefix(host, "["))
}

func isPySpace(s string) bool { return strings.IndexFunc(s, unicode.IsSpace) >= 0 }

var (
	durationRE      = regexp.MustCompile(`(?i)^([0-9]+)` + spaces + `*(ms|s|m|h|d)?$`)
	tokenRE         = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	upstreamSchemes = []string{"udp", "tcp", "tls", "quic", "doq", "https", "doh", "h3", "tcp+pipeline", "tls+pipeline"}
)

// FormatError is format_error: a value-free message for a schema format, ""
// when valid.
func FormatError(name string, value *Node) string {
	if isNone(value) || isStr(value) && envRef(value.Value) {
		return ""
	}
	text, isText := value.Value, isStr(value)
	switch name {
	case "non-empty-string", "nonempty-string":
		if isText && strings.TrimSpace(text) != "" {
			return ""
		}
		return "必须填写非空文本"
	case "duration":
		valid := false
		if v, ok := intOf(value); ok {
			valid = v.Sign() >= 0 && v.Cmp(u64) <= 0
		} else if m := durationRE.FindStringSubmatch(strings.TrimSpace(text)); isText && m != nil {
			_, valid = boundedUint(m[1], u64, true)
		}
		if valid {
			return ""
		}
		return "时长须为非负整数秒或整数加 ms、s、m、h、d 单位"
	case "ip", "ip-address", "ip-network":
		if name == "ip-network" {
			if _, ok := netipx.ParseNetwork(text, false); isText && ok {
				return ""
			}
			return "必须为有效 IP/CIDR"
		}
		if _, err := netip.ParseAddr(text); isText && err == nil && !strings.Contains(text, "%") {
			return ""
		}
		return "必须为有效 IP 地址"
	case "socket-address", "socks5":
		if endpointValid(value, name == "socket-address", name == "socks5") {
			return ""
		}
		return "地址格式无效，请检查主机、端口及 IPv6 方括号"
	case "upstream-address", "http-url":
		if isText && !isPySpace(text) {
			raw := text
			schemes := []string{"http", "https"}
			if name != "http-url" {
				schemes = upstreamSchemes
				if !strings.Contains(text, "//") {
					raw = "udp://" + text
				}
			}
			if u, err := weburl.Parse(raw); err == nil && weburl.Hostname(u) != "" && containsString(schemes, strings.ToLower(u.Scheme)) {
				if _, _, err := weburl.Port(u); err == nil {
					return ""
				}
			}
		}
		return "URL/上游地址无效或协议不受支持"
	case "http-method":
		if isText && tokenRE.MatchString(strings.TrimSpace(text)) {
			return ""
		}
		return "HTTP 方法须为有效的 token"
	case "timezone":
		if isText && validZone(strings.TrimSpace(text)) {
			return ""
		}
		return "时区须为有效 IANA 时区，例如 Asia/Shanghai"
	case "plugin-tag":
		if isText && validTag(text) {
			return ""
		}
		return "需要有效的 1–64 位插件标识"
	case "dns-record", "domain-expression":
		return ValidatePluginFormat(name, value)
	}
	if strings.HasPrefix(name, "dns-") {
		if DNSCodeValid(name, value) {
			return ""
		}
		return "DNS 类型/类别/响应码无效或超出允许范围"
	}
	return fmt.Sprintf(messageUnsupportedFormat, name)
}

// tzPath is CPython's zoneinfo TZPATH (PYTHONTZPATH overrides it).
func tzPath() []string {
	if env, ok := os.LookupEnv("PYTHONTZPATH"); ok {
		return filepath.SplitList(env)
	}
	return []string{"/usr/share/zoneinfo", "/usr/lib/zoneinfo", "/usr/share/lib/zoneinfo", "/etc/zoneinfo"}
}

// validZone is zoneinfo.ZoneInfo(key) succeeding: a relative key without
// up-level parts naming a TZif file on the host's zone path.
func validZone(key string) bool {
	if key == "" || path.IsAbs(key) || path.Clean(key) != key || strings.HasPrefix(key, "../") || key == ".." {
		return false
	}
	for _, dir := range tzPath() {
		info, err := os.Stat(filepath.Join(dir, key))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(filepath.Join(dir, key))
		if err != nil {
			return false
		}
		magic := make([]byte, 4)
		n, _ := f.Read(magic)
		f.Close()
		return n == 4 && string(magic) == "TZif"
	}
	return false
}

func isType(value *Node, kind string) bool {
	switch kind {
	case "object":
		return isMap(value)
	case "array":
		return isSeq(value)
	case "string":
		return isStr(value)
	case "integer":
		return isInt(value)
	case "number":
		return isNum(value)
	case "boolean":
		return isBool(value)
	case "null":
		return isNone(value)
	}
	return false
}

func numberText(n json.Number) string {
	if strings.ContainsAny(string(n), ".eE") {
		f, _ := n.Float64()
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return string(n)
}

// ValidateSchema checks value against schema; issues are errors first.
func ValidateSchema(value *Node, schema *Schema, at Path) []Issue {
	var issues []Issue
	add := func(message, code, severity string, where Path) {
		issues = append(issues, Issue{Path: append(Path{}, where...), Message: message, Code: code, Severity: severity})
	}
	if schema == nil {
		schema = &Schema{}
	}
	if schema.IgnoredArgs {
		return issues
	}
	if isStr(value) && envRef(value.Value) {
		add("含动态占位符，实际值需由目标环境解析；静态校验无法确认展开后的参数", "deferred", "warning", at)
		return issues
	}
	if schema.AnyOf != nil {
		variants := make([][]Issue, len(schema.AnyOf))
		for i, option := range schema.AnyOf {
			variants[i] = ValidateSchema(value, option, at)
			if !hasError(variants[i]) {
				return variants[i]
			}
		}
		best, bestMatches, bestErrors := -1, 0, 0
		consider := func(i int) {
			option, matches, errs := schema.AnyOf[i], 0, 0
			for key, field := range option.Properties {
				if v := get(value, key); has(value, key) && field.enum != nil {
					for _, e := range field.enum {
						if equal(v, e) {
							matches++
							break
						}
					}
				}
			}
			for _, item := range variants[i] {
				if item.Severity == "error" {
					errs++
				}
			}
			if best < 0 || matches > bestMatches || matches == bestMatches && errs < bestErrors {
				best, bestMatches, bestErrors = i, matches, errs
			}
		}
		for i, option := range schema.AnyOf {
			if len(option.Type) == 0 || anyType(value, option.Type) {
				consider(i)
			}
		}
		if best < 0 {
			for i := range schema.AnyOf {
				consider(i)
			}
		}
		return variants[best]
	}
	if len(schema.Type) > 0 && !anyType(value, schema.Type) {
		add("参数类型无效，需要 "+strings.Join(schema.Type, " / "), "type", "error", at)
		return issues
	}
	if schema.Enum != nil {
		found := false
		for _, option := range schema.enum {
			found = found || same(value, option)
		}
		if !found {
			add("不在该参数允许的选项中", "enum", "error", at)
		}
	}
	if isNone(value) {
		return issues
	}
	if r, ok := ratOf(value); ok && isNum(value) {
		for _, bound := range []struct {
			n     json.Number
			fails func(c int) bool
			label string
		}{{schema.Minimum, func(c int) bool { return c < 0 }, "不能小于"},
			{schema.Maximum, func(c int) bool { return c > 0 }, "不能大于"},
			{schema.ExclusiveMinimum, func(c int) bool { return c <= 0 }, "必须大于"}} {
			if bound.n == "" {
				continue
			}
			limit, _ := new(big.Rat).SetString(string(bound.n))
			if bound.fails(r.Cmp(limit)) {
				add(bound.label+" "+numberText(bound.n), "range", "error", at)
			}
		}
	}
	if isStr(value) && utf8.RuneCountInString(value.Value) < schema.MinLength {
		add("文本长度不足", "length", "error", at)
	}
	if isSeq(value) {
		if schema.MinItems != nil && len(value.Content) < *schema.MinItems {
			add("至少需要 "+strconv.Itoa(*schema.MinItems)+" 个条目", "required", "error", at)
		}
		if schema.MaxItems != nil && len(value.Content) > *schema.MaxItems {
			add("条目数量超过 "+strconv.Itoa(*schema.MaxItems), "length", "error", at)
		}
		for i, item := range value.Content {
			issues = append(issues, ValidateSchema(item, schema.Items, at.with(i))...)
		}
	}
	if isMap(value) {
		for _, key := range schema.Required {
			if !has(value, key) {
				add("缺少必填参数", "required", "error", at.with(key))
			}
		}
		pairs(value, func(key string, item *Node) bool {
			switch field, known := schema.Properties[key]; {
			case known:
				issues = append(issues, ValidateSchema(item, field, at.with(key))...)
			case schema.additional != nil:
				issues = append(issues, ValidateSchema(item, schema.additional, at.with(key))...)
			case schema.closed:
				add("官方核心不接受此对象中的未知字段", "unknown-field", "error", at.with(key))
			case schema.Properties != nil:
				add("扩展字段已保留，不在当前版本的校验目录中", "unknown-field", "warning", at.with(key))
			}
			return true
		})
	}
	if schema.Format != "" {
		if message := FormatError(schema.Format, value); message != "" {
			add(message, "format", "error", at)
		}
	}
	return limit(errorsFirst(issues))
}

func anyType(value *Node, kinds []string) bool {
	for _, kind := range kinds {
		if isType(value, kind) {
			return true
		}
	}
	return false
}

func limit(issues []Issue) []Issue {
	if len(issues) > MaxIssues {
		return issues[:MaxIssues]
	}
	return issues
}

var pluginShape = &Schema{Type: typeList{"object"}, Required: []string{"tag", "type"}, Properties: map[string]*Schema{
	"tag": {Type: typeList{"string"}, Format: "plugin-tag"}, "type": {Type: typeList{"string"}, MinLength: 1}, "args": {}}}

// ValidateNativeConfig returns every issue of an expanded native document.
func ValidateNativeConfig(document *Node) []Issue {
	global, plugins := schemas()
	issues := ValidateSchema(document, global, nil)
	if !isMap(document) {
		return issues
	}
	for index, plugin := range items(get(document, "plugins")) {
		at := P("plugins", index)
		issues = append(issues, ValidateSchema(plugin, pluginShape, at)...)
		if !isMap(plugin) {
			continue
		}
		kind := get(plugin, "type")
		schema := plugins[str(kind)]
		if !isStr(kind) || schema == nil {
			issues = append(issues, Issue{Path: at.with("type"), Message: "插件未在当前校验目录中，发布时需由目标核心确认支持", Code: "unknown-plugin", Severity: "warning"})
			continue
		}
		if !has(plugin, "args") && !schema.ArgsOptional {
			issues = append(issues, Issue{Path: at.with("args"), Message: "缺少插件必填参数", Code: "required", Severity: "error"})
		} else if has(plugin, "args") {
			issues = append(issues, ValidateSchema(get(plugin, "args"), schema, at.with("args"))...)
		}
		issues = append(issues, collectPluginIssues(plugin, index)...)
	}
	issues = append(issues, collectGlobalIssues(document)...)
	issues = append(issues, collectReferenceIssues(document, nil)...)
	var result []Issue
	seen := map[string]bool{}
	for _, item := range errorsFirst(issues) {
		if item.Severity == "" {
			item.Severity = "error"
		}
		data, _ := json.Marshal(item.Path)
		key := fmt.Sprintf("%s\x00%s\x00%s", data, item.Message, item.Severity)
		if !seen[key] {
			result = append(result, item)
			seen[key] = true
		}
		if len(result) == MaxIssues {
			break
		}
	}
	if result == nil {
		result = []Issue{}
	}
	return result
}

// RequireValidDocument returns the issues, or a *ValidationError when one
// is an error.
func RequireValidDocument(document *Node) ([]Issue, error) {
	issues := ValidateNativeConfig(document)
	if hasError(issues) {
		return nil, &ValidationError{issues}
	}
	return issues, nil
}

// ValidateExpandedDocument maps issues of an expanded document back to the
// file and plugin index that declared them, and type-checks include globals.
func ValidateExpandedDocument(document *Node, sources *Sources) []Issue {
	if sources == nil {
		sources = &Sources{}
	}
	issues := ValidateNativeConfig(document)
	for i := range issues {
		item := &issues[i]
		item.Source = "config.yaml"
		if len(item.Path) > 1 && item.Path[0] == (Elem{Key: "plugins"}) && item.Path[1].IsIndex && item.Path[1].Index < len(sources.Plugins) {
			origin := sources.Plugins[item.Path[1].Index]
			item.Source = origin.Source
			item.Path = P("plugins", origin.Index, item.Path[2:])
		}
	}
	global, _ := schemas()
	for _, included := range sources.Includes {
		for _, item := range ValidateSchema(included.Globals, global, nil) {
			item.Source = included.Name
			issues = append(issues, item)
		}
	}
	return limit(errorsFirst(issues))
}
