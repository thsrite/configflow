package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"configflow/internal/convert"
	"configflow/internal/ctxx"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
	"configflow/internal/yamlbridge"
)

// Legacy adapter (design n7-n8 2.5): the *jsonx.Object API that consumers
// still use until their own N8-8…N8-10 step migrates them. It holds no logic
// of its own: values cross between the typed core and jsonx by model.Marshal →
// jsonx.Parse and jsonx.Compact → the model codec. Deleted in N8-11.

// workingModel returns the typed working document of the write transaction
// ctx belongs to (a fresh conversion while a legacy updater runs); ok is false
// outside a transaction.
func (r *Repository) workingModel(ctx context.Context) (document *model.Document, ok bool, err error) {
	tx := r.transactionOf(ctx)
	if tx == nil {
		return nil, false, nil
	}
	if tx.legacy == nil {
		return r.systemInView(ctx, tx.document), true, nil
	}
	document, err = modelFromLegacy(tx.legacy.document, tx.base.Entry.Document)
	if err != nil {
		return nil, true, err
	}
	return r.systemInView(ctx, document), true, nil
}

// working returns the legacy view of the working document of the write
// transaction ctx belongs to, or nil: the legacy updater's own copy while one
// runs, else a fresh conversion of the typed working copy.
func (r *Repository) working(ctx context.Context) (*jsonx.Object, error) {
	tx := r.transactionOf(ctx)
	if tx == nil {
		return nil, nil
	}
	if tx.legacy != nil {
		return tx.legacy.document, nil
	}
	return legacyModelDocument(r.systemInView(ctx, tx.document))
}

// modelFromLegacy converts a legacy document into the typed one. Rows whose
// legacy value is unchanged keep the bytes of base (legacyIntentRows).
func modelFromLegacy(legacy *jsonx.Object, base *store.Document) (*model.Document, error) {
	rows, err := legacyIntentRows(legacy, base)
	if err != nil {
		return nil, err
	}
	document, _, err := decodeModelRows(rows)
	if err != nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
	}
	return document, nil
}

// update is the legacy write transaction: fn mutates a *jsonx.Object view of
// the working document, which is converted back afterwards. Members fn did
// not change keep their stored bytes (row level, legacyIntentRows).
func (r *Repository) update(ctx context.Context, options writeOptions, fn func(ctx context.Context, document *jsonx.Object) error) error {
	return r.transact(ctx, options, func(ctx context.Context, tx *transaction) error {
		// A legacy updater always starts from the base generation, so the
		// base rows are the rows of its starting point.
		legacy, err := legacyModelDocument(tx.document)
		if err != nil {
			return err
		}
		tx.legacy = &legacyState{document: legacy}
		err = fn(ctx, legacy)
		tx.legacy = nil
		if err != nil {
			return err
		}
		if tx.document, err = modelFromLegacy(legacy, tx.base.Entry.Document); err != nil {
			return err
		}
		return nil
	})
}

// view is `with self._document() as document`: fn reads the transaction's
// working copy when ctx is inside one, the committed generation otherwise.
// fn must not modify the document or keep references to it.
func (r *Repository) view(ctx context.Context, fn func(document *jsonx.Object) error) error {
	document, _, err := r.committedSource(ctx)
	if err != nil {
		return err
	}
	return fn(document)
}

// committedSource is _committed_source: the document and whether it is a
// committed generation (which is never mutated and may be kept as the source
// of a three-way merge baseline) rather than a transaction's working copy.
func (r *Repository) committedSource(ctx context.Context) (document *jsonx.Object, committed bool, err error) {
	if r.InTransaction(ctx) {
		document, err := r.working(ctx)
		return document, false, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, false, err
	}
	document, err = entry.legacyDocument()
	return document, true, err
}

// threeWayMerge is MergeJSON for jsonx values (nil = absent).
func threeWayMerge(current, baseline, proposed any) (any, error) {
	var encoded [3][]byte
	for index, value := range []any{current, baseline, proposed} {
		data, err := jsonx.Compact(value)
		if err != nil {
			return nil, err
		}
		encoded[index] = data
	}
	merged, changed, err := MergeJSON(encoded[0], encoded[1], encoded[2])
	if err != nil {
		return nil, err
	}
	if !changed {
		return jsonx.DeepCopy(current), nil
	}
	return jsonx.Parse(merged)
}

// legacyValue is a model value in its jsonx form.
func legacyValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

// legacyObject is a model record (or Config) as a *jsonx.Object.
func legacyObject(value any) (*jsonx.Object, error) {
	converted, err := legacyValue(value)
	if err != nil {
		return nil, err
	}
	object, ok := jsonx.AsObject(converted)
	if !ok {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	return object, nil
}

// sameLegacy reports whether two jsonx values encode identically (member
// order and number text included).
func sameLegacy(a, b any) bool {
	left, err := jsonx.Compact(a)
	if err != nil {
		return false
	}
	right, err := jsonx.Compact(b)
	return err == nil && string(left) == string(right)
}

// setChanged stores the members of updated named in keys that differ from
// view (the value a legacy updater was handed) through set, as stored JSON
// (SetMemberJSON): members the updater did not change keep their stored bytes,
// and a value of the wrong shape is kept for write validation to refuse.
func setChanged(keys []string, view, updated *jsonx.Object, set func(member string, data json.RawMessage) error) error {
	for _, key := range keys {
		value, ok := updated.Get(key)
		if !ok {
			continue
		}
		if before, had := view.Get(key); had && sameLegacy(before, value) {
			continue
		}
		data, err := jsonx.Compact(value)
		if err != nil {
			return err
		}
		if err := set(key, data); err != nil {
			return err
		}
	}
	return nil
}

// setLegacy stores a jsonx value as a record member (SetMemberJSON).
func setLegacy(set func(member string, data json.RawMessage) error, member string, value any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return set(member, data)
}

// legacyConfig is the jsonx compat view of a profile in a typed document.
func legacyConfig(document *model.Document, profileID string, roots ResourceRoots, agents bool) (*jsonx.Object, error) {
	config, err := resolveConfig(document, profileID, roots, agents)
	if err != nil {
		return nil, err
	}
	return legacyObject(config)
}

// editLegacyMembers transcodes a legacy callback's delta into the native record.
// Unchanged members retain the original model bytes despite legacy formatting.
func editLegacyMembers(ctx context.Context, record any, keys []string, set func(string, json.RawMessage) error, updater Updater) error {
	view, err := legacyObject(record)
	if err != nil {
		return err
	}
	original := view.DeepCopy()
	updated, err := applyUpdater(ctx, view, updater)
	if err != nil {
		return err
	}
	return setChanged(keys, original, updated, set)
}

// nodeRawDialer reads a typed node through the shared native converter.
func nodeRawDialer(node *model.Node) (any, error) {
	target, err := convert.NodeDialer(*node)
	if err != nil {
		return nil, &Error{Kind: ErrValidation, Message: err.Error(), Cause: err}
	}
	if target == nil {
		return nil, nil
	}
	return yamlbridge.Value(target)
}

// legacyRawDialer only transcodes old-format import nodes to the typed entry.
func legacyRawDialer(node *jsonx.Object) (any, error) {
	data, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}
	var typed model.Node
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, err
	}
	return nodeRawDialer(&typed)
}

// Bridges to the validators of packages that still take jsonx values
// (health/check, convert); they move to typed input with their owners
// (N8-8a/N8-9), and these bridges go with legacy.go.

// legacyRaw is a stored JSON object as a *jsonx.Object (hook arguments that
// still take jsonx).
func legacyRaw(raw json.RawMessage) (*jsonx.Object, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	object, ok := jsonx.AsObject(value)
	if !ok {
		return nil, validationError(msgStoreDocumentObject)
	}
	return object, nil
}

// viewOfLegacy is the member view of a jsonx object (a record a legacy caller
// read earlier and hands back for comparison).
func viewOfLegacy(object *jsonx.Object) (memberView, error) {
	data, err := jsonx.Compact(object)
	if err != nil {
		return memberView{}, err
	}
	return viewOfJSON(data), nil
}

// sameRouter reports whether a stored router equals a legacy caller's copy.
func sameRouter(current memberView, expected *jsonx.Object) bool {
	stored, err := current.fields.MarshalJSON()
	if err != nil {
		return false
	}
	given, err := jsonx.Compact(expected)
	return err == nil && sameStored(stored, given)
}

// decodeLegacyRecord decodes a jsonx object into a model record (members of
// a wrong type kept as given, R5).
func decodeLegacyRecord(object *jsonx.Object, record any) error {
	data, err := jsonx.Compact(object)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, record)
}

// validateLegacyDocument is write validation of a document built as jsonx
// (imports and fresh data directories).
func validateLegacyDocument(document *jsonx.Object) error {
	typed, err := modelFromLegacy(document, nil)
	if err != nil {
		return err
	}
	return validateDocument(typed)
}

// applyUpdater is _apply_updater.
func applyUpdater(ctx context.Context, current *jsonx.Object, updater Updater) (*jsonx.Object, error) {
	if updater == nil {
		return nil, validationError("Updater must be callable")
	}
	replacement, err := updater(ctx, current)
	if err != nil {
		return nil, err
	}
	if replacement != nil {
		return replacement.DeepCopy(), nil
	}
	return current, nil
}

// SanitizationTokensOf is _sanitization_tokens_of(system): the rule proxy
// tokens (current and retired) and the Agent tokens of a system section. It
// only reads.
func SanitizationTokensOf(system *jsonx.Object) (TokenSet, TokenSet) {
	var record model.System
	if decodeLegacyRecord(system, &record) != nil {
		return TokenSet{}, TokenSet{}
	}
	return SanitizationTokens(record)
}

// GetSystem is get_system(): a copy of the system section (system_config,
// backup, agents, _revision).
func (r *Repository) GetSystem(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = document.GetObject("system").DeepCopy()
		return nil
	})
	return result, err
}

// AgentHistory returns a history field (one of AgentHistoryFields) of an
// Agent record, or an error when the record was read without its history.
func AgentHistory(agent *jsonx.Object, field string) (any, error) {
	value := agent.Value(field)
	if value == AgentHistoryNotRead {
		return nil, helperError("RuntimeError", AgentHistoryNotReadMessage)
	}
	return value, nil
}

// GetAgents is get_agents(history=...): copies of the Agent records. With
// history=false the AgentHistoryFields, which outweigh the rest of a record
// many times over, are not copied and hold AgentHistoryNotRead.
func (r *Repository) GetAgents(ctx context.Context, history bool) (result []any, err error) {
	return r.GetAgentsInspect(ctx, history, nil)
}

// GetAgentsInspect calls inspect on each raw record in the same read view,
// before omitting history or copying. inspect must neither mutate nor retain
// the record. This keeps credential snapshots aligned with light reads without
// copying their history. N8-5 must preserve this inspection capability.
func (r *Repository) GetAgentsInspect(ctx context.Context, history bool, inspect func(*jsonx.Object)) (result []any, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		agents := document.GetObject("system").GetList("agents")
		if inspect != nil {
			for _, item := range agents {
				agent, _ := jsonx.AsObject(item)
				inspect(agent)
			}
		}
		if history {
			result, _ = jsonx.DeepCopy(agents).([]any)
			if result == nil {
				result = []any{}
			}
			return nil
		}
		result = make([]any, 0, len(agents))
		for _, item := range agents {
			agent, _ := jsonx.AsObject(item)
			record := jsonx.NewObject()
			for key, value := range agent.All() {
				if key != "deployments" && key != "ipv6_verifications" {
					record.Set(key, jsonx.DeepCopy(value))
				}
			}
			for _, field := range AgentHistoryFields {
				record.Set(field, AgentHistoryNotRead)
			}
			result = append(result, record)
		}
		return nil
	})
	return result, err
}

// GetSystemSettings is get_system_settings(): system_config alone.
func (r *Repository) GetSystemSettings(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = document.GetObject("system").GetObject("system_config").DeepCopy()
		return nil
	})
	return result, err
}

// GetSystemWithSource is get_system_with_source(): a copy of the system
// section and the committed section it was read from (nil inside a
// transaction). The source is shared and must never be modified.
func (r *Repository) GetSystemWithSource(ctx context.Context) (result, source *jsonx.Object, err error) {
	document, committed, err := r.committedSource(ctx)
	if err != nil {
		return nil, nil, err
	}
	system := document.GetObject("system")
	if committed {
		source = system
	}
	return system.DeepCopy(), source, nil
}

// UpdateSystemTransaction is update_system_transaction: updater edits a
// copy of the system section; system_config, backup and agents are taken
// from its result and the revision advances by one. It returns a copy of
// the committed section.
func (r *Repository) UpdateSystemTransaction(ctx context.Context, updater Updater) (result *jsonx.Object, err error) {
	ctx = ctxx.Detach(ctx)
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		system, err := applySystemIn(document, func(system *model.System) error {
			view, err := legacyObject(system)
			if err != nil {
				return err
			}
			original := view.DeepCopy()
			updated, err := applyUpdater(ctx, view, updater)
			if err != nil {
				return err
			}
			return setChanged(systemSections, original, updated, system.SetJSON)
		})
		if err == nil {
			result, err = legacyObject(system)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// revisionConflict is the optimistic check of the save_* methods: without a
// baseline a snapshot that carries a _revision must still be current.
func revisionConflict(snapshot, current, baseline *jsonx.Object) bool {
	if baseline != nil {
		return false
	}
	revision, ok := snapshot.Get("_revision")
	return ok && !jsonx.Equal(revision, current.Value("_revision"))
}

// SaveSystem is save_system(snapshot, baseline): with a baseline the
// caller's changes are merged three-way into the current section; without
// one objects are deep-merged and other values replaced.
func (r *Repository) SaveSystem(ctx context.Context, snapshot, baseline *jsonx.Object) (*jsonx.Object, error) {
	if snapshot == nil {
		return nil, validationError("System data must be an object")
	}
	var proposed model.System
	if err := decodeLegacyRecord(snapshot, &proposed); err != nil {
		return nil, err
	}
	var base *model.System
	if baseline != nil {
		base = &model.System{}
		if err := decodeLegacyRecord(baseline, base); err != nil {
			return nil, err
		}
	}
	result, err := r.SaveSystemRecord(ctx, &proposed, base)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// Updater edits a deep copy of a section inside a write transaction. It may
// change current in place and return nil, or return a replacement object.
// ctx belongs to the transaction: repository reads made with it see the
// working copy, writes made with it fail with ErrInUse. A non-nil error
// aborts the transaction and is returned by the transaction method unchanged.
type Updater func(ctx context.Context, current *jsonx.Object) (replacement *jsonx.Object, err error)

type agentHistoryNotRead struct{}

// AgentHistoryNotRead stands in for the AgentHistoryFields of an Agent
// record read with GetAgents(ctx, false) (Python: AGENT_HISTORY_NOT_READ).
// It is not a jsonx value: the encoders refuse it. Unlike Python, the typed
// jsonx getters cannot refuse it and return zero values, so code that may
// receive such records must read history fields through AgentHistory.
var AgentHistoryNotRead any = &agentHistoryNotRead{}

// AgentHistoryNotReadMessage is the RuntimeError of the Python sentinel.
const AgentHistoryNotReadMessage = "Agent 记录是不带发布历史读取的，需要历史的代码必须用 history=True 读取"

// GetShared is get_shared(): a copy of the shared section (subscriptions,
// nodes, subscription_aggregations, rule_library, _revision).
func (r *Repository) GetShared(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		result, err = legacyObject(&document.Shared)
		return err
	})
	return result, err
}

// GetSharedWithSource is get_shared_with_source(): a copy of the shared
// section and the committed section it was read from (nil inside a
// transaction). The source is shared and must never be modified.
func (r *Repository) GetSharedWithSource(ctx context.Context) (result, source *jsonx.Object, err error) {
	document, committed, err := r.committedSource(ctx)
	if err != nil {
		return nil, nil, err
	}
	shared := document.GetObject("shared")
	if committed {
		source = shared
	}
	return shared.DeepCopy(), source, nil
}

// ruleLibraryRecord decodes a new rule library item given as jsonx; members
// of a wrong type are kept as given (R5) for write validation.
func ruleLibraryRecord(rule *jsonx.Object) (model.RuleLibraryItem, error) {
	var item model.RuleLibraryItem
	data, err := jsonx.Compact(rule)
	if err == nil {
		err = json.Unmarshal(data, &item)
	}
	return item, err
}

// UpdateSharedTransaction is update_shared_transaction: updater edits a copy
// of the shared section; the SharedFields it changed are stored and the
// revision advances by one. It returns a copy of the committed section.
func (r *Repository) UpdateSharedTransaction(ctx context.Context, updater Updater) (*jsonx.Object, error) {
	var saved *model.Shared
	err := r.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
		saved = shared
		return editLegacyMembers(ctx, shared, SharedFields, shared.SetJSON, updater)
	})
	if err != nil {
		return nil, err
	}
	return legacyObject(saved)
}

// SaveShared is save_shared(snapshot, baseline).
func (r *Repository) SaveShared(ctx context.Context, snapshot, baseline *jsonx.Object) (*jsonx.Object, error) {
	if snapshot == nil {
		return nil, validationError("Shared data must be an object")
	}
	var proposed model.Shared
	if err := decodeLegacyRecord(snapshot, &proposed); err != nil {
		return nil, err
	}
	var base *model.Shared
	if baseline != nil {
		base = &model.Shared{}
		if err := decodeLegacyRecord(baseline, base); err != nil {
			return nil, err
		}
	}
	result, err := r.SaveSharedRecord(ctx, &proposed, base)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// Profiles is a ProfileSelector for a fixed list of ids.
func Profiles(ids ...string) ProfileSelector {
	return func(*jsonx.Object, *jsonx.Object) ([]string, error) { return ids, nil }
}

// UpdateSharedAndProfilesTransaction is
// update_shared_and_profiles_transaction: update shared resources and
// profiles in one commit; any failure writes nothing. Each section's
// revision advances once. It returns copies of the shared section and of
// the updated profiles (an object keyed by profile id, in selection order).
func (r *Repository) UpdateSharedAndProfilesTransaction(ctx context.Context, sharedUpdater Updater, profileUpdater ProfileUpdater, profileIDs ProfileSelector) (*jsonx.Object, *jsonx.Object, error) {
	shared, profiles, err := r.updateSharedConfigs(ctx,
		func(ctx context.Context, shared *model.Shared) error {
			return editLegacyMembers(ctx, shared, SharedFields, shared.SetJSON, sharedUpdater)
		},
		func(ctx context.Context, id string, before, after *model.Config) error {
			previous, err := legacyObject(before)
			if err != nil {
				return err
			}
			return editLegacyMembers(ctx, after, ProfileFields, after.Profile.SetJSON, func(ctx context.Context, snapshot *jsonx.Object) (*jsonx.Object, error) {
				return profileUpdater(ctx, id, previous, snapshot)
			})
		},
		func(shared *model.Shared, profiles []*model.Profile) ([]string, error) {
			if profileIDs == nil {
				return nil, validationError("Updater must be callable")
			}
			catalog, err := legacyObject(shared)
			if err != nil {
				return nil, err
			}
			index := jsonx.NewObject()
			for _, profile := range profiles {
				value, err := legacyObject(profile)
				if err != nil {
					return nil, err
				}
				index.Set(profile.Key(), value)
			}
			return profileIDs(catalog, index)
		})
	if err != nil {
		return nil, nil, err
	}
	sharedView, err := legacyObject(shared)
	if err != nil {
		return nil, nil, err
	}
	profileViews := jsonx.NewObject()
	for _, profile := range profiles {
		value, err := legacyObject(profile)
		if err != nil {
			return nil, nil, err
		}
		profileViews.Set(profile.Key(), value)
	}
	return sharedView, profileViews, nil
}

// GetResourceUsage is get_resource_usage(resource_type, resource_id).
func (r *Repository) GetResourceUsage(ctx context.Context, resourceType, resourceID string) (result []any, err error) {
	known := false
	for _, field := range SharedFields {
		known = known || field == resourceType
	}
	if !known {
		return nil, validationError("未知资源类型")
	}
	err = r.read(ctx, func(document *model.Document) error {
		usages, err := resourceUsage(document, resourceType, resourceID)
		if err != nil {
			return err
		}
		result = jsonx.DeepCopy(usages).([]any)
		return nil
	})
	return result, err
}

// PreviewNodeDeletion is preview_node_deletion(node_ids).
func (r *Repository) PreviewNodeDeletion(ctx context.Context, nodeIDs any) (*jsonx.Object, error) {
	ids, err := deletionIDsFromLegacy(nodeIDs)
	if err != nil {
		return nil, err
	}
	result, err := r.PreviewNodeDeletionIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// DeleteNodesWithReferences is delete_nodes_with_references(node_ids,
// confirmation): delete nodes and detach them from groups and aggregations
// in one transaction, provided the plan still has the reviewed
// confirmation digest.
func (r *Repository) DeleteNodesWithReferences(ctx context.Context, nodeIDs any, confirmation string) (*jsonx.Object, error) {
	ids, err := deletionIDsFromLegacy(nodeIDs)
	if err != nil {
		return nil, err
	}
	result, err := r.DeleteNodesWithReferencesIDs(ctx, ids, confirmation)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// ProfileUpdater is the profile_updater of
// UpdateSharedAndProfilesTransaction: before is the profile resolved against
// the shared data as it was, after against the shared updater's result;
// after is edited (or replaced by the returned object) like in any profile
// updater.
type ProfileUpdater func(ctx context.Context, profileID string, before, after *jsonx.Object) (replacement *jsonx.Object, err error)

// ProfileSelector chooses the profiles of UpdateSharedAndProfilesTransaction
// from the current shared and profiles sections, which it must only read.
type ProfileSelector func(shared, profiles *jsonx.Object) ([]string, error)

// profileMetadata is _metadata(profile) of a typed profile.
func profileMetadata(profile *model.Profile) *jsonx.Object {
	result, _ := legacyObject(profileInfo(profile))
	return result
}

// AgentBindingSummary is agent_binding_summary: device identity only, never
// credentials or deployment payloads.
func AgentBindingSummary(agent *jsonx.Object) *jsonx.Object {
	var record model.Agent
	if decodeLegacyRecord(agent, &record) != nil {
		return nil
	}
	result, err := AgentBindingRecord(record)
	if err != nil {
		return nil
	}
	value, _ := legacyObject(result)
	return value
}

// ListProfiles is list_profiles(): metadata of every profile plus revision
// (the generated-config revision, see ConfigRevision), is_default and
// bound_agents.
func (r *Repository) ListProfiles(ctx context.Context) ([]any, error) {
	result, err := r.ListProfileRecords(ctx)
	if err != nil {
		return nil, err
	}
	value, err := legacyValue(result)
	if err != nil {
		return nil, err
	}
	return value.([]any), nil
}

// GetProfileAgentBindings is get_profile_agent_bindings.
func (r *Repository) GetProfileAgentBindings(ctx context.Context, id string) (*jsonx.Object, error) {
	result, err := r.ProfileAgentBindingsRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// ProfileMetadata is _profile_metadata: id, name, description, created_at
// and updated_at of a profile, without copying the profile. It is the cheap
// existence check (ErrNotFound, or ErrValidation for a malformed id).
func (r *Repository) ProfileMetadata(ctx context.Context, id string) (*jsonx.Object, error) {
	// Legacy request preparation must finish before the detached provider work
	// starts, even if its HTTP waiter has already disconnected.
	result, err := r.ProfileMetadataRecord(ctxx.Detach(ctx), id)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// GetProfile is get_profile(): a copy of the stored profile (references
// only, no shared resources).
func (r *Repository) GetProfile(ctx context.Context, profileID string) (result *jsonx.Object, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		result, err = legacyObject(profile)
		return err
	})
	return result, err
}

// ExportProfile is export_profile().
func (r *Repository) ExportProfile(ctx context.Context, id string) (*jsonx.Object, error) {
	raw, err := r.ExportProfileJSON(ctx, id)
	if err != nil {
		return nil, err
	}
	return legacyRaw(raw)
}

// GetPublishSnapshot is get_publish_snapshot(profile_id, agents=...): the
// compat view and its config revision from the same read snapshot.
// agents=false leaves the Agent records out.
func (r *Repository) GetPublishSnapshot(ctx context.Context, profileID string, agents bool) (config *jsonx.Object, revision int64, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		profile, err := profileFor(document, profileID)
		if err != nil {
			return err
		}
		revision = int64(profile.Revision) + int64(document.Shared.Revision)
		config, err = legacyConfig(document, profileID, nil, agents)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return config, revision, nil
}

// GetCompatConfig is get_compat_config(profile_id, resource_roots=...):
// Config as a *jsonx.Object.
func (r *Repository) GetCompatConfig(ctx context.Context, profileID string, roots ResourceRoots) (result *jsonx.Object, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		result, err = legacyConfig(document, profileID, roots, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetCompatConfigWithSource is get_compat_config_with_source: the compat
// view and the committed document it was resolved from (nil inside a
// transaction). The source is shared and must never be modified.
func (r *Repository) GetCompatConfigWithSource(ctx context.Context, profileID string) (result, source *jsonx.Object, err error) {
	result, rebuild, err := r.compatConfigWithBaseline(ctx, profileID)
	if err != nil || rebuild == nil {
		return result, nil, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	source, err = entry.legacyDocument()
	return result, source, err
}

// compatConfigWithBaseline is the compat view and, when it was read from a
// committed generation, a function that resolves the same view again from
// that generation (the three-way merge baseline of SaveConfig).
func (r *Repository) compatConfigWithBaseline(ctx context.Context, profileID string) (*jsonx.Object, func() (*jsonx.Object, error), error) {
	if r.InTransaction(ctx) {
		document, _, err := r.workingModel(ctx)
		if err != nil {
			return nil, nil, err
		}
		result, err := legacyConfig(document, profileID, nil, true)
		return result, nil, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	result, err := legacyConfig(entry.Model, profileID, nil, true)
	if err != nil {
		return nil, nil, err
	}
	return result, func() (*jsonx.Object, error) { return legacyConfig(entry.Model, profileID, nil, true) }, nil
}

// CompatConfigFrom is compat_config_from: resolve again from a document
// returned by GetCompatConfigWithSource (or a candidate built as jsonx); the
// result equals the view of then.
func (r *Repository) CompatConfigFrom(document *jsonx.Object, profileID string) (*jsonx.Object, error) {
	typed, err := modelFromLegacy(document, nil)
	if err != nil {
		return nil, err
	}
	return legacyConfig(typed, profileID, nil, true)
}

// CreateProfile is create_profile(metadata, clone_from): metadata has id
// (optional, generated "profile_<12 hex>"), name (defaults to the id) and
// description. A non-empty cloneFrom copies that profile without its
// routers and client fixed IPs. It returns the new profile's metadata.
func (r *Repository) CreateProfile(ctx context.Context, metadata *jsonx.Object, cloneFrom string) (*jsonx.Object, error) {
	input, err := profileCreationFromLegacy(metadata, false)
	if err != nil {
		return nil, err
	}
	result, err := r.CreateProfileRecord(ctx, input, cloneFrom)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// CloneProfile is clone_profile(source_id, metadata).
func (r *Repository) CloneProfile(ctx context.Context, sourceID string, metadata *jsonx.Object) (*jsonx.Object, error) {
	return r.CreateProfile(ctx, metadata, sourceID)
}

// ImportClientProfile is import_client_profile(metadata, build): create a
// profile and append shared resources in one transaction. build receives a
// copy of the shared section and returns {"shared": {field: [new resources]},
// "profile": {profile fields}, ...}; nothing is written when validation
// fails. The result is build's object with "profile" replaced by the new
// profile's metadata.
func (r *Repository) ImportClientProfile(ctx context.Context, metadata *jsonx.Object, build func(context.Context, *jsonx.Object) (*jsonx.Object, error)) (*jsonx.Object, error) {
	input, err := profileCreationFromLegacy(metadata, true)
	if err != nil {
		return nil, err
	}
	result, err := r.ImportClientProfileRecord(ctx, input, func(ctx context.Context, shared *model.Shared) (*ClientProfileImport, error) {
		view, err := legacyObject(shared)
		if err != nil {
			return nil, err
		}
		built, err := build(ctx, view)
		if err != nil {
			return nil, err
		}
		if built == nil {
			return nil, validationError(msgImportResultShape)
		}
		raw, err := json.Marshal(built)
		if err != nil {
			return nil, err
		}
		var details model.Fields
		if err := details.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		out := &ClientProfileImport{Details: details}
		if additions, present := built.Get("shared"); present {
			raw, err := json.Marshal(additions)
			if err != nil {
				return nil, err
			}
			if jsonKind(raw) != '{' {
				return nil, validationError(msgImportResultShape)
			}
			if err := json.Unmarshal(raw, &out.Shared); err != nil {
				return nil, err
			}
		}
		if profile, present := built.Get("profile"); present {
			raw, err := json.Marshal(profile)
			if err != nil {
				return nil, err
			}
			if jsonKind(raw) != '{' {
				return nil, validationError(msgImportResultShape)
			}
			out.Profile = &model.Profile{}
			if err := json.Unmarshal(raw, out.Profile); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result.Profile)
	if err != nil {
		return nil, err
	}
	if err := result.Details.Set("profile", raw); err != nil {
		return nil, err
	}
	raw, err = result.Details.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return legacyRaw(raw)
}

// UpdateProfileMetadata is update_profile(profile_id, updates): rename or describe a
// profile; updates may only hold "name" and "description".
func (r *Repository) UpdateProfileMetadata(ctx context.Context, id string, updates *jsonx.Object) (*jsonx.Object, error) {
	if updates == nil {
		return nil, validationError(msgMetadataFields)
	}
	var input ProfileMetadataUpdate
	for key, value := range updates.All() {
		if key != "name" && key != "description" {
			return nil, validationError(msgMetadataFields)
		}
		text, ok := value.(string)
		if !ok {
			return nil, validationError(msgMetadataStrings)
		}
		if key == "name" {
			input.Name = &text
		} else {
			input.Description = &text
		}
	}
	result, err := r.UpdateProfileMetadataRecord(ctx, id, input)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// UpdateProfileTransaction is update_profile_transaction(profile_id,
// updater, include_config_revision=True): updater edits the compat view of
// the profile; its ProfileFields are stored. It returns a copy of the
// stored profile and the new config revision.
func (r *Repository) UpdateProfileTransaction(ctx context.Context, profileID string, updater Updater) (*jsonx.Object, int64, error) {
	current, revision, err := r.updateConfig(ctx, profileID, func(ctx context.Context, config *model.Config) error {
		return editLegacyMembers(ctx, config, ProfileFields, config.Profile.SetJSON, updater)
	})
	if err != nil {
		return nil, 0, err
	}
	saved, err := legacyObject(current)
	return saved, revision, err
}

// UpdateProfileFields is update_profile_fields(profile_id, fields,
// baseline): store the ProfileFields found in fields. With a baseline each
// field present in it is merged three-way into the current value; without
// one a _revision in fields must equal the profile's.
func (r *Repository) UpdateProfileFields(ctx context.Context, id string, fields, baseline *jsonx.Object) (*jsonx.Object, int64, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, 0, err
	}
	var base json.RawMessage
	if baseline != nil {
		base, err = json.Marshal(baseline)
		if err != nil {
			return nil, 0, err
		}
	}
	saved, revision, err := r.SaveProfileFieldsJSON(ctx, id, raw, base)
	if err != nil {
		return nil, 0, err
	}
	value, err := legacyObject(saved)
	return value, revision, err
}

// SaveProfile is save_profile(profile_id, data, baseline).
func (r *Repository) SaveProfile(ctx context.Context, profileID string, data, baseline *jsonx.Object) (*jsonx.Object, int64, error) {
	ctx = ctxx.Detach(ctx)
	return r.UpdateProfileFields(ctx, profileID, data, baseline)
}

// ImportProfile is import_profile(profile_id, data): replace the profile's
// fields from an exported profile (or {"config": profile}); shared resource
// copies are refused.
func (r *Repository) ImportProfile(ctx context.Context, id string, data *jsonx.Object) (*jsonx.Object, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	result, err := r.ImportProfileJSON(ctx, id, raw)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// expectedRouterID is expected_router['id'] of the save_router_* methods:
// KeyError when missing, TypeError (Python: the set of locked routers) when
// unhashable. A hashable non-string id matches no record (router ids are
// strings), so the caller reports the router as changed (isString=false).
func expectedRouterID(expectedRouter *jsonx.Object) (routerID string, isString bool, err error) {
	value, ok := expectedRouter.Get("id")
	if !ok {
		return "", false, validationError(msgRouterIDInvalid)
	}
	switch typed := value.(type) {
	case string:
		return typed, true, nil
	case *jsonx.Object, []any:
		return "", false, validationError(msgRouterIDInvalid)
	}
	return "", false, nil
}

// SaveRouterSettings is save_router_settings(profile_id, expected_router,
// replacement): commit a router PUT (replacement) or DELETE (nil) while the
// caller holds this router's job lock. The stored record must still equal
// expectedRouter.
func (r *Repository) SaveRouterSettings(ctx context.Context, profileID string, expectedRouter, replacement *jsonx.Object) error {
	expected, err := json.Marshal(expectedRouter)
	if err != nil {
		return err
	}
	var next json.RawMessage
	if replacement != nil {
		next, err = json.Marshal(replacement)
		if err != nil {
			return err
		}
	}
	return r.SaveRouterSettingsJSON(ctx, profileID, expected, next)
}

// SaveRouterPublication is save_router_publication: persist verified
// readback or rollback under the caller's held job lock. It is the only
// write of an existing record allowed to replace or clear an active
// manifest; it cannot delete a router or alter its connection, and
// unrelated edits made during the device operation are retained.
func (r *Repository) SaveRouterPublication(ctx context.Context, id string, expected *jsonx.Object, p RouterPublication) error {
	raw, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	values := []Optional{p.Manifest, p.DNSGuard, p.IPv6FakeIP, p.IPv6Routes, p.IPv6PDGuard, p.LastRelease}
	out := make([]JSONUpdate, len(values))
	for i, value := range values {
		if !value.Set {
			continue
		}
		out[i].Set = true
		out[i].Value, err = json.Marshal(value.Value)
		if err != nil {
			return err
		}
	}
	return r.SaveRouterPublicationJSON(ctx, id, raw, RouterPublicationJSON{out[0], out[1], out[2], out[3], out[4], out[5]})
}

// DeepMerge is config_repository._deep_merge: a deep copy of base with
// override's keys laid over it, objects merged recursively.
func DeepMerge(base, override *jsonx.Object) *jsonx.Object {
	if base == nil {
		base = jsonx.NewObject()
	}
	if override == nil {
		override = jsonx.NewObject()
	}
	left, _ := json.Marshal(base)
	right, _ := json.Marshal(override)
	raw, err := mergeObjectsJSON(left, right)
	if err != nil {
		return nil
	}
	result, _ := legacyRaw(raw)
	return result
}

// configDeepMerge is common/config.py::_deep_merge: base's key order, then
// override's new keys; values are shared, not copied.
func configDeepMerge(base, override *jsonx.Object) *jsonx.Object { return DeepMerge(base, override) }

func decodeRows(raw *store.Document) (*jsonx.Object, error) {
	document, _, err := decodeModelRows(raw)
	if err != nil {
		return nil, err
	}
	return legacyModelDocument(document)
}

func legacyModelDocument(document *model.Document) (*jsonx.Object, error) {
	encoded, err := model.Marshal(document)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, err
	}
	object, ok := jsonx.AsObject(value)
	if !ok {
		return nil, fmt.Errorf("decode stored model: root must be an object")
	}
	return object, nil
}

// encodeRows translates legacy caller intent first, then makes the model codec
// the stored-document boundary. N8-5 removes legacyIntentRows with the legacy
// mutation API. In particular, two invalid legacy revisions (true/false) must
// remain distinguishable even though both read as model.Int(0) under R5.
func encodeRows(doc *jsonx.Object, base *store.Document) (*store.Document, error) {
	proposed, err := legacyIntentRows(doc, base)
	if err != nil {
		return nil, err
	}
	document, _, err := decodeModelRows(proposed)
	if err != nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
	}
	return encodeModelRows(document, proposed)
}

func legacyIntentRows(doc *jsonx.Object, base *store.Document) (*store.Document, error) {
	if doc == nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	result := &store.Document{Order: doc.Keys()}
	// Preserve the original bytes whenever the legacy value did not change.
	// Compare serialized ordered values, rather than Equal (which ignores key order).
	fields := func(object *jsonx.Object, original []store.Field) ([]store.Field, error) {
		previous := map[string]json.RawMessage{}
		for _, field := range original {
			previous[field.Name] = field.Value
		}
		out := make([]store.Field, 0, object.Len())
		for name, value := range object.All() {
			encoded, err := jsonx.Compact(value)
			if err != nil {
				return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
			}
			if raw, ok := previous[name]; ok {
				before, err := jsonx.Parse(raw)
				if err != nil {
					return nil, err
				}
				normalized, err := jsonx.Compact(before)
				if err != nil {
					return nil, err
				}
				if bytes.Equal(encoded, normalized) {
					encoded = raw
				}
			}
			out = append(out, store.Field{Name: name, Value: encoded})
		}
		return out, nil
	}
	if base == nil {
		base = &store.Document{}
	}
	root := jsonx.NewObject()
	for name, value := range doc.All() {
		switch name {
		case "system", "shared":
			object, ok := jsonx.AsObject(value)
			if !ok {
				// A legacy caller replaced a section: Python's write validation.
				return nil, validationError(msgSectionMissing + name)
			}
			var old []store.Field
			prior := base.System
			if name == "shared" {
				prior = base.Shared
			}
			if prior != nil {
				old = prior.Fields
			}
			encoded, err := fields(object, old)
			if err != nil {
				return nil, err
			}
			if name == "system" {
				result.System = &store.Section{Fields: encoded}
			} else {
				result.Shared = &store.Section{Fields: encoded}
			}
		case "profiles":
			profiles, ok := jsonx.AsObject(value)
			if !ok {
				return nil, validationError(msgSectionMissing + "profiles")
			}
			previous := map[string][]store.Field{}
			for _, p := range base.Profiles {
				previous[p.ID] = p.Fields
			}
			for id, v := range profiles.All() {
				profile, ok := jsonx.AsObject(v)
				if !ok {
					return nil, validationError(msgProfileIDMismatch)
				}
				encoded, err := fields(profile, previous[id])
				if err != nil {
					return nil, err
				}
				result.Profiles = append(result.Profiles, store.ProfileRows{ID: id, Fields: encoded})
			}
		default:
			root.Set(name, value)
		}
	}
	var err error
	result.Root, err = fields(root, base.Root)
	return result, err
}

type storedEntry struct {
	store.Entry
	legacy *legacySnapshot
	Model  *model.Document
	Issues []model.FieldIssue
}

// readJSON is ProfileRepository._read_json: the JSON object in a file.
func readJSON(path string) (*jsonx.Object, error) {
	name := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, repositoryError("Cannot read configuration " + name + ": " + err.Error())
	}
	if !utf8.Valid(data) {
		message := msgInvalidUTF8
		return nil, repositoryError("Cannot read configuration " + name + ": " + message)
	}
	// Text mode with universal newlines: "\r\n" and "\r" are read as "\n"
	// (only error positions can tell).
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	value, err := jsonx.Parse([]byte(text))
	if err != nil {
		return nil, repositoryError("Cannot read configuration " + name + ": " + err.Error())
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, validationError("JSON object expected in " + name)
	}
	return object, nil
}

// ReadSharedJSON reads shared JSON under its bounded in-process leaf lock.
func (r *Repository) ReadSharedJSON(ctx context.Context, relativePath string) (result *jsonx.Object, err error) {
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return nil, err
	}
	err = withFile(ctx, path, func() error {
		result, err = readJSON(path)
		return err
	})
	return result, err
}

// ReadProfileJSON is read_profile_json (no lock).
func (r *Repository) ReadProfileJSON(profileID, relativePath string) (*jsonx.Object, error) {
	path, err := r.ProfilePath(profileID, relativePath)
	if err != nil {
		return nil, err
	}
	return readJSON(path)
}

// pruneRuleDNS is _prune_rule_dns: drop per-rule OxiDNS DNS entries of rules
// the profile no longer has. Rule ids and list entries are matched by their
// JSON value (a non-string id such as 5 keeps an equal entry).
func pruneRuleDNS(profile *jsonx.Object) error {
	var record model.Profile
	if err := decodeLegacyRecord(profile, &record); err != nil {
		return err
	}
	if err := pruneProfileDNS(&record); err != nil {
		return err
	}
	updated, err := legacyObject(record)
	if err != nil {
		return err
	}
	profile.Update(updated)
	return nil
}

func list(items ...any) []any {
	result := make([]any, len(items))
	for index, item := range items {
		if number, ok := item.(int); ok {
			item = jsonx.Int(number)
		}
		result[index] = item
	}
	return result
}

// GetDefaultOxidnsSettings is oxidns_settings.get_default_oxidns_settings.
func GetDefaultOxidnsSettings() *jsonx.Object {
	value, _ := legacyObject(DefaultOxidnsSettings())
	return value
}

// GetDefaultConfig is common/config.py::get_default_config, the
// default_config_factory of the application's repository.
func GetDefaultConfig() *jsonx.Object {
	raw := defaultConfigJSON()
	value, _ := legacyRaw(raw)
	return value
}

// GetInitialConfig is common/config.py::_get_initial_config: the defaults
// with the first-start template merged over them. An unreadable template is
// ignored, as in Python.
func GetInitialConfig() *jsonx.Object {
	raw, _ := mergeObjectsJSON(defaultConfigJSON(), configTemplate)
	value, _ := legacyRaw(raw)
	return value
}

// ExportAll is export_all(desensitize): a copy of the whole document. With
// desensitize the tokens, backup settings, Agents, subscription URLs, node
// credentials, custom configurations and restore points are removed and the
// result goes through the registered redaction hooks; without registered
// hooks it fails instead of exporting secrets.
func (r *Repository) ExportAll(ctx context.Context, desensitize bool) (*jsonx.Object, error) {
	raw, err := r.ExportAllJSON(ctx, desensitize)
	if err != nil {
		return nil, err
	}
	return legacyRaw(raw)
}

func (r *Repository) exportAll(ctx context.Context, desensitize bool) (*jsonx.Object, error) {
	return r.ExportAll(ctx, desensitize)
}

// prepareImport is _prepare_import: the imported data as a validated typed
// document with its rule proxy token maintained.
func (r *Repository) prepareImport(source *jsonx.Object) (*model.Document, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	return r.prepareImportJSON(raw)
}

// ImportAll is import_all(data): replace the whole configuration with a
// current-format export, an original single-profile config or a schema-2
// multi-profile backup. The database is first copied to
// migrations/<ts>-<uuid>/import/. It returns ExportAll.
func (r *Repository) ImportAll(ctx context.Context, source *jsonx.Object) (*jsonx.Object, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	raw, err = r.ImportAllJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return legacyRaw(raw)
}

// ResetAll is reset_all(): ImportAll of the initial configuration.
func (r *Repository) ResetAll(ctx context.Context) (*jsonx.Object, error) {
	raw, err := r.ResetAllJSON(ctx)
	if err != nil {
		return nil, err
	}
	return legacyRaw(raw)
}

// convertImport is _convert_import: turn a current-format document, an
// original single-profile config or a schema-2 multi-profile backup into a
// validated-shape current document. source is not modified.
func (r *Repository) convertImport(source *jsonx.Object, remaps ResourceRemaps) (*jsonx.Object, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	document, err := r.convertImportJSON(raw, remaps)
	if err != nil {
		return nil, err
	}
	return legacyObject(document)
}

// legacyDefaults is _legacy_defaults().
func (r *Repository) legacyDefaults() *jsonx.Object {
	raw, err := r.profileDefaultsJSON()
	if err != nil {
		return nil
	}
	result, _ := legacyRaw(raw)
	return result
}

func (r *Repository) initialConfig() *jsonx.Object {
	raw, err := r.initialSettingsJSON()
	if err != nil {
		return nil
	}
	result, _ := legacyRaw(raw)
	return result
}

// emptyProfile is _empty_profile.
func (r *Repository) emptyProfile(id string, name, description any) *jsonx.Object {
	result, err := r.emptyProfileRecord(id, jsonx.Str(name), jsonx.Str(description))
	if err != nil {
		return nil
	}
	object, _ := legacyObject(result)
	return object
}

func revisionsOf(document *jsonx.Object) *jsonx.Object {
	profiles := jsonx.NewObject()
	for key, value := range document.GetObject("profiles").All() {
		profile, _ := jsonx.AsObject(value)
		profiles.Set(key, profile.GetOr("_revision", jsonx.Int(0)))
	}
	return jsonx.Obj(
		"system", document.GetObject("system").GetOr("_revision", jsonx.Int(0)),
		"shared", document.GetObject("shared").GetOr("_revision", jsonx.Int(0)),
		"profiles", profiles)
}

// recordHooks adapts old registrations only at this boundary. Each effective
// collaborator chooses typed first, so dual registrations never run twice.
func recordHooks() RecordHooks {
	h := currentHooks()
	out := h.RecordHooks
	if out.JobLockRecord == nil && h.JobLock != nil {
		out.JobLockRecord = func(ctx context.Context, profileID, routerID string, router model.Router) (func(), bool) {
			raw, err := legacyObject(router)
			if err != nil {
				return nil, false
			}
			return h.JobLock(ctx, profileID, routerID, raw)
		}
	}
	if out.ConfigurationCommittedRecords == nil && h.ConfigurationCommitted != nil {
		out.ConfigurationCommittedRecords = func(repository *Repository, previous, current *model.Document) {
			before, err := legacyObject(previous)
			if err != nil {
				logger.Errorf(msgCommittedHookSkipped, err)
				return
			}
			after, err := legacyObject(current)
			if err != nil {
				logger.Errorf(msgCommittedHookSkipped, err)
				return
			}
			h.ConfigurationCommitted(repository, before, after)
		}
	}

	if out.PlanNodeDeletionRecords == nil && h.PlanNodeDeletion != nil {
		out.PlanNodeDeletionRecords = func(document *model.Document, ids []string) (*NodeDeletionPreview, *model.Document, error) {
			legacy, err := legacyObject(document)
			if err != nil {
				return nil, nil, err
			}
			values := make([]any, len(ids))
			for i, id := range ids {
				values[i] = id
			}
			preview, candidate, err := h.PlanNodeDeletion(legacy, values)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(preview)
			if err != nil {
				return nil, nil, err
			}
			var result NodeDeletionPreview
			if err := json.Unmarshal(raw, &result); err != nil {
				return nil, nil, err
			}
			raw, err = json.Marshal(candidate)
			if err != nil {
				return nil, nil, err
			}
			next, _, err := model.DecodeDocument(raw)
			return &result, next, err
		}
	}
	if out.SanitizeExternalDocument == nil && h.SanitizeExternalPayload != nil {
		out.SanitizeExternalDocument = func(ctx context.Context, document *model.Document, settings model.SystemConfig) (*model.Document, error) {
			payload, err := legacyObject(document)
			if err != nil {
				return nil, err
			}
			config, err := legacyObject(settings)
			if err != nil {
				return nil, err
			}
			payload, err = h.SanitizeExternalPayload(ctx, payload, config)
			if err != nil {
				return nil, err
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			result, _, err := model.DecodeDocument(raw)
			return result, err
		}
	}
	adapt := func(fn func(any) any) func(json.RawMessage) (json.RawMessage, error) {
		if fn == nil {
			return nil
		}
		return func(raw json.RawMessage) (json.RawMessage, error) {
			value, err := jsonx.Parse(raw)
			if err != nil {
				return nil, err
			}
			return json.Marshal(fn(value))
		}
	}
	if out.RedactOxidnsJSON == nil {
		out.RedactOxidnsJSON = adapt(h.RedactOxidnsSettings)
	}
	if out.RedactEasytierJSON == nil {
		out.RedactEasytierJSON = adapt(h.RedactEasytierSettings)
	}
	if out.RedactRoutersJSON == nil {
		out.RedactRoutersJSON = adapt(h.RedactRouters)
	}

	return out
}

func legacyDefaultSettings(options Options) (json.RawMessage, error) {
	if options.DefaultConfig == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(options.DefaultConfig())
}
func legacyInitialSettings(options Options) (json.RawMessage, bool, error) {
	if options.InitialConfig == nil {
		return nil, false, nil
	}
	raw, err := json.Marshal(options.InitialConfig())
	return raw, true, err
}

// CreateRuleLibraryItem transcodes the legacy request to the typed creation transaction.
func (r *Repository) CreateRuleLibraryItem(ctx context.Context, rule *jsonx.Object, content []byte) (*jsonx.Object, error) {
	item, err := ruleLibraryRecord(rule)
	if err != nil {
		return nil, err
	}
	result, err := r.CreateRuleLibraryRecord(ctx, item, content)
	if err != nil {
		return nil, err
	}
	return legacyObject(result)
}

// Optional is an argument that may be left out (Python: the _MISSING default).
type Optional struct {
	Set   bool
	Value any
}

// Some is an Optional that is set, also to nil.
func Some(value any) Optional { return Optional{Set: true, Value: value} }

// RouterPublication holds the keyword arguments of save_router_publication.
// A set but falsy state removes the field from the router record.
type RouterPublication struct {
	Manifest    Optional // ros_policy_state
	DNSGuard    Optional // dns_guard_state
	IPv6FakeIP  Optional // ipv6_fakeip_state
	IPv6Routes  Optional // ipv6_routes_state
	IPv6PDGuard Optional // ipv6_pd_guard_state
	LastRelease Optional // last_release (stored as given)
}

func profileCreationFromLegacy(metadata *jsonx.Object, requireName bool) (ProfileCreation, error) {
	if metadata == nil {
		return ProfileCreation{}, validationError(msgMetadataObject)
	}
	input := ProfileCreation{}
	if value := metadata.Value("id"); jsonx.Truthy(value) {
		text, ok := value.(string)
		if !ok {
			return input, validationError("Invalid profile id")
		}
		input.ID = text
	}
	if value := metadata.Value("name"); jsonx.Truthy(value) || requireName {
		text, ok := value.(string)
		if !ok {
			return input, validationError(msgProfileNameEmpty)
		}
		input.Name = text
	}
	if value := metadata.Value("description"); jsonx.Truthy(value) {
		input.Description = jsonx.Str(value)
	}
	return input, nil
}
func deletionIDsFromLegacy(value any) ([]string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var ids []string
	if jsonKind(raw) != '[' || json.Unmarshal(raw, &ids) != nil {
		return nil, validationError("请选择要删除的节点")
	}
	return ids, nil
}

// legacySnapshot is a lazy compatibility view; native readers and writers never
// materialize it. Copies of a storedEntry share one immutable view per generation.
type legacySnapshot struct {
	once     sync.Once
	document *jsonx.Object
	err      error
}

func (e storedEntry) legacyDocument() (*jsonx.Object, error) {
	e.legacy.once.Do(func() { e.legacy.document, e.legacy.err = legacyModelDocument(e.Model) })
	return e.legacy.document, e.legacy.err
}

type legacyState struct{ document *jsonx.Object }

func resourceUsage(document *model.Document, kind, id string) ([]any, error) {
	records, err := resourceUsages(document, kind, id)
	if err != nil {
		return nil, err
	}
	value, err := legacyValue(records)
	if err != nil {
		return nil, err
	}
	return value.([]any), nil
}
func profileUsage(profile *model.Profile) *jsonx.Object {
	result, _ := legacyObject(profileUsageRecord(profile))
	return result
}
