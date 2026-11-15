package convert

import (
	"encoding/base64"
	"errors"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"configflow/internal/jsonx"

	"gopkg.in/yaml.v3"
)

var errProxyYAML = errors.New("unsupported YAML")

// yamlOpaque fills the list that stands for a value jsonx cannot hold.
const yamlOpaque = "<recursive>"

// yamlNodeBudget bounds alias expansion (a "billion laughs" document).
const yamlNodeBudget = 1 << 20

// loadProxyYAMLLoose is yaml.safe_load(text) into jsonx values for the
// values yamlx cannot hold (moved from repo/compat_proxystring.go, where it
// was cross-checked against Python): mapping keys become the strings
// json.dumps writes for them (1 -> "1", true -> "true", null -> "null"), and
// timestamps, !!binary and self-referencing containers become a one-element
// list (truthy, not a string). Known gaps: keys 1 and "1" collide, Python's
// 1 == True == 1.0 key collisions do not happen here.
func loadProxyYAMLLoose(text string) (value any, err error) {
	for _, r := range text {
		// PyYAML's reader rejects everything else before parsing.
		if !(r == 0x09 || r == 0x0a || r == 0x0d || (r >= 0x20 && r <= 0x7e) || r == 0x85 ||
			(r >= 0xa0 && r <= 0xd7ff) || (r >= 0xe000 && r <= 0xfffd) || (r >= 0x10000 && r <= 0x10ffff)) {
			return nil, errProxyYAML
		}
	}
	defer func() {
		if recover() != nil { // go-yaml reports some malformed input by panicking
			value, err = nil, errProxyYAML
		}
	}()
	decoder := yaml.NewDecoder(strings.NewReader(text))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	var second yaml.Node
	if err := decoder.Decode(&second); !errors.Is(err, io.EOF) {
		return nil, errProxyYAML // a second document, or a syntax error after the first
	}
	converter := yamlConverter{open: map[*yaml.Node]bool{}, budget: yamlNodeBudget}
	return converter.value(&document)
}

type yamlConverter struct {
	open   map[*yaml.Node]bool
	budget int
}

func (c *yamlConverter) value(node *yaml.Node) (any, error) {
	if c.budget--; c.budget < 0 {
		return nil, errProxyYAML
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return nil, nil
		}
		return c.value(node.Content[0])
	case yaml.AliasNode:
		if node.Alias == nil {
			return nil, errProxyYAML
		}
		if c.open[node.Alias] {
			return []any{yamlOpaque}, nil // a container that holds itself
		}
		return c.value(node.Alias)
	case yaml.ScalarNode:
		return yamlScalar(node)
	case yaml.SequenceNode:
		if node.Style&yaml.TaggedStyle != 0 && node.Tag != "!!seq" {
			return nil, errProxyYAML
		}
		c.open[node] = true
		defer delete(c.open, node)
		result := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			value, err := c.value(item)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	case yaml.MappingNode:
		if node.Style&yaml.TaggedStyle != 0 && node.Tag != "!!map" {
			return nil, errProxyYAML
		}
		c.open[node] = true
		defer delete(c.open, node)
		pairs, err := c.flatten(node)
		if err != nil {
			return nil, err
		}
		result := jsonx.NewObject()
		for i := 0; i < len(pairs); i += 2 {
			key, err := c.key(pairs[i])
			if err != nil {
				return nil, err
			}
			value, err := c.value(pairs[i+1])
			if err != nil {
				return nil, err
			}
			result.Set(key, value)
		}
		return result, nil
	}
	return nil, errProxyYAML
}

func isMergeKey(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Style == 0 && node.Value == "<<"
}

// flatten is SafeConstructor.flatten_mapping: the pairs of merged mappings
// come before the mapping's own pairs, so its own keys win.
func (c *yamlConverter) flatten(node *yaml.Node) ([]*yaml.Node, error) {
	if c.budget--; c.budget < 0 {
		return nil, errProxyYAML
	}
	var merge, own []*yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if !isMergeKey(key) {
			own = append(own, key, value)
			continue
		}
		for value.Kind == yaml.AliasNode {
			if value.Alias == nil || c.open[value.Alias] {
				return nil, errProxyYAML
			}
			value = value.Alias
		}
		switch value.Kind {
		case yaml.MappingNode:
			pairs, err := c.flatten(value)
			if err != nil {
				return nil, err
			}
			merge = append(merge, pairs...)
		case yaml.SequenceNode:
			var submerge [][]*yaml.Node
			for _, item := range value.Content {
				for item.Kind == yaml.AliasNode {
					if item.Alias == nil || c.open[item.Alias] {
						return nil, errProxyYAML
					}
					item = item.Alias
				}
				if item.Kind != yaml.MappingNode {
					return nil, errProxyYAML
				}
				pairs, err := c.flatten(item)
				if err != nil {
					return nil, err
				}
				submerge = append(submerge, pairs)
			}
			for i := len(submerge) - 1; i >= 0; i-- {
				merge = append(merge, submerge[i]...)
			}
		default:
			return nil, errProxyYAML
		}
	}
	return append(merge, own...), nil
}

// key is a mapping key as json.dumps would write it.
func (c *yamlConverter) key(node *yaml.Node) (string, error) {
	for node.Kind == yaml.AliasNode {
		if node.Alias == nil {
			return "", errProxyYAML
		}
		node = node.Alias
	}
	if node.Kind != yaml.ScalarNode {
		return "", errProxyYAML // unhashable key
	}
	if node.Style == 0 && node.Value == "=" {
		return "=", nil
	}
	value, err := yamlScalar(node)
	if err != nil {
		return "", err
	}
	switch key := value.(type) {
	case string:
		return key, nil
	case nil:
		return "null", nil
	case bool:
		if key {
			return "true", nil
		}
		return "false", nil
	case jsonx.Number:
		return key.String(), nil
	}
	return node.Value, nil // a timestamp
}

var (
	yamlBool      = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	yamlFloat     = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	yamlInt       = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	yamlNull      = regexp.MustCompile(`^(?:~|null|Null|NULL|)$`)
	yamlTimestamp = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
)

var yamlBoolValues = map[string]bool{"yes": true, "no": false, "true": true, "false": false, "on": true, "off": false}

// yamlScalar resolves and constructs a scalar like PyYAML's SafeLoader.
func yamlScalar(node *yaml.Node) (any, error) {
	text := node.Value
	if node.Style&yaml.TaggedStyle != 0 {
		switch node.Tag {
		case "!!str":
			return text, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			value, ok := yamlBoolValues[strings.ToLower(text)]
			if !ok {
				return nil, errProxyYAML
			}
			return value, nil
		case "!!int":
			return yamlConstructInt(text)
		case "!!float":
			return yamlConstructFloat(text)
		case "!!binary":
			if _, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), "")); err != nil {
				return nil, errProxyYAML
			}
			return []any{text}, nil
		}
		return nil, errProxyYAML
	}
	if node.Style != 0 { // quoted, literal or folded
		return text, nil
	}
	switch {
	case yamlBool.MatchString(text):
		return yamlBoolValues[strings.ToLower(text)], nil
	case yamlFloat.MatchString(text):
		return yamlConstructFloat(text)
	case yamlInt.MatchString(text):
		return yamlConstructInt(text)
	case yamlNull.MatchString(text):
		return nil, nil
	case yamlTimestamp.MatchString(text):
		if len(text) == 10 {
			if _, err := time.Parse("2006-01-02", text); err != nil {
				return nil, errProxyYAML
			}
		}
		return []any{text}, nil
	case text == "=":
		return nil, errProxyYAML // tag:yaml.org,2002:value has no safe constructor
	}
	return text, nil
}

func yamlConstructInt(text string) (any, error) {
	value := strings.ReplaceAll(text, "_", "")
	if value == "" {
		return nil, errProxyYAML
	}
	negative := value[0] == '-'
	if value[0] == '-' || value[0] == '+' {
		value = value[1:]
	}
	result := new(big.Int)
	ok := true
	switch {
	case value == "0":
	case strings.HasPrefix(value, "0b"):
		_, ok = result.SetString(value[2:], 2)
	case strings.HasPrefix(value, "0x"):
		_, ok = result.SetString(value[2:], 16)
	case strings.HasPrefix(value, "0"):
		_, ok = result.SetString(value, 8)
	case strings.Contains(value, ":"):
		base := big.NewInt(1)
		parts := strings.Split(value, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			digit, valid := new(big.Int).SetString(parts[i], 10)
			if !valid {
				return nil, errProxyYAML
			}
			result.Add(result, digit.Mul(digit, base))
			base = new(big.Int).Mul(base, big.NewInt(60))
		}
	default:
		_, ok = result.SetString(value, 10)
	}
	if !ok || value == "" {
		return nil, errProxyYAML
	}
	if negative {
		result.Neg(result)
	}
	return jsonx.BigInt(result), nil
}

func yamlConstructFloat(text string) (any, error) {
	value := strings.ToLower(strings.ReplaceAll(text, "_", ""))
	if value == "" {
		return nil, errProxyYAML
	}
	sign := 1.0
	if value[0] == '-' {
		sign = -1.0
	}
	if value[0] == '-' || value[0] == '+' {
		value = value[1:]
	}
	switch {
	case value == ".inf":
		return jsonx.Float(sign * math.Inf(1)), nil
	case value == ".nan":
		return jsonx.Float(math.NaN()), nil
	case strings.Contains(value, ":"):
		base, result := 1.0, 0.0
		parts := strings.Split(value, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			digit, err := pyFloat(parts[i])
			if err != nil {
				return nil, errProxyYAML
			}
			result += digit * base
			base *= 60
		}
		return jsonx.Float(sign * result), nil
	}
	result, err := pyFloat(value)
	if err != nil {
		return nil, errProxyYAML
	}
	return jsonx.Float(sign * result), nil
}

// pyFloat is float(text) for the digit strings the YAML float pattern allows.
func pyFloat(text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil && errors.Is(err, strconv.ErrRange) {
		return value, nil
	}
	return value, err
}
