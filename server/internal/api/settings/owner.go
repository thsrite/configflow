package settings

import (
	"context"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/webdav"
)

// saveSettings owns the C06 settings commit. The HTTP layer passes its request
// lifetime unchanged; only the owner drops disconnect cancellation. The repo
// retains the request scope/baseline and performs the existing three-way merge.
func saveSettings(ctx context.Context, config *jsonx.Object) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	return repo.SaveSystemConfig(ctx, config, nil)
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
