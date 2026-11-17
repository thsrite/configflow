package oxidns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
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

// storedSettings is the temporary N8-5 adapter from the existing repository API.
// No other fields are decoded or normalized as a side effect of reading OxiDNS.
func storedSettings(config *jsonx.Object) (model.Dynamic, error) {
	var value model.Dynamic
	raw := []byte(`{}`)
	if config.Has("oxidns") {
		var err error
		raw, err = jsonx.Compact(config.Value("oxidns"))
		if err != nil {
			return value, fmt.Errorf("encode stored OxiDNS: %w", err)
		}
	}
	if err := value.UnmarshalJSON(raw); err != nil {
		return value, fmt.Errorf("decode stored OxiDNS: %w", err)
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
	config, err := repo.GetConfig(ctx, profileID)
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
	saved, revision, err := repository.UpdateProfileTransaction(ctx, id, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		stored, err := storedSettings(current)
		if err != nil {
			return nil, err
		}
		before, err := stored.Node()
		if err != nil {
			return nil, err
		}
		if expected != "" && expected != "*" && expected != settingsETag(before) {
			return nil, ErrSettingsChanged
		}
		replacement, err := InvalidateFollow(NewProfile(current), normalized)
		if err != nil {
			return nil, err
		}
		// N8-5: assign model.Profile.OxiDNS once the typed updater is available.
		current.Set("oxidns", ToJSONX(replacement))
		return current, nil
	})
	if err != nil {
		return SavedSettings{}, err
	}
	settings, err := storedSettings(saved)
	if err != nil {
		return SavedSettings{}, err
	}
	node, err := settings.Node()
	if err != nil {
		return SavedSettings{}, err
	}
	return SavedSettings{Settings: settings, ETag: settingsETag(node), Guard: PublishGuard{ProfileID: id, ServiceType: "oxidns", ConfigRevision: revision}}, nil
}
