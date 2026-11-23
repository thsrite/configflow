package upgrade

import (
	"agent/internal/logx"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var logger = logx.Get("agent/upgrade")

type bootstrapJournal struct {
	Complete  bool       `json:"complete"`
	Snapshots []Snapshot `json:"snapshots"`
}

// EnsureInstalled is the compatibility bridge for an old Agent's binary-only
// updater. It also upgrades config files made by earlier native installers.
func EnsureInstalled(configPath, binary string) error {
	return ensureInstalled(configPath, binary, "/")
}

func ensureInstalled(configPath, binary, systemRoot string) error {
	c, err := ReadConfig(configPath)
	if err != nil {
		return err
	}
	if c.Docker() {
		return nil
	}
	if Pending(binary) {
		if ready, _ := migrationReadyStrict(configPath, binary, systemRoot); ready {
			return nil
		}
		// Older update workers install schema 1 gates before starting this
		// binary. Wait for their transaction to commit before removing files
		// that their in-memory rollback snapshots still own.
		go deferMigration(configPath, binary, systemRoot)
		return nil
	}
	journalPath := filepath.Join(filepath.Dir(configPath), ".agent-migration.json")
	var journal bootstrapJournal
	if saved, readErr := os.ReadFile(journalPath); readErr == nil {
		if err = json.Unmarshal(saved, &journal); err != nil {
			return fmt.Errorf("unreadable migration journal")
		}
		if !journal.Complete {
			if err = Restore(journal.Snapshots); err != nil {
				return fmt.Errorf("interrupted migration restore failed: %w", err)
			}
			c, err = ReadConfig(configPath)
			if err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	// An interrupted bootstrap must restore/reapply its journal even when its
	// final config write happened before the crash but reload never completed.
	if ready, _ := migrationReadyStrict(configPath, binary, systemRoot); ready {
		return nil
	}
	changes, err := MigrationPlan(c, binary, systemRoot)
	if err != nil {
		return legacyFailure(c, binary, nil, err)
	}
	journal = bootstrapJournal{}
	for _, change := range changes {
		snapshot, captureErr := captureChange(change)
		if captureErr != nil {
			return captureErr
		}
		journal.Snapshots = append(journal.Snapshots, snapshot)
	}
	b, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if err = AtomicWrite(journalPath, b, 0600); err != nil {
		return err
	}
	if err = ApplyChanges(changes); err == nil {
		err = reload(c.String("service_manager"))
	}
	if err != nil {
		return legacyFailure(c, binary, journal.Snapshots, err)
	}
	journal.Complete = true
	b, err = json.Marshal(journal)
	if err != nil {
		return err
	}
	return AtomicWrite(journalPath, b, 0600)
}

func legacyFailure(c *Config, binary string, snapshots []Snapshot, cause error) error {
	if err := Restore(snapshots); err != nil {
		return fmt.Errorf("migration failed; configuration rollback failed: %w", err)
	}
	if manager := c.String("service_manager"); manager == "systemd" || manager == "openrc" {
		_ = reload(manager)
	}
	// Only the standard legacy installation has this fixed, verifiable backup
	// location. Never consume another Agent's backup for an arbitrary binary.
	backup := filepath.Join(filepath.Dir(c.Path), "backup/configflow-agent.bak")
	if filepath.Clean(binary) != "/usr/local/bin/configflow-agent" || filepath.Dir(c.Path) != "/opt/configflow-agent" {
		return fmt.Errorf("automatic migration failed: %w", cause)
	}
	previous, err := os.ReadFile(backup)
	if err != nil || len(previous) < 4 || !bytes.Equal(previous[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return fmt.Errorf("migration failed; legacy executable backup unavailable: %w", cause)
	}
	current, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	if bytes.Equal(previous, current) {
		return fmt.Errorf("migration failed; no previous executable: %w", cause)
	}
	if err = AtomicWrite(binary, previous, 0755); err != nil {
		return fmt.Errorf("migration failed; executable restore failed: %w", err)
	}
	reportLegacyFailure(c, cause.Error())
	return syscall.Exec(binary, append([]string{binary}, os.Args[1:]...), os.Environ())
}

func MigrationReady(configPath, binary string) (bool, error) {
	return migrationReady(configPath, binary, "/")
}

func migrationReady(configPath, binary, systemRoot string) (bool, error) {
	ready, err := migrationReadyStrict(configPath, binary, systemRoot)
	if err != nil || ready {
		return ready, err
	}
	c, err := ReadConfig(configPath)
	if err != nil {
		return false, err
	}
	// Let a schema 1 worker commit its update before the deferred cleanup.
	// This exception applies only to its final startup probe, never to a
	// completed update, an unreadable journal, or another Agent's task.
	job, err := loadJob(CurrentPath(binary))
	if err != nil {
		return false, nil
	}
	return c.Values["upgrade_schema"] == float64(1) && job.Mutating &&
		job.AgentID == c.String("agent_id") && job.Binary == binary &&
		job.Version == Version && (job.Status.Status == "restarting" || job.Status.Status == "checking"), nil
}

func migrationReadyStrict(configPath, binary, systemRoot string) (bool, error) {
	c, err := ReadConfig(configPath)
	if err != nil {
		return false, err
	}
	if c.Docker() {
		return false, nil
	}
	if value, ok := c.Values["upgrade_schema"].(float64); !ok || value != migrationSchema {
		return false, nil
	}
	changes, err := MigrationPlan(c, binary, systemRoot)
	if err != nil {
		return false, err
	}
	for _, change := range changes {
		if change.Path == configPath {
			continue
		}
		if change.Delete {
			if _, err := os.Lstat(change.Path); !os.IsNotExist(err) {
				return false, err
			}
			continue
		}
		data, err := os.ReadFile(change.Path)
		if err != nil || !bytes.Equal(data, change.Data) {
			return false, nil
		}
	}
	return true, nil
}

func deferMigration(configPath, binary, systemRoot string) {
	for {
		done, err := finishDeferredMigration(configPath, binary, systemRoot)
		if err != nil {
			logger.Warnf("Could not remove legacy startup recovery dependencies after Agent update: %v", err)
			return
		}
		if done {
			return
		}
		time.Sleep(time.Second)
	}
}

func finishDeferredMigration(configPath, binary, systemRoot string) (bool, error) {
	job, err := loadJob(CurrentPath(binary))
	if err != nil {
		return true, err
	}
	if !terminal(job.Status.Status) {
		return false, nil
	}
	if job.Status.Status != "succeeded" {
		return true, nil
	}
	release, err := lock(filepath.Join(rootFor(binary), "update.lock"))
	if err != nil {
		return false, nil
	}
	defer release()
	// Another update could have been reserved between reading the journal
	// and acquiring its lock. Do not migrate during that transaction.
	if Pending(binary) {
		return false, nil
	}
	return true, ensureInstalled(configPath, binary, systemRoot)
}

func reportLegacyFailure(c *Config, message string) {
	if c.String("server_url") == "" || c.String("agent_id") == "" {
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	base := strings.TrimRight(c.String("server_url"), "/") + "/api/agents/" + c.String("agent_id") + "/upgrade"
	req, err := http.NewRequest("GET", base, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.String("token"))
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	var state struct {
		ID      string `json:"update_id"`
		Version string `json:"target_version"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&state)
	resp.Body.Close()
	if err != nil || state.Version != Version || !idPattern.MatchString(state.ID) {
		return
	}
	body, _ := json.Marshal(map[string]string{"update_id": state.ID, "status": "rolling_back", "error": message})
	req, err = http.NewRequest("POST", base+"/report", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.String("token"))
	req.Header.Set("Content-Type", "application/json")
	if resp, err = client.Do(req); err == nil {
		resp.Body.Close()
	}
}
