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
	"configflow/internal/pystr"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
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

func isPayloadKey(key *yamlx.Node) bool {
	key = yamlx.Resolve(key)
	return key != nil && key.Kind == yaml.ScalarNode && key.ShortTag() == "!!str" && key.Value == "payload"
}

// payloadPairs returns direct pairs only: editing an inherited payload would
// unexpectedly change its merge source.
func payloadPairs(root *yamlx.Node) [][2]*yamlx.Node {
	var pairs [][2]*yamlx.Node
	if root == nil || root.Kind != yaml.MappingNode {
		return pairs
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if isPayloadKey(root.Content[i]) {
			pairs = append(pairs, [2]*yamlx.Node{root.Content[i], root.Content[i+1]})
		}
	}
	return pairs
}

func singleLineStr(node *yamlx.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" && node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 && !strings.ContainsAny(node.Value, "\n\r")
}

// walkRuleNodes visits each physical node once, without expanding aliases.
func walkRuleNodes(root *yamlx.Node, visit func(*yamlx.Node)) {
	seen := map[*yamlx.Node]bool{}
	var walk func(*yamlx.Node)
	walk = func(n *yamlx.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		visit(n)
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(root)
}

// NormalizeMihomoYAML expands flow payloads for Mihomo's line-oriented reader.
// Already-block documents and every refused input retain their original bytes.
func NormalizeMihomoYAML(content string) (string, []any) {
	result, err := normalizeMihomoYAML(content)
	if err != nil {
		return content, []any{problemObject(0, nil, err.Error())}
	}
	return result, []any{}
}

func normalizeMihomoYAML(content string) (string, error) {
	root, err := yamlx.Parse([]byte(content))
	if err != nil {
		return "", err
	}
	if root.Kind != yaml.MappingNode {
		return "", errors.New(msgYAMLPayload)
	}
	var anchor bool
	walkRuleNodes(root, func(n *yamlx.Node) { anchor = anchor || n.Anchor != "" || n.Kind == yaml.AliasNode })
	if anchor {
		return "", errors.New(msgDeliveryAliases)
	}
	pairs := payloadPairs(root)
	if len(pairs) != 1 || pairs[0][1].Kind != yaml.SequenceNode {
		return "", errors.New(msgUniquePayload)
	}
	payload := pairs[0][1]
	for _, node := range payload.Content {
		if !singleLineStr(node) {
			return "", errors.New(msgDeliveryItem)
		}
	}
	if len(payload.Content) == 0 || root.Style&yaml.FlowStyle == 0 && payload.Style&yaml.FlowStyle == 0 {
		return content, nil
	}
	if root.Style&yaml.FlowStyle != 0 {
		var invalid error
		walkRuleNodes(root, func(n *yamlx.Node) {
			if n.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(n.Content); i += 2 {
					if n.Content[i].Kind != yaml.ScalarNode {
						invalid = errors.New(msgComplexMapping)
					}
				}
			}
			if n.Kind == yaml.ScalarNode && (n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.ContainsAny(n.Value, "\n\r")) {
				invalid = errors.New(msgMultilineContent)
			}
		})
		if invalid != nil {
			return "", invalid
		}
	}
	edited, err := editRuleYAML(content, func(doc *yamlx.Node) error {
		p := payloadPairs(doc)[0][1]
		if doc.Style&yaml.FlowStyle != 0 {
			walkRuleNodes(doc, func(n *yamlx.Node) { n.Style &^= yaml.FlowStyle })
		} else {
			p.Style &^= yaml.FlowStyle
		}
		return nil
	})
	return string(edited), err
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
	node       *yamlx.Node
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
// not a string is a ProfileValidationError. Native YAML policy failures are
// reported without changing the original text.
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
		errorsList = append(errorsList, problemObject(0, nil, err.Error()))
		return finish(), nil
	}
	type replacement struct {
		start, end int
		value      string
	}
	var replacements []replacement
	yamlChanges := map[[2]int]string{}
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
		if behavior == "classical" && (strings.Contains(value, ",") || rules.IsRuleType(pystr.Upper(stripped))) {
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
			if e.isYAML {
				yamlChanges[[2]int{e.node.Line, e.node.Column}] = updated
			} else {
				replacements = append(replacements, replacement{e.start, e.end, updated})
			}
		}
	}
	if len(yamlChanges) > 0 {
		edited, err := editRuleYAML(content, func(root *yamlx.Node) error {
			for _, node := range payloadPairs(root)[0][1].Content {
				if value, ok := yamlChanges[[2]int{node.Line, node.Column}]; ok {
					node.Value = value
					node.Tag = "!!str"
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		result.Set("content", string(edited))
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
		root, err := yamlx.Parse([]byte(content))
		if err != nil {
			return nil, err
		}
		if root.Kind != yaml.MappingNode {
			return nil, errors.New(msgYAMLPayload)
		}
		pairs := payloadPairs(root)
		if len(pairs) != 1 {
			return nil, errors.New(msgUniquePayload)
		}
		payload := pairs[0][1]
		if payload.Kind == yaml.AliasNode {
			return nil, errors.New(msgEditPayloadAlias)
		}
		if payload.Kind != yaml.SequenceNode {
			return nil, errors.New(msgUniquePayload)
		}
		for _, node := range payload.Content {
			if !singleLineStr(node) || node.Anchor != "" {
				return nil, errors.New(msgEditPayloadItem)
			}
			entries = append(entries, entry{line: node.Line, value: node.Value, isYAML: true, node: node})
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
			entries = append(entries, entry{line: number + 1, value: value, start: start, end: start + runeLen(value)})
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
		var marked *yamlx.SyntaxError
		if errors.As(err, &marked) {
			line = marked.Line
			if marked.Problem != "" {
				message = marked.Problem
			}
		}
		problem(line, "YAML 格式无效："+message)
		return nil, problems
	}
	root, err := yamlx.Parse([]byte(content))
	if err != nil {
		return fail(err)
	}
	if root.Kind != yaml.MappingNode {
		problem(1, msgRemotePayload)
		return nil, problems
	}
	pairs := payloadPairs(root)
	if len(pairs) != 1 {
		problem(1, msgUniquePayload)
		return nil, problems
	}
	payload := pairs[0][1]
	walkRuleNodes(root, func(n *yamlx.Node) {
		if n.Anchor != "" || n.Kind == yaml.AliasNode {
			problem(n.Line, msgRemoteAliases)
		}
	})
	if payload.Kind != yaml.SequenceNode {
		if payload.Kind != yaml.AliasNode {
			problem(payload.Line, msgPayloadList)
		}
		return nil, problems
	}
	var entries []textEntry
	for _, node := range payload.Content {
		if node.Kind == yaml.AliasNode {
			continue
		}
		if !singleLineStr(node) {
			problem(node.Line, msgRemoteItem)
			continue
		}
		entries = append(entries, textEntry{node.Line, node.Value})
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
		case behavior == "classical" && (strings.Contains(value, ",") || rules.IsRuleType(pystr.Upper(value))):
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
// sequence node for comment-preserving edits) or the text lines (node nil).
func ReadRulePayload(content string) ([]string, *yamlx.Node, error) {
	if !rules.HasYAMLPayload(content) {
		lines := splitLines(content, false)
		if lines == nil {
			lines = []string{}
		}
		return lines, nil, nil
	}
	root, err := yamlx.Parse([]byte(content))
	if err != nil {
		return nil, nil, &repo.Error{Kind: repo.ErrValidation, Message: fmt.Sprintf(msgReadPayloadYAML, err), Cause: err}
	}
	pairs := payloadPairs(root)
	if len(pairs) != 1 || pairs[0][1].Kind != yaml.SequenceNode {
		return nil, nil, validationError(msgUnreadablePayload)
	}
	payload := pairs[0][1]
	lines := make([]string, 0, len(payload.Content))
	for _, item := range payload.Content {
		item = yamlx.Resolve(item)
		if item == nil || item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return nil, nil, validationError(msgUnreadablePayload)
		}
		lines = append(lines, item.Value)
	}
	return lines, payload, nil
}

// AppendRulePayload is append_rule_payload.
func AppendRulePayload(content, line string, payload *yamlx.Node) string {
	return AppendRuleLines(content, []string{line}, payload)
}

// AppendRuleLines is append_rule_lines: insert into the existing sequence,
// preserving comments and surrounding YAML.
func AppendRuleLines(content string, lines []string, payload *yamlx.Node) string {
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
	edited, err := editRuleYAML(content, func(root *yamlx.Node) error {
		pairs := payloadPairs(root)
		if len(pairs) != 1 || pairs[0][1].Kind != yaml.SequenceNode {
			return validationError(msgUnreadablePayload)
		}
		for _, line := range lines {
			pairs[0][1].Content = append(pairs[0][1].Content, yamlx.Str(line))
		}
		return nil
	})
	if err != nil {
		return content
	}
	return string(edited)
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
	chosen := make(map[int]bool, len(selected))
	for _, index := range selected {
		chosen[index] = true
	}
	edited, err := editRuleYAML(content, func(root *yamlx.Node) error {
		sequence := payloadPairs(root)[0][1]
		kept := make([]*yamlx.Node, 0, len(sequence.Content)-len(selected))
		for i, node := range sequence.Content {
			if !chosen[i] {
				kept = append(kept, node)
			}
		}
		sequence.Content = kept
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return string(edited), removed, nil
}

// ReadRulePayloadAny and AppendRulePayloadAny adapt ReadRulePayload and
// AppendRulePayload to rules.ConnectionDeps (payload is a *yamlx.Node or
// nil).
func ReadRulePayloadAny(content string) ([]string, any, error) {
	lines, node, err := ReadRulePayload(content)
	if node == nil {
		return lines, nil, err
	}
	return lines, node, err
}

// AppendRulePayloadAny: see ReadRulePayloadAny.
func AppendRulePayloadAny(content, line string, payload any) string {
	node, _ := payload.(*yamlx.Node)
	return AppendRulePayload(content, line, node)
}
