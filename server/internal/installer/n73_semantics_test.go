package installer

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

var nativeArtifactReferences = sync.OnceValues(func() (map[string]string, error) {
	file, err := os.Open("testdata/n73_semantics.json.gz")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var result map[string]string
	err = json.NewDecoder(reader).Decode(&result)
	return result, err
})

func nativeArtifactDigest(value any) (string, error) {
	data, err := jsonx.Encode(value, jsonx.Options{SortKeys: true, Compact: true})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func nativeZipValue(encoded string) (*jsonx.Object, error) {
	manifest, _, err := yamlxtest.ComposeArchive(encoded)
	return manifest, err
}

func nativeFieldValue(value any, key string) (any, error) {
	if key == "compose" {
		if text, ok := value.(string); ok {
			node, err := yamlx.Parse([]byte(text))
			if err != nil {
				return nil, err
			}
			data, err := yamlx.ToJSON(node)
			if err != nil {
				return nil, err
			}
			return jsonx.Parse(data)
		}
	}
	switch value := value.(type) {
	case *jsonx.Object:
		result := jsonx.Obj()
		for key, item := range value.All() {
			var next any
			var err error
			if key == "content_base64" {
				next, err = nativeZipValue(item.(string))
			} else {
				next, err = nativeFieldValue(item, key)
			}
			if err != nil {
				return nil, err
			}
			result.Set(key, next)
		}
		return result, nil
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			next, err := nativeFieldValue(item, "")
			if err != nil {
				return nil, err
			}
			result[i] = next
		}
		return result, nil
	}
	return value, nil
}

func nativeFieldMatches(key string, value any, recorded string) bool {
	references, err := nativeArtifactReferences()
	if err != nil {
		return false
	}
	expected, ok := references[key+":"+recorded]
	if !ok {
		return false
	}
	semantic, err := nativeFieldValue(value, key)
	if err != nil {
		return false
	}
	digest, err := nativeArtifactDigest(semantic)
	return err == nil && digest == expected
}

func nativeOutcomeEqual(want, got any, key string) bool {
	if key == "compose" {
		a, aok := want.(string)
		b, bok := got.(string)
		return aok && bok && writerYAMLEqual(a, b)
	}
	switch expected := want.(type) {
	case *jsonx.Object:
		actual, ok := got.(*jsonx.Object)
		if !ok || expected == nil || actual == nil {
			return canonical(want) == canonical(got)
		}
		expected, actual = expected.Clone(), actual.Clone()
		if hash := expected.GetString("content_sha256"); hash != "" && actual.Has("content_base64") {
			reference, err := nativeExpectedZipValue(hash)
			if err != nil {
				return false
			}
			manifest, text, err := yamlxtest.ComposeArchive(actual.GetString("content_base64"))
			references, referenceErr := nativeArtifactReferences()
			if err != nil || referenceErr != nil || !writerYAMLEqual(references["yaml:"+hash], text) || canonical(reference) != canonical(manifest) {
				return false
			}
			expected.Delete("content_sha256")
			actual.Delete("content_base64")
		} else if expected.Has("content_base64") && actual.Has("content_base64") {
			a, aText, err := yamlxtest.ComposeArchive(expected.GetString("content_base64"))
			if err != nil {
				return false
			}
			b, bText, err := yamlxtest.ComposeArchive(actual.GetString("content_base64"))
			if err != nil {
				return false
			}
			if !writerYAMLEqual(aText, bText) || canonical(a) != canonical(b) {
				return false
			}
			expected.Delete("content_base64")
			actual.Delete("content_base64")
		}
		if expected.Len() != actual.Len() {
			return false
		}
		for name, value := range expected.All() {
			next, ok := actual.Get(name)
			if !ok || !nativeOutcomeEqual(value, next, name) {
				return false
			}
		}
		return true
	case []any:
		actual, ok := got.([]any)
		if !ok || len(expected) != len(actual) {
			return false
		}
		for i, value := range expected {
			if !nativeOutcomeEqual(value, actual[i], "") {
				return false
			}
		}
		return true
	}
	return canonical(want) == canonical(got)
}

// nativeExpectedZipValue parses independently generated Python bytes with the
// same native schema used to compare both generated outputs.
func nativeExpectedZipValue(identity string) (*jsonx.Object, error) {
	references, err := nativeArtifactReferences()
	if err != nil {
		return nil, err
	}
	value, err := jsonx.ParseString(references["manifest:"+identity])
	if err != nil {
		return nil, err
	}
	manifest, ok := value.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("invalid reference manifest")
	}
	node, err := yamlx.Parse([]byte(references["yaml:"+identity]))
	if err != nil {
		return nil, err
	}
	raw, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	compose, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	manifest.GetObject("compose.yaml").Set("yaml", compose)
	return manifest, nil
}
