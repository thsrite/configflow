package repo

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"

	"configflow/internal/model"
	"configflow/internal/store"
)

// Stored rows and plain model records (O18 T1a). Every row is decoded
// directly into its record member with the lenient stored-data rules
// (model.DecodeStoredMember) and every member is encoded on its own
// (model.EncodeMembers). The store writes only rows whose text changed; rows
// keep their stored text until a transaction changes their typed value
// (keepUnchangedRows), so a commit writes exactly the rows it changed. Rows no
// record declares are dropped and reported once; the next commit deletes them.

// rootSchemaVersion is the only root row besides the sections.
const rootSchemaVersion = "schema_version"

func decodeModelRows(raw *store.Document) (*model.Document, []model.FieldIssue, error) {
	if raw == nil {
		return nil, nil, fmt.Errorf("decode stored model: missing document")
	}
	for _, section := range model.DocumentSections {
		if !slices.Contains(raw.Order, section) {
			return nil, nil, validationError(msgSectionMissing + section)
		}
	}
	if raw.System == nil || raw.Shared == nil {
		return nil, nil, fmt.Errorf("decode stored model: missing section")
	}
	document := &model.Document{}
	var issues []model.FieldIssue
	decode := func(record any, scope, owner, prefix string, fields []store.Field) error {
		for _, field := range fields {
			path := field.Name
			if prefix != "" {
				path = prefix + "." + field.Name
			}
			known, found, err := model.DecodeStoredMember(record, field.Name, field.Value, path)
			if err != nil {
				return fmt.Errorf(msgModelRowInvalid, scope, owner, field.Name, err)
			}
			if !known {
				if err := model.ValidJSON(field.Value); err != nil {
					return fmt.Errorf(msgModelRowInvalid, scope, owner, field.Name, err)
				}
				found = []model.FieldIssue{{Path: path, Kind: model.IssueUnknown}}
			}
			issues = append(issues, found...)
		}
		return nil
	}
	hasVersion := false
	for _, field := range raw.Root {
		if field.Name != rootSchemaVersion {
			if err := model.ValidJSON(field.Value); err != nil {
				return nil, nil, fmt.Errorf(msgModelRowInvalid, "root", "", field.Name, err)
			}
			issues = append(issues, model.FieldIssue{Path: field.Name, Kind: model.IssueUnknown})
			continue
		}
		hasVersion = true
		found, err := model.DecodeStored(field.Value, &document.SchemaVersion, rootSchemaVersion)
		if err != nil {
			return nil, nil, fmt.Errorf(msgModelRowInvalid, "root", "", field.Name, err)
		}
		issues = append(issues, found...)
	}
	if !hasVersion {
		return nil, nil, validationError(msgSchemaMismatch)
	}
	if err := decode(&document.System, "system", "", "system", raw.System.Fields); err != nil {
		return nil, nil, err
	}
	if err := decode(&document.Shared, "shared", "", "shared", raw.Shared.Fields); err != nil {
		return nil, nil, err
	}
	document.Profiles = make([]*model.Profile, 0, len(raw.Profiles))
	for i, rows := range raw.Profiles {
		profile := &model.Profile{}
		if err := decode(profile, "profile", rows.ID, fmt.Sprintf("profiles[%d]", i), rows.Fields); err != nil {
			return nil, nil, err
		}
		profile.SetStoredKey(rows.ID)
		document.Profiles = append(document.Profiles, profile)
	}
	return document, issues, nil
}

// canonicalRootOrder is the root member order Go writes for a new store.
var canonicalRootOrder = []string{rootSchemaVersion, "system", "shared", "profiles"}

func encodeModelRows(document *model.Document, base *store.Document) (*store.Document, error) {
	if document == nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	rows := func(record any, name string) ([]store.Field, error) {
		members, err := model.EncodeMembers(record)
		if err != nil {
			return nil, &store.Error{Kind: store.ErrDocument, Message: fmt.Sprintf(msgStoreSectionObject, name), Cause: err}
		}
		fields := make([]store.Field, len(members))
		for i, member := range members {
			fields[i] = store.Field{Name: member.Name, Value: member.Value}
		}
		return fields, nil
	}
	version, err := model.Marshal(document.SchemaVersion)
	if err != nil {
		return nil, err
	}
	// Keep the stored root order when it names the same members, so a commit
	// does not rewrite it.
	order := canonicalRootOrder
	if base != nil && len(base.Order) == len(order) {
		same := true
		for _, name := range order {
			same = same && slices.Contains(base.Order, name)
		}
		if same {
			order = slices.Clone(base.Order)
		}
	}
	result := &store.Document{Order: slices.Clone(order), Root: []store.Field{{Name: rootSchemaVersion, Value: version}}}
	system, err := rows(&document.System, "system")
	if err != nil {
		return nil, err
	}
	shared, err := rows(&document.Shared, "shared")
	if err != nil {
		return nil, err
	}
	result.System = &store.Section{Fields: system}
	result.Shared = &store.Section{Fields: shared}
	seen := map[string]bool{}
	for _, profile := range document.Profiles {
		if profile == nil {
			return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreProfilesObject}
		}
		owner := profile.Key()
		if seen[owner] {
			return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreProfilesObject, Cause: fmt.Errorf("%w: duplicate profile owner", model.ErrJSON)}
		}
		seen[owner] = true
		fields, err := rows(profile, "profile/"+owner)
		if err != nil {
			return nil, &store.Error{Kind: store.ErrDocument, Message: fmt.Sprintf(msgStoreProfileObject, owner), Cause: err}
		}
		result.Profiles = append(result.Profiles, store.ProfileRows{ID: owner, Fields: fields})
	}
	return result, nil
}

type rowKey struct{ scope, owner, name string }

func eachRow(document *store.Document, visit func(key rowKey, fields *[]store.Field)) {
	if document.System != nil {
		visit(rowKey{scope: "system"}, &document.System.Fields)
	}
	if document.Shared != nil {
		visit(rowKey{scope: "shared"}, &document.Shared.Fields)
	}
	for i := range document.Profiles {
		visit(rowKey{scope: "profile", owner: document.Profiles[i].ID}, &document.Profiles[i].Fields)
	}
	visit(rowKey{scope: "root"}, &document.Root)
}

// keepUnchangedRows restores the stored text and position of every row whose
// typed value the transaction did not change: baseModel is the snapshot the
// transaction started from and base its stored rows. Comparing two Go
// encodings of typed values (not stored text) means rows Python wrote, rows
// with a member read leniently and rows of records not yet migrated are only
// rewritten when their value changes. New rows follow the stored ones.
func keepUnchangedRows(next, base *store.Document, baseModel *model.Document) error {
	if base == nil || baseModel == nil {
		return nil
	}
	stored := map[rowKey][]byte{}
	positions := map[rowKey]int{}
	eachRow(base, func(section rowKey, fields *[]store.Field) {
		for i, field := range *fields {
			key := rowKey{section.scope, section.owner, field.Name}
			stored[key] = field.Value
			positions[key] = i
		}
	})
	records := map[rowKey]any{{scope: "system"}: &baseModel.System, {scope: "shared"}: &baseModel.Shared}
	for _, profile := range baseModel.Profiles {
		records[rowKey{scope: "profile", owner: profile.Key()}] = profile
	}
	var failure error
	eachRow(next, func(section rowKey, fields *[]store.Field) {
		for i, field := range *fields {
			key := rowKey{section.scope, section.owner, field.Name}
			text, ok := stored[key]
			if !ok || bytes.Equal(text, field.Value) {
				continue
			}
			// The stored text differs from the new encoding: keep it only
			// if the snapshot the transaction started from encodes the same.
			var before []byte
			present := false
			if section.scope == "root" {
				before, present = baseRoot(baseModel, field.Name)
			} else if record := records[rowKey{scope: section.scope, owner: section.owner}]; record != nil {
				var err error
				before, present, err = model.EncodeMember(record, field.Name)
				if err != nil && failure == nil {
					failure = err
				}
			}
			if present && bytes.Equal(before, field.Value) {
				(*fields)[i].Value = text
			}
		}
		slices.SortStableFunc(*fields, func(a, b store.Field) int {
			pa, oka := positions[rowKey{section.scope, section.owner, a.Name}]
			pb, okb := positions[rowKey{section.scope, section.owner, b.Name}]
			switch {
			case oka && okb:
				return pa - pb
			case oka:
				return -1
			case okb:
				return 1
			}
			return 0
		})
	})
	return failure
}

func baseRoot(document *model.Document, name string) ([]byte, bool) {
	if name != rootSchemaVersion {
		return nil, false
	}
	data, err := model.Marshal(document.SchemaVersion)
	return data, err == nil
}

var issueIndex = regexp.MustCompile(`\[[0-9]+\]`)

// issuePattern is an issue path without list indices, for summaries.
func issuePattern(path string) string { return issueIndex.ReplaceAllString(path, "[]") }

// reportModelIssues runs only at entry publication/load, never on every cache
// hit. A bounded last-head marker also suppresses repeated warnings for an
// uncached oversized document at the same revision. cacheMu owns both states.
func (r *Repository) reportModelIssues(entry storedEntry) {
	r.cacheMu.Lock()
	if r.modelIssueHeadSet && r.modelIssueHead == entry.Head {
		r.cacheMu.Unlock()
		return
	}
	r.modelIssueHeadSet = true
	r.modelIssueHead = entry.Head
	r.cacheMu.Unlock()
	unknown := map[string]int{}
	var patterns []string
	for _, issue := range entry.Issues {
		if issue.Kind == model.IssueUnknown {
			pattern := issuePattern(issue.Path)
			if unknown[pattern] == 0 {
				patterns = append(patterns, pattern)
			}
			unknown[pattern]++
			continue
		}
		if !r.agentIssuesReported(entry.Head, issue.Path) {
			logger.Warningf(msgModelFieldIssue, issue.Path, issue.Kind)
		}
	}
	for _, pattern := range patterns {
		logger.Warningf(msgModelUnknownMember, pattern, unknown[pattern])
	}
}
