package routes

// Locations are supplemental diagnostics: they are resolved only after a
// validation failure and never change which configurations a core accepts.
import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var diagnosticLineNumber = regexp.MustCompile(`(?i)\bline\s*[:=]?\s*([1-9][0-9]*)\b`)
var diagnosticMihomoRule = regexp.MustCompile(`(?:^|\n|msg=["']|Parse config error: )rules\[([0-9]+)\]`)
var diagnosticMihomoProxy = regexp.MustCompile(`(?:^|\n|msg=["']|Parse config error: )proxy ([0-9]+):`)
var diagnosticMihomoGroup = regexp.MustCompile(`(?:^|\n|msg=["']|Parse config error: )proxy group\[([0-9]+)\]:`)
var diagnosticQuotedYAMLValue = regexp.MustCompile(`(?:^|[\s{,])["']?([a-zA-Z0-9_-]+)["']?\s*:\s*(["'])`)
var diagnosticInformationalLine = regexp.MustCompile(`(?i)(?:^(?:TRACE|DEBUG|INFO)\b|\b(?:level|severity)\s*[=:]\s*["']?(?:trace|debug|info)\b)`)

// Error reporting must not load an arbitrarily large provider into memory.
func readDiagnosticFile(name string) []byte {
	file, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, 2<<20))
	return data
}

func yamlLocation(data []byte, path ...any) (*yaml.Node, string) {
	var document yaml.Node
	if yaml.Unmarshal(data, &document) != nil || len(document.Content) == 0 {
		return nil, ""
	}
	node := document.Content[0]
	var fields []string
	for _, item := range path {
		switch key := item.(type) {
		case string:
			if node.Kind != yaml.MappingNode {
				return nil, ""
			}
			var found *yaml.Node
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Value == key {
					found = node.Content[i+1]
					break
				}
			}
			if found == nil {
				return node, strings.Join(fields, ".")
			}
			node = found
			fields = append(fields, key)
		case int:
			if node.Kind != yaml.SequenceNode || key < 0 || key >= len(node.Content) {
				return nil, ""
			}
			node = node.Content[key]
			if len(fields) > 0 {
				fields[len(fields)-1] += fmt.Sprintf("[%d]", key)
			}
		}
	}
	return node, strings.Join(fields, ".")
}

func sourceLine(data []byte, line int) string {
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	// Never expose a continuation of a multiline credential without its key.
	secretIndent := -1
	var secretQuote byte
	inPrivateKey := false
	for index, text := range lines[:line] {
		if strings.Contains(text, "-----BEGIN ") && strings.Contains(text, "PRIVATE KEY-----") {
			inPrivateKey = true
		}
		if inPrivateKey && index == line-1 {
			return "[REDACTED]"
		}
		if diagnosticPEMEnd.MatchString(text) {
			inPrivateKey = false
		}
		// Malformed flow YAML cannot be parsed for known secret values. Keep
		// track of quoted credentials across lines before taking an excerpt.
		if secretQuote != 0 {
			if index == line-1 {
				return "[REDACTED]"
			}
			secretQuote = unfinishedDiagnosticQuote(text, secretQuote)
		}
		for _, match := range diagnosticQuotedYAMLValue.FindAllStringSubmatchIndex(text, -1) {
			key := text[match[2]:match[3]]
			if sensitiveURLQueryKey(key) || strings.EqualFold(key, "uuid") {
				secretQuote = unfinishedDiagnosticQuote(text[match[5]:], text[match[4]])
			}
		}
		if secretQuote != 0 && index == line-1 {
			return "[REDACTED]"
		}
		indent := len(text) - len(strings.TrimLeft(text, " \t"))
		if secretIndent >= 0 && strings.TrimSpace(text) != "" && indent <= secretIndent {
			secretIndent = -1
		}
		if secretIndent >= 0 && index == line-1 {
			return "[REDACTED]"
		}
		if colon := strings.IndexByte(text, ':'); colon >= 0 {
			key := strings.Trim(strings.TrimSpace(text[:colon]), "\"'")
			if sensitiveURLQueryKey(key) || key == "uuid" {
				if index == line-1 {
					return key + ": [REDACTED]"
				}
				secretIndent = indent
			}
		}
	}
	return strings.TrimSpace(lines[line-1])
}

func unfinishedDiagnosticQuote(text string, quote byte) byte {
	for index := 0; index < len(text); index++ {
		if quote == '"' && text[index] == '\\' {
			index++
			continue
		}
		if text[index] == quote {
			if quote == '\'' && index+1 < len(text) && text[index+1] == '\'' {
				index++
				continue
			}
			return 0
		}
	}
	return quote
}

func diagnosticLocationDetails(context, details string) string {
	// A stage wrapper must not truncate a previously resolved file/line header
	// merely because the native core filled the diagnostic output budget.
	if strings.HasPrefix(details, "文件：") || strings.HasPrefix(details, "发布阶段：") {
		if boundary := strings.Index(details, "\n\n"); boundary >= 0 {
			context += "\n" + details[:boundary]
			details = details[boundary+2:]
		}
	}
	context = boundedDiagnostic(sanitizeDiagnostic(context), 4096)
	return context + "\n\n" + boundedDiagnosticTail(details, maxDeploymentErrorDetails-len(context)-2)
}

func diagnosticLocation(err error, file, source, field string, line int, data []byte, generated bool) error {
	if err == nil {
		return nil
	}
	summary, details := deploymentErrorFields(err)
	context := "文件：" + file
	if generated {
		context += "（生成配置）"
	}
	if source != "" {
		context += "\n来源：" + source
	}
	if field != "" {
		context += "\n位置：" + field
	}
	if line > 0 {
		context += fmt.Sprintf("\n行号：%d", line)
		if value := sourceLine(data, line); value != "" {
			context += "\n内容：" + boundedDiagnostic(sanitizeDiagnostic(value), 1024)
		}
	}
	if details == "" {
		details = summary
	}
	result := &deploymentDiagnosticError{summary: summary, details: diagnosticLocationDetails(context, details), cause: err}
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) == nil {
		return diagnosticWithSecrets(result, configurationDiagnosticSecrets(doc)...)
	}
	return result
}

func configurationLocation(err error, file string, data []byte, path ...any) error {
	if err == nil {
		return nil
	}
	node, field := yamlLocation(data, path...)
	line := 0
	if node != nil && field != "" {
		line = node.Line
	}
	return diagnosticLocation(err, file, "", field, line, data, true)
}

func pluginLocation(err error, stage string, manifest *DeploymentManifest, index int, field ...any) error {
	data := readDiagnosticFile(filepath.Join(stage, manifest.ConfigPath))
	path := append([]any{"plugins", index}, field...)
	node, position := yamlLocation(data, path...)
	line := 0
	if node != nil {
		line = node.Line
	}
	source := ""
	if tag, _ := yamlLocation(data, "plugins", index, "tag"); tag != nil && tag.Kind == yaml.ScalarNode {
		source = "插件 " + tag.Value
	}
	return diagnosticLocation(err, manifest.ConfigPath, source, position, line, data, true)
}

func diagnosticFilePathPattern(stage, relative string) string {
	relativePattern := `(?:\./)?` + regexp.QuoteMeta(relative)
	absolutePattern := regexp.QuoteMeta(filepath.ToSlash(filepath.Join(stage, relative)))
	return `(?:^|[\s"'(\[=])(?:` + relativePattern + `|` + absolutePattern + `)`
}

func diagnosticFileColumnLine(output, pathPattern string) int {
	// OxiDNS emits <exact file>:<line>:<column>: error. An arbitrary
	// file-looking suffix is not evidence about the generated configuration.
	position := regexp.MustCompile(pathPattern + `["']?:([1-9][0-9]*):[1-9][0-9]*:`)
	if match := position.FindStringSubmatch(output); match != nil {
		line, _ := strconv.Atoi(match[1])
		return line
	}
	return 0
}

// Resolve only locations explicitly supplied by the core. A YAML line in an
// identified provider file is not a line in config.yaml, and vice versa.
func nativeConfigurationLocation(err error, stage, config string, manifest *DeploymentManifest, mihomo bool) error {
	if err == nil {
		return nil
	}
	_, details := deploymentErrorFields(err)
	if details == "" {
		details = err.Error()
	}
	data := readDiagnosticFile(config)
	file := manifest.ConfigPath
	if filepath.Base(config) != filepath.Base(manifest.ConfigPath) {
		file += "（核心校验临时副本）"
	}
	for _, entry := range manifest.Files {
		if entry.Path == manifest.ConfigPath || !safeRelative(entry.Path) {
			continue
		}
		// A filename suffix is not a resource identity: rules/a.txt must not
		// match other/rules/a.txt or an unrelated absolute path outside stage.
		pathPattern := diagnosticFilePathPattern(stage, entry.Path)
		fileMention := regexp.MustCompile(pathPattern + `(?:$|[\s"':),\]])`)
		for _, outputLine := range strings.Split(details, "\n") {
			if !fileMention.MatchString(outputLine) || diagnosticInformationalLine.MatchString(outputLine) {
				continue
			}
			lower := strings.ToLower(outputLine)
			if !strings.Contains(lower, "error") && !strings.Contains(lower, "failed") && !strings.Contains(lower, "invalid") && !strings.Contains(lower, "cannot") && !strings.Contains(lower, "missing") && !diagnosticLineNumber.MatchString(lower) {
				continue
			}
			line := diagnosticFileColumnLine(outputLine, pathPattern)
			if line == 0 {
				if match := diagnosticLineNumber.FindStringSubmatch(outputLine); match != nil {
					line, _ = strconv.Atoi(match[1])
				}
			}
			if readErr := rejectSymlinkPath(stage, entry.Path); readErr != nil {
				return diagnosticLocation(err, entry.Path, entry.Name, "", 0, nil, false)
			}
			content := readDiagnosticFile(filepath.Join(stage, entry.Path))
			return diagnosticLocation(err, entry.Path, entry.Name, "", line, content, false)
		}
	}
	if relative, pathErr := filepath.Rel(stage, config); pathErr == nil && safeRelative(filepath.ToSlash(relative)) {
		pathPattern := diagnosticFilePathPattern(stage, filepath.ToSlash(relative))
		for _, outputLine := range strings.Split(details, "\n") {
			if !diagnosticInformationalLine.MatchString(outputLine) {
				if line := diagnosticFileColumnLine(outputLine, pathPattern); line > 0 {
					return diagnosticLocation(err, file, "", "", line, data, true)
				}
			}
		}
	}
	if mihomo {
		for _, item := range []struct {
			pattern *regexp.Regexp
			field   string
		}{{diagnosticMihomoRule, "rules"}, {diagnosticMihomoProxy, "proxies"}, {diagnosticMihomoGroup, "proxy-groups"}} {
			if match := item.pattern.FindStringSubmatch(details); match != nil {
				index, _ := strconv.Atoi(match[1])
				return configurationLocation(err, file, data, item.field, index)
			}
		}
	}
	// A generic "line" may belong to a nested resource. Preserve it in core
	// output, but do not label it as a config line without a proven location.
	return diagnosticLocation(err, file, "", "", 0, data, true)
}

func deploymentStageLocation(err error, cfg *Config, stage string) error {
	if err == nil {
		return nil
	}
	summary, details := deploymentErrorFields(err)
	context := "发布阶段：" + stage + "\n目标配置：" + cfg.ConfigPath
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		context += "\n文件：" + pathError.Path + "\n文件操作：" + pathError.Op
	}
	if details == "" {
		details = summary
	}
	return &deploymentDiagnosticError{summary: summary, details: diagnosticLocationDetails(context, details), cause: err}
}
