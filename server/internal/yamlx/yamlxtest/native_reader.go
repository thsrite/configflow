package yamlxtest

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// NativeReaderMode selects the former direct yaml.v3 reader being migrated.
type NativeReaderMode uint8

const (
	// NativeAST reads one document without decoding its values.
	NativeAST NativeReaderMode = iota
	// NativeDecoded also validates the document by decoding into an interface.
	NativeDecoded
)

// CompareNativeReader compares the former native decoder with Parse. It is
// migration-only evidence; server packages never use the direct decoder.
func CompareNativeReader(source string, mode NativeReaderMode) (string, error) {
	document, oldErr := FormerNativeDocument(source)
	old := yamlx.Null()
	if oldErr == nil && len(document.Content) != 0 {
		old = document.Content[0]
	}
	if oldErr == nil && mode == NativeDecoded {
		var value any
		oldErr = old.Decode(&value)
	}
	native, err := yamlx.Parse([]byte(source))
	switch {
	case errors.Is(err, yamlx.ErrAliasRecursion), errors.Is(err, yamlx.ErrAliasBudget):
		return "unsafe-alias", nil
	case errors.Is(err, yamlx.ErrDuplicateKey):
		return "D1-duplicate", nil
	case errors.Is(oldErr, yamlx.ErrMultipleDocuments) && errors.Is(err, yamlx.ErrMultipleDocuments):
		// The former direct reader supplied no position for the second document.
		return "诊断位置", nil
	case oldErr != nil && err != nil:
		return "native-diagnostics", nil
	case errors.Is(err, yamlx.ErrPolicy):
		return "reader-policy", nil
	case oldErr == nil && err == nil && yamlx.Equal(old, native):
		return "equal", nil
	default:
		return "", fmt.Errorf("unclassified native reader change: old=%v new=%v", oldErr, err)
	}
}

// FormerNativeDocument is the single test-only pre-facade yaml.v3 AST reader.
// It deliberately applies no new policies: migration tests compare the actual
// old native reader and unchanged builders to the validated production facade.
// Runtime packages must never import yamlxtest.
func FormerNativeDocument(source string) (*yamlx.Node, error) {
	d := yaml.NewDecoder(strings.NewReader(source))
	var document, extra yaml.Node
	if err := d.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			document.Kind = yaml.DocumentNode
			return &document, nil
		}
		return nil, err
	}
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = yamlx.ErrMultipleDocuments
		}
		return nil, err
	}
	return &document, nil
}
