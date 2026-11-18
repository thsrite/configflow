package subs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"regexp"
	"strings"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func validLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		if c == '-' && i != 0 && i != len(label)-1 {
			continue
		}
		return false
	}
	return true
}
func hostDomain(text string) string {
	name := hostPatternText(text)
	if name == "" || len([]rune(name)) > 253 {
		return ""
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return ""
	}
	allDigits := true
	for _, label := range labels {
		if !validLabel(label) {
			return ""
		}
		for _, c := range label {
			if c < '0' || c > '9' {
				allDigits = false
			}
		}
	}
	if allDigits {
		return ""
	}
	return name
}
func hostValue(key string, value *yamlx.Node) (*yamlx.Node, string) {
	value = yamlx.Resolve(value)
	if value == nil {
		return nil, "invalid_value"
	}
	items := []*yamlx.Node{value}
	if value.Kind == yaml.SequenceNode {
		items = value.Content
	}
	if len(items) == 0 {
		return nil, "invalid_value"
	}
	for _, item := range items {
		if _, ok := dnsText(item); !ok {
			return nil, "invalid_value"
		}
	}
	if len(items) > MaxAddresses {
		return nil, "too_many_addresses"
	}
	out := yamlx.Seq()
	seen := map[string]bool{}
	all := true
	for _, item := range items {
		text, _ := dnsText(item)
		address := usableAddress(text)
		if address == "" {
			all = false
			break
		}
		if !seen[address] {
			seen[address] = true
			out.Content = append(out.Content, yamlx.Str(address))
		}
	}
	if all {
		return out, ""
	}
	if len(items) == 1 {
		text, _ := dnsText(items[0])
		if target := hostDomain(text); target != "" && target != key {
			return yamlx.Str(target), ""
		}
	}
	return nil, "invalid_value"
}

// hostsBlock selects the independently parseable top-level hosts section. An
// unrelated malformed proxy section must not discard usable subscription hosts.
var hostLineBreak = regexp.MustCompile(`\r\n|[\n\r\x{85}\x{2028}\x{2029}]`)

func hostsBlock(text string) (string, bool) {
	// Locate only a top-level key at a physical source-line boundary. YAML's
	// additional scalar line separators matter only inside that selected block.
	offset := -1
	position := 0
	for _, line := range strings.Split(text, "\n") {
		if keyLine(line) {
			offset = position
			break
		}
		position += len(line) + 1
	}
	if offset < 0 {
		return "", false
	}
	lines := hostLineBreak.Split(text[offset:], -1)
	end := 1
	for end < len(lines) {
		next := lines[end]
		if strings.HasPrefix(next, " ") || strings.HasPrefix(next, "\t") || strings.TrimSpace(next) == "" || strings.HasPrefix(next, "#") {
			end++
			continue
		}
		break
	}
	return strings.Join(lines[:end], "\n"), true
}

func keyLine(line string) bool {
	for _, prefix := range []string{"hosts", `"hosts"`, `'hosts'`} {
		if rest, ok := strings.CutPrefix(line, prefix); ok && strings.HasPrefix(strings.TrimLeft(rest, " \t"), ":") {
			return true
		}
	}
	return false
}
func generatedHosts(entries *yamlx.Node, total, skippedCount int, skipped []HostSkip) *HostsData {
	type sample struct {
		Domain string `json:"domain"`
		Reason string `json:"reason"`
	}
	samples := make([]sample, 0, len(skipped))
	for _, item := range skipped {
		samples = append(samples, sample{item.Domain, item.Reason})
	}
	raw, err := yamlx.ToJSON(entries)
	if err != nil {
		return nil
	}
	output := struct {
		Entries      json.RawMessage `json:"entries"`
		Total        int             `json:"total"`
		SkippedCount int             `json:"skipped_count"`
		Skipped      []sample        `json:"skipped"`
	}{raw, total, skippedCount, samples}
	data, err := jsonresp.Encode(output)
	if err != nil {
		return nil
	}
	var result HostsData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return &result
}
func hostFailure(reason string) *HostsData {
	return generatedHosts(yamlx.Map(), 0, 1, []HostSkip{{Domain: "", Reason: reason}})
}

// ExtractHosts returns bounded validated metadata from the subscription's own
// hosts section. Open YAML keys retain their native scalar text; only exact
// valid domains and usable addresses or CNAME targets are accepted.
func ExtractHosts(text string) *HostsData {
	if !strings.Contains(text, "hosts") {
		return nil
	}
	block, ok := hostsBlock(text)
	if !ok {
		return nil
	}
	doc, err := yamlx.Parse([]byte(block))
	if err != nil {
		return hostFailure("parse_error")
	}
	raw := yamlx.Resolve(yamlx.Get(doc, "hosts"))
	if dnsNull(raw) {
		return nil
	}
	if raw.Kind != yaml.MappingNode {
		return hostFailure("not_mapping")
	}
	if err := ValidateHostsGraph(raw); err != nil {
		return hostFailure("parse_error")
	}
	entries := yamlx.Map()
	skipped := []HostSkip{}
	skippedCount, index := 0, 0
	for key, value := range yamlx.AllPairs(raw) {
		key = yamlx.Resolve(key)
		name := ""
		if key != nil && key.Kind == yaml.ScalarNode {
			switch key.ShortTag() {
			case "!!str", "!!int", "!!float", "!!bool":
				text := key.Value
				if key.ShortTag() != "!!str" && key.ShortTag() != "!!bool" {
					data, err := yamlx.ToJSON(key)
					if err != nil {
						return hostFailure("parse_error")
					}
					var scalar model.String
					if err := scalar.UnmarshalJSON(data); err == nil {
						text = string(scalar)
					}
				}
				name = cases.Lower(language.Und).String(strings.TrimSpace(text))
			}
		}
		reason := ""
		switch {
		case index >= MaxEntries:
			reason = "over_limit"
		case strings.Contains(name, "*") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "+."):
			reason = "wildcard"
		case hostDomain(name) == "" || usableAddress(name) != "":
			reason = "invalid_domain"
		default:
			name = hostDomain(name)
			var normalized *yamlx.Node
			normalized, reason = hostValue(name, value)
			if reason == "" {
				yamlx.Set(entries, name, normalized)
			}
		}
		if reason != "" {
			skippedCount++
			if len(skipped) < MaxSkippedSamples {
				sample := []rune(name)
				if len(sample) > 253 {
					sample = sample[:253]
				}
				skipped = append(skipped, HostSkip{Domain: string(sample), Reason: reason})
			}
		}
		index++
	}
	return generatedHosts(entries, index, skippedCount, skipped)
}

// HostDigest is the native D2 identity of validated host entries. Switching
// encoders deliberately reports one change; it is not a device configuration hash.
func HostDigest(record *HostsData) (string, error) {
	entries := model.Map[model.Dynamic]{}
	if record != nil && record.Entries.Len() > 0 {
		entries = record.Entries
	}
	data, err := model.Marshal(entries)
	if err != nil {
		return "", err
	}
	data, err = jsonresp.Canonical(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// HostChange counts host entry additions, removals and value changes.
type HostChange struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

// CompareHosts compares entry values without changing or normalizing stored metadata.
func CompareHosts(previous, current *HostsData) (HostChange, error) {
	oldEntries, newEntries := model.Map[model.Dynamic]{}, model.Map[model.Dynamic]{}
	if previous != nil {
		oldEntries = previous.Entries
	}
	if current != nil {
		newEntries = current.Entries
	}
	old := make(map[string]model.Dynamic, oldEntries.Len())
	for key, value := range oldEntries.All() {
		old[key] = value
	}
	change := HostChange{}
	for key, value := range newEntries.All() {
		prior, ok := old[key]
		if !ok {
			change.Added++
			continue
		}
		left, err := value.Node()
		if err != nil {
			return HostChange{}, err
		}
		right, err := prior.Node()
		if err != nil {
			return HostChange{}, err
		}
		if !yamlx.Equal(left, right) {
			change.Changed++
		}
		delete(old, key)
	}
	change.Removed = len(old)
	return change, nil
}

// ValidateHostsGraph validates the hosts transition without coercing scalar
// keys, which the hosts business rules must inspect in source order. Values
// must fit their own alias/JSON budget.
func ValidateHostsGraph(n *yamlx.Node) error {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for key, value := range yamlx.AllPairs(n) {
		resolved := yamlx.Resolve(key)
		if resolved == nil || resolved.Kind != yaml.ScalarNode {
			line := 0
			if key != nil {
				line = key.Line
			}
			return &hostYAMLKeyError{line: line}
		}
		if _, err := yamlx.ToJSON(value); err != nil {
			return err
		}
	}
	return nil
}

type hostYAMLKeyError struct{ line int }

func (e *hostYAMLKeyError) Error() string { return fmt.Sprintf(msgHostsKeyShape, e.line) }
func (e *hostYAMLKeyError) Unwrap() error { return yamlx.ErrPolicy }
