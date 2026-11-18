package rulesync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/deploy"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// ReadBundleFiles verifies a trusted immutable deployment and returns its files.
func ReadBundleFiles(b *deploy.Bundle) (map[string]deploy.Content, error) {
	if b == nil {
		return nil, refuse(msgSnapshotVerify)
	}
	return b.Contents()
}

// FetchSnapshot downloads authenticated managed live files without proxying,
// redirects, extraction or accepting files outside the successful deployment.
func FetchSnapshot(ctx context.Context, m *agents.Manager, agent *agents.Record, baseline Baseline) (*deploy.Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previous := baseline.Bundle
	if previous == nil && baseline.Receipt != nil {
		previous = baseline.Receipt.Bundle
	}
	expected, err := ReadBundleFiles(previous)
	if err != nil {
		return nil, err
	}
	origin, err := monitor.AgentOrigin(agent)
	if err != nil {
		return nil, refuse(msgSnapshotVerify)
	}
	a, err := decodeAgent(agent)
	if err != nil {
		return nil, err
	}
	res, err := m.Client().Do(ctx, agents.Request{Method: http.MethodGet, URL: origin + "/api/rule-sync/snapshot", Header: http.Header{"Authorization": {"Bearer " + a.Token}, "Accept": {"application/gzip"}}, Timeout: agents.Pair(3, 90), NoProxy: true, NoRedirects: true, MaxBody: deploy.MaxTotalBytes + 8<<20})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, refuse(msgSnapshotVerify)
	}
	if res.StatusCode != http.StatusOK {
		return nil, refuse(msgSnapshotUnavailable)
	}
	if len(res.Body) > deploy.MaxTotalBytes+8<<20 {
		return nil, refuse(msgSnapshotLarge)
	}
	if res.BodyError != nil {
		return nil, refuse(msgSnapshotVerify)
	}
	version := a.ConfigVersion
	if a.AppliedContent != nil && a.AppliedContent.ConfigVersion != "" {
		version = a.AppliedContent.ConfigVersion
	}
	return parseSnapshot(ctx, res.Body, previous.Manifest(), expected, version)
}

// parseSnapshot bounds both compressed bytes and every expanded byte, including
// tar extension headers and trailing data; nothing is ever extracted to disk.
func parseSnapshot(ctx context.Context, data []byte, expected deploy.Manifest, expectedFiles map[string]deploy.Content, version string) (*deploy.Bundle, error) {
	if len(data) > deploy.MaxTotalBytes+8<<20 {
		return nil, refuse(msgSnapshotLarge)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, refuse(msgSnapshotVerify)
	}
	defer gz.Close()
	limit := int64(deploy.MaxTotalBytes + deploy.MaxFiles*2048 + 3<<20)
	budget := &io.LimitedReader{R: gz, N: limit + 1}
	archive := tar.NewReader(budget)
	files := make(map[string]deploy.Content, len(expectedFiles))
	seen := make(map[string]bool, len(expectedFiles)+1)
	var manifest *deploy.Manifest
	total := int64(0)
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		h, e := archive.Next()
		if budget.N <= 0 {
			return nil, refuse(msgSnapshotExpanded)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, refuse(msgSnapshotVerify)
		}
		if seen[h.Name] || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) || len(seen) >= deploy.MaxFiles+1 {
			return nil, refuse(msgSnapshotInvalid)
		}
		seen[h.Name] = true
		if h.Name == "manifest.json" {
			if h.Size > 2<<20 {
				return nil, refuse(msgSnapshotManifestLarge)
			}
			raw, e := io.ReadAll(io.LimitReader(archive, (2<<20)+1))
			if e != nil {
				return nil, refuse(msgSnapshotVerify)
			}
			var v deploy.Manifest
			if json.Unmarshal(raw, &v) != nil {
				return nil, refuse(msgSnapshotVerify)
			}
			manifest = &v
			continue
		}
		if len(h.Name) < 6 || h.Name[:6] != "files/" {
			return nil, refuse(msgSnapshotForeign)
		}
		path := h.Name[6:]
		original, ok := expectedFiles[path]
		if !ok || h.Size < 0 || h.Size > deploy.MaxFileBytes {
			return nil, refuse(msgSnapshotForeign)
		}
		total += h.Size
		if total > deploy.MaxTotalBytes {
			return nil, refuse(msgSnapshotLarge)
		}
		content, e := io.ReadAll(archive)
		if e != nil {
			return nil, refuse(msgSnapshotVerify)
		}
		files[path] = deploy.Content{Data: content, Role: original.Role}
	}
	// Consume gzip's checksum and any extra tar padding under the same budget.
	if _, err = io.Copy(io.Discard, budget); err != nil {
		return nil, refuse(msgSnapshotVerify)
	}
	if budget.N <= 0 {
		return nil, refuse(msgSnapshotExpanded)
	}
	if manifest == nil || len(files) != len(expectedFiles) {
		return nil, refuse(msgSnapshotIncomplete)
	}
	m := *manifest
	if m.ProtocolVersion != expected.ProtocolVersion || m.DeploymentID != expected.DeploymentID || m.AgentID != expected.AgentID || m.ProfileID != expected.ProfileID || m.ServiceType != expected.ServiceType || m.ConfigPath != expected.ConfigPath {
		return nil, refuse(msgSnapshotChanged)
	}
	if len(m.Files) != len(files) {
		return nil, refuse(msgManifestIncomplete)
	}
	checked := make(map[string]bool, len(files))
	for _, entry := range m.Files {
		file, ok := files[entry.Path]
		if checked[entry.Path] || !ok {
			return nil, refuse(msgManifestInvalid)
		}
		checked[entry.Path] = true
		if entry.Role != file.Role || entry.Size != int64(len(file.Data)) || entry.SHA256 != digest(file.Data) {
			return nil, refuse(msgSnapshotDigest)
		}
	}
	main, ok := files[expected.ConfigPath]
	if !ok {
		return nil, refuse(msgSnapshotIncomplete)
	}
	sum := digest(main.Data)
	if version != sum && version != sum[:8] {
		return nil, refuse(msgMainChanged)
	}
	if _, err = deploy.Fingerprint(m); err != nil {
		return nil, refuse(msgSnapshotVerify)
	}
	return deploy.Pack(m, files, sum, nil)
}
