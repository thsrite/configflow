// Command baseline migrates historical YAML formatting only in proddiff copies.
// It is never linked into the panel or exposed as a production migration.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"

	"configflow/internal/deploy"
	"configflow/internal/yamlx"
)

type proof struct {
	YAMLEqual        bool   `json:"yaml_equal"`
	NonMainEqual     bool   `json:"non_main_equal"`
	MainBytesChanged bool   `json:"main_bytes_changed"`
	OldFingerprint   string `json:"old_fingerprint"`
	NewFingerprint   string `json:"new_fingerprint"`
}

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// certify compares independently reopened archives. Equality of a file list or
// of digests alone is insufficient: every non-main member must have equal bytes.
func certify(old, new *deploy.Bundle) (proof, error) {
	var result proof
	a, b := old.Manifest(), new.Manifest()
	if a.ConfigPath != b.ConfigPath || a.ServiceType != b.ServiceType {
		return result, errors.New("baseline target changed")
	}
	expected := a
	expected.Files = append([]deploy.File(nil), a.Files...)
	for i, file := range expected.Files {
		if file.Role == deploy.RoleConfig {
			for _, next := range b.Files {
				if next.Path == file.Path {
					expected.Files[i].SHA256, expected.Files[i].Size = next.SHA256, next.Size
				}
			}
		}
	}
	if !reflect.DeepEqual(expected, b) || old.ConfigVersion() != new.ConfigVersion() {
		return result, errors.New("baseline metadata changed outside main byte identity")
	}
	left, err := old.Files()
	if err != nil {
		return result, err
	}
	right, err := new.Files()
	if err != nil {
		return result, err
	}
	if len(left) != len(right) {
		return result, errors.New("baseline file set changed")
	}
	result.NonMainEqual = true
	for path, content := range left {
		other, ok := right[path]
		if !ok {
			return result, errors.New("baseline member missing")
		}
		if path != a.ConfigPath && !bytes.Equal(content, other) {
			result.NonMainEqual = false
			return result, errors.New("non-main bytes changed")
		}
	}
	x, err := yamlx.Parse(left[a.ConfigPath])
	if err != nil {
		return result, err
	}
	y, err := yamlx.Parse(right[b.ConfigPath])
	if err != nil {
		return result, err
	}
	result.YAMLEqual = yamlx.Equal(x, y)
	result.MainBytesChanged = !bytes.Equal(left[a.ConfigPath], right[b.ConfigPath])
	if !result.YAMLEqual {
		return result, errors.New("main YAML semantics changed")
	}
	result.OldFingerprint, err = deploy.Fingerprint(deploy.ComparisonManifest(a, old.Comparison()))
	if err != nil {
		return result, err
	}
	result.NewFingerprint, err = deploy.Fingerprint(deploy.ComparisonManifest(b, new.Comparison()))
	return result, err
}

func converted(old *deploy.Bundle, nativeMain []byte) (*deploy.Bundle, proof, error) {
	m := old.Manifest()
	contents, err := old.Contents()
	if err != nil {
		return nil, proof{}, err
	}
	found := false
	for i, f := range m.Files {
		if f.Role == deploy.RoleConfig {
			if f.Path != m.ConfigPath || found {
				return nil, proof{}, errors.New("ambiguous baseline main")
			}
			found = true
			if comparison := old.Comparison(); comparison != nil && (comparison.SHA256 != f.SHA256 || comparison.Size != f.Size) {
				return nil, proof{}, errors.New("adapted baseline has no original YAML bytes")
			}
			f.SHA256 = hash(nativeMain)
			f.Size = int64(len(nativeMain))
			m.Files[i] = f
			contents[f.Path] = deploy.Content{Data: nativeMain, Role: deploy.RoleConfig}
		}
	}
	if !found {
		return nil, proof{}, errors.New("baseline has no main")
	}
	var main deploy.File
	for _, f := range m.Files {
		if f.Role == deploy.RoleConfig {
			main = f
		}
	}
	next, err := deploy.Pack(m, contents, old.ConfigVersion(), &main)
	if err != nil {
		return nil, proof{}, err
	}
	raw, err := next.Archive()
	if err != nil {
		return nil, proof{}, err
	}
	// The proof reads the actual encoded output, not the proposed in-memory map.
	reopened, err := deploy.Open(next.Manifest(), raw, hash(raw), next.ConfigVersion(), next.Comparison())
	if err != nil {
		return nil, proof{}, fmt.Errorf("reopen migrated baseline: %w", err)
	}
	result, err := certify(old, reopened)
	return reopened, result, err
}
