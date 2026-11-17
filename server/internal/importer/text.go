package importer

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type warningBucket struct {
	category string
	details  []string
	seen     map[string]struct{}
}
type warnings struct{ buckets []warningBucket }

func (w *warnings) add(category, detail string) {
	for i := range w.buckets {
		if w.buckets[i].category == category {
			if _, exists := w.buckets[i].seen[detail]; detail != "" && !exists {
				w.buckets[i].seen[detail] = struct{}{}
				if len(w.buckets[i].details) < 8 {
					w.buckets[i].details = append(w.buckets[i].details, detail)
				}
			}
			return
		}
	}
	details := []string{}
	if detail != "" {
		details = append(details, detail)
	}
	seen := map[string]struct{}{}
	if detail != "" {
		seen[detail] = struct{}{}
	}
	w.buckets = append(w.buckets, warningBucket{category, details, seen})
}
func (w warnings) list() []string {
	out := []string{}
	for _, b := range w.buckets {
		line := b.category
		if len(b.details) > 0 {
			line += "：" + strings.Join(b.details[:min(8, len(b.details))], "、")
			if len(b.seen) > 8 {
				line += fmt.Sprintf(msgMore, len(b.seen))
			}
		}
		out = append(out, line)
	}
	return out
}
func appendUnique(items []string, value string) []string {
	if !slices.Contains(items, value) {
		return append(items, value)
	}
	return items
}
func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '\'' || value[0] == '"') {
		return value[1 : len(value)-1]
	}
	return value
}
func splitArgs(text string, separator rune) []string {
	out := []string{}
	var buf strings.Builder
	depth := 0
	var quote rune
	for _, ch := range text {
		if quote != 0 {
			buf.WriteRune(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		}
		if ch == separator && depth == 0 {
			out = append(out, strings.TrimSpace(buf.String()))
			buf.Reset()
			continue
		}
		buf.WriteRune(ch)
	}
	tail := strings.TrimSpace(buf.String())
	if tail != "" || len(out) > 0 {
		out = append(out, tail)
	}
	return out
}

var optionKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func splitOptions(items []string) ([]string, map[string]string) {
	pos := []string{}
	options := map[string]string{}
	for _, item := range items {
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if ok && optionKey.MatchString(key) {
			options[strings.ToLower(key)] = unquote(value)
		} else {
			pos = append(pos, unquote(item))
		}
	}
	return pos, options
}
func enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
func integer(text string) *int {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return nil
	}
	return &value
}
func integerOr(text string, fallback int) int {
	if value := integer(text); value != nil {
		return *value
	}
	return fallback
}
func port(text string) (int, error) {
	value := integer(text)
	if value == nil || *value < 1 || *value > 65535 {
		return 0, fmt.Errorf(msgInvalidPort, text)
	}
	return *value, nil
}
func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func at(items []string, index int) string {
	if index >= len(items) {
		return ""
	}
	return items[index]
}
func option(options map[string]string, key, fallback string) string {
	if v, ok := options[key]; ok {
		return v
	}
	return fallback
}
func nameValue(line string) (string, string, error) {
	name, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", fmt.Errorf(msgMissingEqual)
	}
	return unquote(name), strings.TrimSpace(value), nil
}

type section struct {
	name  string
	lines []string
}
type sections []section

func parseSections(text string) sections {
	out := sections{}
	indices := map[string]int{}
	index := -1
	for _, raw := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n") {
		clean := strings.TrimSpace(raw)
		if strings.HasPrefix(clean, "[") && strings.HasSuffix(clean, "]") && !strings.Contains(clean[1:len(clean)-1], "]") && len(clean) > 2 {
			name := strings.TrimSpace(clean[1 : len(clean)-1])
			var exists bool
			index, exists = indices[name]
			if !exists {
				index = len(out)
				indices[name] = index
				out = append(out, section{name: name})
			}
			continue
		}
		if index >= 0 {
			out[index].lines = append(out[index].lines, strings.TrimRight(raw, " \t"))
		}
	}
	return out
}
func (s sections) find(name string) ([]string, bool) {
	for _, item := range s {
		if item.name == name {
			return item.lines, true
		}
	}
	return nil, false
}
func contentLines(lines []string) []string {
	out := []string{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
			continue
		}
		out = append(out, line)
	}
	return out
}
func (s sections) content(name string) []string { lines, _ := s.find(name); return contentLines(lines) }
func customSections(s sections, skip func(string) bool) string {
	blocks := []string{}
	for _, item := range s {
		if skip(item.name) {
			continue
		}
		body := strings.Trim(strings.Join(item.lines, "\n"), "\n")
		block := "[" + item.name + "]"
		if body != "" {
			block += "\n" + body
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

var rulesetExtension = regexp.MustCompile(`(?i)\.(list|txt|conf|yaml|yml|mrs|lsr|snippet)$`)

func rulesetName(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return "RuleSet"
	}
	path := strings.TrimRight(u.Path, "/")
	_, stem, found := strings.Cut(path, "/")
	if found {
		parts := strings.Split(path, "/")
		stem = parts[len(parts)-1]
	} else {
		stem = path
	}
	stem = rulesetExtension.ReplaceAllString(stem, "")
	if stem == "" {
		stem = "RuleSet"
	}
	return stem
}
func subscriptionName(source string) string {
	u, err := url.Parse(source)
	if err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	return msgDefaultSubscription
}
func uniqueName(base string, taken map[string]bool, library bool) string {
	candidate := base
	for i := 2; taken[candidate]; i++ {
		if library {
			candidate = fmt.Sprintf("%s_%d", base, i)
		} else {
			candidate = fmt.Sprintf("%s (%d)", base, i)
		}
	}
	return candidate
}
