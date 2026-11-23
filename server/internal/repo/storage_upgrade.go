package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"configflow/internal/store"
)

// upgradeRowHook is a test seam (export_test.go): it becomes the store's
// BeforeUpgradeRow for the next upgrade.
var upgradeRowHook func(i int) error

// BackupsDirName is the directory inside the data directory that keeps the
// online backup made before a storage upgrade, until the next upgrade.
const BackupsDirName = "backups"

// upgradeLocked brings an older storage version to store.StorageVersion
// under .config.lock, before any reader: an online backup first, then one
// store transaction that moves the Agent state rows (agent_state.go) out of
// system.agents. Any failure refuses the start and leaves the file as it was.
func (r *Repository) upgradeLocked(ctx context.Context) error {
	version, err := r.store.Version()
	if err != nil {
		return err
	}
	if version >= store.StorageVersion {
		return nil
	}
	backups := filepath.Join(r.dataDir, BackupsDirName)
	backup, err := r.upgradeBackup(backups, version)
	if err != nil {
		return &Error{Kind: ErrRepository, Cause: err, Message: fmt.Sprintf(msgUpgradeBackupFailed, version, store.StorageVersion, err, backups)}
	}
	moved := 0
	r.store.BeforeUpgradeRow = upgradeRowHook
	defer func() { r.store.BeforeUpgradeRow = nil }()
	upgraded, err := r.store.Upgrade(ctx, func(tx *store.UpgradeTx) error {
		raw, present, err := tx.Field("system", "", agentsField)
		if err != nil || !present {
			return err
		}
		config, rows := splitAgents(raw)
		if len(rows) == 0 {
			return nil
		}
		if err := tx.ReplaceField("system", "", agentsField, config); err != nil {
			return err
		}
		tx.AddState(rows)
		moved = len(rows)
		return nil
	})
	if err != nil {
		return &Error{Kind: ErrRepository, Cause: err, Message: fmt.Sprintf(msgUpgradeFailed, version, store.StorageVersion, err, backup)}
	}
	if upgraded {
		logger.Infof(msgUpgraded, store.StorageVersion, moved, backup)
	}
	return nil
}

// upgradeBackup writes and checks configflow-v<version>-<UTC>.sqlite3.
func (r *Repository) upgradeBackup(directory string, version int) (string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(directory, fmt.Sprintf("configflow-v%d-%s.sqlite3", version, stamp))
	for i := 1; pathExists(path); i++ {
		path = filepath.Join(directory, fmt.Sprintf("configflow-v%d-%s-%d.sqlite3", version, stamp, i))
	}
	if err := r.store.BackupTo(path); err != nil {
		os.Remove(path)
		return "", err
	}
	copied := store.New(path, LockTimeout)
	defer copied.Close()
	err := copied.Check()
	if err == nil {
		var got int
		if got, err = copied.Version(); err == nil && got != version {
			err = errors.New("storage_version mismatch")
		}
	}
	if err != nil {
		os.Remove(path)
		os.Remove(path + "-journal")
		return "", fmt.Errorf(msgBackupCheck, err)
	}
	return path, nil
}
