package routes

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const maxDeploymentErrorDetails = 16 * 1024
const maxDeploymentErrorSummary = 768
const maxCommandDiagnosticOutput = 32 * 1024

// Keep the end of command output, where cores normally print their final error.
// A cut first line is discarded so a truncated credential cannot lose its key.
type diagnosticOutput struct {
	data    []byte
	dropped bool
}

func (b *diagnosticOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= maxCommandDiagnosticOutput {
		b.data = append(b.data[:0], p[len(p)-maxCommandDiagnosticOutput:]...)
		b.dropped = true
	} else {
		if extra := len(b.data) + len(p) - maxCommandDiagnosticOutput; extra > 0 {
			copy(b.data, b.data[extra:])
			b.data = b.data[:len(b.data)-extra]
			b.dropped = true
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *diagnosticOutput) Bytes() []byte {
	if !b.dropped {
		return b.data
	}
	if index := bytes.IndexByte(b.data, '\n'); index >= 0 {
		data := b.data[index+1:]
		// A dropped YAML field name could leave only its multiline secret.
		// Discard leading continuation lines rather than displaying an orphaned
		// block whose meaning can no longer be determined from this tail.
		for len(data) > 0 && (data[0] == ' ' || data[0] == '\t' || data[0] == '\r' || data[0] == '\n') {
			next := bytes.IndexByte(data, '\n')
			if next < 0 {
				data = nil
				break
			}
			data = data[next+1:]
		}
		return append([]byte("[earlier output omitted]\n"), data...)
	}
	return []byte("[oversized output line omitted]")
}

var diagnosticANSI = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
var diagnosticURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)
var diagnosticCredential = regexp.MustCompile(`(?i)(["']?(?:[a-z0-9_-]*(?:password|passwd|token|secret|credential|authorization|api[_-]?key|private[_-]?key|access[_-]?key)[a-z0-9_-]*)["']?\s*[:=]\s*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;}]+)`)
var diagnosticAuthorization = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[a-z0-9._~+/=-]+`)
var diagnosticPEM = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?(?:-----END [^-]*PRIVATE KEY-----|$)`)
var diagnosticPEMEnd = regexp.MustCompile(`-----END [^-]*PRIVATE KEY-----`)
var diagnosticYAMLBlock = regexp.MustCompile(`(?i)^([ \t]*["']?[a-z0-9_-]*(?:password|passwd|token|secret|credential|api[_-]?key|private[_-]?key)[a-z0-9_-]*["']?\s*:\s*)[|>][-+0-9]*\s*(?:#.*)?$`)

func boundedDiagnostic(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const suffix = "\n[output truncated]"
	end := limit - len(suffix)
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix
}

func boundedDiagnosticTail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const prefix = "[earlier output omitted]\n"
	start := len(value) - limit + len(prefix)
	if index := strings.IndexByte(value[start:], '\n'); index >= 0 {
		start += index + 1
	} else {
		for start < len(value) && !utf8.RuneStart(value[start]) {
			start++
		}
	}
	return prefix + value[start:]
}

// Known configuration credentials may be repeated without their field name in
// a core error. Replace those literal values too; the cause remains unwrap-able.
func diagnosticWithSecrets(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	summary := sanitizeDiagnostic(err.Error())
	_, details := deploymentErrorFields(err)
	for _, secret := range diagnosticSecretParts(secrets) {
		if secret == "" {
			continue
		}
		summary = strings.ReplaceAll(summary, secret, "[REDACTED]")
		details = strings.ReplaceAll(details, secret, "[REDACTED]")
	}
	return &deploymentDiagnosticError{summary: boundedDiagnostic(summary, maxDeploymentErrorSummary), details: boundedDiagnosticTail(details, maxDeploymentErrorDetails), cause: err}
}

func diagnosticSecretParts(secrets []string) []string {
	parts := append([]string(nil), secrets...)
	for _, secret := range secrets {
		if strings.Contains(secret, "\n") {
			for _, line := range strings.Split(secret, "\n") {
				if value := strings.TrimSpace(line); value != "" {
					parts = append(parts, value)
				}
			}
		}
	}
	return parts
}

func configurationDiagnosticSecrets(doc map[string]any) []string {
	var secrets []string
	var visit func(any)
	visit = func(value any) {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				if text, ok := child.(string); ok && (sensitiveURLQueryKey(key) || key == "uuid") && text != "" {
					secrets = append(secrets, text)
				}
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		case string:
			for _, raw := range diagnosticURL.FindAllString(item, -1) {
				parsed, err := url.Parse(raw)
				if err != nil {
					continue
				}
				if parsed.User != nil {
					if password, ok := parsed.User.Password(); ok && password != "" {
						secrets = append(secrets, password)
					}
				}
				for key, values := range parsed.Query() {
					if sensitiveURLQueryKey(key) {
						secrets = append(secrets, values...)
					}
				}
			}
		}
	}
	visit(doc)
	return secrets
}

func sanitizeDiagnostic(value string) string {
	value = strings.ToValidUTF8(value, "")
	value = diagnosticANSI.ReplaceAllString(value, "")
	// An oversized output may begin in the middle of an unindented PEM value.
	if end := diagnosticPEMEnd.FindStringIndex(value); end != nil && !strings.Contains(value[:end[0]], "-----BEGIN ") {
		value = "[PRIVATE KEY REDACTED]" + value[end[1]:]
	}
	var cleanLines []string
	blockIndent := -1
	for _, line := range strings.Split(value, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if blockIndent >= 0 && (strings.TrimSpace(line) == "" || indent > blockIndent) {
			continue
		}
		blockIndent = -1
		if match := diagnosticYAMLBlock.FindStringSubmatch(line); match != nil {
			cleanLines = append(cleanLines, match[1]+"[REDACTED]")
			blockIndent = indent
		} else {
			cleanLines = append(cleanLines, line)
		}
	}
	value = strings.Join(cleanLines, "\n")
	value = diagnosticPEM.ReplaceAllString(value, "[PRIVATE KEY REDACTED]")
	value = diagnosticURL.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return "[URL REDACTED]"
		}
		// Proxy share links can carry credentials in their host or path.
		if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "udp" && parsed.Scheme != "tcp" && parsed.Scheme != "tls" {
			return parsed.Scheme + "://[REDACTED]"
		}
		parsed.User = nil
		parsed.Fragment, parsed.RawFragment = "", ""
		query := parsed.Query()
		for key := range query {
			query.Set(key, "[REDACTED]")
		}
		parsed.RawQuery = query.Encode()
		return strings.ReplaceAll(parsed.String(), "%5BREDACTED%5D", "[REDACTED]")
	})
	value = diagnosticAuthorization.ReplaceAllString(value, "$1 [REDACTED]")
	value = diagnosticCredential.ReplaceAllString(value, "${1}[REDACTED]")
	return strings.TrimSpace(value)
}

type deploymentDiagnosticError struct {
	summary string
	details string
	cause   error
}

func (e *deploymentDiagnosticError) Error() string { return e.summary }
func (e *deploymentDiagnosticError) Unwrap() error { return e.cause }

func diagnosticFailure(summary string, cause error, output []byte, secrets ...string) error {
	redactKnown := func(text string) string {
		for _, secret := range diagnosticSecretParts(secrets) {
			if secret != "" {
				text = strings.ReplaceAll(text, secret, "[REDACTED]")
			}
		}
		return text
	}
	summary = redactKnown(summary)
	details := sanitizeDiagnostic(redactKnown(string(output)))
	var nested *deploymentDiagnosticError
	if details == "" && errors.As(cause, &nested) {
		details = redactKnown(nested.details)
	}
	reason, lastLine := "", ""
	for _, line := range strings.Split(details, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[earlier output") {
			continue
		}
		if reason != "" && (strings.HasPrefix(line, "exit status ") || line == "context deadline exceeded") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "configuration file ") && strings.HasSuffix(lower, "test failed") {
			continue
		}
		lastLine = line
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "fatal") || strings.Contains(lower, "invalid") {
			reason = line
		}
	}
	if reason == "" {
		reason = lastLine
	}
	if cause != nil {
		causeText := sanitizeDiagnostic(redactKnown(cause.Error()))
		if reason == "" {
			reason = causeText
		}
		if !strings.Contains(details, causeText) {
			if details != "" {
				details += "\n"
			}
			details += causeText
		}
	}
	if reason != "" && reason != summary {
		summary += ": " + reason
	}
	return &deploymentDiagnosticError{
		summary: boundedDiagnostic(sanitizeDiagnostic(summary), maxDeploymentErrorSummary),
		details: boundedDiagnosticTail(details, maxDeploymentErrorDetails), cause: cause,
	}
}

func deploymentErrorFields(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	text := sanitizeDiagnostic(err.Error())
	summary := boundedDiagnostic(text, maxDeploymentErrorSummary)
	var diagnostic *deploymentDiagnosticError
	if errors.As(err, &diagnostic) {
		return summary, boundedDiagnosticTail(sanitizeDiagnostic(diagnostic.details), maxDeploymentErrorDetails)
	}
	if text != summary {
		return summary, boundedDiagnostic(text, maxDeploymentErrorDetails)
	}
	return summary, ""
}

func withPreviousDiagnostic(action string, previous, current error) error {
	first, firstDetails := deploymentErrorFields(previous)
	second, secondDetails := deploymentErrorFields(current)
	return diagnosticFailure(action, current, []byte(first+"\n"+firstDetails+"\n"+second+"\n"+secondDetails))
}

// Preserve the underlying error so callers can still recognize manager exit
// codes with errors.As. Never include command arguments in returned diagnostics.
func runDiagnosticCommand(ctx context.Context, cmd *exec.Cmd, action string, secrets ...string) ([]byte, error) {
	var output diagnosticOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	// A child may inherit these pipes after the command exits or is killed. Do
	// not extend the existing command timeout merely to collect its diagnostics.
	cmd.WaitDelay = 250 * time.Millisecond
	err := cmd.Run()
	data := output.Bytes()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() && (ctx == nil || ctx.Err() == nil) {
		err = nil
	}
	if err == nil {
		return data, nil
	}
	safeOutput := string(data)
	for _, secret := range diagnosticSecretParts(secrets) {
		if secret != "" {
			safeOutput = strings.ReplaceAll(safeOutput, secret, "[REDACTED]")
		}
	}
	err = diagnosticWithSecrets(err, secrets...)
	if ctx != nil && ctx.Err() != nil {
		return data, diagnosticFailure(action+" timed out", ctx.Err(), []byte(safeOutput))
	}
	return data, diagnosticFailure(action+" failed", err, []byte(safeOutput))
}
