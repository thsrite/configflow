package settings

import (
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/webdav"
)

// saveSystemConfig / saveBackup own the C06 settings commits. The HTTP layer
// passes its request lifetime unchanged; only the owner drops disconnect
// cancellation. The repo performs three-way merges on the single section without
// copying or encoding the Agent registry.
type systemConfigEdit struct {
	*model.SystemConfig
	baseline *model.SystemConfig
}

type backupEdit struct {
	*model.Backup
	baseline *model.Backup
}

func loadSystemConfig(ctx context.Context) (*systemConfigEdit, error) {
	ctx = lifecycle.Detach(ctx)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	// The edit is handed to SaveSystemConfig: an explicit owned copy.
	settings, err := r.CloneSystemConfig(ctx)
	if err != nil {
		return nil, err
	}
	base := *settings
	return &systemConfigEdit{settings, &base}, nil
}

func loadBackup(ctx context.Context) (*backupEdit, error) {
	ctx = lifecycle.Detach(ctx)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	// The edit is handed to SaveBackup: an explicit owned copy.
	backup, err := r.CloneBackup(ctx)
	if err != nil {
		return nil, err
	}
	base := *backup
	return &backupEdit{backup, &base}, nil
}

func settingValue(config *systemConfigEdit, key string, fallback json.RawMessage) json.RawMessage {
	raw, present, err := config.SystemConfig.MemberJSON(key)
	if err != nil || !present {
		return fallback
	}
	return raw
}

func saveSystemConfig(ctx context.Context, config *systemConfigEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.SaveSystemConfig(ctx, config.SystemConfig, config.baseline)
	if err == nil {
		repo.ResetConfigContext(ctx)
	}
	return err
}

func saveBackup(ctx context.Context, config *backupEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.SaveBackup(ctx, config.Backup, config.baseline)
	if err == nil {
		repo.ResetConfigContext(ctx)
	}
	return err
}

// testBackupConnection can create remote directories, so it is a write owner
// even though it does not save the panel settings.
func testBackupConnection(ctx context.Context, settings webdav.Settings) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return webdav.Default().Test(ctx, settings)
}

// runManualBackup owns capture -> upload/receipt as one disconnected request.
// Shared WebDAV/taskstatus final commit windows belong to O3-6c; do not replace
// its receipt helper or suppress a stopped Runtime with Uninterruptible here.
func runManualBackup(ctx context.Context, settings webdav.Settings) (webdav.Result, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return webdav.Result{}, err
	}
	snapshot, err := webdav.Capture(ctx)
	if err != nil {
		return webdav.Result{}, err
	}
	snapshot.Settings = settings
	if err := ctx.Err(); err != nil {
		return webdav.Result{}, err
	}
	return webdav.Run(ctx, snapshot, false)
}
