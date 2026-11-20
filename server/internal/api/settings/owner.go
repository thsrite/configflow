package settings

import (
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/webdav"
)

// saveSettings owns the C06 settings commit. The HTTP layer passes its request
// lifetime unchanged; only the owner drops disconnect cancellation. The repo
// retains the request scope/baseline and performs the existing three-way merge.
type settingsEdit struct {
	*model.System
	baseline *model.System
}

func loadSettings(ctx context.Context) (*settingsEdit, error) {
	ctx = lifecycle.Detach(ctx)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	system, err := r.System(ctx)
	if err != nil {
		return nil, err
	}
	// Record provenance is immutable. Settings edits replace members; they never
	// mutate Agent slices or pointer values shared with this baseline.
	base := *system
	return &settingsEdit{system, &base}, nil
}
func settingValue(config *settingsEdit, key string, fallback json.RawMessage) json.RawMessage {
	raw, present, err := config.SystemConfig.MemberJSON(key)
	if err != nil || !present {
		return fallback
	}
	return raw
}
func saveSettings(ctx context.Context, config *settingsEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.SaveSystemRecord(ctx, config.System, config.baseline)
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
