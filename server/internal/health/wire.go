package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"

	"configflow/internal/agents"
	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// GetProbeStore is domain_probe.get_probe_store(): DATA_DIR/probes.
func GetProbeStore(ctx context.Context) (*check.ProbeStore, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return check.NewProbeStore(filepath.Join(repository.DataDir(), "probes")), nil
}

// GetRegionStore is region_check.get_region_store(): DATA_DIR/regions.
func GetRegionStore(ctx context.Context) (*check.RegionStore, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	return check.NewRegionStore(filepath.Join(repository.DataDir(), "regions")), nil
}

func deleteProbeStore(ctx context.Context, agentID string) error {
	store, err := GetProbeStore(ctx)
	if err != nil {
		return err
	}
	return pythonOSError(store.Delete(agentID))
}

func deleteRegionStore(ctx context.Context, agentID string) error {
	store, err := GetRegionStore(ctx)
	if err != nil {
		return err
	}
	return pythonOSError(store.Delete(agentID))
}

// osError is an OSError whose text is str(exc) as Python renders it: the
// DELETE route answers 500 with that text.
type osError struct {
	err  error
	text string
}

func (e *osError) Error() string { return e.text }
func (e *osError) Unwrap() error { return e.err }

// pythonOSError gives a failed os.remove the text of Python's OSError
// ("[Errno 21] Is a directory: '/data/probes/a.json'").
func pythonOSError(err error) error {
	var pathErr *fs.PathError
	var errno syscall.Errno
	if !errors.As(err, &pathErr) || !errors.As(pathErr.Err, &errno) {
		return err
	}
	text := errno.Error()
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	return &osError{err, fmt.Sprintf("[Errno %d] %s: %s", int(errno), text, jsonx.Repr(pathErr.Path))}
}

// Wire is C16's entry in internal/server wirePackages: the DELETE
// /api/agents/<id> route clears the Agent's domain probe store, then its
// region check store (get_probe_store().delete, get_region_store().delete,
// after the traffic store). C16 owns no loops and no task rows.
func Wire() {
	agents.OnAgentDeleted("domain_probe", deleteProbeStore)
	agents.OnAgentDeleted("region_check", deleteRegionStore)
}
