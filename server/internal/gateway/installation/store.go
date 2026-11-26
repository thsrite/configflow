package installation

import (
	"configflow/internal/fsx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"unicode/utf8"

	"configflow/internal/lockx"
	"configflow/internal/repo"
)

const maxRecordBytes = 2 * 1024 * 1024

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Creation precedes the installation operation; C36's setup operation has the
// same L2 suborder and only calls our lock-free readers.
var creates = lockx.NewKeyed[string](lockx.LevelSetupSession, "gateway-install-create")
var sessions = lockx.NewKeyed[string](lockx.LevelDevice, "gateway-install-session")
var publications = lockx.NewKeyed[string](lockx.LevelJob, "gateway-install-publish")

func (s *Service) root() string {
	return filepath.Join(s.Repository.DataDir(), "gateway-installations")
}
func (s *Service) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", failure(msgID, 404)
	}
	return filepath.Join(s.root(), id+".json"), nil
}
func (s *Service) locked(ctx context.Context, id string, run func(context.Context) error) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	ctx, release, err := sessions.Lock(ctx, path, repo.LockTimeout())
	if err != nil {
		return err
	}
	defer release()
	if err = ctx.Err(); err != nil {
		return err
	}
	return run(ctx)
}
func (s *Service) read(ctx context.Context, id string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, failure(msgMissing, 404)
	}
	if err != nil {
		return nil, fmt.Errorf("open installation: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRecordBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read installation: %w", err)
	}
	var r Record
	if len(data) > maxRecordBytes || !utf8.Valid(data) || json.Unmarshal(data, &r) != nil || r.ID != id || r.ProfileID == "" || r.Token == "" || r.Expires <= 0 || r.ConfigRevision < 0 || r.AgentIDs == nil || r.Registered == nil || r.Deployments == nil || r.Ports["mihomo"] == 0 || r.Ports["oxidns"] == 0 {
		return nil, failure(msgCorrupt, 409)
	}
	return &r, ctx.Err()
}
func (s *Service) write(ctx context.Context, r *Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(r.ID)
	if err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode installation: %w", err)
	}
	if len(b) > maxRecordBytes {
		return failure(msgCorrupt, 409)
	}
	if err = os.MkdirAll(s.root(), 0700); err != nil {
		return fmt.Errorf("create installation directory: %w", err)
	}
	if err = fsx.WriteFile(ctx, path, b, fsx.Options{Pattern: ".gateway-*"}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("replace installation: %w", err)
	}
	return nil
}
func (s *Service) records(ctx context.Context, profile string) ([]*Record, error) {
	paths, err := filepath.Glob(filepath.Join(s.root(), "*.json"))
	if err != nil {
		return nil, err
	}
	out := []*Record{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := filepath.Base(path)
		id = id[:len(id)-5]
		r, e := s.read(ctx, id)
		if e == nil && r.ProfileID == profile {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// HasInstallation conservatively detects any parseable installation for a profile,
// including records with invalid private capability data.
func (s *Service) HasInstallation(ctx context.Context, profile string) (bool, error) {
	paths, err := filepath.Glob(filepath.Join(s.root(), "*.json"))
	if err != nil {
		return false, err
	}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		f, e := os.Open(p)
		if e != nil {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(f, maxRecordBytes+1))
		f.Close()
		if e == nil && text(object(b)["profile_id"]) == profile {
			return true, nil
		}
	}
	return false, nil
}
