package rulelibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"

	"configflow/internal/lifecycle"
	"configflow/internal/rulefetch"
)

type sourceChange struct{ Old, Next record }
type profileWarning struct {
	Profile string `json:"profile"`
	Message string `json:"message"`
}
type mutationResult struct {
	Success  bool              `json:"success"`
	Data     *record           `json:"data,omitempty"`
	Synced   *int              `json:"synced_count,omitempty"`
	Warnings *[]profileWarning `json:"warnings,omitempty"`
}

func same(a, b record) bool {
	x, err := revision(a)
	if err != nil {
		return false
	}
	y, err := revision(b)
	return err == nil && x == y
}
func different(a, b record, defaults map[string]json.RawMessage) bool {
	for key, def := range defaults {
		x, ok := a.Get(key)
		if !ok {
			x = def
		}
		y, ok := b.Get(key)
		if !ok {
			y = def
		}
		var ca, cb bytes.Buffer
		if json.Compact(&ca, x) != nil || json.Compact(&cb, y) != nil || !bytes.Equal(ca.Bytes(), cb.Bytes()) {
			return true
		}
	}
	return false
}

var declarationFields = map[string]json.RawMessage{"url": json.RawMessage("null"), "source_type": json.RawMessage("null"), "behavior": json.RawMessage("null"), "format": json.RawMessage("null"), "content": json.RawMessage("null")}
var routingFields = map[string]json.RawMessage{"enabled": json.RawMessage("true"), "behavior": json.RawMessage(`"domain"`)}
var downloadFields = map[string]json.RawMessage{"url": json.RawMessage("null"), "source_type": json.RawMessage(`"url"`)}

func affects(profile record, changes []sourceChange) (routing, downloads bool, err error) {
	rules, err := rows(profile, "rule_configs")
	if err != nil {
		return false, false, err
	}
	for _, item := range rules {
		for _, change := range changes {
			if text(item, "library_rule_id") == text(change.Old, "id") {
				routing = routing || different(change.Old, change.Next, routingFields)
				downloads = downloads || different(change.Old, change.Next, downloadFields)
			}
		}
	}
	return routing, downloads, nil
}

// Mutate replaces or removes a shared source without overwriting an intervening
// edit, and reconstructs the affected native profiles in the same transaction.
func (s *Service) Mutate(ctx context.Context, id string, next record, remove bool, base string) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := mutationResult{Success: true}
	var baseline record
	var prepared []byte
	changed := false
	var err error
	if !remove {
		baseline, err = source(ctx, id)
		if err != nil {
			return out, itemMissing(err)
		}
		if err = put(&next, "id", id); err != nil {
			return out, err
		}
		if err = validateContent(next, &baseline); err != nil {
			return out, err
		}
		changed = different(next, baseline, declarationFields)
		if changed {
			prepared, err = s.readSource(ctx, next)
			if err != nil {
				if rulefetch.IsRequestException(err) {
					return out, fail(502, msgDownloadEdit)
				}
				return out, err
			}
			if prepared == nil {
				return out, fail(400, msgSource)
			}
		} else {
			prepared, err = readCache(ctx, baseline)
			if err != nil {
				return out, err
			}
		}
	}
	var identity record
	if err = put(&identity, "id", id); err != nil {
		return out, err
	}
	ctx, release, err := editLease(ctx, identity)
	if err != nil {
		return out, err
	}
	defer release()
	if !remove && prepared != nil && sourceType(next) == "url" {
		path, err := cachePath(ctx, next)
		if err != nil {
			return out, err
		}
		_, statErr := os.Stat(path)
		if changed || errors.Is(statErr, os.ErrNotExist) {
			if err = writeCache(ctx, next, prepared); err != nil {
				return out, err
			}
		}
	}
	var previous record
	var stage *stagedCache
	committed := false
	defer func() {
		if !committed {
			stage.abort()
		}
	}()
	changes := []sourceChange{}
	if !remove {
		changes = append(changes, sourceChange{baseline, next})
	}
	warnings, err := editWithProfiles(ctx, changes, func(tx context.Context, catalog *record) error {
		library, err := rows(*catalog, "rule_library")
		if err != nil {
			return err
		}
		i := find(library, id)
		if i < 0 {
			return fail(404, "Rule not found")
		}
		previous = library[i]
		if remove {
			library = append(library[:i:i], library[i+1:]...)
			return put(catalog, "rule_library", library)
		}
		if !same(previous, baseline) {
			return fail(409, msgDraftConflict)
		}
		next.Delete("base_url")
		next.Delete("has_ip_part")
		if sourceType(next) == "content" {
			next.Delete("url")
		} else {
			next.Delete("content")
		}
		if err = validateContent(next, &previous); err != nil {
			return err
		}
		if !next.Has("update_interval") {
			if interval, ok := previous.Get("update_interval"); ok {
				if err = next.Set("update_interval", interval); err != nil {
					return err
				}
			}
		}
		if sourceType(next) == "content" && prepared != nil {
			stage, err = stageCache(tx, next, prepared, s.replaceCache)
			if err != nil {
				return err
			}
		}
		library[i] = next
		return put(catalog, "rule_library", library)
	}, base)
	if err != nil {
		return out, err
	}
	committed = true
	if remove {
		if err = removeCache(ctx, previous); err != nil {
			return out, err
		}
	} else if err = stage.commit(); err != nil {
		return out, fail(500, msgCacheFailed)
	}
	if remove || changed {
		if err = forget(ctx, id); err != nil {
			return out, err
		}
	}
	if name := text(previous, "name"); name != "" && (remove || name != text(next, "name")) {
		if err = removeLegacy(ctx, name); err != nil {
			return out, err
		}
	}
	if !remove {
		count, err := usage(ctx, id)
		if err != nil {
			return out, err
		}
		out.Data = &next
		out.Synced = &count
		out.Warnings = &warnings
	}
	return out, nil
}
func itemMissing(err error) error {
	var f *fault
	if errors.As(err, &f) && f.status == 404 {
		return fail(404, "Rule not found")
	}
	return err
}
