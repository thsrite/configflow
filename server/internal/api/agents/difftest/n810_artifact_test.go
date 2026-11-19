package difftest

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// installerJSONText changes only delimited configuration JSON and the JSON
// literal in the fixed bootstrap program. All surrounding program bytes survive.
func installerJSONText(text string) (string, error) {
	for _, marker := range []string{"CONFIGFLOW_JSON_EOF", "CONFIGFLOW_MIHOMO_JSON_EOF", "CONFIGFLOW_OXIDNS_JSON_EOF"} {
		opening := "<<'" + marker + "'\n"
		start := 0
		for {
			offset := strings.Index(text[start:], opening)
			if offset < 0 {
				break
			}
			first := start + offset + len(opening)
			end := strings.Index(text[first:], "\n"+marker+"\n")
			if end < 0 {
				return "", fmt.Errorf("unterminated JSON heredoc")
			}
			last := first + end
			value, err := jsonx.ParseString(text[first:last])
			if err != nil {
				return "", err
			}
			data, err := jsonx.Sorted(value)
			if err != nil {
				return "", err
			}
			text = text[:first] + string(data) + text[last:]
			start = first + len(data) + len(marker) + 2
		}
	}
	const prefix = "settings = json.loads("
	start := 0
	for {
		offset := strings.Index(text[start:], prefix)
		if offset < 0 {
			break
		}
		first := start + offset + len(prefix)
		end := strings.Index(text[first:], ")\npath = ")
		if end < 0 {
			start = first
			continue
		}
		last := first + end
		literal := text[first:last]
		if len(literal) < 2 || literal[len(literal)-1] != literal[0] || (literal[0] != '\'' && literal[0] != '"') {
			return "", fmt.Errorf("invalid settings literal")
		}
		quote := literal[0]
		source := literal[1 : len(literal)-1]
		var raw strings.Builder
		for source != "" {
			r, _, tail, err := strconv.UnquoteChar(source, quote)
			if err != nil {
				return "", err
			}
			raw.WriteRune(r)
			source = tail
		}
		value, err := jsonx.ParseString(raw.String())
		if err != nil {
			return "", err
		}
		data, err := jsonx.Sorted(value)
		if err != nil {
			return "", err
		}
		normalized := strconv.Quote(string(data))
		text = text[:first] + normalized + text[last:]
		start = first + len(normalized)
	}
	return text, nil
}
func installerJSONValues(value any) (any, error) {
	switch x := value.(type) {
	case string:
		return installerJSONText(x)
	case *jsonx.Object:
		result := x.Clone()
		for key, item := range x.All() {
			v, err := installerJSONValues(item)
			if err != nil {
				return nil, err
			}
			result.Set(key, v)
		}
		return result, nil
	case []any:
		result := make([]any, len(x))
		for i, item := range x {
			v, err := installerJSONValues(item)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	}
	return value, nil
}
func installerComposeSettings(root *yamlx.Node) error {
	for _, service := range yamlx.Pairs(yamlx.Get(root, "services")) {
		env := yamlx.Get(service, "environment")
		value := yamlx.Get(env, "CONFIGFLOW_COMPOSE_INSTALL")
		if value == nil {
			continue
		}
		settings, err := jsonx.ParseString(value.Value)
		if err != nil {
			return err
		}
		data, err := jsonx.Sorted(settings)
		if err != nil {
			return err
		}
		yamlx.Set(env, "CONFIGFLOW_COMPOSE_INSTALL", yamlx.Str(string(data)))
	}
	return nil
}
func installerArchive(encoded string) (*jsonx.Object, string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", err
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range reader.File {
		input, err := entry.Open()
		if err != nil {
			return nil, "", err
		}
		data, err := io.ReadAll(input)
		input.Close()
		if err != nil {
			return nil, "", err
		}
		if entry.Name == "enrollment.json" {
			value, err := jsonx.Parse(data)
			if err != nil {
				return nil, "", err
			}
			data, err = jsonx.Sorted(value)
			if err != nil {
				return nil, "", err
			}
		}
		header := entry.FileHeader
		member, err := writer.CreateHeader(&header)
		if err != nil {
			return nil, "", err
		}
		if _, err = member.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return yamlxtest.ComposeArchive(base64.StdEncoding.EncodeToString(buffer.Bytes()))
}
func TestN810ArtifactComparisonKeepsShellBytesAndJSONValues(t *testing.T) {
	base := "echo before\ncat <<'CONFIGFLOW_JSON_EOF'\n{\"name\":\"设备\",\"port\":8080}\nCONFIGFLOW_JSON_EOF\necho after\n"
	normalized, err := installerJSONText(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		source string
		equal  bool
	}{
		{strings.Replace(base, "设备", `\u8bbe\u5907`, 1), true},
		{strings.Replace(base, "8080", "8081", 1), false},
		{strings.Replace(base, "echo before", "echo changed", 1), false},
		{strings.Replace(base, "echo after", "echo changed", 1), false},
	} {
		actual, err := installerJSONText(test.source)
		if err != nil || (actual == normalized) != test.equal {
			t.Fatalf("comparison erased meaningful bytes: %v", err)
		}
	}
}
