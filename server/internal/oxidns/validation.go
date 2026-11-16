package oxidns

import (
	"fmt"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/resource"
	"configflow/internal/urlx"
)

// Port of backend/common/oxidns_validation.py (module C23): schemas,
// validate_schema, format_error and their helpers, OxidnsValidationError
// (C22 ported these first: the template converter validates the live
// download options with them), validate_native_config,
// require_valid_document, validate_expanded_document and validate_settings.

// MaxIssues is MAX_ISSUES.
const MaxIssues = 200

var u64 = new(big.Int).SetUint64(18446744073709551615)

// envSearch is ENV.search(value) for ENV = (?<!\$)\$\{[^}]+\}: a "${…}"
// with at least one character inside that does not follow a "$".
func envSearch(value string) bool {
	for i := 0; i+1 < len(value); i++ {
		if value[i] != '$' || value[i+1] != '{' || (i > 0 && value[i-1] == '$') {
			continue
		}
		if j := strings.IndexByte(value[i+2:], '}'); j > 0 {
			return true
		}
	}
	return false
}

// validationError is OxidnsValidationError(issues).
func validationError(issues []any) *Error {
	location, message := "config.yaml", "请检查参数"
	for _, issue := range issues {
		if get(issue, "severity", "error") == "error" {
			location = FormatPath(asList(item(issue, "path")))
			message = str(item(issue, "message"))
			break
		}
	}
	return &Error{Class: ClassValidation, Message: "OxiDNS 配置校验未通过：" + location + " " + message,
		Issues: issues, HasIssues: true}
}

func asList(v any) []any {
	list, _ := v.([]any)
	return list
}

// FormatPath is format_path(path): "plugins[3].args.files", or
// "config.yaml" for the empty path.
func FormatPath(path []any) string {
	var b strings.Builder
	for index, part := range path {
		if n, ok := part.(jsonx.Number); ok && n.IsInt() {
			b.WriteString("[" + n.String() + "]")
			continue
		}
		if index > 0 {
			b.WriteString(".")
		}
		b.WriteString(str(part))
	}
	if b.Len() == 0 {
		return "config.yaml"
	}
	return b.String()
}

var (
	schemaOnce    sync.Once
	schemaGlobal  *jsonx.Object
	schemaPlugins *jsonx.Object
	schemaCodes   *jsonx.Object
	schemaErr     error
)

func loadSchemaFile(name string) (*jsonx.Object, error) {
	data, err := os.ReadFile(resource.FromEnv().Backend("schemas/oxidns/" + name))
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("schemas/oxidns/%s: not an object", name)
	}
	return object, nil
}

func loadSchemas() {
	schemaOnce.Do(func() {
		global, err := loadSchemaFile("global.json")
		if err != nil {
			schemaErr = err
			return
		}
		plugins, err := loadSchemaFile("plugins.json")
		if err != nil {
			schemaErr = err
			return
		}
		codes, err := loadSchemaFile("dns-codes.json")
		if err != nil {
			schemaErr = err
			return
		}
		merged := jsonx.NewObject()
		for key, value := range global.GetObject("plugins").All() {
			merged.Set(key, value)
		}
		for key, value := range plugins.GetObject("plugins").All() {
			merged.Set(key, value)
		}
		schemaGlobal, schemaPlugins, schemaCodes = global.GetObject("global"), merged, codes
	})
	if schemaErr != nil {
		raise(schemaErr)
	}
}

// Schemas is schemas(): the global schema and the plugin catalog (type ->
// args schema), read once from backend/schemas/oxidns under the resource
// root (resource.FromEnv). The values are shared: do not modify them.
func Schemas() (global, plugins *jsonx.Object, err error) {
	defer catch(&err)
	loadSchemas()
	return schemaGlobal, schemaPlugins, nil
}

// uintText is _uint(raw, maximum): the value of "+"-optional ASCII digits
// not above maximum.
func uintText(raw any, maximum *big.Int) (*big.Int, bool) {
	text, ok := raw.(string)
	if !ok || !plusDigits.MatchString(text) {
		return nil, false
	}
	digits := strings.TrimLeft(strings.TrimLeft(text, "+"), "0")
	if digits == "" {
		digits = "0"
	}
	value, _ := new(big.Int).SetString(digits, 10)
	if value.Cmp(maximum) > 0 {
		return nil, false
	}
	return value, true
}

var plusDigits = regexp.MustCompile(`^\+?[0-9]+$`)

// dnsCodeValid is dns_code_valid(kind, token).
func dnsCodeValid(kind string, token any) bool {
	var raw string
	switch x := token.(type) {
	case string:
		raw = x
	case jsonx.Number:
		if !x.IsInt() {
			return false
		}
		raw = x.String()
	default:
		return false
	}
	if plusDigits.MatchString(raw) {
		maximum := int64(65535)
		if kind == "base-rcode" || kind == "dns-base-rcode" {
			maximum = 15
		}
		_, ok := uintText(raw, big.NewInt(maximum))
		return ok
	}
	loadSchemas()
	key := strings.TrimPrefix(kind, "dns-")
	upper := pystr.Upper(raw)
	names := asList(schemaCodes.Value(key))
	if key == "base-rcode" {
		names = asList(schemaCodes.Value("rcode"))
		if len(names) > 11 {
			names = names[:11]
		}
	}
	for _, name := range names {
		if name == upper {
			return true
		}
	}
	return false
}

// endpoint is _endpoint(value, listen=, socks=).
func endpoint(value any, listen, socks bool) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	if socks {
		raw = pystr.Strip(raw)
		if strings.Contains(raw, "@") {
			i := strings.LastIndexByte(raw, '@')
			credentials := raw[:i]
			raw = raw[i+1:]
			if !strings.Contains(credentials, ":") {
				return false
			}
		}
	}
	i := strings.LastIndexByte(raw, ':')
	if i < 0 {
		return false
	}
	host, port := raw[:i], raw[i+1:]
	if _, ok := uintText(port, big.NewInt(65535)); !ok {
		return false
	}
	if socks {
		if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
			if !(strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]")) {
				return false
			}
			host = host[1 : len(host)-1]
		}
		return host != ""
	}
	if host == "" {
		return listen
	}
	bracketed := strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") && len(host) >= 2
	inner := host
	if bracketed {
		inner = host[1 : len(host)-1]
	}
	address, ok := convert.ParseIPAddress(inner)
	if !ok {
		return false
	}
	return !strings.Contains(host, "%") && (address.Is4() || strings.HasPrefix(host, "["))
}

var (
	durationPattern = regexp.MustCompile(`^([0-9]+)([\s\S]*)$`)
	httpToken       = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	upstreamSchemes = []string{"udp", "tcp", "tls", "quic", "doq", "https", "doh", "h3", "tcp+pipeline", "tls+pipeline"}
)

// durationValid is the str branch of format_error's duration check:
// re.fullmatch(r'([0-9]+)\s*(ms|s|m|h|d)?', value.strip(), re.I) and the
// digits within u64. Under re.I the "s" also matches U+017F.
func durationValid(value string) bool {
	m := durationPattern.FindStringSubmatch(pystr.Strip(value))
	if m == nil {
		return false
	}
	rest := strings.TrimLeftFunc(m[2], pystr.IsSpace)
	unit := strings.ToLower(strings.ReplaceAll(rest, "ſ", "s"))
	switch unit {
	case "", "ms", "s", "m", "h", "d":
	default:
		return false
	}
	// Only ASCII letters (and U+017F) may make up the unit.
	for _, r := range rest {
		if r > 0x7f && r != 0x17f {
			return false
		}
	}
	_, ok := uintText(m[1], u64)
	return ok
}

// FormatError is format_error(name, value): a value-free diagnostic, or
// nil (None) when the value has the format.
func FormatError(name string, value any) (message *string, err error) {
	defer catch(&err)
	if text := formatError(name, value); text != "" {
		return &text, nil
	}
	return nil, nil
}

// formatError is format_error(name, value) with "" for None.
func formatError(name string, value any) string {
	if text, ok := value.(string); value == nil || ok && envSearch(text) {
		return ""
	}
	switch name {
	case "non-empty-string", "nonempty-string":
		if text, ok := value.(string); ok && pystr.Strip(text) != "" {
			return ""
		}
		return "必须填写非空文本"
	case "duration":
		valid := false
		if n, ok := value.(jsonx.Number); ok && n.IsInt() {
			b := bigOf(n)
			valid = b.Sign() >= 0 && b.Cmp(u64) <= 0
		} else if text, ok := value.(string); ok {
			valid = durationValid(text)
		}
		if valid {
			return ""
		}
		return "时长须为非负整数秒或整数加 ms、s、m、h、d 单位"
	case "ip", "ip-address", "ip-network":
		text, ok := value.(string)
		valid := ok && !strings.Contains(text, "%")
		if valid {
			if name == "ip-network" {
				valid = convert.IsIPNetwork(text)
			} else {
				_, valid = convert.ParseIPAddress(text)
			}
		}
		if valid {
			return ""
		}
		if name == "ip-network" {
			return "必须为有效 IP/CIDR"
		}
		return "必须为有效 IP 地址"
	case "socket-address", "socks5":
		if endpoint(value, name == "socket-address", name == "socks5") {
			return ""
		}
		return "地址格式无效，请检查主机、端口及 IPv6 方括号"
	case "upstream-address", "http-url":
		if upstreamURLValid(name, value) {
			return ""
		}
		return "URL/上游地址无效或协议不受支持"
	case "http-method":
		if text, ok := value.(string); ok && httpToken.MatchString(pystr.Strip(text)) {
			return ""
		}
		return "HTTP 方法须为有效的 token"
	case "timezone":
		if text, ok := value.(string); ok && zoneInfoValid(pystr.Strip(text)) {
			return ""
		}
		return "时区须为有效 IANA 时区，例如 Asia/Shanghai"
	case "plugin-tag":
		if text, ok := value.(string); ok && validPluginTag(text) {
			return ""
		}
		return "需要有效的 1–64 位插件标识"
	case "dns-record", "domain-expression":
		return validatePluginFormat(name, value)
	}
	if strings.HasPrefix(name, "dns-") {
		if dnsCodeValid(name, value) {
			return ""
		}
		return "DNS 类型/类别/响应码无效或超出允许范围"
	}
	raise(pyErr("RuntimeError", "Unsupported OxiDNS schema format: "+name))
	return ""
}

// validPluginTag is the plugin-tag rule: TAG, 1–64 characters, no reserved
// qs.exec./qs.match./qs.cron. prefix.
func validPluginTag(tag string) bool {
	return len(tag) > 0 && runeLen(tag) <= 64 && Tag.MatchString(tag) &&
		!strings.HasPrefix(tag, "qs.exec.") && !strings.HasPrefix(tag, "qs.match.") && !strings.HasPrefix(tag, "qs.cron.")
}

func upstreamURLValid(name string, value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, r := range text {
		if pystr.IsSpace(r) {
			return false
		}
	}
	raw := text
	if !strings.Contains(text, "//") && name != "http-url" {
		raw = "udp://" + text
	}
	parts, err := urlx.Split(raw)
	if err != nil {
		return false
	}
	schemes := upstreamSchemes
	if name == "http-url" {
		schemes = []string{"http", "https"}
	}
	if parts.Hostname() == "" || !strIn(parts.Scheme, schemes...) {
		return false
	}
	if _, _, err := parts.Port(); err != nil {
		return false
	}
	return true
}

// isType is _is_type(value, kind).
func isType(value any, kind any) bool {
	switch kind {
	case "object":
		return isDict(value)
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		return isStr(value)
	case "integer":
		return isInt(value)
	case "number":
		n, ok := value.(jsonx.Number)
		if !ok {
			return false
		}
		if n.IsInt() {
			return true
		}
		return finite(n)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	}
	return false
}

func finite(n jsonx.Number) bool {
	switch n {
	case "NaN", "Infinity", "-Infinity":
		return false
	}
	f := n.Float64()
	return f == f && f-f == 0
}

// kindsOf is `kinds if isinstance(kinds, list) else [kinds]`.
func kindsOf(kinds any) []any {
	if list, ok := kinds.([]any); ok {
		return list
	}
	return []any{kinds}
}

// sameType is type(a) is type(b) for loaded values.
func sameType(a, b any) bool { return typeName(a) == typeName(b) }

// compareNumbers is cmp(a, b) for Python int/float values (exact).
func compareNumbers(a, b jsonx.Number) int {
	if a.IsInt() && b.IsInt() {
		return bigOf(a).Cmp(bigOf(b))
	}
	return bigFloat(a).Cmp(bigFloat(b))
}

func bigFloat(n jsonx.Number) *big.Float {
	if n.IsInt() {
		return new(big.Float).SetInt(bigOf(n))
	}
	return new(big.Float).SetFloat64(n.Float64())
}

func issue(path []any, message, code, severity string) *jsonx.Object {
	return jsonx.Obj("path", append([]any{}, path...), "message", message, "code", code, "severity", severity)
}

// ValidateSchema is validate_schema(value, schema, path): the issues
// ({path, message, code, severity}) of value against one schema of the
// catalog, errors first, at most MaxIssues.
func ValidateSchema(value any, schema *jsonx.Object, path []any) (issues []any, err error) {
	defer catch(&err)
	return validateSchema(value, schema, path), nil
}

func errorCount(issues []any) int {
	count := 0
	for _, i := range issues {
		if get(i, "severity", nil) == "error" {
			count++
		}
	}
	return count
}

func validateSchema(value any, schema *jsonx.Object, path []any) []any {
	issues := []any{}
	add := func(message, code, severity string, at []any) {
		issues = append(issues, issue(at, message, code, severity))
	}
	if truthy(schema.Value("x-ignored-args")) {
		return issues
	}
	if text, ok := value.(string); ok && envSearch(text) {
		add("含动态占位符，实际值需由目标环境解析；静态校验无法确认展开后的参数", "deferred", "warning", path)
		return issues
	}
	if anyOf, present := schema.Get("anyOf"); present {
		options := asList(anyOf)
		variants := make([][]any, len(options))
		for i, option := range options {
			variants[i] = validateSchema(value, asObject(option), path)
		}
		for _, items := range variants {
			if errorCount(items) == 0 {
				return items
			}
		}
		// Prefer the matching structural alternative for useful nested paths.
		type candidate struct {
			option *jsonx.Object
			items  []any
		}
		var candidates []candidate
		for i, raw := range options {
			option := asObject(raw)
			kinds := option.Value("type")
			matches := !truthy(kinds)
			if !matches {
				for _, kind := range kindsOf(kinds) {
					if isType(value, kind) {
						matches = true
						break
					}
				}
			}
			if matches {
				candidates = append(candidates, candidate{option, variants[i]})
			}
		}
		if len(candidates) == 0 {
			for i, raw := range options {
				candidates = append(candidates, candidate{asObject(raw), variants[i]})
			}
		}
		rank := func(c candidate) (int, int) {
			matches := 0
			for key, field := range asObject(c.option.GetOr("properties", jsonx.NewObject())).All() {
				fieldObject := asObject(field)
				if isDict(value) && has(value, key) && fieldObject.Has("enum") && listContains(asList(fieldObject.Value("enum")), get(value, key, nil)) {
					matches++
				}
			}
			return -matches, errorCount(c.items)
		}
		best := 0
		bestA, bestB := rank(candidates[0])
		for i := 1; i < len(candidates); i++ {
			a, b := rank(candidates[i])
			if a < bestA || a == bestA && b < bestB {
				best, bestA, bestB = i, a, b
			}
		}
		return candidates[best].items
	}
	kinds := schema.Value("type")
	if truthy(kinds) {
		matches := false
		names := []string{}
		for _, kind := range kindsOf(kinds) {
			names = append(names, str(kind))
			if isType(value, kind) {
				matches = true
			}
		}
		if !matches {
			add("参数类型无效，需要 "+strings.Join(names, " / "), "type", "error", path)
			return issues
		}
	}
	if enum, present := schema.Get("enum"); present {
		found := false
		for _, option := range iterate(enum) {
			if sameType(value, option) && jsonx.Equal(value, option) {
				found = true
				break
			}
		}
		if !found {
			add("不在该参数允许的选项中", "enum", "error", path)
		}
	}
	if value == nil {
		return issues
	}
	if n, ok := value.(jsonx.Number); ok {
		for _, check := range []struct {
			key   string
			fails func(int) bool
			label string
		}{
			{"minimum", func(c int) bool { return c < 0 }, "不能小于"},
			{"maximum", func(c int) bool { return c > 0 }, "不能大于"},
			{"exclusiveMinimum", func(c int) bool { return c <= 0 }, "必须大于"},
		} {
			if limit, present := schema.Get(check.key); present {
				bound, isNumber := limit.(jsonx.Number)
				if !isNumber {
					raise(pyErr("TypeError", "'<' not supported between instances of '"+typeName(value)+"' and '"+typeName(limit)+"'"))
				}
				if check.fails(compareNumbers(n, bound)) {
					add(check.label+" "+str(limit), "range", "error", path)
				}
			}
		}
	}
	if text, ok := value.(string); ok {
		if minimum, ok := schema.GetOr("minLength", jsonx.Int(0)).(jsonx.Number); ok && compareNumbers(jsonx.Int(runeLen(text)), minimum) < 0 {
			add("文本长度不足", "length", "error", path)
		}
	}
	if list, ok := value.([]any); ok {
		if minimum, ok := schema.GetOr("minItems", jsonx.Int(0)).(jsonx.Number); ok && compareNumbers(jsonx.Int(len(list)), minimum) < 0 {
			add("至少需要 "+str(schema.Value("minItems"))+" 个条目", "required", "error", path)
		}
		if maximum, present := schema.Get("maxItems"); present {
			if bound, ok := maximum.(jsonx.Number); ok && compareNumbers(jsonx.Int(len(list)), bound) > 0 {
				add("条目数量超过 "+str(maximum), "length", "error", path)
			}
		}
		items := asObject(schema.GetOr("items", jsonx.NewObject()))
		for index, element := range list {
			issues = append(issues, validateSchema(element, items, appendPath(path, jsonx.Int(index)))...)
		}
	}
	if object, ok := value.(*jsonx.Object); ok && object != nil {
		properties := asObject(schema.GetOr("properties", jsonx.NewObject()))
		for _, key := range iterate(schema.GetOr("required", []any{})) {
			name := str(key)
			if !object.Has(name) {
				add("缺少必填参数", "required", "error", appendPath(path, key))
			}
		}
		additional := schema.Value("additionalProperties")
		for key, element := range object.All() {
			switch {
			case properties.Has(key):
				issues = append(issues, validateSchema(element, asObject(properties.Value(key)), appendPath(path, key))...)
			case isDict(additional):
				issues = append(issues, validateSchema(element, asObject(additional), appendPath(path, key))...)
			case additional == false:
				add("官方核心不接受此对象中的未知字段", "unknown-field", "error", appendPath(path, key))
			case schema.Has("properties"):
				add("扩展字段已保留，不在当前版本的校验目录中", "unknown-field", "warning", appendPath(path, key))
			}
		}
	}
	if format, ok := schema.Value("format").(string); ok && format != "" {
		if message := formatError(format, value); message != "" {
			add(message, "format", "error", path)
		}
	}
	return errorsFirst(issues)
}

// errorsFirst is sorted(issues, key=severity != 'error')[:MAX_ISSUES].
func errorsFirst(issues []any) []any {
	sort.SliceStable(issues, func(i, j int) bool {
		return get(issues[i], "severity", "error") == "error" && get(issues[j], "severity", "error") != "error"
	})
	if len(issues) > MaxIssues {
		issues = issues[:MaxIssues]
	}
	return issues
}

// appendPath is [*path, *parts] as a new list.
func appendPath(path []any, parts ...any) []any {
	out := make([]any, 0, len(path)+len(parts))
	out = append(out, path...)
	return append(out, parts...)
}

// listContains is `value in list` (Python ==).
func listContains(list []any, value any) bool {
	for _, element := range list {
		if jsonx.Equal(element, value) {
			return true
		}
	}
	return false
}

// pluginEntrySchema is validate_native_config's schema of one plugin entry.
var pluginEntrySchema = jsonx.Obj("type", "object", "required", []any{"tag", "type"}, "properties", jsonx.Obj(
	"tag", jsonx.Obj("type", "string", "format", "plugin-tag"),
	"type", jsonx.Obj("type", "string", "minLength", jsonx.Int(1)),
	"args", jsonx.NewObject()))

// ValidateNativeConfig is validate_native_config(document): every issue of
// a native document (global schema, plugin schemas, plugin/global
// constraints, references), errors first, de-duplicated, at most MaxIssues.
func ValidateNativeConfig(document any) (issues []any, err error) {
	defer catch(&err)
	return validateNativeConfig(document), nil
}

func validateNativeConfig(document any) []any {
	loadSchemas()
	globalSchema, catalog := schemaGlobal, schemaPlugins
	issues := validateSchema(document, globalSchema, []any{})
	if !isDict(document) {
		return issues
	}
	var plugins []any
	if list, ok := get(document, "plugins", []any{}).([]any); ok {
		plugins = list
	}
	for index, plugin := range plugins {
		path := []any{"plugins", jsonx.Int(index)}
		issues = append(issues, validateSchema(plugin, pluginEntrySchema, path)...)
		if !isDict(plugin) {
			continue
		}
		var schema any
		if kind, ok := get(plugin, "type", nil).(string); ok {
			schema = catalog.Value(kind)
		}
		if schema == nil {
			issues = append(issues, issue(appendPath(path, "type"), "插件未在当前校验目录中，发布时需由目标核心确认支持", "unknown-plugin", "warning"))
			continue
		}
		if !has(plugin, "args") && !truthy(get(schema, "x-args-optional", nil)) {
			issues = append(issues, issue(appendPath(path, "args"), "缺少插件必填参数", "required", "error"))
		} else if has(plugin, "args") {
			issues = append(issues, validateSchema(item(plugin, "args"), asObject(schema), appendPath(path, "args"))...)
		}
		issues = append(issues, collectPluginIssues(plugin, jsonx.Int(index))...)
	}
	issues = append(issues, collectGlobalIssues(document)...)
	issues = append(issues, collectReferenceIssues(document, catalog, nil, nil)...)
	result := []any{}
	seen := map[string]bool{}
	for _, entry := range errorsFirstAll(issues) {
		object := asObject(entry)
		object.SetDefault("severity", "error")
		key, _ := jsonx.Encode([]any{item(object, "path"), item(object, "message"), item(object, "severity")}, jsonx.Options{Compact: true})
		if !seen[string(key)] {
			result = append(result, entry)
			seen[string(key)] = true
		}
		if len(result) == MaxIssues {
			break
		}
	}
	return result
}

// errorsFirstAll is sorted(issues, key=severity != 'error') without the
// truncation.
func errorsFirstAll(issues []any) []any {
	sorted := append([]any{}, issues...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return get(sorted[i], "severity", "error") == "error" && get(sorted[j], "severity", "error") != "error"
	})
	return sorted
}

// RequireValidDocument is require_valid_document(document): the issues, or
// an OxidnsValidationError when one of them is an error.
func RequireValidDocument(document any) (issues []any, err error) {
	defer catch(&err)
	return requireValidDocument(document), nil
}

func requireValidDocument(document any) []any {
	issues := validateNativeConfig(document)
	for _, entry := range issues {
		if get(entry, "severity", nil) == "error" {
			raise(validationError(issues))
		}
	}
	return issues
}

// ValidateExpandedDocument is validate_expanded_document(document,
// sources) with sources as Python's dict ({'plugins': [{source, index}],
// 'includes': [[name, globals], ...]}): issues carry their source file and
// the path inside it; included globals are type-checked too.
func ValidateExpandedDocument(document any, sources any) (issues []any, err error) {
	defer catch(&err)
	return validateExpandedSources(document, sources), nil
}

// validateExpandedDocument is validate_expanded_document for the metadata
// of nativeDocument.
func validateExpandedDocument(document *jsonx.Object, sources *NativeSources) []any {
	return validateExpandedSources(document, sources.Object())
}

func validateExpandedSources(document any, sources any) []any {
	issues := validateNativeConfig(document)
	for _, entry := range issues {
		object := asObject(entry)
		object.Set("source", "config.yaml")
		path := asList(item(object, "path"))
		if len(path) > 1 && path[0] == "plugins" && isInt(path[1]) {
			origins := get(sources, "plugins", []any{})
			origin := pyIndex(origins, path[1].(jsonx.Number))
			object.Set("source", item(origin, "source"))
			object.Set("path", append([]any{"plugins", item(origin, "index")}, path[2:]...))
		}
	}
	loadSchemas()
	for _, include := range iterate(get(sources, "includes", []any{})) {
		pair := iterate(include)
		if len(pair) != 2 {
			if len(pair) > 2 {
				raise(pyErr("ValueError", "too many values to unpack (expected 2)"))
			}
			raise(pyErr("ValueError", "not enough values to unpack (expected 2, got "+itoa(len(pair))+")"))
		}
		for _, entry := range validateSchema(pair[1], schemaGlobal, []any{}) {
			copied := asObject(entry).Clone()
			copied.Set("source", pair[0])
			issues = append(issues, copied)
		}
	}
	return errorsFirst(issues)
}

// pyIndex is sequence[i] for a list and an int index (IndexError, negative
// indexes from the end).
func pyIndex(sequence any, index jsonx.Number) any {
	list, ok := sequence.([]any)
	if !ok {
		raise(pyErr("TypeError", "'"+typeName(sequence)+"' object is not subscriptable"))
	}
	i, exact := index.Int64()
	if exact && i < 0 {
		i += int64(len(list))
	}
	if !exact || i < 0 || i >= int64(len(list)) {
		raise(pyErr("IndexError", "list index out of range"))
	}
	return list[i]
}

// ValidateSettings is validate_settings(settings, config, base_url):
// non-persistent editor diagnostics with the publication rules ({valid,
// issues, schema_version, core_check_required}). config nil is None.
func ValidateSettings(settings any, config any, baseURL string) (result *jsonx.Object, err error) {
	defer catch(&err)
	return validateSettings(settings, config, baseURL), nil
}

func validateSettings(settings any, config any, baseURL string) *jsonx.Object {
	var issues []any
	func() {
		defer func() {
			if r := recover(); r != nil {
				x, ok := r.(raised)
				if !ok {
					panic(r)
				}
				e, ok := AsError(x.err)
				if !ok {
					panic(r)
				}
				if e.Class == ClassValidation {
					issues = asList(copyValue(e.Issues))
					return
				}
				if e.HasIssues {
					issues = e.Issues
					return
				}
				issues = []any{jsonx.Obj("path", []any{}, "message", e.Error(), "code", "structure", "severity", "error", "source", "config.yaml")}
			}
		}()
		if !truthy(config) {
			config = jsonx.NewObject()
		}
		draft := prepareEditor(settings, config, baseURL)
		sources := &NativeSources{}
		document := nativeDocument(draft, false, sources)
		issues = validateExpandedDocument(document, sources)
	}()
	valid := true
	for _, entry := range issues {
		if item(entry, "severity") == "error" {
			valid = false
		}
	}
	return jsonx.Obj("valid", valid, "issues", issues, "schema_version", "1.6.1", "core_check_required", true)
}
