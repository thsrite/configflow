package settings

import (
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/webdav"
)

func (h *Handler) repo(ctx context.Context) (*repo.Repository, error) {
	if h != nil && h.Repo != nil {
		return h.Repo, nil
	}
	return repo.GetRepository(ctx)
}

func (h *Handler) withRepo(ctx context.Context) context.Context {
	if h != nil && h.Repo != nil {
		return repo.WithRepository(ctx, h.Repo)
	}
	return ctx
}

func (h *Handler) substore() *substore.Service {
	if h != nil && h.SubStore != nil {
		return h.SubStore
	}
	return substore.NewService(substore.NewEnv())
}

func (h *Handler) webdav() *webdav.Service {
	if h != nil && h.WebDAV != nil {
		return h.WebDAV
	}
	return webdav.NewService(nil)
}

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

func (h *Handler) loadSystemConfig(ctx context.Context) (*systemConfigEdit, error) {
	ctx = lifecycle.Detach(ctx)
	r, err := h.repo(ctx)
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

func (h *Handler) loadBackup(ctx context.Context) (*backupEdit, error) {
	ctx = lifecycle.Detach(ctx)
	r, err := h.repo(ctx)
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
	raw, present, err := model.EncodeMember(config.SystemConfig, key)
	if err != nil || !present {
		return fallback
	}
	return raw
}

func (h *Handler) saveSystemConfig(ctx context.Context, config *systemConfigEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := h.repo(ctx)
	if err != nil {
		return err
	}
	_, err = r.SaveSystemConfig(ctx, config.SystemConfig, config.baseline)
	if err == nil {
		repo.ResetConfigContext(ctx)
	}
	return err
}

func (h *Handler) saveBackup(ctx context.Context, config *backupEdit) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := h.repo(ctx)
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
func (h *Handler) testBackupConnection(ctx context.Context, settings webdav.Settings) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return h.webdav().Test(ctx, settings)
}

// runManualBackup owns capture -> upload/receipt as one disconnected request.
// Shared WebDAV/taskstatus final commit windows belong to O3-6c; do not replace
// its receipt helper or suppress a stopped Runtime with Uninterruptible here.
func (h *Handler) runManualBackup(ctx context.Context, settings webdav.Settings) (webdav.Result, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return webdav.Result{}, err
	}
	r, err := h.repo(ctx)
	if err != nil {
		return webdav.Result{}, err
	}
	wd := h.webdav()
	snapshot, err := wd.Capture(ctx, r)
	if err != nil {
		return webdav.Result{}, err
	}
	snapshot.Settings = settings
	if err := ctx.Err(); err != nil {
		return webdav.Result{}, err
	}
	return wd.Run(ctx, snapshot, false)
}
