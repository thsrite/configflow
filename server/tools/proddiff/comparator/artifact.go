package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// artifactRoutes are the manual artifact POSTs whose independent preview IDs,
// expiry and digests are proven before pairing.
var artifactRoutes = map[string]bool{"/api/generate/<service>/artifact": true,
	"/api/profiles/<profile_id>/generate/<service>/artifact": true}

var artifactID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// errArtifactProof is any failed proof; the side is reported as a difference.
var errArtifactProof = errors.New("artifact proof failed")

// errArtifactMainFile is the framework failure of a proven record without its main file.
var errArtifactMainFile = errors.New("artifact proof has no main config file")

type artifactProof struct {
	body     any
	semantic []any
}

// artifactProofs proves frozen bytes against each side's private data copy
// before pairing independent manual-artifact identities.
type artifactProofs struct {
	roots []string
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]artifactProof
	pairs map[string]map[string]any
}

func newArtifactProofs(roots []string) *artifactProofs {
	return &artifactProofs{roots: roots, now: time.Now, cache: map[string]artifactProof{},
		pairs: map[string]map[string]any{"id": {}, "deployment_id": {}}}
}

func field(v any, key string) (any, error) {
	m, ok := v.(*Map)
	if !ok {
		return nil, errArtifactProof
	}
	value, ok := m.Get(key)
	if !ok {
		return nil, errArtifactProof
	}
	return value, nil
}

func stringField(v any, key string) (string, error) {
	value, err := field(v, key)
	if err != nil {
		return "", err
	}
	s, ok := value.(string)
	if !ok {
		return "", errArtifactProof
	}
	return s, nil
}

func (p *artifactProofs) prove(sideIndex int, body *Map) ([]any, error) {
	key, err := stringField(body, "id")
	if err != nil || !artifactID.MatchString(key) {
		return nil, errArtifactProof
	}
	cacheKey := string(rune('0'+sideIndex)) + key
	p.mu.Lock()
	cached, ok := p.cache[cacheKey]
	p.mu.Unlock()
	if ok {
		if !pyEqual(cached.body, body) {
			return nil, errArtifactProof
		}
		return cached.semantic, nil
	}
	agentID, _ := field(body, "agent_id")
	profileID, _ := field(body, "profile_id")
	targets, _ := field(body, "target_ids")
	deploymentID, err := stringField(body, "deployment_id")
	if err != nil {
		return nil, err
	}
	expires, err := field(body, "expires_at")
	if err != nil {
		return nil, err
	}
	expiresAt, isInt := expires.(int64)
	now := float64(p.now().UnixNano()) / 1e9
	if !pyEqual(agentID, "manual") || !truthy(profileID) || !pyEqual(targets, []any{}) ||
		!artifactID.MatchString(deploymentID) || !isInt ||
		!(now+3400 < float64(expiresAt) && float64(expiresAt) <= now+3601) {
		return nil, errArtifactProof
	}
	folder := filepath.Join(p.roots[sideIndex], ".generated-previews", key)
	for _, path := range []string{folder, filepath.Join(folder, "record.json"), filepath.Join(folder, "bundle.tar.gz")} {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errArtifactProof
		}
	}
	recordBytes, err := os.ReadFile(filepath.Join(folder, "record.json"))
	if err != nil {
		return nil, errArtifactProof
	}
	record, err := loadJSONLoose(recordBytes)
	if err != nil {
		return nil, errArtifactProof
	}
	for _, name := range []string{"id", "agent_id", "profile_id", "service_type", "deployment_id", "expires_at"} {
		stored, err := field(record, name)
		if err != nil {
			return nil, err
		}
		public, err := field(body, name)
		if err != nil {
			return nil, err
		}
		if !pyEqual(stored, public) {
			return nil, errArtifactProof
		}
	}
	meta, err := field(record, "bundle_meta")
	if err != nil {
		return nil, err
	}
	archive, err := os.ReadFile(filepath.Join(folder, "bundle.tar.gz"))
	if err != nil {
		return nil, errArtifactProof
	}
	sum := sha256.Sum256(archive)
	digest, err := field(meta, "sha256")
	if err != nil || !pyEqual(digest, hex.EncodeToString(sum[:])) {
		return nil, errArtifactProof
	}
	manifest, err := field(meta, "manifest")
	if err != nil {
		return nil, err
	}
	for _, pair := range [][2]string{{"files", "files"}, {"config_path", "config_path"}, {"agent_id", "agent_id"},
		{"profile_id", "profile_id"}, {"service_type", "service_type"}, {"deployment_id", "deployment_id"}} {
		m, err := field(manifest, pair[0])
		if err != nil {
			return nil, err
		}
		b, err := field(body, pair[1])
		if err != nil {
			return nil, err
		}
		if !pyEqual(m, b) {
			return nil, errArtifactProof
		}
	}
	data, err := readArtifactTar(archive)
	if err != nil {
		return nil, err
	}
	manifestBytes, ok := data["manifest.json"]
	if !ok {
		return nil, errArtifactProof
	}
	delete(data, "manifest.json")
	packed, err := loadJSONLoose(manifestBytes)
	if err != nil || !pyEqual(packed, manifest) {
		return nil, errArtifactProof
	}
	filesValue, _ := field(body, "files")
	files, ok := filesValue.([]any)
	if !ok {
		return nil, errArtifactProof
	}
	expected := map[string]bool{}
	for _, file := range files {
		path, err := stringField(file, "path")
		if err != nil {
			return nil, err
		}
		expected["files/"+path] = true
	}
	if len(expected) != len(data) {
		return nil, errArtifactProof
	}
	for name := range data {
		if !expected[name] {
			return nil, errArtifactProof
		}
	}
	configPath, _ := field(body, "config_path")
	content, _ := field(body, "content")
	root := p.roots[sideIndex]
	var semantic []any
	for _, file := range files {
		path, _ := stringField(file, "path")
		if path == "" || strings.Contains(path, `\`) || strings.HasPrefix(path, "/") {
			return nil, errArtifactProof
		}
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, errArtifactProof
			}
		}
		raw := data["files/"+path]
		size, err := field(file, "size")
		if err != nil {
			return nil, err
		}
		fileDigest, err := field(file, "sha256")
		if err != nil {
			return nil, err
		}
		rawSum := sha256.Sum256(raw)
		if !pyEqual(size, int64(len(raw))) || !pyEqual(fileDigest, hex.EncodeToString(rawSum[:])) {
			return nil, errArtifactProof
		}
		scrubbed := bytes.ReplaceAll(raw, []byte(root), []byte("<DATA_DIR>"))
		if pyEqual(configPath, path) {
			if !utf8Valid(scrubbed) || !pyEqual(string(scrubbed), content) {
				return nil, errArtifactProof
			}
		}
		role, err := field(file, "role")
		if err != nil {
			return nil, err
		}
		parsedContent, err := parsed(scrubbed, "", "", path)
		if err != nil {
			return nil, errArtifactProof
		}
		crlf := bytes.Contains(raw, []byte("\r\n"))
		cr := bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\r"))
		entry := newMap()
		entry.Set("path", path)
		entry.Set("role", role)
		entry.Set("content", parsedContent)
		entry.Set("eol", []any{crlf, cr})
		semantic = append(semantic, entry)
	}
	p.mu.Lock()
	p.cache[cacheKey] = artifactProof{body: deepCopy(body), semantic: semantic}
	p.mu.Unlock()
	return semantic, nil
}

func readArtifactTar(archive []byte) (map[string][]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, errArtifactProof
	}
	tr := tar.NewReader(zr)
	data := map[string][]byte{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, errArtifactProof
		}
		regular := header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA || header.Typeflag == tar.TypeCont
		if _, dup := data[header.Name]; !regular || dup || header.Size > 64<<20 {
			return nil, errArtifactProof
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			return nil, errArtifactProof
		}
		data[header.Name] = raw
	}
}

// normalize proves both sides, then pairs and masks the independent fields.
func (p *artifactProofs) normalize(a, b any) ([]diff, error) {
	am, aok := a.(*Map)
	bm, bok := b.(*Map)
	if !aok || !bok {
		return nil, nil
	}
	as, _ := am.Get("success")
	bs, _ := bm.Get("success")
	if as != true || bs != true {
		return nil, nil
	}
	var proven [2][]any
	for i, body := range []*Map{am, bm} {
		semantic, err := p.prove(i, body)
		if err != nil {
			// The side label is the historical one: index 0 was the Python reference.
			return []diff{{"body.files", "invalid-artifact-proof-" + []string{"python", "go"}[i]}}, nil
		}
		proven[i] = semantic
	}
	p.mu.Lock()
	for _, name := range []string{"id", "deployment_id"} {
		left, _ := am.Get(name)
		right, _ := bm.Get(name)
		pairs := p.pairs[name]
		leftKey := left.(string)
		if old, ok := pairs[leftKey]; ok && !pyEqual(old, right) {
			p.mu.Unlock()
			return []diff{{"body." + name, "unstable-artifact-identity"}}, nil
		}
		for other, value := range pairs {
			if other != leftKey && pyEqual(value, right) {
				p.mu.Unlock()
				return []diff{{"body." + name, "unstable-artifact-identity"}}, nil
			}
		}
		pairs[leftKey] = right
	}
	p.mu.Unlock()
	ae, _ := am.Get("expires_at")
	be, _ := bm.Get("expires_at")
	skew := ae.(int64) - be.(int64)
	if skew > 120 || skew < -120 {
		return []diff{{"body.expires_at", "artifact-expiration-skew"}}, nil
	}
	for i, body := range []*Map{am, bm} {
		body.Set("id", "<proven-independent-identity>")
		body.Set("deployment_id", "<proven-independent-identity>")
		body.Set("expires_at", "<proven-one-hour-expiration>")
		filesValue, _ := body.Get("files")
		for _, file := range filesValue.([]any) {
			file.(*Map).Set("size", "<verified-frozen-bytes>")
			file.(*Map).Set("sha256", "<verified-frozen-bytes>")
		}
		configPath, _ := body.Get("config_path")
		var main any
		found := false
		for _, entry := range proven[i] {
			path, _ := entry.(*Map).Get("path")
			if pyEqual(path, configPath) {
				main, _ = entry.(*Map).Get("content")
				found = true
				break
			}
		}
		if !found {
			return nil, errArtifactMainFile
		}
		body.Set("content", main)
		body.Set("frozen_files", proven[i])
	}
	return nil, nil
}

// loadJSONLoose is json.loads without the duplicate-key check (private records).
func loadJSONLoose(data []byte) (any, error) {
	if !utf8Valid(data) {
		return nil, errArtifactProof
	}
	return parseJSON(string(data), true)
}
