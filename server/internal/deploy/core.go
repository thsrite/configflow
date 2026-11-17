package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Bounds apply to each deployment, including supplemental resources.
const (
	MaxFileBytes       = 64 << 20
	MaxTotalBytes      = 256 << 20
	MaxFiles           = 4096
	maxRawCacheBytes   = 16 << 20
	maxRawCacheEntries = 128
	downloadWorkers    = 8
)

var digestPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func preparation(message string) error { return &PreparationError{Message: message} }
func digest(data []byte) string        { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func encodeJSON(v any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}

// RelativePath validates a delivery path, removing leading ./ components.
func RelativePath(v string) (string, error) {
	if v == "" || strings.Contains(v, `\`) || strings.IndexFunc(v, func(r rune) bool { return r < 32 }) >= 0 {
		return "", preparation("Invalid deployment file path")
	}
	for strings.HasPrefix(v, "./") {
		v = strings.TrimPrefix(v, "./")
	}
	if path.IsAbs(v) || v == "" {
		return "", preparation("Deployment files must use safe relative paths")
	}
	for _, s := range strings.Split(v, "/") {
		if s == "" || s == "." || s == ".." {
			return "", preparation("Deployment files must use safe relative paths")
		}
	}
	if strings.HasPrefix(v, ".configflow") || v == "manifest.json" {
		return "", preparation("Reserved deployment file path")
	}
	return v, nil
}

func canonical(v string) (string, error) {
	p, err := RelativePath(v)
	if err != nil {
		return "", err
	}
	if p != v || strings.ContainsRune(p, 127) {
		return "", preparation("Manifest paths must be canonical relative paths")
	}
	return p, nil
}

// Fingerprint identifies materialized file bytes independently of transaction metadata.
// Field order and compact UTF-8 JSON are frozen to the persisted Python v1 identity.
func Fingerprint(m Manifest) (string, error) {
	if m.ProtocolVersion != 1 || (m.ServiceType != "mihomo" && m.ServiceType != "mosdns" && m.ServiceType != "oxidns") {
		return "", preparation("Invalid deployment content manifest")
	}
	cp, err := canonical(m.ConfigPath)
	if err != nil {
		return "", err
	}
	if strings.Contains(cp, "/") {
		return "", preparation("Agent configuration filename must be a basename")
	}
	if len(m.Files) == 0 || len(m.Files) > MaxFiles {
		return "", preparation("Manifest must contain a complete deployment file list")
	}
	type entry struct {
		Path   string `json:"path"`
		Role   Role   `json:"role"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	entries := make([]entry, 0, len(m.Files))
	paths := map[string]bool{}
	normalized := map[string]bool{}
	configs := 0
	var total int64
	for _, f := range m.Files {
		p, err := canonical(f.Path)
		if err != nil {
			return "", err
		}
		if (f.Role != RoleConfig && f.Role != RoleProvider && f.Role != RoleRule && f.Role != RoleCustom) || f.Size < 0 || f.Size > MaxFileBytes || !digestPattern.MatchString(f.SHA256) {
			return "", preparation("Invalid deployment manifest file metadata")
		}
		if paths[p] {
			return "", preparation("Duplicate deployment manifest path")
		}
		paths[p] = true
		if f.Role == RoleConfig {
			configs++
			if p != cp || f.Size == 0 {
				return "", preparation("Invalid main configuration manifest entry")
			}
			p = "config.yaml"
		} else if p == cp {
			return "", preparation("Main configuration entry must have config role")
		}
		if normalized[p] {
			return "", preparation("Conflicting canonical deployment paths")
		}
		normalized[p] = true
		total += f.Size
		if total > MaxTotalBytes {
			return "", preparation("Deployment exceeds size limit")
		}
		entries = append(entries, entry{p, f.Role, strings.ToLower(f.SHA256), f.Size})
	}
	if configs != 1 {
		return "", preparation("Manifest must contain exactly one main configuration")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	b, err := encodeJSON(struct {
		Files       []entry `json:"files"`
		ServiceType string  `json:"service_type"`
		Version     int     `json:"version"`
	}{entries, m.ServiceType, 1})
	if err != nil {
		return "", err
	}
	return "v1:" + digest(b), nil
}

// Digests returns the validated canonical file digests used by Fingerprint.
func Digests(m Manifest) (map[string]string, error) {
	if _, err := Fingerprint(m); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m.Files))
	for _, f := range m.Files {
		p := f.Path
		if f.Role == RoleConfig {
			p = "config.yaml"
		}
		out[p] = strings.ToLower(f.SHA256)
	}
	return out, nil
}

// ComparisonManifest substitutes the original server main-file identity.
func ComparisonManifest(m Manifest, comparison *File) Manifest {
	m.Files = append([]File(nil), m.Files...)
	if comparison != nil {
		for i, f := range m.Files {
			if f.Role == RoleConfig {
				m.Files[i] = *comparison
			}
		}
	}
	return m
}

// Retarget changes only the installed main filename, keeping its bytes.
func Retarget(b *Bundle, configPath string) (*Bundle, error) {
	p, err := RelativePath(configPath)
	if err != nil {
		return nil, err
	}
	if strings.Contains(p, "/") {
		return nil, preparation("Agent configuration filename must be a basename")
	}
	m := b.Manifest()
	if p == m.ConfigPath {
		return b, nil
	}
	for _, f := range m.Files {
		if f.Path == p {
			return nil, preparation("Agent configuration filename collides with a managed file")
		}
	}
	files, err := b.Contents()
	if err != nil {
		return nil, err
	}
	files[p] = files[m.ConfigPath]
	delete(files, m.ConfigPath)
	for i := range m.Files {
		if m.Files[i].Path == m.ConfigPath {
			m.Files[i].Path = p
		}
	}
	m.ConfigPath = p
	comparison := b.Comparison()
	if comparison != nil {
		comparison.Path = p
	}
	return Pack(m, files, b.ConfigVersion(), comparison)
}

func checkContents(m Manifest, files map[string]Content) error {
	if len(files) != len(m.Files) || len(files) > MaxFiles {
		return preparation("Deployment file list does not match manifest")
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		p, err := canonical(f.Path)
		if err != nil {
			return err
		}
		if seen[p] {
			return preparation("Duplicate deployment manifest path")
		}
		seen[p] = true
		v, ok := files[p]
		if !ok || v.Role != f.Role || int64(len(v.Data)) != f.Size || digest(v.Data) != f.SHA256 {
			return preparation(fmt.Sprintf("Deployment file does not match manifest: %s", p))
		}
		if f.Size > MaxFileBytes || f.Size < 0 {
			return preparation("Deployment file exceeds size limit")
		}
		total += f.Size
		if total > MaxTotalBytes {
			return preparation("Deployment exceeds size limit")
		}
	}
	return nil
}
