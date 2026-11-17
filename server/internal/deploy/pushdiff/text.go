package pushdiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"configflow/internal/agents/diag"
	"configflow/internal/redact"
)

type chunk struct {
	text   string
	secret bool
}

// Text is either plain text or ordered plain and credential pieces on the wire.
// Its zero value is an empty string. Offsets exposed by Unpack are code points.
type Text struct{ pieces []chunk }

func (t *Text) add(s string, secret bool) {
	if s == "" {
		return
	}
	t.pieces = append(t.pieces, chunk{s, secret})
}
func (t Text) merged() []chunk {
	out := []chunk{}
	var b strings.Builder
	secret := false
	for i, p := range t.pieces {
		if i > 0 && p.secret != secret {
			out = append(out, chunk{b.String(), secret})
			b.Reset()
		}
		secret = p.secret
		b.WriteString(p.text)
	}
	if b.Len() > 0 {
		out = append(out, chunk{b.String(), secret})
	}
	return out
}

func plain(s string) Text { var t Text; t.add(s, false); return t }
func markedString(s string, secret bool, escape func(string) string) Text {
	if escape == nil {
		escape = func(s string) string { return s }
	}
	var t Text
	if secret {
		t.add(escape(s), true)
		return t
	}
	pos := 0
	for _, r := range diag.CredentialSpans(s) {
		t.add(escape(s[pos:r[0]]), false)
		t.add(escape(s[r[0]:r[1]]), true)
		pos = r[1]
	}
	t.add(escape(s[pos:]), false)
	return t
}
func (t *Text) append(other Text) {
	for _, p := range other.pieces {
		t.add(p.text, p.secret)
	}
}
func (t Text) clipped(limit int) Text {
	total := 0
	for _, p := range t.merged() {
		total += utf8.RuneCountInString(p.text)
	}
	if total <= limit {
		return t
	}
	var out Text
	for _, p := range t.merged() {
		r := []rune(p.text)
		take := min(limit, len(r))
		out.add(string(r[:take]), p.secret)
		limit -= take
		if limit == 0 {
			break
		}
	}
	out.add(fmt.Sprintf(clippedMessage, total), false)
	return out
}

// Unpack returns the displayed text and credential ranges in code points.
func (t Text) Unpack() (string, []redact.Range) {
	var b strings.Builder
	var ranges []redact.Range
	pos := 0
	for _, p := range t.merged() {
		n := utf8.RuneCountInString(p.text)
		if p.secret {
			ranges = append(ranges, redact.Range{Start: pos, End: pos + n})
		}
		b.WriteString(p.text)
		pos += n
	}
	return b.String(), ranges
}

// MarshalJSON writes the plain-string / marked-piece union.
func (t Text) MarshalJSON() ([]byte, error) {
	secret := false
	for _, p := range t.merged() {
		secret = secret || p.secret
	}
	if !secret {
		s, _ := t.Unpack()
		return json.Marshal(s)
	}
	pieces := make([]json.RawMessage, 0, len(t.pieces))
	for _, p := range t.merged() {
		var b []byte
		if p.secret {
			b, _ = json.Marshal(struct {
				Secret string `json:"secret"`
			}{p.text})
		} else {
			b, _ = json.Marshal(p.text)
		}
		pieces = append(pieces, b)
	}
	return json.Marshal(pieces)
}

// UnmarshalJSON reads a display text without accepting arbitrary JSON objects.
func (t *Text) UnmarshalJSON(b []byte) error {
	*t = Text{}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		t.add(s, false)
		return nil
	}
	var pieces []json.RawMessage
	if err := json.Unmarshal(b, &pieces); err != nil {
		return err
	}
	for _, p := range pieces {
		var s string
		if len(p) > 0 && p[0] == '"' {
			if err := json.Unmarshal(p, &s); err != nil {
				return err
			}
			t.add(s, false)
		} else {
			var piece struct {
				Secret *string `json:"secret"`
			}
			if err := json.Unmarshal(p, &piece); err != nil {
				return err
			}
			if piece.Secret == nil {
				return fmt.Errorf("invalid credential piece")
			}
			t.add(*piece.Secret, true)
		}
	}
	return nil
}

func jsonString(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
func escapeJSON(s string) string { s = jsonString(s); return s[1 : len(s)-1] }

func mergeRanges(ranges []redact.Range, n int) []redact.Range {
	ranges = slices.Clone(ranges)
	slices.SortFunc(ranges, func(a, b redact.Range) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return a.End - b.End
	})
	out := []redact.Range{}
	for _, r := range ranges {
		r.Start = max(0, r.Start)
		r.End = min(n, r.End)
		if r.End <= r.Start {
			continue
		}
		if len(out) > 0 && r.Start <= out[len(out)-1].End {
			out[len(out)-1].End = max(out[len(out)-1].End, r.End)
		} else {
			out = append(out, r)
		}
	}
	return out
}
func (t Text) withoutStored(find func(string) []redact.Range) Text {
	if find == nil {
		return t
	}
	s, ranges := t.Unpack()
	found := find(s)
	if len(found) == 0 {
		return t
	}
	r := []rune(s)
	var out Text
	pos := 0
	for _, span := range mergeRanges(append(ranges, found...), len(r)) {
		out.add(string(r[pos:span.Start]), false)
		touched := false
		for _, f := range found {
			touched = touched || (f.Start < span.End && f.End > span.Start)
		}
		if touched {
			out.add(redact.Redacted, true)
		} else {
			out.add(string(r[span.Start:span.End]), true)
		}
		pos = span.End
	}
	out.add(string(r[pos:]), false)
	return out
}

// WithoutStored replaces stored credentials only in display texts, preserving
// useful context. The returned JSON-compatible value is independent of v.
func WithoutStored(v any, find func(string) []redact.Range) any {
	return mapTexts(v, func(t Text) any { return t.withoutStored(find) })
}

// WithoutSecrets renders the dialog's hidden-credential view.
func WithoutSecrets(v any) any {
	return mapTexts(v, func(t Text) any {
		var b strings.Builder
		for _, p := range t.merged() {
			if p.secret {
				b.WriteString(Hidden)
			} else {
				b.WriteString(p.text)
			}
		}
		return b.String()
	})
}

func mapTexts(v any, rewrite func(Text) any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	var visit func(any) any
	convert := func(v any) any {
		data, err := json.Marshal(v)
		if err != nil {
			return v
		}
		var t Text
		if t.UnmarshalJSON(data) != nil {
			return v
		}
		return rewrite(t)
	}
	visit = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				switch k {
				case "name", "old", "new", "text", "info":
					if item != nil {
						x[k] = convert(item)
					}
				case "added_items", "removed_items", "lines":
					if list, ok := item.([]any); ok {
						for i, t := range list {
							list[i] = convert(t)
						}
					}
				default:
					x[k] = visit(item)
				}
			}
		case []any:
			for i, item := range x {
				x[i] = visit(item)
			}
		}
		return v
	}
	return visit(value)
}
