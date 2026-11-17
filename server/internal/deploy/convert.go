package deploy

import (
	"bytes"
	"configflow/internal/convert"
	"configflow/internal/mosdns"
	"configflow/internal/rulesetfmt"
	"configflow/internal/yamlx"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type conversionKey struct {
	Digest, RuleType string
	Mixed            bool
}
type conversionValue struct {
	key  conversionKey
	data []byte
}

// L5: conversion cache contains immutable bytes only; never held over parsing.
var conversions = struct {
	sync.Mutex
	values []conversionValue
	bytes  int
}{}

// ConvertMosDNSRules converts a shared source for its final domain/IP consumer.
func ConvertMosDNSRules(content []byte, ruleType string, mixed bool) ([]byte, error) {
	if ruleType != "domain_set" && ruleType != "ip_set" {
		return nil, preparation("Unsupported MosDNS rule plugin type")
	}
	key := conversionKey{digest(content), ruleType, mixed}
	conversions.Lock()
	for i, v := range conversions.values {
		if v.key == key {
			out := bytes.Clone(v.data)
			conversions.values = append(append(conversions.values[:i:i], conversions.values[i+1:]...), v)
			conversions.Unlock()
			return out, nil
		}
	}
	conversions.Unlock()
	out, err := convertRules(content, ruleType, mixed)
	if err != nil {
		return nil, err
	}
	if len(out) <= 16<<20 {
		conversions.Lock()
		for len(conversions.values) > 0 && (len(conversions.values) >= 256 || conversions.bytes+len(out) > 16<<20) {
			conversions.bytes -= len(conversions.values[0].data)
			conversions.values = conversions.values[1:]
		}
		conversions.values = append(conversions.values, conversionValue{key, bytes.Clone(out)})
		conversions.bytes += len(out)
		conversions.Unlock()
	}
	return out, nil
}

func convertRules(content []byte, ruleType string, mixed bool) ([]byte, error) {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(content) {
		return nil, preparation("MosDNS requires text rules; binary MRS is only supported by Mihomo")
	}
	text := string(content)
	if strings.ContainsRune(text, 0) {
		return nil, preparation("MosDNS rule source is binary")
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	// The shared detector also treats quoted payload keys and flow/JSON mappings
	// as YAML documents, so a JSON rule set is parsed (or refused), never read
	// line by line into an empty rule file.
	payload := rulesetfmt.HasYAMLPayload(text)
	invalid := func(message string, index int) error {
		n := index + 1
		at := Position{Line: &n}
		if payload {
			at = Position{PayloadIndex: &index}
		}
		return SourceError(message, text, at, nil, msgBeforeConversion)
	}
	if payload {
		n, err := yamlx.Parse(content)
		p := yamlx.Resolve(yamlx.Get(n, "payload"))
		if err != nil {
			return nil, SourceError("Invalid rule YAML payload", text, Position{}, err, msgBeforeConversion)
		}
		if p == nil || p.Kind != yaml.SequenceNode {
			idx := -1
			return nil, SourceError("Invalid rule YAML payload", text, Position{PayloadIndex: &idx}, nil, msgBeforeConversion)
		}
		lines = make([]string, 0, len(p.Content))
		for i, item := range p.Content {
			item = yamlx.Resolve(item)
			if item == nil || item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return nil, invalid("Invalid rule YAML payload", i)
			}
			lines = append(lines, item.Value)
		}
	}
	out := []string{}
	mapping := map[string]string{"DOMAIN": "full", "DOMAIN-SUFFIX": "domain", "DOMAIN-KEYWORD": "keyword", "DOMAIN-REGEX": "regexp"}
	native := func(s string) bool {
		for _, prefix := range []string{"domain:", "full:", "keyword:", "regexp:"} {
			if strings.HasPrefix(s, prefix) {
				return true
			}
		}
		return false
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if ruleType == "ip_set" {
			if native(line) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, "ip:"))
			if strings.Contains(line, ",") {
				kind, v := mosdns.ParseClashRuleValue(line)
				if kind != "IP-CIDR" && kind != "IP-CIDR6" {
					continue
				}
				value = v
			}
			if !convert.IsIPNetwork(value) {
				if mixed && !strings.Contains(line, ",") && !strings.HasPrefix(line, "ip:") {
					continue
				}
				return nil, invalid("Invalid IP network in MosDNS ip_set rule source", i)
			}
			out = append(out, value)
			continue
		}
		if strings.HasPrefix(line, "ip:") {
			continue
		}
		if native(line) {
			out = append(out, line)
		} else if strings.Contains(line, ",") {
			kind, value := mosdns.ParseClashRuleValue(line)
			if prefix, ok := mapping[kind]; ok {
				out = append(out, prefix+":"+value)
			}
		} else if !convert.IsIPNetwork(line) {
			expression, ok := mosdns.DomainPatternForMosDNS(line)
			if !ok {
				return nil, invalid("Unrecognized MosDNS rule source content", i)
			}
			out = append(out, expression)
		}
	}
	return []byte(strings.Join(out, "\n")), nil
}
