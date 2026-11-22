package repo

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

var sensitiveSystemSettings = map[string]bool{"config_token": true, "rule_proxy_token": true, "retired_rule_proxy_tokens": true}

// checkMigrationPath is _check_migration_path: path is inside the data
// directory and no component below it is a symbolic link.
func (r *Repository) checkMigrationPath(path string) error {
	for part := path; ; part = filepath.Dir(part) {
		if part == r.dataDir {
			return nil
		}
		if info, err := os.Lstat(part); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return validationError(msgMigrationPathSymlink)
		}
		if part == filepath.Dir(part) {
			return validationError(msgMigrationPathOutside)
		}
	}
}

// createMigrationSnapshotDir is _create_migration_snapshot_dir:
// migrations/<UTC timestamp>-<uuid hex>.
func (r *Repository) createMigrationSnapshotDir() (string, error) {
	if err := os.MkdirAll(r.migrationsDir, 0o777); err != nil {
		return "", err
	}
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	for range 16 {
		candidate := filepath.Join(r.migrationsDir, timestamp+"-"+randomHex(32))
		if err := os.Mkdir(candidate, 0o777); err == nil {
			return candidate, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", repositoryError("Unable to allocate a unique migration snapshot directory")
}

// snapshot is _snapshot(reason) for the reasons this port still has
// ("import"): copy the database aside into migrations/<ts>-<uuid>/<reason>/.
// Callers hold .config.lock and no SQLite transaction.
func (r *Repository) snapshot(reason string) (string, error) {
	if err := r.checkMigrationPath(r.migrationsDir); err != nil {
		return "", err
	}
	parent, err := r.createMigrationSnapshotDir()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(parent, reason)
	if err := os.Mkdir(destination, 0o777); err != nil {
		return "", err
	}
	if info, err := os.Stat(r.dbPath); err == nil && info.Mode().IsRegular() {
		if err := r.store.BackupTo(filepath.Join(destination, DatabaseFilename)); err != nil {
			return "", err
		}
	}
	var directories []string
	err = filepath.WalkDir(destination, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != destination {
				directories = append(directories, path)
			}
			return nil
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		handle, err := os.Open(path)
		if err != nil {
			return err
		}
		defer handle.Close()
		return handle.Sync()
	})
	if err != nil {
		return "", err
	}
	sort.SliceStable(directories, func(i, j int) bool {
		return strings.Count(directories[i], "/") > strings.Count(directories[j], "/")
	})
	for _, directory := range append(directories, destination, parent, r.migrationsDir, r.dataDir) {
		if err := fsyncDir(directory); err != nil {
			return "", err
		}
	}
	return destination, nil
}

// applyImport is _apply_import: replace the open transaction's document with
// an imported one. The database is copied aside first; the previous rule
// proxy tokens are retired and every revision advances.
func (r *Repository) applyImport(tx *transaction, document *model.Document, reason string) (string, error) {
	snapshot, err := r.snapshot(reason)
	if err != nil {
		return "", err
	}
	previous := tx.document
	settings := &document.System.SystemConfig
	oldView, err := viewOf(&previous.System.SystemConfig)
	if err != nil {
		return "", err
	}
	tokens := []string{previous.System.SystemConfig.RuleProxyToken}
	var oldRetired []json.RawMessage
	if raw, _ := oldView.get("retired_rule_proxy_tokens"); jsonKind(raw) == '[' {
		_ = json.Unmarshal(raw, &oldRetired)
	}
	for _, item := range oldRetired {
		if token, ok := jsonString(item); ok {
			tokens = append(tokens, token)
		}
	}
	for _, token := range tokens {
		if token == "" || token == settings.RuleProxyToken || slices.Contains(settings.RetiredRuleProxyTokens, token) {
			continue
		}
		settings.RetiredRuleProxyTokens = append(slices.Clone(settings.RetiredRuleProxyTokens), token)
	}
	if settings.RetiredRuleProxyTokens == nil {
		settings.RetiredRuleProxyTokens = []string{}
	}
	document.System.Revision = previous.System.Revision + 1
	document.Shared.Revision = previous.Shared.Revision + 1
	for _, profile := range document.Profiles {
		profile.Revision = 1
		if old := profileByKey(previous, profile.Key()); old != nil {
			profile.Revision = old.Revision + 1
		}
	}
	tx.document = document
	return snapshot, nil
}

func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// ResourceRemaps records, per profile of an imported old-format document,
// old resource id → new shared id for each of SharedFields.
type ResourceRemaps map[string]map[string]map[string]string

// ExportAllJSON exports an owned document, retaining unchanged stored member
// bytes. Bootstrap registration sessions are deliberately never portable.
func (r *Repository) ExportAllJSON(ctx context.Context, desensitize bool) (json.RawMessage, error) {
	if desensitize {
		var result json.RawMessage
		err := r.WithReadSnapshot(WithRepository(ctx, r), func(held context.Context) error {
			var err error
			result, err = r.exportDocumentJSON(held, true)
			return err
		})
		return result, err
	}
	return r.exportDocumentJSON(ctx, false)
}
func (r *Repository) exportDocumentJSON(ctx context.Context, desensitize bool) (json.RawMessage, error) {
	var document *model.Document
	err := r.View(ctx, func(source *model.Document) error {
		raw, err := model.Marshal(source)
		if err != nil {
			return err
		}
		document, _, err = model.DecodeDocument(raw)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := document.System.Clear("agent_registration_sessions"); err != nil {
		return nil, err
	}
	if !desensitize {
		return model.Marshal(document)
	}
	h := recordHooks()
	if h.SanitizeExternalDocument == nil || h.RedactOxidnsJSON == nil || h.RedactRoutersJSON == nil || h.RedactEasytierJSON == nil {
		return nil, repositoryError(msgExportHooksMissing)
	}
	settings := document.System.SystemConfig
	for key := range sensitiveSystemSettings {
		if err := document.System.SystemConfig.Clear(key); err != nil {
			return nil, err
		}
	}
	if err := document.System.SetJSON("backup", []byte(`{}`)); err != nil {
		return nil, err
	}
	if err := document.System.SetJSON("agents", []byte(`[]`)); err != nil {
		return nil, err
	}
	for i := range document.Shared.Subscriptions {
		if err := setRecordMemberJSON(&document.Shared.Subscriptions[i], "url", []byte(`"[REDACTED]"`)); err != nil {
			return nil, err
		}
	}
	for i := range document.Shared.Nodes {
		node := &document.Shared.Nodes[i]
		for _, key := range []string{"server", "password", "uuid", "private-key", "proxy_string"} {
			if _, present, err := recordMemberJSON(node, key); err != nil {
				return nil, err
			} else if present {
				if err := setRecordMemberJSON(node, key, []byte(`"[REDACTED]"`)); err != nil {
					return nil, err
				}
			}
		}
		if err := setRecordMemberJSON(node, "params", []byte(`{}`)); err != nil {
			return nil, err
		}
	}
	for _, profile := range document.Profiles {
		if raw, present, err := recordMemberJSON(profile, "oxidns"); err != nil {
			return nil, err
		} else if present {
			raw, err = h.RedactOxidnsJSON(raw)
			if err != nil {
				return nil, err
			}
			if err := profile.SetJSON("oxidns", raw); err != nil {
				return nil, err
			}
		}
		if raw, present, err := recordMemberJSON(profile, "routers"); err != nil {
			return nil, err
		} else if present {
			raw, err = h.RedactRoutersJSON(raw)
			if err != nil {
				return nil, err
			}
			if err := profile.SetJSON("routers", raw); err != nil {
				return nil, err
			}
		}

		for _, engine := range []string{"mihomo", "surge", "mosdns", "loon"} {
			raw, present, err := recordMemberJSON(profile, engine)
			if err != nil {
				return nil, err
			}
			if engine == "loon" && (!present || jsonKind(raw) != '{') {
				continue
			}
			var fields model.Fields
			if jsonKind(raw) == '{' {
				if err := fields.UnmarshalJSON(raw); err != nil {
					return nil, err
				}
			}
			if err := fields.Set("custom_config", []byte(`""`)); err != nil {
				return nil, err
			}
			raw, err = fields.MarshalJSON()
			if err != nil {
				return nil, err
			}
			if err := profile.SetJSON(engine, raw); err != nil {
				return nil, err
			}
		}
		profile.Mihomo.LinuxGatewayRestorePoint = model.Dynamic{}
		if raw, present, err := recordMemberJSON(profile.Mihomo, "easytier"); err != nil {
			return nil, err
		} else if present {
			raw, err = h.RedactEasytierJSON(raw)
			if err != nil {
				return nil, err
			}
			if err := setRecordMemberJSON(&profile.Mihomo, "easytier", raw); err != nil {
				return nil, err
			}
		}
	}
	document, err = h.SanitizeExternalDocument(ctx, document, settings)
	if err != nil {
		return nil, err
	}
	return model.Marshal(document)
}

func (r *Repository) prepareImportJSON(raw json.RawMessage) (*model.Document, error) {
	document, err := r.convertImportJSON(raw, nil)
	if err != nil {
		return nil, err
	}
	if _, err := ensureSystemTokens(&document.System); err != nil {
		return nil, err
	}
	if err := validateDocument(document); err != nil {
		return nil, err
	}
	return document, nil
}

// ImportAllJSON imports a current, single-profile or schema-2 backup atomically.
// Unknown members remain opaque bytes throughout decoding and storage.
func (r *Repository) ImportAllJSON(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := r.prepareImportJSON(raw)
	if err != nil {
		return nil, err
	}
	batch := false
	err = r.transact(ctx, writeOptions{batch: &batch}, func(ctx context.Context, tx *transaction) error {
		if err := beginCommitBatch(ctx, &batch); err != nil {
			return err
		}
		_, err := r.applyImport(tx, document, "import")
		return err
	})
	if err != nil {
		return nil, err
	}
	return model.Marshal(document)
}

// ResetAllJSON resets the repository and returns its resulting JSON export.
func (r *Repository) ResetAllJSON(ctx context.Context) (json.RawMessage, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := r.initialSettingsJSON()
	if err != nil {
		return nil, err
	}
	return r.ImportAllJSON(ctx, raw)
}
