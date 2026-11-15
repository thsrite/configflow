package pyyaml

import (
	"regexp"
	"strings"

	"configflow/internal/jsonx"
)

// Parser (yaml/parser.py) and composer (yaml/composer.py).

type eventKind int

const (
	eventStreamStart eventKind = iota
	eventStreamEnd
	eventDocumentStart
	eventDocumentEnd
	eventAlias
	eventScalar
	eventSequenceStart
	eventSequenceEnd
	eventMappingStart
	eventMappingEnd
)

type event struct {
	kind       eventKind
	anchor     string
	hasAnchor  bool
	tag        string
	hasTag     bool
	implicit   [2]bool // scalars: (plain, quoted); collections: implicit[0]
	value      string
	style      rune
	flowStyle  bool
	start, end Mark
}

type parser struct {
	*scanner
	current    *event
	tagHandles map[string]string
	states     []func() *event
	marks      []Mark
	state      func() *event
}

var defaultTags = map[string]string{"!": "!", "!!": "tag:yaml.org,2002:"}

func parserError(context string, contextMark *Mark, problem string, problemMark Mark) *Error {
	return newError("ParserError", context, contextMark, problem, problemMark)
}

func (p *parser) checkEvent(kinds ...eventKind) bool {
	if p.current == nil && p.state != nil {
		p.current = p.state()
	}
	if p.current != nil {
		if len(kinds) == 0 {
			return true
		}
		for _, k := range kinds {
			if p.current.kind == k {
				return true
			}
		}
	}
	return false
}

func (p *parser) peekEvent() *event {
	if p.current == nil && p.state != nil {
		p.current = p.state()
	}
	return p.current
}

func (p *parser) getEvent() *event {
	if p.current == nil && p.state != nil {
		p.current = p.state()
	}
	value := p.current
	p.current = nil
	return value
}

func (p *parser) popState() func() *event {
	state := p.states[len(p.states)-1]
	p.states = p.states[:len(p.states)-1]
	return state
}

func (p *parser) popMark() {
	p.marks = p.marks[:len(p.marks)-1]
}

func (p *parser) parseStreamStart() *event {
	token := p.getToken()
	p.state = p.parseImplicitDocumentStart
	return &event{kind: eventStreamStart, start: token.Start, end: token.End}
}

func (p *parser) parseImplicitDocumentStart() *event {
	if !p.checkToken(TokenDirective, TokenDocumentStart, TokenStreamEnd) {
		p.tagHandles = defaultTags
		token := p.peekToken()
		p.states = append(p.states, p.parseDocumentEnd)
		p.state = p.parseBlockNode
		return &event{kind: eventDocumentStart, start: token.Start, end: token.Start}
	}
	return p.parseDocumentStart()
}

func (p *parser) parseDocumentStart() *event {
	for p.checkToken(TokenDocumentEnd) {
		p.getToken()
	}
	if !p.checkToken(TokenStreamEnd) {
		start := p.peekToken().Start
		p.processDirectives()
		if !p.checkToken(TokenDocumentStart) {
			token := p.peekToken()
			p.fail(parserError("", nil, "expected '<document start>', but found "+reprString(token.Kind.ID()), token.Start))
		}
		token := p.getToken()
		p.states = append(p.states, p.parseDocumentEnd)
		p.state = p.parseDocumentContent
		return &event{kind: eventDocumentStart, start: start, end: token.End}
	}
	token := p.getToken()
	p.state = nil
	return &event{kind: eventStreamEnd, start: token.Start, end: token.End}
}

func (p *parser) parseDocumentEnd() *event {
	token := p.peekToken()
	start, end := token.Start, token.Start
	if p.checkToken(TokenDocumentEnd) {
		end = p.getToken().End
	}
	p.state = p.parseDocumentStart
	return &event{kind: eventDocumentEnd, start: start, end: end}
}

func (p *parser) parseDocumentContent() *event {
	if p.checkToken(TokenDirective, TokenDocumentStart, TokenDocumentEnd, TokenStreamEnd) {
		e := processEmptyScalar(p.peekToken().Start)
		p.state = p.popState()
		return e
	}
	return p.parseBlockNode()
}

func (p *parser) processDirectives() {
	hasVersion := false
	p.tagHandles = map[string]string{}
	for p.checkToken(TokenDirective) {
		token := p.getToken()
		switch token.Value {
		case "YAML":
			if hasVersion {
				p.fail(parserError("", nil, "found duplicate YAML directive", token.Start))
			}
			if token.Major != 1 {
				p.fail(parserError("", nil, "found incompatible YAML document (version 1.* is required)", token.Start))
			}
			hasVersion = true
		case "TAG":
			if _, ok := p.tagHandles[token.TagHandle]; ok {
				p.fail(parserError("", nil, "duplicate tag handle "+reprString(token.TagHandle), token.Start))
			}
			p.tagHandles[token.TagHandle] = token.Tag
		}
	}
	for key, value := range defaultTags {
		if _, ok := p.tagHandles[key]; !ok {
			p.tagHandles[key] = value
		}
	}
}

func (p *parser) parseBlockNode() *event { return p.parseNode(true, false) }
func (p *parser) parseFlowNode() *event  { return p.parseNode(false, false) }
func (p *parser) parseBlockNodeOrIndentlessSequence() *event {
	return p.parseNode(true, true)
}

func (p *parser) parseNode(block, indentlessSequence bool) *event {
	if p.checkToken(TokenAlias) {
		token := p.getToken()
		p.state = p.popState()
		return &event{kind: eventAlias, anchor: token.Value, hasAnchor: true, start: token.Start, end: token.End}
	}
	e := &event{}
	var start, end, tagMark *Mark
	var handle, suffix string
	hasHandle := false
	if p.checkToken(TokenAnchor) {
		token := p.getToken()
		start, end = &token.Start, &token.End
		e.anchor, e.hasAnchor = token.Value, true
		if p.checkToken(TokenTag) {
			token := p.getToken()
			tagMark, end = &token.Start, &token.End
			e.hasTag, handle, hasHandle, suffix = true, token.Handle, token.HasHandle, token.Suffix
		}
	} else if p.checkToken(TokenTag) {
		token := p.getToken()
		start, tagMark, end = &token.Start, &token.Start, &token.End
		e.hasTag, handle, hasHandle, suffix = true, token.Handle, token.HasHandle, token.Suffix
		if p.checkToken(TokenAnchor) {
			token := p.getToken()
			end = &token.End
			e.anchor, e.hasAnchor = token.Value, true
		}
	}
	if e.hasTag {
		if hasHandle {
			prefix, ok := p.tagHandles[handle]
			if !ok {
				p.fail(parserError("while parsing a node", start, "found undefined tag handle "+reprString(handle), *tagMark))
			}
			e.tag = prefix + suffix
		} else {
			e.tag = suffix
		}
	}
	if start == nil {
		mark := p.peekToken().Start
		start, end = &mark, &mark
	}
	implicit := !e.hasTag || e.tag == "!"
	e.start, e.end = *start, *end
	if indentlessSequence && p.checkToken(TokenBlockEntry) {
		e.end = p.peekToken().End
		e.kind, e.implicit[0] = eventSequenceStart, implicit
		p.state = p.parseIndentlessSequenceEntry
		return e
	}
	switch {
	case p.checkToken(TokenScalar):
		token := p.getToken()
		e.end = token.End
		switch {
		case (token.Plain && !e.hasTag) || e.tag == "!":
			e.implicit = [2]bool{true, false}
		case !e.hasTag:
			e.implicit = [2]bool{false, true}
		default:
			e.implicit = [2]bool{false, false}
		}
		e.kind, e.value, e.style = eventScalar, token.Value, token.Style
		p.state = p.popState()
	case p.checkToken(TokenFlowSequenceStart):
		e.end = p.peekToken().End
		e.kind, e.implicit[0], e.flowStyle = eventSequenceStart, implicit, true
		p.state = p.parseFlowSequenceFirstEntry
	case p.checkToken(TokenFlowMappingStart):
		e.end = p.peekToken().End
		e.kind, e.implicit[0], e.flowStyle = eventMappingStart, implicit, true
		p.state = p.parseFlowMappingFirstKey
	case block && p.checkToken(TokenBlockSequenceStart):
		e.end = p.peekToken().Start
		e.kind, e.implicit[0] = eventSequenceStart, implicit
		p.state = p.parseBlockSequenceFirstEntry
	case block && p.checkToken(TokenBlockMappingStart):
		e.end = p.peekToken().Start
		e.kind, e.implicit[0] = eventMappingStart, implicit
		p.state = p.parseBlockMappingFirstKey
	case e.hasAnchor || e.hasTag:
		if p.scanner.libyaml {
			// libyaml: an empty node is implicit only without a tag ("!"
			// included), so "! " composes to a str.
			implicit = !e.hasTag
		}
		e.kind, e.implicit = eventScalar, [2]bool{implicit, false}
		p.state = p.popState()
	default:
		node := "flow"
		if block {
			node = "block"
		}
		token := p.peekToken()
		p.fail(parserError("while parsing a "+node+" node", start,
			"expected the node content, but found "+reprString(token.Kind.ID()), token.Start))
	}
	return e
}

func (p *parser) parseBlockSequenceFirstEntry() *event {
	token := p.getToken()
	p.marks = append(p.marks, token.Start)
	return p.parseBlockSequenceEntry()
}

func (p *parser) parseBlockSequenceEntry() *event {
	if p.checkToken(TokenBlockEntry) {
		token := p.getToken()
		if !p.checkToken(TokenBlockEntry, TokenBlockEnd) {
			p.states = append(p.states, p.parseBlockSequenceEntry)
			return p.parseBlockNode()
		}
		p.state = p.parseBlockSequenceEntry
		return processEmptyScalar(token.End)
	}
	if !p.checkToken(TokenBlockEnd) {
		token := p.peekToken()
		mark := p.marks[len(p.marks)-1]
		p.fail(parserError("while parsing a block collection", &mark,
			"expected <block end>, but found "+reprString(token.Kind.ID()), token.Start))
	}
	token := p.getToken()
	p.state = p.popState()
	p.popMark()
	return &event{kind: eventSequenceEnd, start: token.Start, end: token.End}
}

func (p *parser) parseIndentlessSequenceEntry() *event {
	if p.checkToken(TokenBlockEntry) {
		token := p.getToken()
		if !p.checkToken(TokenBlockEntry, TokenKey, TokenValue, TokenBlockEnd) {
			p.states = append(p.states, p.parseIndentlessSequenceEntry)
			return p.parseBlockNode()
		}
		p.state = p.parseIndentlessSequenceEntry
		return processEmptyScalar(token.End)
	}
	token := p.peekToken()
	p.state = p.popState()
	return &event{kind: eventSequenceEnd, start: token.Start, end: token.Start}
}

func (p *parser) parseBlockMappingFirstKey() *event {
	token := p.getToken()
	p.marks = append(p.marks, token.Start)
	return p.parseBlockMappingKey()
}

func (p *parser) parseBlockMappingKey() *event {
	if p.checkToken(TokenKey) {
		token := p.getToken()
		if !p.checkToken(TokenKey, TokenValue, TokenBlockEnd) {
			p.states = append(p.states, p.parseBlockMappingValue)
			return p.parseBlockNodeOrIndentlessSequence()
		}
		p.state = p.parseBlockMappingValue
		return processEmptyScalar(token.End)
	}
	if !p.checkToken(TokenBlockEnd) {
		token := p.peekToken()
		mark := p.marks[len(p.marks)-1]
		p.fail(parserError("while parsing a block mapping", &mark,
			"expected <block end>, but found "+reprString(token.Kind.ID()), token.Start))
	}
	token := p.getToken()
	p.state = p.popState()
	p.popMark()
	return &event{kind: eventMappingEnd, start: token.Start, end: token.End}
}

func (p *parser) parseBlockMappingValue() *event {
	if p.checkToken(TokenValue) {
		token := p.getToken()
		if !p.checkToken(TokenKey, TokenValue, TokenBlockEnd) {
			p.states = append(p.states, p.parseBlockMappingKey)
			return p.parseBlockNodeOrIndentlessSequence()
		}
		p.state = p.parseBlockMappingKey
		return processEmptyScalar(token.End)
	}
	p.state = p.parseBlockMappingKey
	return processEmptyScalar(p.peekToken().Start)
}

func (p *parser) parseFlowSequenceFirstEntry() *event {
	token := p.getToken()
	p.marks = append(p.marks, token.Start)
	return p.parseFlowSequenceEntryWith(true)
}

func (p *parser) parseFlowSequenceEntry() *event { return p.parseFlowSequenceEntryWith(false) }

func (p *parser) parseFlowSequenceEntryWith(first bool) *event {
	if !p.checkToken(TokenFlowSequenceEnd) {
		if !first {
			if p.checkToken(TokenFlowEntry) {
				p.getToken()
			} else {
				token := p.peekToken()
				mark := p.marks[len(p.marks)-1]
				p.fail(parserError("while parsing a flow sequence", &mark,
					"expected ',' or ']', but got "+reprString(token.Kind.ID()), token.Start))
			}
		}
		if p.checkToken(TokenKey) {
			token := p.peekToken()
			p.state = p.parseFlowSequenceEntryMappingKey
			return &event{kind: eventMappingStart, implicit: [2]bool{true, false}, flowStyle: true, start: token.Start, end: token.End}
		} else if !p.checkToken(TokenFlowSequenceEnd) {
			p.states = append(p.states, p.parseFlowSequenceEntry)
			return p.parseFlowNode()
		}
	}
	token := p.getToken()
	p.state = p.popState()
	p.popMark()
	return &event{kind: eventSequenceEnd, start: token.Start, end: token.End}
}

func (p *parser) parseFlowSequenceEntryMappingKey() *event {
	token := p.getToken()
	if !p.checkToken(TokenValue, TokenFlowEntry, TokenFlowSequenceEnd) {
		p.states = append(p.states, p.parseFlowSequenceEntryMappingValue)
		return p.parseFlowNode()
	}
	p.state = p.parseFlowSequenceEntryMappingValue
	return processEmptyScalar(token.End)
}

func (p *parser) parseFlowSequenceEntryMappingValue() *event {
	if p.checkToken(TokenValue) {
		token := p.getToken()
		if !p.checkToken(TokenFlowEntry, TokenFlowSequenceEnd) {
			p.states = append(p.states, p.parseFlowSequenceEntryMappingEnd)
			return p.parseFlowNode()
		}
		p.state = p.parseFlowSequenceEntryMappingEnd
		return processEmptyScalar(token.End)
	}
	p.state = p.parseFlowSequenceEntryMappingEnd
	return processEmptyScalar(p.peekToken().Start)
}

func (p *parser) parseFlowSequenceEntryMappingEnd() *event {
	p.state = p.parseFlowSequenceEntry
	token := p.peekToken()
	return &event{kind: eventMappingEnd, start: token.Start, end: token.Start}
}

func (p *parser) parseFlowMappingFirstKey() *event {
	token := p.getToken()
	p.marks = append(p.marks, token.Start)
	return p.parseFlowMappingKeyWith(true)
}

func (p *parser) parseFlowMappingKey() *event { return p.parseFlowMappingKeyWith(false) }

func (p *parser) parseFlowMappingKeyWith(first bool) *event {
	if !p.checkToken(TokenFlowMappingEnd) {
		if !first {
			if p.checkToken(TokenFlowEntry) {
				p.getToken()
			} else {
				token := p.peekToken()
				mark := p.marks[len(p.marks)-1]
				p.fail(parserError("while parsing a flow mapping", &mark,
					"expected ',' or '}', but got "+reprString(token.Kind.ID()), token.Start))
			}
		}
		if p.checkToken(TokenKey) {
			token := p.getToken()
			if !p.checkToken(TokenValue, TokenFlowEntry, TokenFlowMappingEnd) {
				p.states = append(p.states, p.parseFlowMappingValue)
				return p.parseFlowNode()
			}
			p.state = p.parseFlowMappingValue
			return processEmptyScalar(token.End)
		} else if !p.checkToken(TokenFlowMappingEnd) {
			p.states = append(p.states, p.parseFlowMappingEmptyValue)
			return p.parseFlowNode()
		}
	}
	token := p.getToken()
	p.state = p.popState()
	p.popMark()
	return &event{kind: eventMappingEnd, start: token.Start, end: token.End}
}

func (p *parser) parseFlowMappingValue() *event {
	if p.checkToken(TokenValue) {
		token := p.getToken()
		if !p.checkToken(TokenFlowEntry, TokenFlowMappingEnd) {
			p.states = append(p.states, p.parseFlowMappingKey)
			return p.parseFlowNode()
		}
		p.state = p.parseFlowMappingKey
		return processEmptyScalar(token.End)
	}
	p.state = p.parseFlowMappingKey
	return processEmptyScalar(p.peekToken().Start)
}

func (p *parser) parseFlowMappingEmptyValue() *event {
	p.state = p.parseFlowMappingKey
	return processEmptyScalar(p.peekToken().Start)
}

func processEmptyScalar(mark Mark) *event {
	return &event{kind: eventScalar, implicit: [2]bool{true, false}, start: mark, end: mark}
}

func reprString(s string) string { return jsonx.Repr(s) }

// NodeKind is the node class.
type NodeKind int

// Node classes of yaml/nodes.py.
const (
	ScalarNode NodeKind = iota
	SequenceNode
	MappingNode
)

// Node is a composed node. Value is the scalar text; Items the sequence
// entries; Pairs the mapping entries ([key, value]). An alias yields the
// same *Node as its anchor.
type Node struct {
	Kind       NodeKind
	Tag        string
	Value      string
	Items      []*Node
	Pairs      [][2]*Node
	Start, End Mark
	Style      rune // scalars: 0 (None), '\'', '"', '|', '>'
	FlowStyle  bool // collections: flow_style is True
}

// Tags.
const (
	TagStr = "tag:yaml.org,2002:str"
	TagSeq = "tag:yaml.org,2002:seq"
	TagMap = "tag:yaml.org,2002:map"
)

type composer struct {
	*parser
	anchors map[string]*Node
	depth   int
	resolve func(value string, implicit [2]bool) string
}

func composerError(context string, contextMark *Mark, problem string, problemMark Mark) *Error {
	return newError("ComposerError", context, contextMark, problem, problemMark)
}

// Compose is yaml.compose(text) (SafeLoader resolver): the single document's
// root node, or nil for an empty stream.
func Compose(text string) (root *Node, err error) {
	return ComposeWith(text, ComposeOptions{})
}

// ComposeOptions tune ComposeWith.
type ComposeOptions struct {
	// Name is the stream name in marks; empty means "<unicode string>".
	Name string
	// Resolve is the implicit scalar resolver (Resolver.resolve for a
	// ScalarNode); nil means SafeLoader's, ResolveScalar.
	Resolve func(value string, implicit [2]bool) string
	// Libyaml rejects double-quoted escapes of surrogates and of code points
	// beyond U+10FFFF with a ScannerError, as libyaml (CSafeLoader) does;
	// PyYAML's pure scanner accepts surrogates.
	Libyaml bool
}

// ComposeWith is Compose with a stream name and a scalar resolver, as a
// loader class with its own yaml_implicit_resolvers composes.
func ComposeWith(text string, opts ComposeOptions) (root *Node, err error) {
	s, err := newScanner(text, opts.Name)
	if err != nil {
		return nil, err
	}
	s.libyaml = opts.Libyaml
	p := &parser{scanner: s}
	p.state = p.parseStreamStart
	resolve := opts.Resolve
	if resolve == nil {
		resolve = ResolveScalar
	}
	c := &composer{parser: p, anchors: map[string]*Node{}, resolve: resolve}
	defer recoverScan(&err)
	c.getEvent()
	var document *Node
	if !c.checkEvent(eventStreamEnd) {
		document = c.composeDocument()
	}
	if !c.checkEvent(eventStreamEnd) {
		e := c.getEvent()
		var start *Mark
		if document != nil {
			start = &document.Start
		}
		c.fail(composerError("expected a single document in the stream", start, "but found another document", e.start))
	}
	c.getEvent()
	return document, nil
}

func (c *composer) composeDocument() *Node {
	c.getEvent()
	node := c.composeNode()
	c.getEvent()
	c.anchors = map[string]*Node{}
	return node
}

func (c *composer) composeNode() *Node {
	if c.checkEvent(eventAlias) {
		e := c.getEvent()
		node, ok := c.anchors[e.anchor]
		if !ok {
			c.fail(composerError("", nil, "found undefined alias "+reprString(e.anchor), e.start))
		}
		return node
	}
	e := c.peekEvent()
	if e.hasAnchor {
		if previous, ok := c.anchors[e.anchor]; ok {
			mark := previous.Start
			c.fail(composerError("found duplicate anchor "+reprString(e.anchor)+"; first occurrence", &mark,
				"second occurrence", e.start))
		}
	}
	switch e.kind {
	case eventScalar:
		return c.composeScalarNode()
	case eventSequenceStart:
		return c.composeCollection(SequenceNode)
	default:
		return c.composeCollection(MappingNode)
	}
}

func (c *composer) composeScalarNode() *Node {
	e := c.getEvent()
	tag := e.tag
	if !e.hasTag || tag == "!" {
		tag = c.resolve(e.value, e.implicit)
	}
	node := &Node{Kind: ScalarNode, Tag: tag, Value: e.value, Start: e.start, End: e.end, Style: e.style}
	if e.hasAnchor {
		c.anchors[e.anchor] = node
	}
	return node
}

func (c *composer) composeCollection(kind NodeKind) *Node {
	c.depth++
	if c.depth > RecursionLimit {
		c.fail(&RecursionError{"maximum recursion depth exceeded"})
	}
	start := c.getEvent()
	tag := start.tag
	if !start.hasTag || tag == "!" {
		tag = TagSeq
		if kind == MappingNode {
			tag = TagMap
		}
	}
	node := &Node{Kind: kind, Tag: tag, Start: start.start, FlowStyle: start.flowStyle}
	if start.hasAnchor {
		c.anchors[start.anchor] = node
	}
	if kind == SequenceNode {
		for !c.checkEvent(eventSequenceEnd) {
			node.Items = append(node.Items, c.composeNode())
		}
	} else {
		for !c.checkEvent(eventMappingEnd) {
			key := c.composeNode()
			value := c.composeNode()
			node.Pairs = append(node.Pairs, [2]*Node{key, value})
		}
	}
	node.End = c.getEvent().end
	c.depth--
	return node
}

// Resolver (yaml/resolver.py, Resolver's implicit scalar resolvers).

type implicitResolver struct {
	tag   string
	re    *regexp.Regexp
	first string
	empty bool
}

var resolvers = []implicitResolver{
	{"tag:yaml.org,2002:bool", regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`), "yYnNtTfFoO", false},
	{"tag:yaml.org,2002:float", regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`), "-+0123456789.", false},
	{"tag:yaml.org,2002:int", regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`), "-+0123456789", false},
	{"tag:yaml.org,2002:merge", regexp.MustCompile(`^(?:<<)$`), "<", false},
	{"tag:yaml.org,2002:null", regexp.MustCompile(`^(?:~|null|Null|NULL|)$`), "~nN", true},
	{"tag:yaml.org,2002:timestamp", regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`), "0123456789", false},
	{"tag:yaml.org,2002:value", regexp.MustCompile(`^(?:=)$`), "=", false},
	{"tag:yaml.org,2002:yaml", regexp.MustCompile(`^(?:!|&|\*)$`), "!&*", false},
}

// ResolveScalar is SafeLoader's Resolver.resolve(ScalarNode, value, implicit).
func ResolveScalar(value string, implicit [2]bool) string {
	if !implicit[0] {
		return TagStr
	}
	// Python's "$" also matches before one trailing newline.
	text := strings.TrimSuffix(value, "\n")
	var first rune
	for _, r := range value {
		first = r
		break
	}
	for _, r := range resolvers {
		if value == "" {
			if !r.empty {
				continue
			}
		} else if !strings.ContainsRune(r.first, first) {
			continue
		}
		if r.re.MatchString(text) {
			return r.tag
		}
	}
	return TagStr
}
