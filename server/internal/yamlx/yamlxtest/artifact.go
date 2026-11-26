package yamlxtest

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"configflow/internal/yamlx"
)

// ComposeArchive reads a generated Compose ZIP without ignoring member names,
// permissions or non-Compose bytes. It returns the strict manifest and raw YAML
// separately so callers can enforce YAML tag/value equality before comparing it.
func ComposeArchive(encoded string) (map[string]any, string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", err
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	result := map[string]any{}
	var compose string
	for _, file := range archive.File {
		if _, exists := result[file.Name]; exists {
			return nil, "", fmt.Errorf("duplicate archive member")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, "", err
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return nil, "", err
		}
		member := map[string]any{"mode": file.ExternalAttrs >> 16, "yaml": nil, "bytes": nil}
		if file.Name == "compose.yaml" {
			compose = string(data)
			node, err := yamlx.Parse(data)
			if err != nil {
				return nil, "", err
			}
			raw, err := yamlx.ToJSON(node)
			if err != nil {
				return nil, "", err
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			var value any
			if err := dec.Decode(&value); err != nil {
				return nil, "", err
			}
			member["yaml"] = value
		} else {
			sum := sha256.Sum256(data)
			member["bytes"] = hex.EncodeToString(sum[:])
		}
		result[file.Name] = member
	}
	return result, compose, nil
}
