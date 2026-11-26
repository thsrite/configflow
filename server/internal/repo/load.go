package repo

import "configflow/internal/model"

// Loading a stored generation and writing one are validated differently
// (design n7-n8, N8-5 prerequisite note).
//
// validateStored runs on every load that misses the cache. It checks only what
// the repository needs to address the document at all: the storage version,
// (the three sections are checked while decoding the rows), profile section keys that are valid ids and match their
// profile's id (profile ids name directories), and the default profile. Member
// types are not checked here: a wrong-typed member reads as its zero value
// with a load warning (model.DecodeStored), so one bad value never locks the
// UI out.
//
// ValidateDocument (business rules: names, revisions, references, Agent
// bindings, shapes) runs on every commit. A stored document that fails it can
// still be read and repaired: the write that fixes the offending value passes,
// unrelated writes are refused with the validation message until then.
func validateStored(document *model.Document) error {
	if document.SchemaVersion != SchemaVersion {
		return validationError(msgSchemaMismatch)
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
