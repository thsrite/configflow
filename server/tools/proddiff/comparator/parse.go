package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var serviceRoute = regexp.MustCompile(`/(mihomo|mosdns|oxidns)(/preview)?$`)

// parsed turns a body into the value model: JSON by content type or member
// suffix, ZIP archives member by member, YAML by type/suffix/service route,
// otherwise text lines. Undecodable non-YAML bodies compare as bytes.
func parsed(body []byte, contentType, route, member string) (any, error) {
	if strings.Contains(contentType, "json") || strings.HasSuffix(member, ".json") {
		return loadJSON(body)
	}
	if bytes.HasPrefix(body, []byte("PK\x03\x04")) {
		return parseZIP(body, route)
	}
	yamlLike := strings.Contains(contentType, "yaml") || strings.HasSuffix(member, ".yaml") || strings.HasSuffix(member, ".yml")
	if !utf8.Valid(body) {
		if yamlLike {
			return nil, errors.New("invalid utf-8 yaml")
		}
		return body, nil
	}
	if yamlLike || member == "" && serviceRoute.MatchString(route) {
		return loadYAML(body)
	}
	return textLines(string(body)), nil
}

func parseZIP(body []byte, route string) (any, error) {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, file := range archive.File {
		if names[file.Name] {
			return nil, errDuplicateKey
		}
		names[file.Name] = true
	}
	result := newMap()
	for _, file := range archive.File {
		if strings.HasSuffix(file.Name, "/") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return nil, err
		}
		value, err := parsed(raw, "", route, file.Name)
		if err != nil {
			return nil, err
		}
		result.Set(file.Name, value)
		crlf, cr := lineEndings(raw)
		result.Set(file.Name+"#eol", []any{crlf, cr})
		// Unix mode bits: an install script that loses +x is a real regression.
		result.Set(file.Name+"#mode", int64(file.ExternalAttrs>>16))
	}
	return result, nil
}

// normalText folds CR/CRLF to LF and keeps exactly one final newline.
func normalText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimRight(value, "\n") + "\n"
}

// textLines compares text line by line; only the number of final newlines is ignored.
func textLines(value string) any {
	lines := splitLines(normalText(value))
	out := make([]any, len(lines))
	for i, line := range lines {
		out[i] = line
	}
	return out
}

// splitLines is Python str.splitlines(): every Unicode line boundary splits,
// and a trailing boundary adds no empty line.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, s[start:i])
			i += size
			start = i
			continue
		case '\r':
			lines = append(lines, s[start:i])
			i += size
			if i < len(s) && s[i] == '\n' {
				i++
			}
			start = i
			continue
		}
		i += size
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// lineEndings reports CRLF and lone-CR use; CRLF in a shell script is a functional change.
func lineEndings(body []byte) (bool, bool) {
	crlf := bytes.Count(body, []byte("\r\n"))
	return crlf > 0, bytes.Count(body, []byte("\r"))-crlf > 0
}

func utf8Valid(b []byte) bool { return utf8.Valid(b) }
