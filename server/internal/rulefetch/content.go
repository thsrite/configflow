package rulefetch

// Port of backend/utils/ruleset_content.py: inspect editable providers,
// normalize safe Classical delivery syntax, validate remote responses and
// edit payload lists losslessly.

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch/pyyaml"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// ResultCache is the thread-safe LRU for pure results keyed by content
// digests, bounded by entries and (when maxSize > 0) by size.
type ResultCache struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	order      *list.List
	maxEntries int
	maxSize    int
	size       int
}

type cacheEntry struct {
	key   string
	value any
	size  int
}

var (
	cachesMu sync.Mutex
	caches   []*ResultCache
)

// NewResultCache is ResultCache(max_entries, max_size); it is registered
// for ClearRuleContentCaches.
func NewResultCache(maxEntries, maxSize int) *ResultCache {
	c := &ResultCache{entries: map[string]*list.Element{}, order: list.New(), maxEntries: maxEntries, maxSize: maxSize}
	cachesMu.Lock()
	caches = append(caches, c)
	cachesMu.Unlock()
	return c
}

// Get returns the value and moves it to the end.
func (c *ResultCache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToBack(element)
	return element.Value.(*cacheEntry).value, true
}

// Put stores the value unless it alone exceeds maxSize.
func (c *ResultCache) Put(key string, value any, size int) {
	if c.maxSize > 0 && size > c.maxSize {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if previous, ok := c.entries[key]; ok {
		c.size -= previous.Value.(*cacheEntry).size
		c.order.Remove(previous)
	}
	c.entries[key] = c.order.PushBack(&cacheEntry{key, value, size})
	c.size += size
	for c.order.Len() > c.maxEntries || (c.maxSize > 0 && c.size > c.maxSize) {
		front := c.order.Front()
		entry := front.Value.(*cacheEntry)
		c.order.Remove(front)
		delete(c.entries, entry.key)
		c.size -= entry.size
	}
}

// Clear empties the cache.
func (c *ResultCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*list.Element{}
	c.order.Init()
	c.size = 0
}

// ContentDigest is content_digest: "b"+sha256 for bytes, "s"+sha256 for
// text, "" (None) for other values.
func ContentDigest(content any) string {
	switch v := content.(type) {
	case []byte:
		sum := sha256.Sum256(v)
		return "b" + hex.EncodeToString(sum[:])
	case string:
		sum := sha256.Sum256([]byte(v))
		return "s" + hex.EncodeToString(sum[:])
	}
	return ""
}

var (
	validatedCache  = NewResultCache(1024, 0)
	normalizedCache = NewResultCache(256, 16*1024*1024)
)

// ClearRuleContentCaches is clear_rule_content_caches.
func ClearRuleContentCaches() {
	cachesMu.Lock()
	defer cachesMu.Unlock()
	for _, c := range caches {
		c.Clear()
	}
}

const (
	msgDomainClassical = "Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。"
	msgDomainPattern   = "请填写域名或通配符，例如 example.com、+.example.com。"
)

// domainLine is _domain_line.
func domainLine(value string) (string, error) {
	if strings.Contains(value, ",") {
		return "", errors.New(msgDomainClassical)
	}
	if !rules.ValidPattern(value) {
		return "", errors.New(msgDomainPattern)
	}
	return value, nil
}

func classicalOrDomain(behavior any, value string) (string, error) {
	if behavior == "classical" {
		return rules.ClassicalLine(value)
	}
	return domainLine(value)
}

// MetadataError is _metadata_error(behavior, rule_format, source_type) ("" for
// None). Pass nil for a missing format.
func MetadataError(behavior, ruleFormat, sourceType any) string {
	if sourceType != "content" && sourceType != "url" {
		return "规则来源必须是 content 或 url。"
	}
	if behavior != "domain" && behavior != "ipcidr" && behavior != "classical" {
		return "规则集类型必须是 domain、ipcidr 或 classical。"
	}
	if ruleFormat != nil && ruleFormat != "" && ruleFormat != "text" && ruleFormat != "yaml" && ruleFormat != "mrs" {
		return "规则集格式必须是 text、yaml 或 mrs。"
	}
	if ruleFormat == "mrs" && sourceType == "content" {
		return "MRS 是二进制规则格式，不能作为手写文本保存；请使用 text/yaml 或订阅 MRS 文件。"
	}
	if ruleFormat == "mrs" && behavior == "classical" {
		return "MRS 只支持 domain 或 ipcidr，不支持 classical。"
	}
	return ""
}

func problemObject(line int, before *string, message string) *jsonx.Object {
	o := jsonx.Obj("line", jsonx.Int(int64(line)))
	if before != nil {
		o.Set("before", *before)
	}
	o.Set("message", message)
	return o
}

func strp(s string) *string { return &s }

// NormalizeClassicalLines is normalize_classical_lines: the delivery-only
// normalization and its warnings (warnings followed by errors).
func NormalizeClassicalLines(content string) (string, []any, error) {
	checked, err := InspectDomainContent(content, "classical", nil)
	if err != nil {
		return "", nil, err
	}
	warnings := append(append([]any{}, checked.GetList("warnings")...), checked.GetList("errors")...)
	return checked.GetString("content"), warnings, nil
}

var commentPattern = regexp.MustCompile(`#[^\r\n]*`)

// yamlFailure is a caught (yaml.YAMLError, ValueError, RecursionError).
type yamlFailure struct{ err error }

func (f yamlFailure) Error() string { return f.err.Error() }

func valueError(msg string) error { return yamlFailure{errors.New(msg)} }

func isPayloadKey(key *pyyaml.Node) bool {
	return key.Kind == pyyaml.ScalarNode && key.Value == "payload"
}

func payloadPairs(root *pyyaml.Node) [][2]*pyyaml.Node {
	var pairs [][2]*pyyaml.Node
	for _, pair := range root.Pairs {
		if isPayloadKey(pair[0]) {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func singleLineStr(node *pyyaml.Node) bool {
	return node.Kind == pyyaml.ScalarNode && node.Tag == pyyaml.TagStr && node.Style != '|' && node.Style != '>' &&
		node.Start.Line == node.End.Line && !strings.ContainsAny(node.Value, "\n\r")
}

// NormalizeMihomoYAML is normalize_mihomo_yaml: expand flow payloads for
// Mihomo's line-oriented YAML provider reader, keeping block documents
// byte for byte.
func NormalizeMihomoYAML(content string) (string, []any) {
	result, err := normalizeMihomoYAML(content)
	if err != nil {
		return content, []any{problemObject(0, nil, err.Error())}
	}
	return result, []any{}
}

func normalizeMihomoYAML(content string) (string, error) {
	root, err := pyyaml.Compose(content)
	if err != nil {
		return "", err
	}
	if root == nil || root.Kind != pyyaml.MappingNode {
		return "", valueError("YAML 规则需要 payload 列表。")
	}
	pairs := payloadPairs(root)
	if len(pairs) != 1 || pairs[0][1].Kind != pyyaml.SequenceNode {
		return "", valueError("YAML 规则需要唯一的 payload 列表。")
	}
	key, payload := pairs[0][0], pairs[0][1]
	tokens, err := pyyaml.Scan(content)
	if err != nil {
		return "", err
	}
	for _, token := range tokens {
		if token.Kind == pyyaml.TokenAnchor || token.Kind == pyyaml.TokenAlias {
			return "", valueError("请先展开 YAML 锚点或别名；Mihomo 下发保留了原文。")
		}
	}
	for _, node := range payload.Items {
		if !singleLineStr(node) {
			return "", valueError("payload 每项应为一行文本；复杂 YAML 内容已保留原文。")
		}
	}
	if len(payload.Items) == 0 || !(root.FlowStyle || payload.FlowStyle) {
		return content, nil
	}

	runes := []rune(content)
	var positions []int
	var texts []string
	cursor := 0
	spans := [][2]int{}
	for _, token := range tokens {
		if token.Kind == pyyaml.TokenScalar {
			spans = append(spans, [2]int{token.Start.Index, token.End.Index})
		}
	}
	spans = append(spans, [2]int{len(runes), len(runes)})
	for _, span := range spans {
		if span[0] > cursor {
			gap := string(runes[cursor:span[0]])
			for _, loc := range commentPattern.FindAllStringIndex(gap, -1) {
				positions = append(positions, cursor+runeLen(gap[:loc[0]]))
				texts = append(texts, gap[loc[0]:loc[1]])
			}
		}
		cursor = span[1]
	}
	comments := func(start, end int) []string {
		i := sort.SearchInts(positions, start)
		j := sort.SearchInts(positions, end)
		if j < i {
			return nil
		}
		return texts[i:j]
	}

	var rendered, prefix string
	var start, end int
	if root.FlowStyle {
		document, err := yamlx.SafeLoad(content)
		if err != nil {
			return "", yamlFailure{err}
		}
		var unique func(node *pyyaml.Node) error
		unique = func(node *pyyaml.Node) error {
			switch node.Kind {
			case pyyaml.MappingNode:
				seen := map[string]bool{}
				for _, pair := range node.Pairs {
					if pair[0].Kind != pyyaml.ScalarNode || seen[pair[0].Value] {
						return valueError("YAML 映射键重复或过于复杂，已保留原文。")
					}
					seen[pair[0].Value] = true
				}
				for _, pair := range node.Pairs {
					if err := unique(pair[1]); err != nil {
						return err
					}
				}
			case pyyaml.SequenceNode:
				for _, item := range node.Items {
					if err := unique(item); err != nil {
						return err
					}
				}
			default:
				if node.Style == '|' || node.Style == '>' || node.Start.Line != node.End.Line {
					return valueError("多行 YAML 内容已保留原文，请展开为普通 payload 列表。")
				}
			}
			return nil
		}
		if err := unique(root); err != nil {
			return "", err
		}
		dumped, err := yamlx.Dump(document, yamlx.DumpOptions{AllowUnicode: true, SortKeys: false,
			FlowStyle: yamlx.FlowBlock, Width: 1<<31 - 1})
		if err != nil {
			return "", yamlFailure{err}
		}
		rendered = strings.TrimRight(dumped, "\n")
		if notes := comments(root.Start.Index, root.End.Index); len(notes) > 0 {
			rendered = strings.Join(notes, "\n") + "\n" + rendered
		}
		start, end = root.Start.Index, root.End.Index
		prefix = string(runes[:start])
		if strings.Trim(prefix, "\uFEFF \t") != "" && !strings.HasSuffix(prefix, "\n") {
			prefix = strings.TrimRight(prefix, " \t") + "\n"
		}
	} else {
		indent := strings.Repeat(" ", key.Start.Column+2)
		var lines []string
		cursor := payload.Start.Index
		for _, node := range payload.Items {
			for _, note := range comments(cursor, node.Start.Index) {
				lines = append(lines, indent+note)
			}
			lines = append(lines, indent+"- "+jsonDumps(node.Value, false))
			cursor = node.End.Index
		}
		for _, note := range comments(cursor, payload.End.Index) {
			lines = append(lines, indent+note)
		}
		rendered = strings.Join(lines, "\n")
		start, end = payload.Start.Index, payload.End.Index
		prefix = strings.TrimRight(string(runes[:start]), " \t")
		if !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
	}
	return prefix + rendered + string(runes[end:]), nil
}

type normalized struct {
	content  string
	warnings []any
}

// NormalizeMihomoRuleset is normalize_mihomo_ruleset: delivery-only syntax
// and YAML-layout compatibility, cached by content digest.
func NormalizeMihomoRuleset(content string, behavior, ruleFormat any) (string, []any, error) {
	if ruleFormat == "mrs" {
		return content, []any{}, nil
	}
	key := ContentDigest(content) + "\x00" + jsonx.Repr(behavior) + "\x00" + jsonx.Repr(ruleFormat)
	if cached, ok := normalizedCache.Get(key); ok {
		entry := cached.(normalized)
		return entry.content, jsonx.DeepCopy(entry.warnings).([]any), nil
	}
	result, warnings, err := normalizeMihomoRuleset(content, behavior, ruleFormat)
	if err != nil {
		return "", nil, err
	}
	normalizedCache.Put(key, normalized{result, jsonx.DeepCopy(warnings).([]any)}, runeLen(result))
	return result, warnings, nil
}

var (
	textLineBreaks = regexp.MustCompile("\r([^\n]|$)|[\v\f\x1c-\x1e\u0085  ]")
	crOnly         = regexp.MustCompile("\r([^\n]|$)")
)

func normalizeMihomoRuleset(content string, behavior, ruleFormat any) (string, []any, error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	pattern := crOnly
	if ruleFormat == "text" {
		pattern = textLineBreaks
	}
	content = replaceLoneCR(content, pattern)
	warnings := []any{}
	if behavior == "classical" {
		var err error
		content, warnings, err = NormalizeClassicalLines(content)
		if err != nil {
			return "", nil, err
		}
	}
	if ruleFormat == "yaml" {
		var yamlWarnings []any
		content, yamlWarnings = NormalizeMihomoYAML(content)
		warnings = append(warnings, yamlWarnings...)
	}
	return content, warnings, nil
}

// replaceLoneCR is re.sub(r'\r(?!\n)|<others>', '\n', content) without
// look-ahead: a "\r" not followed by "\n" and the other separators become
// "\n".
func replaceLoneCR(content string, pattern *regexp.Regexp) string {
	all := pattern == textLineBreaks
	var b strings.Builder
	runes := []rune(content)
	for i, r := range runes {
		switch {
		case r == '\r' && (i+1 >= len(runes) || runes[i+1] != '\n'):
			b.WriteByte('\n')
		case all && (r == '\v' || r == '\f' || (r >= 0x1c && r <= 0x1e) || r == 0x85 || r == 0x2028 || r == 0x2029):
			b.WriteByte('\n')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type entry struct {
	line       int
	value      string
	start, end int
	isYAML     bool
}

var foreignPrefix = mustPyRegex(`^(?:full|domain|keyword|regexp|regex|ip):`, true)

func mustPyRegex(pattern string, ignoreCase bool) *rules.PyRegex {
	re, err := rules.CompilePyRegex(pattern, ignoreCase)
	if err != nil {
		panic(err)
	}
	return re
}

// validationError is ProfileValidationError(message).
func validationError(message string) error { return repo.NewValidationError(message) }

// InspectDomainContent is inspect_domain_content: a lossless, explicit
// repair proposal ({content, changes, errors[, warnings]}). content that is
// not a string is a ProfileValidationError; a *pyyaml.RecursionError is
// returned as is (Python does not catch it here).
func InspectDomainContent(contentValue, behavior, ruleFormat any) (*jsonx.Object, error) {
	content, ok := contentValue.(string)
	if !ok {
		return nil, validationError("规则内容必须是文本。")
	}
	errorsList, changes, warnings := []any{}, []any{}, []any{}
	result := jsonx.Obj("content", content, "changes", changes, "errors", errorsList)
	finish := func() *jsonx.Object {
		result.Set("changes", changes)
		result.Set("errors", errorsList)
		if behavior == "classical" {
			result.Set("warnings", warnings)
		}
		return result
	}
	if behavior == "classical" {
		result.Set("warnings", warnings)
	}
	if message := MetadataError(behavior, ruleFormat, "content"); message != "" {
		errorsList = append(errorsList, problemObject(0, nil, message))
		return finish(), nil
	}
	yamlPayload := rules.HasYAMLPayload(content)
	if ruleFormat == "text" && yamlPayload {
		errorsList = append(errorsList, problemObject(0, nil, "内容是 YAML payload，请选择 YAML 格式后保存。"))
		return finish(), nil
	}
	entries, err := domainEntries(content, ruleFormat == "yaml" || yamlPayload)
	if err != nil {
		var recursion *pyyaml.RecursionError
		if errors.As(err, &recursion) {
			return nil, err
		}
		errorsList = append(errorsList, problemObject(0, nil, err.Error()))
		return finish(), nil
	}
	type replacement struct {
		start, end int
		value      string
	}
	var replacements []replacement
	for _, e := range entries {
		value := e.value
		stripped := pyStrip(value)
		if stripped == "" || hasAnyPrefix(stripped, "#", "//") {
			continue
		}
		if behavior == "ipcidr" {
			if message := rules.CIDRError(value); message != "" {
				errorsList = append(errorsList, problemObject(e.line, &value, message))
			}
			continue
		}
		if behavior == "classical" && (strings.Contains(value, ",") || rules.IsRuleType(rules.PyUpper(stripped))) {
			if message := rules.ClassicalLineError(stripped); message != "" {
				errorsList = append(errorsList, problemObject(e.line, &value, message))
			}
			continue
		}
		updated, err := classicalOrDomain(behavior, value)
		if err != nil {
			if behavior == "classical" {
				address := stripped
				if strings.HasPrefix(stripped, "+.") {
					address = stripped[2:]
				}
				if !foreignPrefix.Search(stripped) && rules.LooksLikeIP(address) {
					errorsList = append(errorsList, problemObject(e.line, &value, "不是有效 IP 地址或 CIDR；请检查地址与前缀长度。"))
				} else {
					warnings = append(warnings, problemObject(e.line, &value, err.Error()))
				}
			} else {
				errorsList = append(errorsList, problemObject(e.line, nil, err.Error()))
			}
			continue
		}
		if updated != value {
			changes = append(changes, jsonx.Obj("line", jsonx.Int(int64(e.line)), "before", value, "after", updated))
			text := updated
			if e.isYAML {
				text = jsonDumps(updated, false)
			}
			replacements = append(replacements, replacement{e.start, e.end, text})
		}
	}
	if len(replacements) > 0 {
		runes := []rune(content)
		var b strings.Builder
		cursor := 0
		for _, r := range replacements {
			b.WriteString(string(runes[cursor:r.start]))
			b.WriteString(r.value)
			cursor = r.end
		}
		b.WriteString(string(runes[cursor:]))
		result.Set("content", b.String())
	}
	return finish(), nil
}

func domainEntries(content string, asYAML bool) ([]entry, error) {
	var entries []entry
	if asYAML {
		root, err := pyyaml.Compose(content)
		if err != nil {
			return nil, err
		}
		if root == nil || root.Kind != pyyaml.MappingNode {
			return nil, valueError("YAML 规则需要 payload 列表。")
		}
		pairs := payloadPairs(root)
		if len(pairs) != 1 || pairs[0][1].Kind != pyyaml.SequenceNode {
			return nil, valueError("YAML 规则需要唯一的 payload 列表。")
		}
		key, payload := pairs[0][0], pairs[0][1]
		if payload.Start.Index < key.End.Index {
			return nil, valueError("请将 payload 的 YAML 别名展开后再编辑。")
		}
		var anchors []int
		if strings.Contains(content, "&") {
			tokens, err := pyyaml.Scan(content)
			if err != nil {
				return nil, err
			}
			for _, token := range tokens {
				if token.Kind == pyyaml.TokenAnchor {
					anchors = append(anchors, token.Start.Index)
				}
			}
		}
		seen := map[*pyyaml.Node]bool{}
		for _, node := range payload.Items {
			i := sort.SearchInts(anchors, node.Start.Index)
			hasAnchor := i < len(anchors) && anchors[i] < node.End.Index
			if node.Kind != pyyaml.ScalarNode || node.Tag != pyyaml.TagStr || node.Start.Index < payload.Start.Index ||
				seen[node] || node.Style == '|' || node.Style == '>' || hasAnchor {
				return nil, valueError("payload 每项应为一行文本；请先展开 YAML 锚点、别名或多行内容。")
			}
			seen[node] = true
			entries = append(entries, entry{node.Start.Line + 1, node.Value, node.Start.Index, node.End.Index, true})
		}
		return entries, nil
	}
	offset := 0
	for number, raw := range splitLines(content, true) {
		line := raw
		if offset == 0 && strings.HasPrefix(raw, "\uFEFF") {
			line = raw[len("\uFEFF"):]
		}
		value := pyStrip(line)
		if value != "" && !hasAnyPrefix(value, "#", "//") {
			start := offset + runeLen(raw) - runeLen(pyLstrip(line))
			entries = append(entries, entry{number + 1, value, start, start + runeLen(value), false})
		}
		offset += runeLen(raw)
	}
	return entries, nil
}

// ValidateDomainContent is validate_domain_content(rule, previous); previous
// nil is None.
func ValidateDomainContent(rule, previous *jsonx.Object) error {
	if previous != nil && previous.Len() > 0 {
		same := true
		for _, key := range []string{"source_type", "behavior", "content", "format"} {
			if !jsonx.Equal(rule.Value(key), previous.Value(key)) {
				same = false
				break
			}
		}
		if same {
			return nil
		}
	}
	behavior := rule.GetOr("behavior", "domain")
	if message := MetadataError(behavior, rule.Value("format"), rule.GetOr("source_type", "url")); message != "" {
		return validationError(message)
	}
	if rule.Value("source_type") != "content" {
		return nil
	}
	checked, err := InspectDomainContent(rule.GetOr("content", ""), behavior, rule.Value("format"))
	if err != nil {
		return err
	}
	if problems := checked.GetList("errors"); len(problems) > 0 {
		first := problems[0].(*jsonx.Object)
		line := first.GetInt("line")
		if line != 0 {
			return validationError(fmt.Sprintf("规则内容第 %d 行：%s", line, first.GetString("message")))
		}
		return validationError(first.GetString("message"))
	}
	return nil
}

// RemoteRuleValidationError is RemoteRuleValidationError: downloaded rules
// did not match their declared type or representation. It is a
// ProfileValidationError (errors.Is(err, repo.ErrValidation)).
type RemoteRuleValidationError struct {
	Message    string
	Errors     []any // of {line, before, message}
	Behavior   any   // nil for the message-only form
	RuleFormat any
}

func (e *RemoteRuleValidationError) Error() string { return e.Message }

// Unwrap makes the error a ProfileValidationError.
func (e *RemoteRuleValidationError) Unwrap() error { return repo.NewValidationError(e.Message) }

// NewRemoteRuleMessage is RemoteRuleValidationError(message).
func NewRemoteRuleMessage(message string) *RemoteRuleValidationError {
	return &RemoteRuleValidationError{Message: message,
		Errors: []any{jsonx.Obj("line", jsonx.Int(0), "before", "", "message", message)}}
}

// NewRemoteRuleError is RemoteRuleValidationError(rule, rule_format, errors).
func NewRemoteRuleError(rule *jsonx.Object, ruleFormat any, problems []any) *RemoteRuleValidationError {
	e := &RemoteRuleValidationError{Errors: problems, Behavior: rule.GetOr("behavior", "domain"), RuleFormat: ruleFormat}
	name := rule.Value("name")
	if !jsonx.Truthy(name) {
		name = rule.Value("id")
	}
	if !jsonx.Truthy(name) {
		name = "未命名规则集"
	}
	first := problems[0].(*jsonx.Object)
	location := "文件"
	if line := first.Value("line"); jsonx.Truthy(line) {
		location = "第 " + jsonx.Str(line) + " 行"
	}
	original := jsonx.Str(first.GetOr("before", ""))
	preview := original
	if runes := []rune(original); len(runes) > 200 {
		preview = string(runes[:200]) + "…"
	}
	detail := ""
	if original != "" {
		detail = "，原文 " + jsonx.Repr(preview)
	}
	remainder := ""
	if len(problems) > 1 {
		remainder = fmt.Sprintf("（共 %d 处错误）", len(problems))
	}
	e.Message = "远端规则集「" + jsonx.Str(name) + "」声明为 " + jsonx.Str(e.Behavior) + "/" + jsonx.Str(ruleFormat) +
		"，" + location + detail + "：" + jsonx.Str(first.Value("message")) + remainder
	return e
}

func remoteProblem(line int, before, message string) *jsonx.Object {
	return jsonx.Obj("line", jsonx.Int(int64(line)), "before", before, "message", message)
}

type textEntry struct {
	line  int
	value string
}

// remoteTextEntries is _remote_text_entries.
func remoteTextEntries(content string, ruleFormat any) ([]textEntry, []any) {
	sourceLines := splitLines(content, false)
	var problems []any
	problem := func(line int, message string) {
		before := ""
		if line > 0 && line <= len(sourceLines) {
			before = sourceLines[line-1]
		}
		problems = append(problems, remoteProblem(line, before, message))
	}
	if ruleFormat != "yaml" {
		if rules.HasYAMLPayload(content) {
			first := 1
			for i, value := range sourceLines {
				if strings.TrimLeft(value, "\uFEFF \t") != "" && !hasAnyPrefix(pyLstrip(value), "#", "//") {
					first = i + 1
					break
				}
			}
			problem(first, "声明为 text，但内容是 YAML payload；请检查订阅链接或选择 YAML 格式。")
			return nil, problems
		}
		entries := make([]textEntry, 0, len(sourceLines))
		for i, value := range sourceLines {
			if i == 0 {
				value = strings.TrimPrefix(value, "\uFEFF")
			}
			entries = append(entries, textEntry{i + 1, value})
		}
		return entries, problems
	}
	fail := func(err error) ([]textEntry, []any) {
		line, message := 0, err.Error()
		var marked *pyyaml.Error
		if errors.As(err, &marked) && marked.Marked() {
			if marked.ProblemMark != nil {
				line = marked.ProblemMark.Line + 1
			}
			if marked.Problem != "" {
				message = marked.Problem
			}
		}
		problem(line, "YAML 格式无效："+message)
		return nil, problems
	}
	root, err := pyyaml.Compose(content)
	if err != nil {
		return fail(err)
	}
	if root == nil || root.Kind != pyyaml.MappingNode {
		problem(1, "声明为 YAML，但文件需要包含 payload 列表；请检查订阅链接或文件格式。")
		return nil, problems
	}
	pairs := payloadPairs(root)
	if len(pairs) != 1 {
		line := 1
		if len(pairs) > 1 {
			line = pairs[1][0].Start.Line + 1
		}
		problem(line, "YAML 规则需要唯一的 payload 列表。")
		return nil, problems
	}
	payload := pairs[0][1]
	if payload.Kind != pyyaml.SequenceNode {
		problem(payload.Start.Line+1, "YAML payload 必须是列表。")
		return nil, problems
	}
	tokens, err := pyyaml.Scan(content)
	if err != nil {
		return fail(err)
	}
	for _, token := range tokens {
		if token.Kind == pyyaml.TokenAnchor || token.Kind == pyyaml.TokenAlias {
			problem(token.Start.Line+1, "远端 YAML 规则不支持锚点或别名；请使用展开后的 payload 列表。")
		}
	}
	var entries []textEntry
	for _, node := range payload.Items {
		number := node.Start.Line + 1
		if !singleLineStr(node) {
			problem(number, "payload 每项必须是单行文本；不接受数字、布尔值、嵌套结构或多行内容。")
			continue
		}
		entries = append(entries, textEntry{number, node.Value})
	}
	return entries, problems
}

// ValidateRemoteRuleContent is validate_remote_rule_content(content, rule,
// config_data, rule_format=ruleFormat): content is string or []byte (other
// values fail like Python); config nil is None; ruleFormat "" is None. It
// never returns a normalized replacement.
func ValidateRemoteRuleContent(content any, rule, config *jsonx.Object, ruleFormat string) error {
	behavior := rule.GetOr("behavior", "domain")
	declared := rule.Value("format")
	var selected any = ruleFormat
	if ruleFormat == "" {
		if config == nil {
			config = jsonx.NewObject()
		}
		format, err := convert.RulesetFormat(rule, config, "")
		if err != nil {
			return err
		}
		selected = format
	}
	message := MetadataError(behavior, declared, "url")
	if message == "" {
		message = MetadataError(behavior, selected, "url")
	}
	if message != "" {
		return NewRemoteRuleError(rule, selected, []any{remoteProblem(0, "", message)})
	}
	digest := ContentDigest(content)
	key := digest + "\x00" + jsonx.Repr(behavior) + "\x00" + jsonx.Repr(selected)
	if digest != "" {
		if _, ok := validatedCache.Get(key); ok {
			return nil
		}
	}
	if err := validateRemoteContent(content, rule, behavior, selected); err != nil {
		return err
	}
	if digest != "" {
		validatedCache.Put(key, true, 0)
	}
	return nil
}

const textLineBreakChars = "\n\r\v\f\x1c\x1d\x1e\u0085  "

func validateRemoteContent(content any, rule *jsonx.Object, behavior, selected any) error {
	if selected == "mrs" {
		return validateRemoteMRS(content, rule)
	}
	if data, ok := content.([]byte); ok {
		if start, end, bad := utf8ErrorRange(data); bad {
			prefix := string(data[:start])
			line := len(splitLines(prefix, false))
			if prefix == "" || strings.ContainsRune(textLineBreakChars, []rune(prefix)[runeLen(prefix)-1]) {
				line++
			}
			if line < 1 {
				line = 1
			}
			return NewRemoteRuleError(rule, selected, []any{remoteProblem(line, bytesRepr(data[start:end]),
				"内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。")})
		}
		content = string(data)
	}
	text, ok := content.(string)
	if !ok {
		return NewRemoteRuleError(rule, selected, []any{remoteProblem(0, "", "规则内容必须是文本或字节。")})
	}
	entries, problems := remoteTextEntries(text, selected)
	for _, e := range entries {
		value := pyStrip(e.value)
		if value == "" || hasAnyPrefix(value, "#", "//") {
			continue
		}
		message := ""
		switch {
		case behavior == "ipcidr":
			message = rules.CIDRError(value)
		case behavior == "classical" && (strings.Contains(value, ",") || rules.IsRuleType(rules.PyUpper(value))):
			message = rules.ClassicalLineError(value)
		default:
			if _, err := classicalOrDomain(behavior, value); err != nil {
				message = strings.ReplaceAll(err.Error(), "，已保留原文", "")
			}
		}
		if message != "" {
			problems = append(problems, remoteProblem(e.line, e.value, message))
		}
	}
	if len(problems) > 0 {
		sort.SliceStable(problems, func(i, j int) bool {
			a := problems[i].(*jsonx.Object).GetInt("line")
			b := problems[j].(*jsonx.Object).GetInt("line")
			return a < b
		})
		return NewRemoteRuleError(rule, selected, problems)
	}
	return nil
}

// ReadRulePayload is read_rule_payload: the payload lines of YAML (with its
// sequence node for a lossless append) or the text lines (node nil).
func ReadRulePayload(content string) ([]string, *pyyaml.Node, error) {
	if !rules.HasYAMLPayload(content) {
		lines := splitLines(content, false)
		if lines == nil {
			lines = []string{}
		}
		return lines, nil, nil
	}
	invalid := validationError("规则集的 payload 格式无法读取，请先在规则仓库检查内容。")
	node, err := pyyaml.Compose(content)
	if err != nil {
		return nil, nil, invalid
	}
	document, err := yamlx.SafeLoad(content)
	var nonString *yamlx.NonStringKeyError
	if errors.As(err, &nonString) {
		document, err = payloadDocument(node)
	}
	if err != nil {
		return nil, nil, invalid
	}
	object, isObject := document.(*jsonx.Object)
	payloadValue, _ := object.Get("payload")
	items, isList := payloadValue.([]any)
	if !isObject || !isList || node == nil || node.Kind != pyyaml.MappingNode {
		return nil, nil, invalid
	}
	var key, payload *pyyaml.Node
	for _, pair := range node.Pairs {
		if isPayloadKey(pair[0]) {
			key, payload = pair[0], pair[1]
			break
		}
	}
	if key == nil || payload.Start.Index < key.End.Index {
		return nil, nil, invalid
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, nil, invalid
		}
		lines = append(lines, text)
	}
	return lines, payload, nil
}

// AppendRulePayload is append_rule_payload.
func AppendRulePayload(content, line string, payload *pyyaml.Node) string {
	return AppendRuleLines(content, []string{line}, payload)
}

// AppendRuleLines is append_rule_lines: insert into the existing sequence,
// preserving comments and surrounding YAML.
func AppendRuleLines(content string, lines []string, payload *pyyaml.Node) string {
	if len(lines) == 0 {
		return content
	}
	if payload == nil {
		separator := ""
		if content != "" && !strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		return content + separator + strings.Join(lines, "\n") + "\n"
	}
	quoted := make([]string, len(lines))
	for i, line := range lines {
		quoted[i] = jsonDumps(line, true)
	}
	runes := []rune(content)
	if payload.FlowStyle {
		index := payload.End.Index - 1
		separator := ""
		if len(payload.Items) > 0 {
			index = payload.Items[len(payload.Items)-1].End.Index
			separator = ", "
		}
		return string(runes[:index]) + separator + strings.Join(quoted, ", ") + string(runes[index:])
	}
	index := payload.End.Index
	prefix, suffix := string(runes[:index]), string(runes[index:])
	indent := strings.Repeat(" ", payload.Start.Column)
	var b strings.Builder
	b.WriteString(prefix)
	if !strings.HasSuffix(prefix, "\n") {
		b.WriteString("\n")
	}
	for _, line := range quoted {
		b.WriteString(indent + "- " + line + "\n")
	}
	b.WriteString(suffix)
	return b.String()
}

// RemoveRulePayload is remove_rule_payload: remove matching entries, keeping
// YAML valid for a later add or reload.
func RemoveRulePayload(content string, matches func(string) bool) (string, []string, error) {
	lines, payload, err := ReadRulePayload(content)
	if err != nil {
		return "", nil, err
	}
	removed := []string{}
	var selected []int
	for i, line := range lines {
		if matches(line) {
			removed = append(removed, line)
			selected = append(selected, i)
		}
	}
	if len(removed) == 0 {
		return content, removed, nil
	}
	if payload == nil {
		var b strings.Builder
		for _, raw := range splitLines(content, true) {
			if !matches(pyStrip(raw)) {
				b.WriteString(raw)
			}
		}
		return b.String(), removed, nil
	}
	runes := []rune(content)
	if len(selected) == len(lines) {
		return string(runes[:payload.Start.Index]) + "[]\n" + string(runes[payload.End.Index:]), removed, nil
	}
	if payload.FlowStyle {
		for k := len(selected) - 1; k >= 0; k-- {
			index := selected[k]
			_, current, err := ReadRulePayload(content)
			if err != nil {
				return "", nil, err
			}
			runes = []rune(content)
			node := current.Items[index]
			var start, end int
			if index+1 < len(current.Items) {
				start, end = node.Start.Index, current.Items[index+1].Start.Index
			} else {
				start, end = current.Items[index-1].End.Index, node.End.Index
			}
			content = string(runes[:start]) + string(runes[end:])
		}
		return content, removed, nil
	}
	for k := len(selected) - 1; k >= 0; k-- {
		node := payload.Items[selected[k]]
		start := lastIndexRune(runes, '\n', node.Start.Index) + 1
		end := indexRune(runes, '\n', node.End.Index)
		if end >= 0 {
			end++
		} else {
			end = len(runes)
		}
		runes = append(append([]rune{}, runes[:start]...), runes[end:]...)
	}
	return string(runes), removed, nil
}

// lastIndexRune is str.rfind(ch, 0, end).
func lastIndexRune(runes []rune, ch rune, end int) int {
	if end > len(runes) {
		end = len(runes)
	}
	for i := end - 1; i >= 0; i-- {
		if runes[i] == ch {
			return i
		}
	}
	return -1
}

// indexRune is str.find(ch, start).
func indexRune(runes []rune, ch rune, start int) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == ch {
			return i
		}
	}
	return -1
}

// payloadDocument stands in for yaml.safe_load when the document has a
// mapping key jsonx cannot hold (`1: x`): a mapping root yields an object
// with the last "payload" entry, whose sequence items are their scalar text
// when tagged str (other items are kept as nil, failing the str check).
// Merge keys are not applied here.
func payloadDocument(root *pyyaml.Node) (any, error) {
	if root == nil || root.Kind != pyyaml.MappingNode {
		return nil, errors.New("not a mapping")
	}
	document := jsonx.NewObject()
	for _, pair := range root.Pairs {
		if !isPayloadKey(pair[0]) || pair[0].Tag != pyyaml.TagStr {
			continue
		}
		if pair[1].Kind != pyyaml.SequenceNode {
			document.Set("payload", nil)
			continue
		}
		items := []any{}
		for _, item := range pair[1].Items {
			if item.Kind == pyyaml.ScalarNode && item.Tag == pyyaml.TagStr {
				items = append(items, item.Value)
			} else {
				items = append(items, nil)
			}
		}
		document.Set("payload", items)
	}
	return document, nil
}
