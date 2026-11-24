package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/repo/staterows"
	"configflow/internal/server"
	"configflow/internal/yamlx"
)

type plan struct {
	folder            string
	archive, metadata []byte
	sha               string
}

func privateWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
func writeJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return privateWrite(path, raw)
}
func run() (resultErr error) {
	if len(os.Args) != 3 || os.Getenv("PRODDIFF_BASELINE_MIGRATION") != "1" {
		return errors.New("requires isolated proddiff mode and private proof directory")
	}
	data, err := filepath.Abs(os.Args[1])
	if err != nil {
		return err
	}
	proofDir, err := filepath.Abs(os.Args[2])
	if err != nil {
		return err
	}
	report := filepath.Dir(proofDir)
	relative, err := filepath.Rel(report, data)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.Base(data) != "go" || !strings.HasPrefix(filepath.Base(filepath.Dir(data)), "copies-") {
		return errors.New("refuse migration outside a proddiff Go copy")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	if len(interfaces) != 1 || interfaces[0].Flags&net.FlagLoopback == 0 {
		return errors.New("baseline migration requires isolated loopback network")
	}
	if filepath.Base(proofDir) != "baseline-format" || !strings.HasPrefix(filepath.Base(report), "run-") {
		return errors.New("baseline proofs must remain in private run directory")
	}
	defer func() {
		if resultErr != nil {
			_ = privateWrite(filepath.Join(proofDir, "error.txt"), []byte(resultErr.Error()))
		}
	}()
	repository, err := repo.OpenDefault(data)
	if err != nil {
		return err
	}
	if os.Getenv("PRODDIFF_BASELINE_UPGRADE") == "1" {
		// Opening upgraded the copy to the current storage version; the
		// driver takes its "before" rows only after this step.
		repository.Close()
		return nil
	}
	repo.SetRepository(repository)
	_ = server.NewApp() // normal generator/owner hooks; no periodic rounds are started.
	if err = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
		return err
	}
	lifecycle.Start(context.Background())
	defer lifecycle.Stop(8 * time.Second)
	ctx := lifecycle.BindRequest(repo.WithRepository(context.Background(), repository))
	manager, err := agents.New(ctx, repository)
	if err != nil {
		return err
	}
	defer manager.Close()
	system, err := repository.System(ctx)
	if err != nil {
		return err
	}
	records := system.Agents
	receipts := deploy.NewReceipts(data)
	plans := []plan{}
	migrated := map[int]bool{}
	for i := range records {
		record := &records[i]
		applied := record.AppliedContent
		if applied == nil {
			continue
		}
		appliedRaw, _, err := model.EncodeMember(record, "applied_content")
		if err != nil {
			return err
		}
		var appliedFields map[string]json.RawMessage
		if err := json.Unmarshal(appliedRaw, &appliedFields); err != nil || len(appliedFields) == 0 {
			continue
		}
		target, err := deploy.NewTypedTarget(record)
		if err != nil {
			return err
		}
		receipt, err := receipts.LoadAgent(ctx, target.Record, true)
		if err != nil {
			return err
		}
		if receipt == nil {
			return errors.New("historical applied baseline has no readable receipt")
		}
		cfg, err := repository.Config(ctx, applied.ProfileID, nil)
		if err != nil {
			return err
		}
		cfgRaw, err := model.Marshal(cfg)
		if err != nil {
			return err
		}
		source, err := deploy.SourceFromJSON(cfgRaw, nil)
		if err != nil {
			return err
		}
		candidate, err := deploy.New(deploy.Options{}).PrepareContentBundle(ctx, manager, target, source, receipt.BaseURL, "format-proof")
		if err != nil {
			return fmt.Errorf("prepare native YAML for baseline proof: %w", err)
		}
		native, err := candidate.Files(candidate.Manifest().ConfigPath)
		if err != nil {
			return err
		}
		next, evidence, proofErr := converted(receipt.Bundle, native[candidate.Manifest().ConfigPath])
		key := hash([]byte(target.ID))
		folder := filepath.Join(proofDir, key)
		oldBytes, err := receipt.Bundle.Archive()
		if err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(folder, "before.tar.gz"), oldBytes); err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(folder, "candidate-main.yaml"), native[candidate.Manifest().ConfigPath]); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(folder, "proof.json"), evidence); err != nil {
			return err
		}
		if proofErr != nil {
			return proofErr
		}
		if evidence.OldFingerprint != applied.Fingerprint {
			return errors.New("applied fingerprint does not attest old archive")
		}
		oldDigests, err := deploy.Digests(deploy.ComparisonManifest(receipt.Manifest, receipt.ComparisonConfig))
		if err != nil {
			return err
		}
		oldRaw, err := json.Marshal(oldDigests)
		if err != nil {
			return err
		}
		oldNode, err := yamlx.FromJSON(oldRaw)
		if err != nil {
			return err
		}
		appliedFilesRaw := appliedFields["files"]
		appliedFilesNode, err := yamlx.FromJSON(appliedFilesRaw)
		if err != nil || !yamlx.Equal(oldNode, appliedFilesNode) {
			return errors.New("applied file digests do not attest old archive")
		}
		if !evidence.MainBytesChanged {
			continue
		}
		after, err := next.Archive()
		if err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(folder, "after.tar.gz"), after); err != nil {
			return err
		}
		agentFolder := filepath.Join(data, ".agent-rule-sync", key, hash([]byte(applied.DeploymentID)))
		original, err := os.ReadFile(filepath.Join(agentFolder, "receipt.json"))
		if err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(folder, "receipt-before.json"), original); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(original, &fields); err != nil {
			return err
		}
		fields["manifest"], err = json.Marshal(next.Manifest())
		if err != nil {
			return err
		}
		fields["comparison_config"], err = json.Marshal(next.Comparison())
		if err != nil {
			return err
		}
		fields["sha256"], err = json.Marshal(hash(after))
		if err != nil {
			return err
		}
		metadata, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(folder, "receipt-after.json"), metadata); err != nil {
			return err
		}
		// Keep reported device identity/revisions untouched. Only the private file
		// representation and its dependent byte fingerprints are migrated.
		applied.Fingerprint = evidence.NewFingerprint
		digests, err := deploy.Digests(deploy.ComparisonManifest(next.Manifest(), next.Comparison()))
		if err != nil {
			return err
		}
		fileMap := model.OrderedMap[string]{}
		for path, sha := range digests {
			fileMap.Set(path, sha)
		}
		applied.Files = &fileMap
		if _, has := appliedFields["deployed_config_sha256"]; has {
			applied.DeployedConfigSHA256 = new(next.Comparison().SHA256)
		}
		plans = append(plans, plan{agentFolder, after, metadata, hash(after)})
		migrated[i] = true
	}
	// Apply only after every independent proof succeeded. The driver patches the
	// applied_content state rows without changing revision/generation metadata.
	for _, p := range plans {
		if err = privateWrite(filepath.Join(p.folder, "bundle-"+p.sha+".tar.gz"), p.archive); err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(p.folder, "receipt.json"), p.metadata); err != nil {
			return err
		}
	}
	loaded, err := repository.Store().Load()
	if err != nil {
		return err
	}
	var stored []byte
	if loaded.Document.System != nil {
		for _, field := range loaded.Document.System.Fields {
			if field.Name == "agents" {
				stored = field.Value
			}
		}
	}
	// The registry as one row (agent_state merged in, design/o17-runtime-state.md).
	after, err := patchedAgents(staterows.Merge(stored, loaded.Document.State), records, migrated)
	if err != nil {
		return err
	}
	if err = privateWrite(filepath.Join(proofDir, "agents-after.json"), after); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(proofDir, "applied-after.json"), appliedRows(after, records, migrated)); err != nil {
		return err
	}
	return writeJSON(filepath.Join(proofDir, "summary.json"), struct {
		Converted int    `json:"converted"`
		Proof     string `json:"proof"`
	}{len(plans), "yamlx.Equal and exact non-main bytes"})
}

// patchedAgents returns the stored agents row (raw, as read from the store)
// with only the private format identity of each converted
// Agent's applied_content replaced: fingerprint, files and, when stored,
// deployed_config_sha256. Every other byte of the row stays as stored, so the
// typed record codec (which drops nulls and undeclared members, O18 T1b)
// never rewrites the rest of the row.
func patchedAgents(raw []byte, records []model.Agent, migrated map[int]bool) ([]byte, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("stored agents row: %w", err)
	}
	if len(rows) != len(records) {
		return nil, errors.New("stored agents row does not match the decoded Agents")
	}
	for i := range rows {
		if !migrated[i] {
			continue
		}
		var agent model.OrderedMap[json.RawMessage]
		if err := json.Unmarshal(rows[i], &agent); err != nil {
			return nil, err
		}
		var id string
		if member, ok := agent.Get("id"); !ok || json.Unmarshal(member, &id) != nil || id != records[i].ID {
			return nil, errors.New("stored agents row order does not match the decoded Agents")
		}
		member, _ := agent.Get("applied_content")
		var stored model.OrderedMap[json.RawMessage]
		if err := json.Unmarshal(member, &stored); err != nil {
			return nil, err
		}
		applied := records[i].AppliedContent
		for _, name := range []string{"fingerprint", "files", "deployed_config_sha256"} {
			if _, present := stored.Get(name); !present && name == "deployed_config_sha256" {
				continue
			}
			value, present, err := model.EncodeMember(applied, name)
			if err != nil {
				return nil, err
			}
			if !present {
				return nil, fmt.Errorf("converted applied_content lacks %s", name)
			}
			stored.Set(name, value)
		}
		encoded, err := json.Marshal(stored)
		if err != nil {
			return nil, err
		}
		agent.Set("applied_content", encoded)
		if rows[i], err = json.Marshal(agent); err != nil {
			return nil, err
		}
	}
	return json.Marshal(rows)
}

// appliedPatch is one applied_content state row the driver rewrites.
type appliedPatch struct {
	AgentID string `json:"agent_id"`
	Value   string `json:"value"`
}

// appliedRows returns the applied_content state rows of the converted Agents
// in the patched registry row.
func appliedRows(after []byte, records []model.Agent, migrated map[int]bool) []appliedPatch {
	ids := map[string]bool{}
	for i := range records {
		if migrated[i] {
			ids[records[i].ID] = true
		}
	}
	_, rows := staterows.Split(after)
	patches := []appliedPatch{}
	for _, row := range rows {
		if row.Member == "applied_content" && ids[row.Owner] {
			patches = append(patches, appliedPatch{AgentID: row.Owner, Value: string(row.Value)})
		}
	}
	return patches
}

func main() {
	if err := run(); err != nil {
		logx.Get("proddiff.baseline").Error("isolated baseline format migration failed; inspect private proofs")
		os.Exit(2)
	}
}
