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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/server"
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
	system, err := repository.GetSystem(ctx)
	if err != nil {
		return err
	}
	records := system.GetList("agents")
	receipts := deploy.NewReceipts(data)
	plans := []plan{}
	for _, value := range records {
		record, ok := value.(*jsonx.Object)
		if !ok {
			continue
		}
		applied := record.GetObject("applied_content")
		if applied.Len() == 0 {
			continue
		}
		target := deploy.NewTarget(record)
		receipt, err := receipts.LoadAgent(ctx, target.Record, true)
		if err != nil {
			return err
		}
		if receipt == nil {
			return errors.New("historical applied baseline has no readable receipt")
		}
		cfg, err := repository.GetCompatConfig(ctx, applied.GetString("profile_id"), nil)
		if err != nil {
			return err
		}
		candidate, err := deploy.PrepareContentBundle(ctx, manager, target, deploy.NewSource(cfg, nil), receipt.BaseURL, "format-proof")
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
		if evidence.OldFingerprint != applied.GetString("fingerprint") {
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
		oldFields, err := jsonx.Parse(oldRaw)
		if err != nil {
			return err
		}
		if !jsonx.Equal(oldFields, applied.Value("files")) {
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
		agentFolder := filepath.Join(data, ".agent-rule-sync", key, hash([]byte(applied.GetString("deployment_id"))))
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
		applied.Set("fingerprint", evidence.NewFingerprint)
		digests, err := deploy.Digests(deploy.ComparisonManifest(next.Manifest(), next.Comparison()))
		if err != nil {
			return err
		}
		raw, err := json.Marshal(digests)
		if err != nil {
			return err
		}
		files, err := jsonx.Parse(raw)
		if err != nil {
			return err
		}
		applied.Set("files", files)
		if applied.Has("deployed_config_sha256") {
			applied.Set("deployed_config_sha256", next.Comparison().SHA256)
		}
		plans = append(plans, plan{agentFolder, after, metadata, hash(after)})
	}
	// Apply only after every independent proof succeeded. The driver patches the
	// single raw agents row without changing revision/generation metadata.
	for _, p := range plans {
		if err = privateWrite(filepath.Join(p.folder, "bundle-"+p.sha+".tar.gz"), p.archive); err != nil {
			return err
		}
		if err = privateWrite(filepath.Join(p.folder, "receipt.json"), p.metadata); err != nil {
			return err
		}
	}
	if err = writeJSON(filepath.Join(proofDir, "agents-after.json"), records); err != nil {
		return err
	}
	return writeJSON(filepath.Join(proofDir, "summary.json"), struct {
		Converted int    `json:"converted"`
		Proof     string `json:"proof"`
	}{len(plans), "yamlx.Equal and exact non-main bytes"})
}
func main() {
	if err := run(); err != nil {
		logx.Get("proddiff.baseline").Error("isolated baseline format migration failed; inspect private proofs")
		os.Exit(2)
	}
}
