package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// Restore only the current active revision, never an arbitrary historical
// backup. The operation lock also prevents a concurrent publish/restart/update.
func manualRollbackHandler(cfg *Config, id string, w http.ResponseWriter, r *http.Request) {
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		deploymentFailure(w, 409, err)
		return
	}
	handedOff := false
	defer func() {
		if !handedOff {
			release()
		}
	}()
	s, err := loadDeployment(cfg, id)
	if err != nil {
		deploymentFailure(w, 404, fmt.Errorf("deployment not found"))
		return
	}
	if s.ManualRollback && s.Status == "rolled_back" {
		JsonResponse(w, 200, s)
		return
	}
	if err := deploymentBlocked(cfg); err != nil {
		deploymentFailure(w, 409, err)
		return
	}
	if err := ValidateOxiDNSServiceDirectory(cfg); err != nil {
		deploymentFailure(w, 409, err)
		return
	}
	if err := validateManualRollback(cfg, s); err != nil {
		deploymentFailure(w, 409, err)
		return
	}
	s.ManualRollback = true
	if err := saveDeployment(cfg, s, "rolling_back"); err != nil {
		deploymentFailure(w, 500, err)
		return
	}
	JsonResponse(w, 202, DeploymentAcceptedResponse{DeploymentID: id, ManualRollback: true, Status: "rolling_back", Success: true})
	handedOff = true
	go func() { defer release(); rollbackDeployment(cfg, s, true) }()
}

func validateManualRollback(cfg *Config, s *DeploymentState) error {
	if s.Status != "succeeded" || !s.BackupComplete {
		return fmt.Errorf("only a successful deployment with complete backups can be rolled back")
	}
	if err := validateServiceLifecycle(cfg); err != nil {
		return err
	}
	root := filepath.Dir(cfg.ConfigPath)
	data, err := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	if err != nil {
		return fmt.Errorf("active deployment manifest unavailable")
	}
	var active DeploymentManifest
	if json.Unmarshal(data, &active) != nil || active.DeploymentID != s.DeploymentID || active.ServiceType != cfg.ServiceType || active.ConfigPath != filepath.Base(cfg.ConfigPath) {
		return fmt.Errorf("only the current active deployment can be rolled back")
	}
	// External edits must not be silently overwritten under an outdated label.
	// The configuration itself is verified strictly, so it is safe to read the
	// refreshable rule targets from it; core refreshes are not external edits.
	config, err := os.ReadFile(filepath.Join(root, active.ConfigPath))
	if err != nil {
		return fmt.Errorf("active managed files changed since deployment; refusing rollback")
	}
	if err := verifyActiveManagedFiles(root, &active, config); err != nil {
		return fmt.Errorf("active managed files changed since deployment; refusing rollback")
	}
	configBackup := false
	for _, b := range s.Backups {
		if !safeRelative(b.Path) {
			return fmt.Errorf("invalid rollback backup path")
		}
		if b.Path == active.ConfigPath {
			configBackup = true
		}
		if err := rejectSymlinkPath(root, b.Path); err != nil {
			return err
		}
		if b.Existed {
			backup := filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup", b.Path)
			if err := rejectSymlinkPath(filepath.Join(deploymentPath(cfg, s.DeploymentID), "backup"), b.Path); err != nil {
				return err
			}
			digest, _, err := hashFile(backup)
			if err != nil || digest != b.SHA256 {
				return fmt.Errorf("rollback backup checksum mismatch: %s", b.Path)
			}
		}
	}
	if !configBackup {
		return fmt.Errorf("configuration rollback backup missing")
	}
	return nil
}
