package repo

import (
	"configflow/internal/fsx"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/model"
	"configflow/internal/pathx"
	"configflow/internal/store"
)

var metadataKeys = []string{"id", "name", "description", "created_at", "updated_at"}

// DefaultProfileID is default_profile_id(): system_config.default_profile_id,
// "default" when it was never switched.
func (r *Repository) DefaultProfileID(ctx context.Context) (result string, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		result = defaultProfileIn(document)
		return nil
	})
	return result, err
}

// defaultProfileIn is the default profile id of a typed document.
func defaultProfileIn(document *model.Document) string {
	if id := document.System.SystemConfig.DefaultProfileID; id != nil && *id != "" {
		return *id
	}
	return DefaultProfileID
}

// ConfigRevision is config_revision(): the revision of the generated
// configuration; it grows when the profile or the shared resources change
// and tells whether an Agent is out of date.
func (r *Repository) ConfigRevision(ctx context.Context, profileID string) (result int64, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		result = int64(profile.Revision) + int64(document.Shared.Revision)
		return nil
	})
	return result, err
}

// PublishConfig is GetPublishSnapshot on typed values: the resolved view of
// a profile (full Agents only when agents is set) and its config revision, from
// one generation. The value is a caller-owned deep copy: publishing hands it to
// deploy jobs that outlive the request and may adjust it.
func (r *Repository) PublishConfig(ctx context.Context, profileID string, agents bool) (config *model.Config, revision int64, err error) {
	projection := agentCopyNone
	if agents {
		projection = agentCopyFull
	}
	err = r.View(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		revision = int64(profile.Revision) + int64(document.Shared.Revision)
		config, err = resolveConfig(document, profileID, nil, projection, false)
		return err
	})
	return config, revision, err
}

// Config returns the resolved view of a profile: its own members,
// system_config, backup, the shared resources its composition selects plus
// roots, the whole rule library, and rulesets hydrated from it. Agents are
// omitted by default so hot readers never copy deployment or IPv6 verification
// histories; OxiDNS policy/rule-DNS callers use ConfigWithOxiDNSAgents.
// The value borrows the snapshot (see "Borrowed reads" in config_borrow.go):
// only the Config and its top-level slices belong to the caller.
func (r *Repository) Config(ctx context.Context, profileID string, roots ResourceRoots) (result *model.Config, err error) {
	err = r.borrow(ctx, func(document *model.Document, shared bool) error {
		result, err = resolveConfig(document, profileID, roots, agentCopyNone, shared)
		return err
	})
	return result, err
}

// ConfigAt is Config together with the committed head it was resolved from:
// two reads with equal heads resolve the same configuration. head is nil
// inside a write transaction or section updater (a working copy has no head).
func (r *Repository) ConfigAt(ctx context.Context, profileID string, roots ResourceRoots) (result *model.Config, head *store.Head, err error) {
	head, err = r.borrowAt(ctx, func(document *model.Document, shared bool) error {
		result, err = resolveConfig(document, profileID, roots, agentCopyNone, shared)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return result, head, nil
}

// ConfigWithOxiDNSAgents returns the resolved view of a profile together with
// only the Agent fields OxiDNS policy and rule-DNS readers inspect (id, name,
// host, service_type, profile_id, enabled, dns_follow, rule_sync), without
// copying deployment or IPv6 verification histories. It borrows the snapshot
// like Config; the projected Agents are the caller's own.
func (r *Repository) ConfigWithOxiDNSAgents(ctx context.Context, profileID string, roots ResourceRoots) (result *model.Config, err error) {
	err = r.borrow(ctx, func(document *model.Document, shared bool) error {
		result, err = resolveConfig(document, profileID, roots, agentCopyOxiDNS, shared)
		return err
	})
	return result, err
}

// SetDefaultProfile is set_default_profile: make another profile the
// default; it reports whether anything changed. Stored Agents always name
// their profile, so no device moves.
func (r *Repository) SetDefaultProfile(ctx context.Context, profileID string) (changed bool, err error) {
	ctx = ctxx.Detach(ctx)
	err = r.Update(ctx, func(_ context.Context, document *model.Document) error {
		if _, err := profileFor(document, profileID); err != nil {
			return err
		}
		if defaultProfileIn(document) == profileID {
			return nil
		}
		id := profileID
		document.System.SystemConfig.DefaultProfileID = &id
		document.System.Revision++
		changed = true
		return nil
	})
	return changed && err == nil, err
}

// DeleteProfile is delete_profile(profile_id, new_default_id): deleting the
// default profile needs the profile that takes over (newDefaultID, "" for
// none). The profile's directory is moved to
// migrations/deleted-<id>-<uuid>/ together with deleted-profile.json (the
// export_profile shape, mode 0600), and moved back when the commit fails.
func (r *Repository) DeleteProfile(ctx context.Context, profileID, newDefaultID string) error {
	ctx = ctxx.Detach(ctx)
	return r.withProfileOperation(ctx, profileID, func(ctx context.Context) error {
		var releases []func()
		defer func() {
			for index := len(releases) - 1; index >= 0; index-- {
				releases[index]()
			}
		}()
		tombstone, exported := "", ""
		moved := false
		directory, err := r.ProfileDir(profileID)
		if err != nil {
			return err
		}
		lockedRouters := map[routerKey]bool{}
		batch := false
		err = r.transact(ctx, writeOptions{lockedRouters: lockedRouters, batch: &batch}, func(ctx context.Context, tx *transaction) error {
			document := tx.document
			profile, err := profileFor(document, profileID)
			if err != nil {
				return err
			}
			isDefault := profileID == defaultProfileIn(document)
			if isDefault {
				if len(document.Profiles) < 2 {
					return inUse(msgDefaultProfileOnly)
				}
				if newDefaultID == "" {
					return inUse(msgDefaultReplacementRequired)
				}
				if newDefaultID == profileID || profileByKey(document, newDefaultID) == nil {
					return validationError(msgDefaultReplacementPrefix + newDefaultID)
				}
			}
			var usages []any
			var names []string
			systemView, err := viewOf(&document.System)
			if err != nil {
				return err
			}
			agentsRaw, _ := systemView.get("agents")
			agents, err := itemsOf(agentsRaw, "Agent")
			if err != nil {
				return err
			}
			for _, id := range agents.ids {
				agent := agents.byID[id]
				if bound, _ := jsonString(mustGet(agent, "profile_id")); bound != profileID {
					continue
				}
				var name any = id
				if raw, _ := agent.get("name"); jsonTruthy(raw) {
					name = storedAny(agent, "name", id)
				}
				usages = append(usages, map[string]any{"kind": "agent", "id": id, "name": name, "profile_id": profileID, "profile_name": profile.Name})
				names = append(names, fmt.Sprint(name))
			}
			if len(usages) > 0 {
				return inUse(msgProfileInUsePrefix+profile.Name+msgProfileAgentsInUseSuffix+strings.Join(names, "、"), usages...)
			}
			// Keep job locks until after the document commits, and take
			// them before moving profile-local artifacts out of the way.
			records, err := routerRecordsIn(document)
			if err != nil {
				return err
			}
			remaining := routerRecordSet{values: map[routerKey]memberView{}}
			for _, key := range records.keys {
				if key.ProfileID != profileID {
					remaining.keys = append(remaining.keys, key)
					remaining.values[key] = records.values[key]
				}
			}
			release, err := r.routerGuard(ctx, document, records, remaining, nil, nil)
			if err != nil {
				return err
			}
			releases = append(releases, release)
			for _, key := range records.keys {
				if key.ProfileID == profileID {
					lockedRouters[key] = true
				}
			}
			exportedProfile, err := model.Marshal(profile)
			if err != nil {
				return err
			}
			if err := beginCommitBatch(ctx, &batch); err != nil {
				return err
			}
			if err := os.Mkdir(r.migrationsDir, 0o777); err != nil && !os.IsExist(err) {
				return err
			}
			tombstone = filepath.Join(r.migrationsDir, "deleted-"+profileID+"-"+randomHex(32))
			if pathx.Accessible(directory) {
				if err := os.Rename(directory, tombstone); err != nil {
					return err
				}
				moved = true
			} else if err := os.MkdirAll(tombstone, 0o777); err != nil {
				return err
			}
			// The database keeps no old generations: retain the deleted
			// profile itself (export_profile shape, 0600: it holds credentials).
			exported = filepath.Join(tombstone, "deleted-profile.json")
			if pathx.Accessible(exported) {
				exported = filepath.Join(tombstone, "deleted-profile-"+randomHex(32)+".json")
			}
			text, err := fileJSON(json.RawMessage(exportedProfile))
			if err != nil {
				return err
			}
			temporary, err := writeTemp(exported, text)
			if err != nil {
				return err
			}
			if err := os.Rename(temporary, exported); err != nil {
				os.Remove(temporary)
				return err
			}
			if err := fsx.SyncDir(tombstone); err != nil {
				return err
			}
			document.Profiles = slices.DeleteFunc(document.Profiles, func(p *model.Profile) bool { return p.Key() == profileID })
			if isDefault {
				id := newDefaultID
				document.System.SystemConfig.DefaultProfileID = &id
				document.System.Revision++
			}
			return nil
		})
		if err != nil {
			if exported != "" {
				os.Remove(exported)
			}
			if tombstone != "" && pathx.Accessible(tombstone) {
				if moved {
					os.Rename(tombstone, directory)
				} else {
					os.RemoveAll(tombstone)
				}
			}
			return err
		}
		return nil
	})
}

// JSONUpdate distinguishes an untouched publication field, replacement, and
// explicit null (clear). It is a boundary DTO; callers retain native models.
type JSONUpdate struct {
	Set   bool
	Value json.RawMessage
}

// RouterPublicationJSON is the native JSON boundary of RouterPublication.
type RouterPublicationJSON struct {
	Manifest, DNSGuard, IPv6FakeIP, IPv6Routes, IPv6PDGuard, LastRelease JSONUpdate
}

// SaveRouterPublicationJSON decodes only at the legacy repository boundary and
// delegates the single guarded publication transaction, including its revision.
func (r *Repository) SaveRouterPublicationJSON(ctx context.Context, profileID string, expected json.RawMessage, publication RouterPublicationJSON) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if jsonKind(expected) != '{' {
		return validationError(msgRouterIDInvalid)
	}
	given := viewOfJSON(expected)
	raw, _ := given.get("id")
	routerID, isString := jsonString(raw)
	if !given.has("id") || jsonKind(raw) == '{' || jsonKind(raw) == '[' {
		return validationError(msgRouterIDInvalid)
	}
	key := routerKey{profileID, routerID}
	options := writeOptions{lockedRouters: map[routerKey]bool{key: true}, policyRouter: &key}
	return r.transact(ctx, options, func(ctx context.Context, tx *transaction) error {
		profile, err := profileFor(tx.document, profileID)
		if err != nil {
			return err
		}
		records, err := routerRecordsIn(tx.document)
		if err != nil {
			return err
		}
		current, found := records.values[key]
		if !isString || !found || routerFieldsDiffer(current, &given, RouterConnectionFields) {
			return inUse(msgRouterConnectionChanged)
		}
		var router *model.Router
		for i := range profile.Routers.Items {
			if profile.Routers.Items[i].ID == routerID {
				router = &profile.Routers.Items[i]
			}
		}
		if router == nil {
			return inUse(msgRouterConnectionChanged)
		}
		fields := []JSONUpdate{publication.Manifest, publication.DNSGuard, publication.IPv6FakeIP, publication.IPv6Routes, publication.IPv6PDGuard}
		for i, field := range RouterManifestFields {
			update := fields[i]
			if !update.Set {
				continue
			}
			if !jsonTruthy(update.Value) {
				if err := router.Clear(field); err != nil {
					return err
				}
			} else if err := router.SetJSON(field, update.Value); err != nil {
				return err
			}
		}
		if publication.LastRelease.Set {
			raw := publication.LastRelease.Value
			if len(raw) == 0 {
				raw = []byte(`null`)
			}
			if err := router.SetJSON("last_release", raw); err != nil {
				return err
			}
		}
		profile.Revision++
		profile.UpdatedAt = nowISO()
		return nil
	})
}
