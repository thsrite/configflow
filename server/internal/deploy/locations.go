package deploy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Position carries real source coordinates (one-based), or a payload index.
// PayloadIndex -1 refers to the payload value rather than one of its items.
type Position struct{ Line, Column, PayloadIndex *int }

// Locations implements provider's diagnostic hook without altering validation.
type Locations struct{}

const secretKeys = `(?:password|passwd|passphrase|secret|token|api[_-]?key|private[_-]?key|access[_-]?key|authorization|credential|uuid)`

var secretLine = regexp.MustCompile(`(?i)^([ \t]*)(-\s+)?["']?` + secretKeys + `["']?\s*:`)
var quotedSecret = regexp.MustCompile(`(?is)["']?\b` + secretKeys + `["']?\s*[:=][ \t]*(?:"(?:\\.|[^"\\])*(?:"|$)|'(?:''|[^'])*(?:'|$))`)
var uuidValue = regexp.MustCompile(`(?i)(["']?\buuid["']?\s*[:=][ \t]*)(?:"(?:\\.|[^"\\])*"|'(?:''|\\.|[^'\\])*'|[^\s,;}]+)`)
var privateStart = regexp.MustCompile(`-----BEGIN .*PRIVATE KEY-----`)
var privateEnd = regexp.MustCompile(`-----END .*PRIVATE KEY-----`)

// SourceExcerpt returns the actual line with indentation and credentials hidden.
func SourceExcerpt(text string, line int) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if line == len(lines)+1 {
		return msgEOF
	}
	if line <= 0 || line > len(lines) {
		return ""
	}
	// Look across lines before extracting one: a quoted secret can begin on a
	// previous line without repeating its credential key on the continuation.
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	start := 0
	for _, s := range lines[:line-1] {
		start += len(s) + 1
	}
	// Python checks the redaction pattern (a value may follow "Bearer" on the
	// next line), the uuid pattern and quoted values, in that order.
	spans := diag.SecretSpans(normalized)
	for _, pattern := range []*regexp.Regexp{uuidValue, quotedSecret} {
		for _, loc := range pattern.FindAllStringIndex(normalized, -1) {
			spans = append(spans, [2]int{loc[0], loc[1]})
		}
	}
	for _, span := range spans {
		if span[0] < start && span[1] > start {
			return msgHidden
		}
	}
	secretIndent := -1
	private := false
	for i, value := range lines[:line] {
		indent := len(value) - len(strings.TrimLeft(value, " \t"))
		if secretIndent >= 0 && strings.TrimSpace(value) != "" && indent <= secretIndent {
			secretIndent = -1
		}
		private = private || privateStart.MatchString(value)
		if match := secretLine.FindStringSubmatch(value); match != nil {
			secretIndent = len(match[1]) + len(match[2])
		}
		if i == line-1 {
			if secretIndent >= 0 || private {
				return msgHidden
			}
			return diag.FailureText(msgExcerpt+uuidValue.ReplaceAllString(value, "${1}[REDACTED]"), nil, 2048)
		}
		if privateEnd.MatchString(value) {
			private = false
		}
	}
	return ""
}

// SourceError attaches coordinates and safe excerpts only when positions exist.
func SourceError(message, text string, at Position, cause error, sourceLabel string) *PreparationError {
	if at.PayloadIndex != nil {
		if n, err := yamlx.Parse([]byte(text)); err == nil {
			payload := yamlx.Get(n, "payload")
			if payload != nil {
				payload = yamlx.Resolve(payload)
				idx := *at.PayloadIndex
				mark := payload
				if idx >= 0 {
					if payload.Kind == yaml.SequenceNode && idx < len(payload.Content) {
						mark = yamlx.Resolve(payload.Content[idx])
					} else {
						mark = nil
					}
				}
				if mark != nil {
					l, c := mark.Line, mark.Column
					at.Line, at.Column = &l, &c
				}
			}
		}
	}
	var syntax *yamlx.SyntaxError
	if at.Line == nil && errors.As(cause, &syntax) {
		if syntax.Line > 0 {
			l := syntax.Line
			at.Line = &l
			if syntax.Column > 0 {
				c := syntax.Column
				at.Column = &c
			}
		}
	}
	diagnostic := ""
	if cause != nil {
		diagnostic = cause.Error()
		if syntax != nil {
			diagnostic = syntax.Problem
		}
	}
	return sourceDiagnostic(message, text, at, cause, sourceLabel, diagnostic)
}

// sourceDiagnostic renders explicit coordinates and keeps the original cause.
// Caller-owned diagnostic text is not reinterpreted by the presentation layer.
func sourceDiagnostic(message, text string, at Position, cause error, sourceLabel, diagnostic string) *PreparationError {
	parts := []string{message}
	if at.Line != nil {
		if sourceLabel != "" {
			parts = append(parts, fmt.Sprintf(msgSource, sourceLabel))
		}
		location := fmt.Sprintf(msgLine, *at.Line)
		if at.Column != nil {
			location += fmt.Sprintf(msgColumn, *at.Column)
		}
		parts = append(parts, location)
		if excerpt := SourceExcerpt(text, *at.Line); excerpt != "" {
			parts = append(parts, excerpt)
		}
	}
	if diagnostic != "" {
		parts = append(parts, fmt.Sprintf(msgDiagnostic, diag.FailureText(diagnostic, nil, 0)))
	}
	return &PreparationError{Message: strings.Join(parts, "\n"), cause: cause}
}

// PreparationContext identifies the artifact while preserving safe source details.
func PreparationContext(err error, name, path, kind string) *PreparationError {
	if kind == "" {
		kind = msgRules
	}
	original, location, _ := strings.Cut(err.Error(), "\n")
	localized, _ := agents.DeploymentMessage(original).(string)
	explanation, raw, _ := strings.Cut(localized, "\n")
	name = strings.ReplaceAll(diag.FailureText(name, nil, 512), "\n", `\n`)
	path = strings.ReplaceAll(diag.FailureText(path, nil, 1024), "\n", `\n`)
	label := kind
	if name != "" {
		label = fmt.Sprintf(msgArtifact, kind, name)
	}
	parts := []string{fmt.Sprintf(msgPreparation, label, explanation)}
	if path != "" {
		parts = append(parts, fmt.Sprintf(msgFile, path))
	}
	if location != "" {
		parts = append(parts, location)
	}
	if raw != "" {
		parts = append(parts, raw)
	}
	out := &PreparationError{Message: diag.FailureText(strings.Join(parts, "\n"), nil, 0), cause: err}
	var previous *PreparationError
	if errors.As(err, &previous) {
		out.Remedy = previous.Remedy
		out.Issues = previous.Issues
	}
	return out
}

// PreparationContext implements provider.Locations.
func (Locations) PreparationContext(message, name, path, kind string) string {
	return PreparationContext(preparation(message), name, path, kind).Error()
}
