package yamlx

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/pyyaml"
)

// The constructor is a port of PyYAML 6.0.1's BaseConstructor and
// SafeConstructor (yaml/constructor.py) working on internal/pyyaml nodes,
// plus oxidns_native.NativeLoader's overrides. It keeps PyYAML's order of
// work, because the order decides which error a document reports:
//
//   - construct_object dispatches on the node's tag alone (a scalar tag on a
//     collection node reaches construct_scalar and fails there);
//   - collections are generators: construct_object returns the empty container
//     at once and fills it later, level by level (construct_document runs the
//     queued generators in rounds), so a scalar error deeper in the tree loses
//     to one at a shallower level;
//   - flatten_mapping mutates the node tree (merged pairs, "=" keys retagged
//     to str), and aliases share nodes, so the mutation is visible elsewhere.
//
// Values are built in Python's model first (lists, dicts with any hashable
// key, sets, omap/pairs tuples) and converted to the jsonx model when the
// whole document is constructed (see convert).

// pyList, pyDict, pySet and pyPairs are the containers while constructing.
type pyList struct{ items []any }

type pyDict struct {
	keys, values []any
	keyNodes     []*pyyaml.Node
	index        map[string]int
}

func newDict() *pyDict { return &pyDict{index: map[string]int{}} }

// set is `mapping[key] = value`: an equal key keeps its place and its
// first key object, the value is replaced.
func (d *pyDict) set(key, value any, keyNode *pyyaml.Node) {
	h, comparable := hashKey(key)
	if i, ok := d.index[h]; ok && comparable {
		d.values[i] = value
		return
	}
	if comparable {
		d.index[h] = len(d.keys)
	}
	d.keys = append(d.keys, key)
	d.values = append(d.values, value)
	d.keyNodes = append(d.keyNodes, keyNode)
}

func (d *pyDict) has(key any) bool {
	h, comparable := hashKey(key)
	_, ok := d.index[h]
	return ok && comparable
}

type pySet struct{ dict *pyDict }

type pyPairs struct {
	kind  string // "omap" or "pairs"
	items [][2]any
}

// floatValue is a float while constructing. singular marks
// SafeConstructor.nan_value: every ".nan" is that one object, so two ".nan"
// keys are the same dict key (float("nan") objects are all different).
type floatValue struct {
	f        float64
	singular bool
}

// hashKey gives equal strings for keys Python's dict treats as equal:
// 1 == 1.0 == True, 0 == -0.0 == False, aware datetimes by their instant.
// comparable is false for a NaN that is not SafeConstructor.nan_value: it
// equals no other key.
func hashKey(key any) (h string, comparable bool) {
	switch k := key.(type) {
	case string:
		return "s" + k, true
	case nil:
		return "n", true
	case bool:
		if k {
			return "i1", true
		}
		return "i0", true
	case jsonx.Number:
		if k.IsInt() {
			return "i" + string(k), true
		}
		return hashFloat(k.Float64(), false)
	case floatValue:
		return hashFloat(k.f, k.singular)
	case []byte:
		return "b" + string(k), true
	case Timestamp:
		if !k.HasTime {
			return fmt.Sprintf("d%d-%d-%d", k.Year, k.Month, k.Day), true
		}
		if k.HasZone {
			return "a" + k.Time().UTC().Format("2006-01-02T15:04:05.000000"), true
		}
		return "t" + k.Time().Format("2006-01-02T15:04:05.000000"), true
	}
	return fmt.Sprintf("p%p", key), true
}

func hashFloat(f float64, singular bool) (string, bool) {
	switch {
	case math.IsNaN(f):
		return "nan", singular
	case math.IsInf(f, 0):
		return "f" + strconv.FormatFloat(f, 'g', -1, 64), true
	case f == math.Trunc(f):
		i, _ := new(big.Float).SetFloat64(f).Int(nil)
		return "i" + i.String(), true
	}
	return "f" + strconv.FormatFloat(f, 'g', -1, 64), true
}

func hashable(v any) bool {
	switch v.(type) {
	case *pyList, *pyDict, *pySet, *pyPairs:
		return false
	}
	return true
}

func pyTypeName(v any) string {
	switch v.(type) {
	case *pyList, *pyPairs:
		return "list"
	case *pyDict:
		return "dict"
	case *pySet:
		return "set"
	}
	return "object"
}

// Unhashable is the key LoadOptions.Key receives for a collection used as a
// mapping key: "list", "dict" or "set" (the Python type).
type Unhashable string

func nodeID(n *pyyaml.Node) string {
	switch n.Kind {
	case pyyaml.SequenceNode:
		return "sequence"
	case pyyaml.MappingNode:
		return "mapping"
	}
	return "scalar"
}

func nodeClass(n *pyyaml.Node) string {
	switch n.Kind {
	case pyyaml.SequenceNode:
		return "SequenceNode"
	case pyyaml.MappingNode:
		return "MappingNode"
	}
	return "ScalarNode"
}

// constructor is one construct_document run.
type constructor struct {
	opts        LoadOptions
	constructed map[*pyyaml.Node]any
	recursive   map[*pyyaml.Node]bool
	generators  []func() error
}

func pyErr(class, msg string) *Error { return &Error{Class: class, Msg: msg} }

func constructorError(context string, contextMark *pyyaml.Mark, problem string, problemMark pyyaml.Mark) *Error {
	e := &pyyaml.Error{Class: "ConstructorError", Context: context, ContextMark: contextMark, Problem: problem, ProblemMark: &problemMark}
	return pyErr("ConstructorError", e.Error())
}

func (c *constructor) document(root *pyyaml.Node) (any, error) {
	data, err := c.object(root)
	if err != nil {
		return nil, err
	}
	for len(c.generators) > 0 {
		round := c.generators
		c.generators = nil
		for _, generator := range round {
			if err := generator(); err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

// object is construct_object(node) (deep=False).
func (c *constructor) object(n *pyyaml.Node) (any, error) {
	if v, ok := c.constructed[n]; ok {
		return v, nil
	}
	if c.recursive[n] {
		return nil, constructorError("", nil, "found unconstructable recursive node", n.Start)
	}
	c.recursive[n] = true
	data, generator, err := c.dispatch(n)
	if err != nil {
		return nil, err
	}
	if generator != nil {
		c.generators = append(c.generators, generator)
	}
	c.constructed[n] = data
	delete(c.recursive, n)
	return data, nil
}

func (c *constructor) dispatch(n *pyyaml.Node) (any, func() error, error) {
	switch n.Tag {
	case tagNull:
		_, err := c.scalar(n, 0)
		return nil, nil, err
	case tagBool:
		v, err := c.scalar(n, 0)
		if err != nil {
			return nil, nil, err
		}
		switch lower := pystr.Lower(v); lower {
		case "yes", "true", "on":
			return true, nil, nil
		case "no", "false", "off":
			return false, nil, nil
		default:
			return nil, nil, pyErr("KeyError", jsonx.Repr(lower))
		}
	case tagInt:
		v, err := c.scalar(n, 0)
		if err != nil {
			return nil, nil, err
		}
		if c.opts.Schema == SchemaNative {
			i, err := nativeInt(v)
			return i, nil, err
		}
		i, err := constructInt(v)
		return i, nil, err
	case tagFloat:
		v, err := c.scalar(n, 0)
		if err != nil {
			return nil, nil, err
		}
		f, err := constructFloat(v)
		return f, nil, err
	case tagBinary:
		v, err := c.scalar(n, 0)
		if err != nil {
			return nil, nil, err
		}
		b, err := constructBinary(v, n)
		return b, nil, err
	case tagTimestamp:
		if _, err := c.scalar(n, 0); err != nil {
			return nil, nil, err
		}
		if n.Kind != pyyaml.ScalarNode {
			return nil, nil, pyErr("TypeError", "expected string or bytes-like object, got 'list'")
		}
		t, err := constructTimestamp(n.Value)
		return t, nil, err
	case tagStr:
		v, err := c.scalar(n, 0)
		return v, nil, err
	case tagSeq:
		list := &pyList{}
		return list, func() error {
			if n.Kind != pyyaml.SequenceNode {
				return constructorError("", nil, "expected a sequence node, but found "+nodeID(n), n.Start)
			}
			list.items = make([]any, 0, len(n.Items))
			for _, child := range n.Items {
				item, err := c.object(child)
				if err != nil {
					return err
				}
				list.items = append(list.items, item)
			}
			return nil
		}, nil
	case tagMap:
		dict := newDict()
		return dict, func() error { return c.mapping(n, dict) }, nil
	case tagSet:
		set := &pySet{dict: newDict()}
		return set, func() error { return c.mapping(n, set.dict) }, nil
	case tagOmap, tagPairs:
		kind, context := "omap", "while constructing an ordered map"
		if n.Tag == tagPairs {
			kind, context = "pairs", "while constructing pairs"
		}
		pairs := &pyPairs{kind: kind}
		return pairs, func() error {
			start := n.Start
			if n.Kind != pyyaml.SequenceNode {
				return constructorError(context, &start, "expected a sequence, but found "+nodeID(n), n.Start)
			}
			for _, sub := range n.Items {
				if sub.Kind != pyyaml.MappingNode {
					return constructorError(context, &start, "expected a mapping of length 1, but found "+nodeID(sub), sub.Start)
				}
				if len(sub.Pairs) != 1 {
					return constructorError(context, &start, fmt.Sprintf("expected a single mapping item, but found %d items", len(sub.Pairs)), sub.Start)
				}
				key, err := c.object(sub.Pairs[0][0])
				if err != nil {
					return err
				}
				value, err := c.object(sub.Pairs[0][1])
				if err != nil {
					return err
				}
				pairs.items = append(pairs.items, [2]any{key, value})
			}
			return nil
		}, nil
	}
	return nil, nil, constructorError("", nil, "could not determine a constructor for the tag "+jsonx.Repr(n.Tag), n.Start)
}

// maxScalarDepth stands for Python's recursion limit in construct_scalar's
// recursion through "=" keys.
const maxScalarDepth = 490

// scalar is SafeConstructor.construct_scalar: a mapping node gives the value
// of its "=" key (tag:yaml.org,2002:value), anything else must be a scalar.
func (c *constructor) scalar(n *pyyaml.Node, depth int) (string, error) {
	if depth > maxScalarDepth {
		return "", pyErr("RecursionError", "maximum recursion depth exceeded")
	}
	if n.Kind == pyyaml.MappingNode {
		for _, pair := range n.Pairs {
			if pair[0].Tag == tagValue {
				return c.scalar(pair[1], depth+1)
			}
		}
	}
	if n.Kind != pyyaml.ScalarNode {
		return "", constructorError("", nil, "expected a scalar node, but found "+nodeID(n), n.Start)
	}
	return n.Value, nil
}

// flatten is SafeConstructor.flatten_mapping, mutating the node like Python.
func (c *constructor) flatten(n *pyyaml.Node) error {
	var merge [][2]*pyyaml.Node
	index := 0
	for index < len(n.Pairs) {
		keyNode, valueNode := n.Pairs[index][0], n.Pairs[index][1]
		switch keyNode.Tag {
		case tagMerge:
			n.Pairs = append(n.Pairs[:index:index], n.Pairs[index+1:]...)
			start := n.Start
			switch valueNode.Kind {
			case pyyaml.MappingNode:
				if err := c.flatten(valueNode); err != nil {
					return err
				}
				merge = append(merge, valueNode.Pairs...)
			case pyyaml.SequenceNode:
				var submerge [][][2]*pyyaml.Node
				for _, sub := range valueNode.Items {
					if sub.Kind != pyyaml.MappingNode {
						return constructorError("while constructing a mapping", &start,
							"expected a mapping for merging, but found "+nodeID(sub), sub.Start)
					}
					if err := c.flatten(sub); err != nil {
						return err
					}
					submerge = append(submerge, sub.Pairs)
				}
				for i := len(submerge) - 1; i >= 0; i-- {
					merge = append(merge, submerge[i]...)
				}
			default:
				return constructorError("while constructing a mapping", &start,
					"expected a mapping or list of mappings for merging, but found "+nodeID(valueNode), valueNode.Start)
			}
		case tagValue:
			keyNode.Tag = tagStr
			index++
		default:
			index++
		}
	}
	if len(merge) > 0 {
		n.Pairs = append(merge, n.Pairs...)
	}
	return nil
}

// mapping fills dict: SafeConstructor.construct_mapping, or
// NativeLoader.construct_mapping for SchemaNative.
func (c *constructor) mapping(n *pyyaml.Node, dict *pyDict) error {
	if c.opts.Schema == SchemaNative {
		return c.nativeMapping(n, dict)
	}
	if n.Kind == pyyaml.MappingNode {
		if err := c.flatten(n); err != nil {
			return err
		}
	}
	if n.Kind != pyyaml.MappingNode {
		return constructorError("", nil, "expected a mapping node, but found "+nodeID(n), n.Start)
	}
	for _, pair := range n.Pairs {
		key, err := c.object(pair[0])
		if err != nil {
			return err
		}
		if !hashable(key) {
			start := n.Start
			return constructorError("while constructing a mapping", &start, "found unhashable key", pair[0].Start)
		}
		if c.opts.Key != nil {
			if err := c.opts.Key(publicKey(key), dict.has(key)); err != nil {
				return err
			}
		}
		value, err := c.object(pair[1])
		if err != nil {
			return err
		}
		dict.set(key, value, pair[0])
	}
	return nil
}

// nativeMapping is NativeLoader.construct_mapping: no merge keys, no node
// kind check (it iterates node.value whatever it is), every key goes to the
// Key hook.
func (c *constructor) nativeMapping(n *pyyaml.Node, dict *pyDict) error {
	switch n.Kind {
	case pyyaml.ScalarNode:
		if n.Value != "" {
			return pyErr("ValueError", "not enough values to unpack (expected 2, got 1)")
		}
		return nil
	case pyyaml.SequenceNode:
		if len(n.Items) > 0 {
			return pyErr("TypeError", "cannot unpack non-iterable "+nodeClass(n.Items[0])+" object")
		}
		return nil
	}
	for _, pair := range n.Pairs {
		key, err := c.object(pair[0])
		if err != nil {
			return err
		}
		if c.opts.Key != nil {
			duplicate := hashable(key) && dict.has(key)
			if err := c.opts.Key(publicKey(key), duplicate); err != nil {
				return err
			}
		}
		if !hashable(key) {
			return pyErr("TypeError", "unhashable type: '"+pyTypeName(key)+"'")
		}
		value, err := c.object(pair[1])
		if err != nil {
			return err
		}
		dict.set(key, value, pair[0])
	}
	return nil
}

// publicKey is what the Key hook sees: scalars as loaded, collections as
// Unhashable.
func publicKey(key any) any {
	switch k := key.(type) {
	case *pyList, *pyPairs, *pyDict, *pySet:
		return Unhashable(pyTypeName(key))
	case floatValue:
		return jsonx.Float(k.f)
	}
	return key
}

// constructInt is SafeConstructor.construct_yaml_int.
func constructInt(raw string) (any, error) {
	value := strings.ReplaceAll(raw, "_", "")
	if value == "" {
		return nil, pyErr("IndexError", "string index out of range")
	}
	negative := value[0] == '-'
	if value[0] == '+' || value[0] == '-' {
		value = value[1:]
	}
	finish := func(n *big.Int, err error) (any, error) {
		if err != nil {
			return nil, pyErr("ValueError", err.Error())
		}
		if negative {
			n.Neg(n)
		}
		return jsonx.BigInt(n), nil
	}
	switch {
	case value == "0":
		return jsonx.Int(0), nil
	case strings.HasPrefix(value, "0b"):
		return finish(pystr.Int(value[2:], 2))
	case strings.HasPrefix(value, "0x"):
		return finish(pystr.Int(value[2:], 16))
	case value == "":
		return nil, pyErr("IndexError", "string index out of range")
	case value[0] == '0':
		return finish(pystr.Int(value, 8))
	case strings.Contains(value, ":"):
		parts := strings.Split(value, ":")
		digits := make([]*big.Int, len(parts))
		for i, part := range parts {
			d, err := pystr.Int(part, 10)
			if err != nil {
				return finish(nil, err)
			}
			digits[i] = d
		}
		total, base := new(big.Int), big.NewInt(1)
		for i := len(digits) - 1; i >= 0; i-- {
			total.Add(total, new(big.Int).Mul(digits[i], base))
			base.Mul(base, big.NewInt(60))
		}
		return finish(total, nil)
	}
	return finish(pystr.Int(value, 10))
}

// nativeInt is NativeLoader's int constructor:
// int(s.replace('_', ''), 0 if s.lstrip('-+').startswith(('0x', '0o', '0b')) else 10).
func nativeInt(s string) (any, error) {
	base := 10
	trimmed := strings.TrimLeft(s, "-+")
	if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0o") || strings.HasPrefix(trimmed, "0b") {
		base = 0
	}
	n, err := pystr.Int(strings.ReplaceAll(s, "_", ""), base)
	if err != nil {
		return nil, pyErr("ValueError", err.Error())
	}
	return jsonx.BigInt(n), nil
}

// constructFloat is SafeConstructor.construct_yaml_float.
func constructFloat(raw string) (any, error) {
	value := pystr.Lower(strings.ReplaceAll(raw, "_", ""))
	if value == "" {
		return nil, pyErr("IndexError", "string index out of range")
	}
	sign := 1.0
	if value[0] == '-' {
		sign = -1
	}
	if value[0] == '+' || value[0] == '-' {
		value = value[1:]
	}
	switch {
	case value == ".inf":
		return floatValue{f: sign * math.Inf(1)}, nil
	case value == ".nan":
		return floatValue{f: math.NaN(), singular: true}, nil
	case strings.Contains(value, ":"):
		parts := strings.Split(value, ":")
		digits := make([]float64, len(parts))
		for i, part := range parts {
			d, err := pystr.Float(part)
			if err != nil {
				return nil, pyErr("ValueError", err.Error())
			}
			digits[i] = d
		}
		total, base := 0.0, 1.0
		for i := len(digits) - 1; i >= 0; i-- {
			total += digits[i] * base
			base *= 60
		}
		return floatValue{f: sign * total}, nil
	}
	f, err := pystr.Float(value)
	if err != nil {
		return nil, pyErr("ValueError", err.Error())
	}
	return floatValue{f: sign * f}, nil
}

// constructBinary is SafeConstructor.construct_yaml_binary:
// base64.decodebytes(value.encode('ascii')).
func constructBinary(value string, n *pyyaml.Node) (any, error) {
	runes := []rune(value)
	for i, r := range runes {
		if r < 128 {
			continue
		}
		end := i + 1
		for end < len(runes) && runes[end] >= 128 {
			end++
		}
		var problem string
		if end == i+1 {
			problem = fmt.Sprintf("'ascii' codec can't encode character %s in position %d: ordinal not in range(128)", escapedChar(r), i)
		} else {
			problem = fmt.Sprintf("'ascii' codec can't encode characters in position %d-%d: ordinal not in range(128)", i, end-1)
		}
		return nil, constructorError("", nil, "failed to convert base64 data into ascii: "+problem, n.Start)
	}
	decoded, err := decodeBase64(value)
	if err != nil {
		return nil, constructorError("", nil, "failed to decode base64 data: "+err.Error(), n.Start)
	}
	return decoded, nil
}

// escapedChar is the '\xNN' / '\uNNNN' / '\UNNNNNNNN' form UnicodeEncodeError uses.
func escapedChar(r rune) string {
	switch {
	case r <= 0xFF:
		return fmt.Sprintf("'\\x%02x'", r)
	case r <= 0xFFFF:
		return fmt.Sprintf("'\\u%04x'", r)
	}
	return fmt.Sprintf("'\\U%08x'", r)
}

func base64Value(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 26
	case c >= '0' && c <= '9':
		return int(c-'0') + 52
	case c == '+':
		return 62
	case c == '/':
		return 63
	}
	return -1
}

// decodeBase64 is binascii.a2b_base64 (strict_mode=False, CPython 3.11):
// characters outside the alphabet are skipped, enough padding after two or
// three data characters ends the input, a lone data character or missing
// padding is an error.
func decodeBase64(text string) ([]byte, error) {
	var out []byte
	quad, pads := 0, 0
	var left byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '=' {
			if quad >= 2 {
				pads++
				if quad+pads >= 4 {
					return out, nil
				}
			}
			continue
		}
		v := base64Value(c)
		if v < 0 {
			continue
		}
		pads = 0
		b := byte(v)
		switch quad {
		case 0:
			quad, left = 1, b
		case 1:
			quad = 2
			out = append(out, left<<2|b>>4)
			left = b & 0x0f
		case 2:
			quad = 3
			out = append(out, left<<4|b>>2)
			left = b & 0x03
		case 3:
			quad = 0
			out = append(out, left<<6|b)
			left = 0
		}
	}
	switch quad {
	case 0:
		return out, nil
	case 1:
		return nil, fmt.Errorf("Invalid base64-encoded string: number of data characters (%d) cannot be 1 more than a multiple of 4", len(out)/3*4+1)
	}
	return nil, fmt.Errorf("Incorrect padding")
}

var timestampRe = regexp.MustCompile(`^([0-9][0-9][0-9][0-9])-([0-9][0-9]?)-([0-9][0-9]?)(?:(?:[Tt]|[ \t]+)([0-9][0-9]?):([0-9][0-9]):([0-9][0-9])(?:\.([0-9]*))?(?:[ \t]*(Z|([-+])([0-9][0-9]?)(?::([0-9][0-9]))?))?)?$`)

func daysIn(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// constructTimestamp is SafeConstructor.construct_yaml_timestamp; errors are
// the AttributeError of a failed match and the ValueErrors of timezone(),
// date() and datetime(), in Python's order (the zone is built first).
func constructTimestamp(value string) (any, error) {
	text := value
	if strings.HasSuffix(text, "\n") {
		text = text[:len(text)-1] // re's "$" also matches before a final newline
	}
	m := timestampRe.FindStringSubmatch(text)
	if m == nil {
		return nil, pyErr("AttributeError", "'NoneType' object has no attribute 'groupdict'")
	}
	atoi := func(s string) int { v, _ := strconv.Atoi(s); return v }
	valueError := func(msg string) error { return pyErr("ValueError", msg) }
	t := Timestamp{Year: atoi(m[1]), Month: atoi(m[2]), Day: atoi(m[3])}
	checkDate := func() error {
		if t.Year < 1 {
			return valueError(fmt.Sprintf("year %d is out of range", t.Year))
		}
		if t.Month < 1 || t.Month > 12 {
			return valueError("month must be in 1..12")
		}
		if t.Day < 1 || t.Day > daysIn(t.Year, t.Month) {
			return valueError("day is out of range for month")
		}
		return nil
	}
	if m[4] == "" {
		if err := checkDate(); err != nil {
			return nil, err
		}
		return t, nil
	}
	t.HasTime = true
	t.Hour, t.Minute, t.Second = atoi(m[4]), atoi(m[5]), atoi(m[6])
	if m[7] != "" {
		fraction := m[7]
		if len(fraction) > 6 {
			fraction = fraction[:6]
		}
		for len(fraction) < 6 {
			fraction += "0"
		}
		t.Microsecond = atoi(fraction)
	}
	if m[9] != "" {
		offset := atoi(m[10])*3600 + atoi(m[11])*60
		if m[9] == "-" {
			offset = -offset
		}
		if offset >= 24*3600 || offset <= -24*3600 {
			return nil, valueError("offset must be a timedelta strictly between -timedelta(hours=24) and timedelta(hours=24), not " + timedeltaRepr(offset) + ".")
		}
		t.HasZone, t.ZoneOffsetSeconds = true, offset
	} else if m[8] != "" {
		t.HasZone = true
	}
	if err := checkDate(); err != nil {
		return nil, err
	}
	switch {
	case t.Hour > 23:
		return nil, valueError("hour must be in 0..23")
	case t.Minute > 59:
		return nil, valueError("minute must be in 0..59")
	case t.Second > 59:
		return nil, valueError("second must be in 0..59")
	}
	return t, nil
}

// converter turns the constructed Python values into the jsonx model.
type converter struct {
	keep   bool
	done   map[any]any
	active map[any]bool
}

func (v *converter) convert(value any) (any, error) {
	switch x := value.(type) {
	case floatValue:
		return jsonx.Float(x.f), nil
	case *pyList, *pyDict, *pySet, *pyPairs:
	default:
		return value, nil
	}
	if v.active[value] {
		return nil, &UnrepresentableError{Kind: "recursive", Msg: "the document has a recursive alias (a collection that contains itself); jsonx values cannot hold it"}
	}
	if out, ok := v.done[value]; ok {
		return out, nil
	}
	v.active[value] = true
	defer delete(v.active, value)
	var out any
	switch x := value.(type) {
	case *pySet:
		return nil, &UnrepresentableError{Kind: "set", Msg: "!!set values cannot be held by jsonx values"}
	case *pyPairs:
		if len(x.items) == 0 {
			out = []any{} // an empty list of tuples is an empty list
			break
		}
		return nil, &UnrepresentableError{Kind: x.kind, Msg: "!!" + x.kind + " values cannot be held by jsonx values"}
	case *pyList:
		list := make([]any, len(x.items))
		v.done[value] = list
		for i, item := range x.items {
			converted, err := v.convert(item)
			if err != nil {
				return nil, err
			}
			list[i] = converted
		}
		out = list
	case *pyDict:
		var nonString any
		var nonStringNode *pyyaml.Node
		for i, key := range x.keys {
			if _, ok := key.(string); !ok {
				nonString, nonStringNode = key, x.keyNodes[i]
				break
			}
		}
		if nonStringNode == nil {
			object := jsonx.NewObject()
			v.done[value] = object
			for i, key := range x.keys {
				converted, err := v.convert(x.values[i])
				if err != nil {
					return nil, err
				}
				object.Set(key.(string), converted)
			}
			out = object
			break
		}
		if !v.keep {
			key, _ := v.convert(nonString)
			line := nonStringNode.Start.Line + 1
			return nil, &NonStringKeyError{Key: key, Line: line,
				Msg: fmt.Sprintf("mapping key %s at line %d is not a string; jsonx objects have string keys only", jsonx.Repr(keyForRepr(key)), line)}
		}
		mapping := &Mapping{Pairs: make([]Pair, len(x.keys))}
		v.done[value] = mapping
		for i, key := range x.keys {
			k, _ := v.convert(key)
			converted, err := v.convert(x.values[i])
			if err != nil {
				return nil, err
			}
			mapping.Pairs[i] = Pair{Key: k, Value: converted}
		}
		out = mapping
	}
	return out, nil
}

// timedeltaRepr is repr(datetime.timedelta(seconds=seconds)): days and
// seconds normalised as Python stores them (seconds in 0..86399).
func timedeltaRepr(seconds int) string {
	days := seconds / 86400
	rest := seconds % 86400
	if rest < 0 {
		days, rest = days-1, rest+86400
	}
	var parts []string
	if days != 0 {
		parts = append(parts, fmt.Sprintf("days=%d", days))
	}
	if rest != 0 {
		parts = append(parts, fmt.Sprintf("seconds=%d", rest))
	}
	if len(parts) == 0 {
		return "datetime.timedelta(0)"
	}
	return "datetime.timedelta(" + strings.Join(parts, ", ") + ")"
}

func keyForRepr(key any) any {
	switch key.(type) {
	case Timestamp, []byte:
		return fmt.Sprint(key)
	}
	return key
}
