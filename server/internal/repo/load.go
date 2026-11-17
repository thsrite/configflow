package repo

import (
	"encoding/json"
	"slices"

	"configflow/internal/model"
)

// Loading a stored generation and writing one are validated differently
// (design n7-n8, N8-5 prerequisite note).
//
// validateStored runs on every load that misses the cache. It checks only what
// the repository needs to address the document at all: the storage version,
// the three sections, profile section keys that are valid ids and match their
// profile's id (profile ids name directories), and the default profile. Member
// types are not checked here: under R5 a wrong-typed member reads as its zero
// value and keeps its stored bytes, so one bad value never locks the UI out.
//
// ValidateDocument (business rules: names, revisions, references, Agent
// bindings, shapes) runs on every commit. A stored document that fails it can
// still be read and repaired: the write that fixes the offending value passes,
// unrelated writes are refused with the validation message until then.
func validateStored(document *model.Document) error {
	version, ok := document.Root.Get("schema_version")
	var number model.Int
	if !ok || json.Unmarshal(version, &number) != nil || number != SchemaVersion {
		return validationError(msgSchemaMismatch)
	}
	order := document.Order()
	for _, section := range []string{"system", "shared", "profiles"} {
		if !slices.Contains(order, section) {
			return validationError(msgSectionMissing + section)
		}
	}
	defaultID := DefaultProfileID
	if id := document.System.SystemConfig.DefaultProfileID; id != nil && *id != "" {
		defaultID = *id
	}
	found := false
	for _, profile := range document.Profiles {
		key := profile.Key()
		if err := ValidateProfileID(key); err != nil {
			return err
		}
		if profile.ID != key {
			return validationError(msgProfileIDMismatch)
		}
		found = found || key == defaultID
	}
	if !found {
		return validationError(msgDefaultProfileRequired)
	}
	return nil
}
