package deploy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/configgen"
)

// Artifact is the manual export view of a frozen release candidate. Artifact
// creation never reserves a device or publishes to one.
type Artifact struct {
	PreviewPublic
	Success             bool            `json:"success"`
	SubscriptionHosts   json.RawMessage `json:"subscription_hosts"`
	SubscriptionDialers json.RawMessage `json:"subscription_dialers"`
}

// GenerateArtifact materializes one pinned profile through the shared generator
// and stores its immutable bytes with the Manager's A14 preview owner.
func GenerateArtifact(ctx context.Context, m *agents.Manager, profileID string, engine configgen.Engine, baseURL string) (out Artifact, err error) {
	err = RunDetached(ctx, "manual-artifact", func(ctx context.Context) error {
		candidate, err := configgen.Capture(ctx, m.Repository(), profileID, engine, baseURL)
		if err != nil {
			return err
		}
		source, err := SourceFromJSON(candidate.Source, &candidate.Revision)
		if err != nil {
			return err
		}
		main := "config.yaml"
		switch engine {
		case configgen.Surge:
			main = "surge.conf"
		case configgen.Loon:
			main = "loon.lcf"
		}
		files := map[string]Content{}
		order := []string{}
		if engine == configgen.MosDNS || engine == configgen.OxiDNS {
			archive, err := zip.NewReader(bytes.NewReader(candidate.Archive), int64(len(candidate.Archive)))
			if err != nil {
				return fmt.Errorf("open generated archive: %w", err)
			}
			var total int64
			for _, entry := range archive.File {
				if strings.HasSuffix(entry.Name, "/") {
					continue
				}
				path, err := RelativePath(entry.Name)
				if err != nil || path != entry.Name || len(files) >= MaxFiles {
					return preparation("Invalid generated archive member")
				}
				if _, duplicate := files[path]; duplicate {
					return preparation("Duplicate generated archive member")
				}
				reader, err := entry.Open()
				if err != nil {
					return err
				}
				limit := min(int64(MaxFileBytes), int64(MaxTotalBytes)-total)
				data, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
				closeErr := reader.Close()
				if readErr != nil {
					return readErr
				}
				if closeErr != nil {
					return closeErr
				}
				total += int64(len(data))
				if total > MaxTotalBytes || int64(len(data)) > MaxFileBytes {
					return preparation("Generated archive exceeds size limit")
				}
				role := RoleCustom
				if path == main {
					role = RoleConfig
				}
				files[path] = Content{data, role}
				order = append(order, path)
			}
		} else {
			files[main] = Content{[]byte(candidate.Content), RoleConfig}
			order = append(order, main)
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		manifest := Manifest{ProtocolVersion: 1, AgentID: "manual", DeploymentID: id, ServiceType: string(engine), ProfileID: source.ProfileID(), ConfigPath: main, Files: []File{}}
		for _, path := range order {
			file := files[path]
			manifest.Files = append(manifest.Files, File{Path: path, Role: file.Role, Size: int64(len(file.Data)), SHA256: digest(file.Data)})
		}
		bundle, err := Pack(manifest, files, digest(files[main].Data)[:8], nil)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		pv, err := previewsFor(m).Save(ctx, PreviewInput{Target: Target{ID: "manual", ServiceType: string(engine)}, Source: source, Bundle: bundle, Members: []Target{}})
		if err != nil {
			return err
		}
		out = Artifact{pv.Public(), true, candidate.SubscriptionHosts, candidate.SubscriptionDialers}
		return nil
	})
	return out, err
}
