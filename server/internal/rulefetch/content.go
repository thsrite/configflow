package rulefetch

// Port of backend/utils/ruleset_content.py: inspect editable providers,
// normalize safe Classical delivery syntax, validate remote responses and
// edit payload lists with splice-first source preservation.

import (
	"bytes"
	"configflow/internal/textx"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/rulesetfmt"
	"configflow/internal/yamlx"
	"encoding/json"
	"slices"
	"strconv"
	"unicode/utf8"

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

func dataDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

const (
	normalizedCacheMaxBytes   = 1 * 1024 * 1024
	normalizedCacheMaxItemLen = 512 * 1024
)

var (
	validatedCache  = NewResultCache(1024, 0)
	normalizedCache = NewResultCache(256, normalizedCacheMaxBytes)
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

func classicalOrDomain(behavior string, value string) (string, error) {
	if behavior == "classical" {
		return rules.ClassicalLine(value)
	}
	return domainLine(value)
}

// MetadataError is _metadata_error(behavior, rule_format, source_type) ("" for
// None). Pass nil for a missing format.
func metadataError(behavior, ruleFormat, sourceType string) string {
	if sourceType != "content" && sourceType != "url" {
		return "规则来源必须是 content 或 url。"
	}
	if behavior != "domain" && behavior != "ipcidr" && behavior != "classical" {
		return "规则集类型必须是 domain、ipcidr 或 classical。"
	}
	if ruleFormat != "" && ruleFormat != "text" && ruleFormat != "yaml" && ruleFormat != "mrs" {
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

// Problem is one line-numbered rule-content diagnostic.
type Problem struct {
	Line    int     `json:"line"`
	Before  *string `json:"before,omitempty"`
	Message string  `json:"message"`
}

// Change is one explicit, reviewable repair proposal.
type Change struct {
	Line   int    `json:"line"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Inspection keeps the original content unless an explicit repair is available.
type Inspection struct {
	Content  string     `json:"content"`
	Changes  []Change   `json:"changes"`
	Errors   []Problem  `json:"errors"`
	Warnings *[]Problem `json:"warnings,omitempty"`
}

func problemObject(line int, before *string, message string) Problem {
	return Problem{line, before, message}
}
func strp(s string) *string { return &s }
func cloneProblems(problems []Problem) []Problem {
	result := slices.Clone(problems)
	for i := range result {
		if result[i].Before != nil {
			value := *result[i].Before
			result[i].Before = &value
		}
	}
	return result
}

// NormalizeClassical returns delivery-only normalization and its diagnostics.
func NormalizeClassical(content string) (string, []Problem, error) {
	checked, err := Inspect(content, "classical", "")
	if err != nil {
		return "", nil, err
	}
	warnings := append(append([]Problem{}, (*checked.Warnings)...), checked.Errors...)
	return checked.Content, warnings, nil
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
func NormalizeYAML(content string) (string, []Problem) {
	result, err := normalizeMihomoYAML(content)
	if err != nil {
		return content, []Problem{problemObject(0, nil, err.Error())}
	}
	return result, []Problem{}
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
	source := yamlx.NewSource([]byte(content))
	for _, node := range payload.Content {
		if !singleLineStr(node) || !source.IsSingleLineScalar(node) {
			return "", errors.New(msgDeliveryItem)
		}
	}
	if len(payload.Content) == 0 || root.Style&yaml.FlowStyle == 0 && payload.Style&yaml.FlowStyle == 0 {
		return content, nil
	}
	if root.Style&yaml.FlowStyle != 0 {
		var invalid error
		keys := map[*yamlx.Node]bool{}
		walkRuleNodes(root, func(n *yamlx.Node) {
			if n.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(n.Content); i += 2 {
					keys[n.Content[i]] = true
					if n.Content[i].Kind != yaml.ScalarNode {
						invalid = errors.New(msgComplexMapping)
					}
				}
			}
			if n.Kind == yaml.ScalarNode && (n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.ContainsAny(n.Value, "\n\r") || !keys[n] && !source.IsSingleLineScalar(n)) {
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
	warnings []Problem
}

// NormalizeMihomoRuleset is normalize_mihomo_ruleset: delivery-only syntax
// and YAML-layout compatibility, cached by content digest.
func NormalizeRuleset(content, behavior, ruleFormat string) (string, []Problem, error) {
	if ruleFormat == "mrs" {
		return content, []Problem{}, nil
	}
	key := dataDigest([]byte(content)) + "\x00" + behavior + "\x00" + ruleFormat
	if cached, ok := normalizedCache.Get(key); ok {
		entry := cached.(normalized)
		return entry.content, cloneProblems(entry.warnings), nil
	}
	result, warnings, err := normalizeMihomoRuleset(content, behavior, ruleFormat)
	if err != nil {
		return "", nil, err
	}
	if len(result) <= normalizedCacheMaxItemLen {
		normalizedCache.Put(key, normalized{result, cloneProblems(warnings)}, len(result))
	}
	return result, warnings, nil
}

var (
	textLineBreaks = regexp.MustCompile("\r([^\n]|$)|[\v\f\x1c-\x1e\u0085  ]")
	crOnly         = regexp.MustCompile("\r([^\n]|$)")
)

func normalizeMihomoRuleset(content, behavior, ruleFormat string) (string, []Problem, error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	pattern := crOnly
	if ruleFormat == "text" {
		pattern = textLineBreaks
	}
	content = replaceLoneCR(content, pattern)
	warnings := []Problem{}
	if behavior == "classical" {
		var err error
		content, warnings, err = NormalizeClassical(content)
		if err != nil {
			return "", nil, err
		}
	}
	if ruleFormat == "yaml" {
		var yamlWarnings []Problem
		content, yamlWarnings = NormalizeYAML(content)
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
func Inspect(content, behavior, ruleFormat string) (*Inspection, error) {
	errorsList, changes, warnings := []Problem{}, []Change{}, []Problem{}
	result := &Inspection{Content: content, Changes: changes, Errors: errorsList}
	finish := func() *Inspection {
		result.Changes = changes
		result.Errors = errorsList
		if behavior == "classical" {
			result.Warnings = &warnings
		}
		return result
	}
	if message := metadataError(behavior, ruleFormat, "content"); message != "" {
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
		if behavior == "classical" && (strings.Contains(value, ",") || rules.IsRuleType(strings.ToUpper(stripped))) {
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
			changes = append(changes, Change{e.line, value, updated})
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
		result.Content = string(edited)
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
		result.Content = b.String()
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
	for number, raw := range textx.SplitLines(content, true) {
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

// Declaration is the source of a rule declaration as sent by an editor. The
// plain rule record cannot hold a source, behavior, format or content of the
// wrong JSON type (O18 T1c), so those members are checked as sent, with
// Python's messages.
type Declaration struct {
	SourceType jsondecode.Member[string] `json:"source_type"`
	Behavior   jsondecode.Member[string] `json:"behavior"`
	Format     jsondecode.Member[string] `json:"format"`
	Content    jsondecode.Member[string] `json:"content"`
}

// DeclarationOf is the declaration of a stored rule.
func DeclarationOf(rule model.RuleConfig) Declaration {
	member := func(value *string) jsondecode.Member[string] {
		if value == nil {
			return jsondecode.Member[string]{}
		}
		raw, _ := json.Marshal(*value)
		return jsondecode.Member[string]{Raw: raw, Value: *value, OK: true}
	}
	return Declaration{SourceType: member(rule.SourceType), Behavior: member(rule.Behavior), Format: member(rule.Format), Content: member(rule.Content)}
}

// ValidateContent validates an edited declaration (validate_domain_content);
// previous nil is a new one. Unchanged stored legacy values stay readable.
func ValidateContent(rule model.RuleConfig, previous *model.RuleConfig) error {
	var before *Declaration
	if previous != nil {
		before = new(DeclarationOf(*previous))
	}
	return ValidateDeclaration(DeclarationOf(rule), before)
}

// ValidateContentJSON is ValidateDeclaration on declarations as JSON objects.
func ValidateContentJSON(raw, previous json.RawMessage) error {
	var sent Declaration
	if err := jsondecode.Unmarshal(raw, &sent); err != nil {
		return err
	}
	var before *Declaration
	if previous != nil {
		before = new(Declaration)
		_ = jsondecode.Unmarshal(previous, before)
	}
	return ValidateDeclaration(sent, before)
}

// ValidateDeclaration validates a sent declaration; previous is the stored
// declaration (nil for a new one), whose unchanged members are grandfathered.
func ValidateDeclaration(sent Declaration, previous *Declaration) error {
	if previous != nil && sameDeclaration(sent, *previous) {
		return nil
	}
	behavior, format, source := sent.behavior(), textOrMember(sent.Format, ""), textOrMember(sent.SourceType, "url")
	for _, field := range []struct {
		name   string
		value  jsondecode.Member[string]
		target *string
	}{{"source_type", sent.SourceType, &source}, {"behavior", sent.Behavior, &behavior}, {"format", sent.Format, &format}} {
		if !field.value.Present() || field.name == "format" && field.value.Null() {
			continue
		}
		if !field.value.OK {
			*field.target = "\x00"
			return validationError(metadataError(behavior, format, source))
		}
	}
	if message := metadataError(behavior, format, source); message != "" {
		return validationError(message)
	}
	if source != "content" {
		return nil
	}
	if sent.Content.Present() && !sent.Content.OK {
		return validationError(msgContentText)
	}
	checked, err := Inspect(sent.Content.Value, behavior, format)
	if err != nil {
		return err
	}
	if len(checked.Errors) > 0 {
		first := checked.Errors[0]
		if first.Line != 0 {
			return validationError(fmt.Sprintf(msgContentLine, first.Line, first.Message))
		}
		return validationError(first.Message)
	}
	return nil
}

// behavior is the declared behavior: "domain" when absent; the stored record
// reads null or another type as the empty behavior.
func (d Declaration) behavior() string {
	if !d.Behavior.Present() {
		return "domain"
	}
	return d.Behavior.Value
}

// textOrMember is the sent text, fallback when absent, null or of another type.
func textOrMember(value jsondecode.Member[string], fallback string) string {
	if value.OK {
		return value.Value
	}
	return fallback
}

// sameDeclaration compares the members as sent, so an unchanged wrong-typed
// stored value is grandfathered but a different one never is. Null counts as
// absent.
func sameDeclaration(x, y Declaration) bool {
	same := func(left, right jsondecode.Member[string]) bool {
		if left.Null() || right.Null() {
			return left.Null() && right.Null()
		}
		return bytes.Equal(left.Raw, right.Raw)
	}
	return same(x.SourceType, y.SourceType) && same(x.Behavior, y.Behavior) && same(x.Content, y.Content) && same(x.Format, y.Format)
}

// RemoteRuleValidationError is RemoteRuleValidationError: downloaded rules
// did not match their declared type or representation. It is a
// ProfileValidationError (errors.Is(err, repo.ErrValidation)).
type RemoteRuleValidationError struct {
	Message    string
	Errors     []Problem
	Behavior   *string
	RuleFormat *string
}

func (e *RemoteRuleValidationError) Error() string { return e.Message }

// Unwrap makes the error a ProfileValidationError.
func (e *RemoteRuleValidationError) Unwrap() error { return repo.NewValidationError(e.Message) }

// NewRemoteRuleMessage is RemoteRuleValidationError(message).
func NewRemoteRuleMessage(message string) *RemoteRuleValidationError {
	return &RemoteRuleValidationError{Message: message,
		Errors: []Problem{remoteProblem(0, "", message)}}
}

// NewRemoteRuleError is RemoteRuleValidationError(rule, rule_format, errors).
// NewRemoteError builds a typed, line-numbered download validation error.
func NewRemoteError(rule model.RuleConfig, format string, problems []Problem) *RemoteRuleValidationError {
	behavior := textOr(rule.Behavior, "domain")
	e := &RemoteRuleValidationError{Errors: problems, Behavior: &behavior, RuleFormat: &format}
	name := rule.Name
	if name == "" {
		name = rule.ID
	}
	if name == "" {
		name = msgUnnamedRuleset
	}
	if len(problems) == 0 {
		problems = []Problem{remoteProblem(0, "", msgInvalidRuleContent)}
		e.Errors = problems
	}
	first := problems[0]
	location := msgRuleFile
	if first.Line != 0 {
		location = fmt.Sprintf(msgRuleLine, first.Line)
	}
	original := textOr(first.Before, "")
	preview := original
	if runes := []rune(original); len(runes) > 200 {
		preview = string(runes[:200]) + "…"
	}
	detail := ""
	if original != "" {
		detail = fmt.Sprintf(msgRulePreview, displayPreview(preview))
	}
	remainder := ""
	if len(problems) > 1 {
		remainder = fmt.Sprintf(msgProblemCount, len(problems))
	}
	e.Message = fmt.Sprintf(msgRemoteRule, name, behavior, format, location, detail, first.Message, remainder)
	return e
}

// displayPreview uses a fixed quoted field for the product's diagnostic text;
// it is not a serializer for arbitrary Python/JSON values.
func displayPreview(value string) string {
	quote := byte('\'')
	if strings.ContainsRune(value, '\'') && !strings.ContainsRune(value, '"') {
		quote = '"'
	}
	escaped := strconv.Quote(value)
	escaped = escaped[1 : len(escaped)-1]
	if quote == '\'' {
		escaped = strings.ReplaceAll(escaped, `\"`, `"`)
		escaped = strings.ReplaceAll(escaped, "'", `\'`)
	}
	return string(quote) + escaped + string(quote)
}
func remoteProblem(line int, before, message string) Problem { return Problem{line, &before, message} }

type textEntry struct {
	line  int
	value string
}

// remoteTextEntries is _remote_text_entries.
func remoteTextEntries(content, ruleFormat string) ([]textEntry, []Problem) {
	sourceLines := textx.SplitLines(content, false)
	var problems []Problem
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
	fail := func(err error) ([]textEntry, []Problem) {
		line, message := 0, err.Error()
		var marked *yamlx.SyntaxError
		if errors.As(err, &marked) {
			line = marked.Line
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
// ValidateRemote checks complete bytes against the declared format before
// they can replace a cache. The library is already decoded by the caller.
func ValidateRemote(content Content, rule model.RuleConfig, library []model.RuleLibraryItem, format string) error {
	behavior := textOr(rule.Behavior, "domain")
	selected := format
	if selected == "" {
		var err error
		selected, err = rulesetfmt.RuleFormat(rule, library, "")
		if err != nil {
			return err
		}
	}
	message := metadataError(behavior, textOr(rule.Format, ""), "url")
	if message == "" {
		message = metadataError(behavior, selected, "url")
	}
	if message != "" {
		return NewRemoteError(rule, selected, []Problem{remoteProblem(0, "", message)})
	}
	key := dataDigest(content.Data) + "\x00" + behavior + "\x00" + selected
	if content.Text {
		key += "\x00text"
	}
	if _, ok := validatedCache.Get(key); ok {
		return nil
	}
	if err := validateRemoteContent(content, rule, behavior, selected); err != nil {
		return err
	}
	validatedCache.Put(key, true, 0)
	return nil
}

const textLineBreakChars = "\n\r\v\f\x1c\x1d\x1e\u0085  "

func validateRemoteContent(content Content, rule model.RuleConfig, behavior, selected string) error {
	if selected == "mrs" {
		return validateRemoteMRS(content, rule)
	}
	if !utf8.Valid(content.Data) {
		offset := 0
		for offset < len(content.Data) {
			value, width := utf8.DecodeRune(content.Data[offset:])
			if value == utf8.RuneError && width == 1 {
				break
			}
			offset += width
		}
		prefix := string(content.Data[:offset])
		line := len(textx.SplitLines(prefix, false))
		if prefix == "" || strings.ContainsRune(textLineBreakChars, []rune(prefix)[runeLen(prefix)-1]) {
			line++
		}
		if line < 1 {
			line = 1
		}
		// Retain the editor's byte-evidence notation, restricted to this single
		// invalid byte. No arbitrary bytes/objects are rendered through repr.
		end := offset + 1
		// FullRune treats an invalid prefix as complete. Retain only the
		// still-incomplete prefix, stopping before the offending next byte.
		for end < len(content.Data) && !utf8.FullRune(content.Data[offset:end+1]) {
			end++
		}
		var bytes strings.Builder
		bytes.WriteString("b'")
		for _, value := range content.Data[offset:end] {
			fmt.Fprintf(&bytes, `\x%02x`, value)
		}
		bytes.WriteByte('\'')
		evidence := bytes.String()

		return NewRemoteError(rule, selected, []Problem{remoteProblem(line, evidence, msgInvalidUTF8)})
	}
	text := string(content.Data)
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
		case behavior == "classical" && (strings.Contains(value, ",") || rules.IsRuleType(strings.ToUpper(value))):
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
			a := problems[i].Line
			b := problems[j].Line
			return a < b
		})
		return NewRemoteError(rule, selected, problems)
	}
	return nil
}

// ReadRulePayload is read_rule_payload: the payload lines of YAML (with its
// sequence node for comment-preserving edits) or the text lines (node nil).
func ReadRulePayload(content string) ([]string, *yamlx.Node, error) {
	if !rules.HasYAMLPayload(content) {
		lines := textx.SplitLines(content, false)
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
func AppendRulePayload(content, line string, payload *yamlx.Node) (string, error) {
	return AppendRuleLines(content, []string{line}, payload)
}

// AppendRuleLines is append_rule_lines: insert into the existing sequence,
// preserving comments and surrounding YAML.
func AppendRuleLines(content string, lines []string, payload *yamlx.Node) (string, error) {
	if len(lines) == 0 {
		return content, nil
	}
	if payload == nil && rules.HasYAMLPayload(content) {
		var err error
		_, payload, err = ReadRulePayload(content)
		if err != nil {
			return content, err
		}
	}
	if payload == nil {
		separator := ""
		if content != "" && !strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		return content + separator + strings.Join(lines, "\n") + "\n", nil
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
		return content, payloadEditError(err)
	}
	return string(edited), nil
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
		for _, raw := range textx.SplitLines(content, true) {
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
		return "", nil, payloadEditError(err)
	}
	return string(edited), removed, nil
}

func payloadEditError(err error) error {
	if errors.Is(err, repo.ErrValidation) {
		return err
	}
	return &repo.Error{Kind: repo.ErrValidation, Message: fmt.Sprintf(msgReadPayloadYAML, err), Cause: err}
}

// RuleLines splits text using the rule-source line separators accepted by the
// shared parser, retaining blank lines for diagnostic line numbers.
func RuleLines(content string) []string { return textx.SplitLines(content, false) }
