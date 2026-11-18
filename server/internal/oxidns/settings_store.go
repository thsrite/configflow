package oxidns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// ErrSettingsChanged identifies a failed If-Match guard without any commit.
var ErrSettingsChanged = errors.New("oxidns: settings changed elsewhere")

// PublishGuard binds a later publication to the exact saved draft revision.
type PublishGuard struct {
	ProfileID      string `json:"expected_profile_id"`
	ServiceType    string `json:"expected_service_type"`
	ConfigRevision int64  `json:"expected_config_revision"`
}

// StoredSettings is the normalized editor value and validator of the stored object.
type StoredSettings struct {
	Settings model.Dynamic
	ETag     string
}

// SavedSettings is one atomically replaced draft and its publication guard.
type SavedSettings struct {
	Settings model.Dynamic
	ETag     string
	Guard    PublishGuard
}

// storedSettings reads only the typed OxiDNS member; missing settings use an
// empty object without normalizing any unrelated profile fields.
func storedSettings(profile *model.Profile) (model.Dynamic, error) {
	value := profile.OxiDNS
	node, err := value.Node()
	if err != nil {
		return value, err
	}
	if isNone(node) {
		return dynamicSettings(M())
	}
	return value, nil
}
func dynamicSettings(node *Node) (model.Dynamic, error) {
	var value model.Dynamic
	if err := value.SetNode(node); err != nil {
		return value, fmt.Errorf("encode OxiDNS settings: %w", err)
	}
	return value, nil
}
func settingsETag(value *Node) string {
	if !isMap(value) {
		value = M()
	}
	return `"` + Revision(value) + `"`
}

// ReadSettings reads a draft without saving normalization/defaults. HEAD uses
// this same owner entry point; a body on HEAD never reaches SaveSettings.
func ReadSettings(ctx context.Context, profileID string) (StoredSettings, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return StoredSettings{}, err
	}
	id, err := repo.ResolveProfileID(ctx, profileID)
	if err != nil {
		return StoredSettings{}, err
	}
	config, err := repository.Profile(ctx, id)
	if err != nil {
		return StoredSettings{}, err
	}
	stored, err := storedSettings(config)
	if err != nil {
		return StoredSettings{}, err
	}
	node, err := stored.Node()
	if err != nil {
		return StoredSettings{}, err
	}
	node, _ = storedProjection(node)
	normalized, err := NormalizeSettings(node, false)
	if err != nil {
		return StoredSettings{}, err
	}
	result, err := dynamicSettings(normalized)
	if err != nil {
		return StoredSettings{}, err
	}
	return StoredSettings{Settings: result, ETag: settingsETag(node)}, nil
}

// SaveSettings owns the detached whole-object transaction. The stale-validator
// check and follow invalidation both use the freshly locked complete snapshot.
func SaveSettings(ctx context.Context, profileID string, value *Node, expected string) (SavedSettings, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return SavedSettings{}, err
	}
	if !isMap(value) {
		return SavedSettings{}, fail(SettingsObjectMessage)
	}
	normalized, err := NormalizeSettings(value, false)
	if err != nil {
		return SavedSettings{}, err
	}
	id, err := repo.ResolveProfileID(ctx, profileID)
	if err != nil {
		return SavedSettings{}, err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return SavedSettings{}, err
	}
	expected = strings.TrimPrefix(strings.TrimSpace(expected), "W/")
	defer repo.ResetConfigContext(ctx)
	if err := ctx.Err(); err != nil {
		return SavedSettings{}, err
	}
	var saved model.Dynamic
	revision, err := repository.UpdateProfile(ctx, id, func(ctx context.Context, current *model.Profile) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		stored, err := storedSettings(current)
		if err != nil {
			return err
		}
		before, err := stored.Node()
		if err != nil {
			return err
		}
		if expected != "" && expected != "*" && expected != settingsETag(before) {
			return ErrSettingsChanged
		}
		source, err := repository.Config(ctx, id, nil)
		if err != nil {
			return err
		}
		view, err := NewTypedProfile(source)
		if err != nil {
			return err
		}
		replacement, err := InvalidateFollow(view, normalized)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		saved, err = dynamicSettings(replacement)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		current.OxiDNS = saved
		return nil
	})
	if err != nil {
		return SavedSettings{}, err
	}
	settings := saved
	node, err := settings.Node()
	if err != nil {
		return SavedSettings{}, err
	}
	return SavedSettings{Settings: settings, ETag: settingsETag(node), Guard: PublishGuard{ProfileID: id, ServiceType: "oxidns", ConfigRevision: revision}}, nil
}
