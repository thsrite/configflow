package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/ctxx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// maxReceiptBytes bounds receipt.json, which embeds the whole profile snapshot
// (inline content rules included); Python read it without a limit.
const maxReceiptBytes = 512 << 20

// Receipts owns one data directory's rule-sync baselines. L4 locks serialize
// files/pruning per Agent, never repository reads or writes.
type Receipts struct {
	dataDir string
}

var receiptDirectoryLocks leafLocks

// NewReceipts creates the single receipt owner for a repository data directory.
func NewReceipts(dataDir string) *Receipts { return &Receipts{dataDir: dataDir} }
func (r *Receipts) root(agentID string) string {
	return filepath.Join(r.dataDir, ".agent-rule-sync", digest([]byte(agentID)))
}
func (r *Receipts) folder(agentID, deploymentID string) string {
	return filepath.Join(r.root(agentID), digest([]byte(deploymentID)))
}

// Capture records immutable prepared bytes before upload. Only an applied pointer
// enrolls them; the retained set is captured BEFORE taking the leaf-store lock.
func (r *Receipts) Capture(ctx context.Context, agentID string, b *Bundle, src *Source, baseURL string, comparison *File) error {
	ctx = lifecycle.Detach(ctx)
	if agentID == "" || b.manifest.DeploymentID == "" {
		return preparation("Missing receipt publication identity")
	}
	if _, err := Fingerprint(b.manifest); err != nil {
		return err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	retained, err := retainedReceipts(ctx, repository, agentID, b.manifest.DeploymentID)
	if err != nil {
		return err
	}
	archive, err := b.Archive()
	if err != nil {
		return err
	}
	sha, err := b.SHA256()
	if err != nil {
		return err
	}
	source, err := src.snapshotJSON()
	if err != nil {
		return err
	}
	var config File
	for _, f := range b.manifest.Files {
		if f.Role == RoleConfig {
			config = f
			break
		}
	}
	if comparison == nil {
		comparison = &config
	}
	copyComparison := *comparison
	record := Receipt{Source: source, Manifest: b.Manifest(), SHA256: sha, ConfigVersion: config.SHA256, BaseURL: baseURL, ComparisonConfig: &copyComparison}
	metadata, err := encodeJSON(record)
	if err != nil {
		return err
	}
	release := receiptDirectoryLocks.lock(r.root(agentID))
	defer release()
	// Shutdown while waiting for the leaf lock writes nothing; once started,
	// the receipt directory is completed as a whole.
	if err = ctx.Err(); err != nil {
		return err
	}
	ctx = ctxx.Uninterruptible(ctx)
	folder := r.folder(agentID, b.manifest.DeploymentID)
	if err = privateDir(folder); err != nil {
		return fmt.Errorf("create receipt directory: %w", err)
	}
	if err = atomicWrite(ctx, filepath.Join(folder, "bundle-"+sha+".tar.gz"), archive); err != nil {
		return err
	}
	if err = atomicWrite(ctx, filepath.Join(folder, "receipt.json"), metadata); err != nil {
		return err
	}
	archives, err := filepath.Glob(filepath.Join(folder, "bundle-*.tar.gz"))
	if err != nil {
		return err
	}
	sort.Slice(archives, func(i, j int) bool {
		a, e := os.Stat(archives[i])
		if e != nil {
			return false
		}
		b, e := os.Stat(archives[j])
		return e == nil && a.ModTime().After(b.ModTime())
	})
	for _, old := range archives[min(2, len(archives)):] {
		if err = os.Remove(old); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	children, err := os.ReadDir(r.root(agentID))
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.IsDir() && !retained[child.Name()] {
			if err = os.RemoveAll(filepath.Join(r.root(agentID), child.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load returns no receipt for a missing, stale, foreign or corrupt baseline.
func (r *Receipts) Load(ctx context.Context, agent *agents.Record, withBundle bool) (*Receipt, error) {
	view, err := receiptAgent(ctx, agent)
	if err != nil {
		return nil, err
	}
	a := view.Applied
	if view.ID == "" || a.DeploymentID == "" || a.ProfileID != view.ProfileID || a.ServiceType != view.ServiceType || a.ConfigVersion != view.ConfigVersion || !versionPattern.MatchString(a.ConfigVersion) {
		return nil, nil
	}
	release := receiptDirectoryLocks.lock(r.root(view.ID))
	defer release()
	folder := r.folder(view.ID, a.DeploymentID)
	data, err := readPrivate(filepath.Join(folder, "receipt.json"), maxReceiptBytes)
	if err != nil {
		return nil, nil
	}
	var record Receipt
	if json.Unmarshal(data, &record) != nil {
		return nil, nil
	}
	m := record.Manifest
	if m.DeploymentID != a.DeploymentID || m.ProfileID != a.ProfileID || m.ServiceType != a.ServiceType || !digestPattern.MatchString(record.SHA256) {
		return nil, nil
	}
	if _, err = Fingerprint(m); err != nil {
		return nil, nil
	}
	if withBundle {
		archive, err := readPrivate(filepath.Join(folder, "bundle-"+record.SHA256+".tar.gz"), MaxTotalBytes+8<<20)
		if err != nil {
			return nil, nil
		}
		record.Bundle, err = Open(m, archive, record.SHA256, record.ConfigVersion, record.ComparisonConfig)
		if err != nil {
			return nil, nil
		}
	}
	return &record, nil
}

// Stamp identifies the currently pointed-to receipt without creating directories.
func (r *Receipts) Stamp(agent *agents.Record) (Stamp, bool) {
	view := receiptAgentRaw(agent)
	if view.ID == "" || view.Applied.DeploymentID == "" {
		return Stamp{}, false
	}
	return fileStamp(filepath.Join(r.folder(view.ID, view.Applied.DeploymentID), "receipt.json"))
}

// AdvanceContent stores verified refreshed bytes, then advances only an unchanged
// applied pointer with no unfinished publication. The transaction runs after L4.
func (r *Receipts) AdvanceContent(ctx context.Context, m *agents.Manager, agent *agents.Record, src *Source, b *Bundle) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	record, err := r.Load(ctx, agent, true)
	if err != nil || record == nil {
		return false, err
	}
	view := receiptAgentRaw(agent)
	if err = r.Capture(ctx, view.ID, b, src, record.BaseURL, record.ComparisonConfig); err != nil {
		return false, err
	}
	manifest := ComparisonManifest(b.Manifest(), record.ComparisonConfig)
	fingerprint, err := Fingerprint(manifest)
	if err != nil {
		return false, err
	}
	files, err := Digests(manifest)
	if err != nil {
		return false, err
	}
	return advanceApplied(ctx, m, agent, fingerprint, files)
}
