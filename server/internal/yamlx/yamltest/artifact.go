package yamltest

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// ComposeArchive reads a generated Compose ZIP without ignoring member names,
// permissions or non-Compose bytes. It returns the strict manifest and raw YAML
// separately so callers can enforce YAML tag/value equality before comparing it.
func ComposeArchive(encoded string) (*jsonx.Object, string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", err
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	result := jsonx.Obj()
	var compose string
	for _, file := range archive.File {
		if result.Has(file.Name) {
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
		member := jsonx.Obj("mode", file.ExternalAttrs>>16, "yaml", nil, "bytes", nil)
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
			value, err := jsonx.Parse(raw)
			if err != nil {
				return nil, "", err
			}
			member.Set("yaml", value)
		} else {
			sum := sha256.Sum256(data)
			member.Set("bytes", hex.EncodeToString(sum[:]))
		}
		result.Set(file.Name, member)
	}
	return result, compose, nil
}
