package repo

// Module-level access of backend/common/config.py and the profile selection
// rules of backend/common/profile_context.py.
//
// Python keeps request-scoped state in ContextVars. Here it lives in a Scope
// carried by the context: the HTTP layer installs one per request with
// WithScope (and describes the request with SetRequest), background loops
// install one per task or run without. Without a Scope nothing is cached:
// GetConfig reads the repository every time and saves have no merge baseline.

import (
	"context"
	"os"
	"strings"
	"sync"

	"configflow/internal/jsonx"
)

// DataDir is the DATA_DIR resolution of common/config.py: $DATA_DIR, else
// /data when it exists, else "." (development).
func DataDir() string {
	if value, ok := os.LookupEnv("DATA_DIR"); ok {
		return value
	}
	if _, err := os.Stat("/data"); err == nil {
		return "/data"
	}
	return "."
}

var (
	repositoryMu      sync.Mutex
	defaultRepository *Repository
)

type repositoryKey struct{}

// WithRepository makes repository the one the package-level functions use
// for ctx, instead of the process-wide one. Tests use it to run in parallel.
func WithRepository(ctx context.Context, repository *Repository) context.Context {
	return context.WithValue(ctx, repositoryKey{}, repository)
}

// GetRepository is get_repository(): the repository bound to ctx by
// WithRepository, else the process-wide one, opened on first use with
// OpenDefault(DataDir()).
func GetRepository(ctx context.Context) (*Repository, error) {
	if repository, ok := ctx.Value(repositoryKey{}).(*Repository); ok && repository != nil {
		return repository, nil
	}
	repositoryMu.Lock()
	defer repositoryMu.Unlock()
	if defaultRepository == nil {
		repository, err := OpenDefault(DataDir())
		if err != nil {
			return nil, err
		}
		defaultRepository = repository
	}
	return defaultRepository, nil
}

// SetRepository is set_repository(): replace the process-wide repository
// (nil: reopen on next use). Scopes notice the change and drop their caches.
func SetRepository(repository *Repository) {
	repositoryMu.Lock()
	defaultRepository = repository
	repositoryMu.Unlock()
}

// RequestInfo describes the HTTP request a Scope belongs to, as far as
// profile selection needs it. The HTTP layer fills it from its router.
type RequestInfo struct {
	// Endpoint is Flask's request.endpoint ("blueprint.view"), "" when no
	// route matched.
	Endpoint string
	// Global is IsGlobalRequest for this request.
	Global bool
	// The places a profile id is taken from, in this order; nil = absent:
	// the route's profile_id argument, ?profile=, ?profile_id= and the
	// X-ConfigFlow-Profile header.
	PathProfileID, QueryProfile, QueryProfileID, HeaderProfile *string
}

// RequestRoute is the input of IsGlobalRequest.
type RequestRoute struct {
	HasPathProfileID bool   // request.view_args has a profile_id that is not None
	Endpoint         string // request.endpoint, "" for none
	Blueprint        string // request.blueprint
	Path             string // request.path
}

var profileScopedEndpoints = map[string]bool{
	"subscriptions.get_all_subscription_proxies":         true,
	"subscriptions.get_subscription_proxies":             true,
	"subscription_aggregations.get_aggregation_provider": true,
}

var globalBlueprints = map[string]bool{
	"auth": true, "settings": true, "mcp": true, "agents": true, "logs": true, "nodes": true,
	"subscriptions": true, "subscription_aggregations": true, "rule_library": true, "profiles": true,
	"tasks": true, "notifications": true,
}

var globalEndpoints = map[string]bool{"config.export_config": true, "config.import_config": true, "config.reset_config": true}

// IsGlobalRequest is profile_context._is_global_request: whether a request
// is not bound to a profile (no profile is resolved or validated for it and
// no X-ConfigFlow-Profile response header is set).
func IsGlobalRequest(route RequestRoute) bool {
	if route.HasPathProfileID {
		return false
	}
	if profileScopedEndpoints[route.Endpoint] {
		return false
	}
	return globalBlueprints[route.Blueprint] || globalEndpoints[route.Endpoint] ||
		!strings.HasPrefix(route.Path, "/api/") || route.Endpoint == ""
}

// ProfileAPIPath is profile_context.profile_api_path.
func ProfileAPIPath(configData *jsonx.Object, suffix string) string {
	if profileID := configData.Value("profile_id"); jsonx.Truthy(profileID) {
		return "/api/profiles/" + jsonx.Str(profileID) + suffix
	}
	return suffix
}

// ConfigAPIPath is profile_context.config_api_path.
func ConfigAPIPath(configData *jsonx.Object, target string) string {
	if profileID := configData.Value("profile_id"); jsonx.Truthy(profileID) {
		return "/api/config/" + jsonx.Str(profileID) + "/" + target
	}
	return "/api/config/" + target
}

type baseline struct {
	value *jsonx.Object
	build func() (*jsonx.Object, error) // _LazyBaseline
}

// Scope holds the request-scoped state of common/config.py: the compat
// snapshots handed out by GetConfig, the three-way merge baselines captured
// when data was read, and the profile ids already validated for the request.
// It is safe for concurrent use.
type Scope struct {
	mu         sync.Mutex
	repository *Repository
	cache      map[string]*jsonx.Object
	baselines  map[string]baseline
	request    *RequestInfo
	// resolved profile ids, valid for (repository, commit count)
	resolved        map[resolvedKey]string
	resolvedCommits int64
}

type scopeKey struct{}

// WithScope installs a fresh Scope: call it once per request (or background
// task) and pass the returned context down.
func WithScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &Scope{})
}

// ScopeOf returns the Scope of ctx, or nil.
func ScopeOf(ctx context.Context) *Scope {
	scope, _ := ctx.Value(scopeKey{}).(*Scope)
	return scope
}

// SetRequest tells the Scope of ctx which request it serves (Flask:
// has_request_context()). Without it profile selection only uses explicit
// ids and the default profile.
func SetRequest(ctx context.Context, request RequestInfo) {
	if scope := ScopeOf(ctx); scope != nil {
		scope.mu.Lock()
		scope.request = &request
		scope.mu.Unlock()
	}
}

// ResetConfigContext is reset_config_context(): discard the snapshots,
// baselines and validated profile ids of the Scope at request or task
// boundaries and after saves.
func ResetConfigContext(ctx context.Context) {
	if scope := ScopeOf(ctx); scope != nil {
		scope.mu.Lock()
		scope.cache, scope.baselines, scope.resolved = nil, nil, nil
		scope.mu.Unlock()
	}
}

// bind drops everything when the scope was used with another repository
// (set_repository clears the ContextVars); the caller holds scope.mu.
func (s *Scope) bind(repository *Repository) {
	if s.repository != repository {
		s.repository = repository
		s.cache, s.baselines, s.resolved = nil, nil, nil
	}
}

func (s *Scope) setBaseline(repository *Repository, key string, value baseline) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bind(repository)
	if s.baselines == nil {
		s.baselines = map[string]baseline{}
	}
	s.baselines[key] = value
}

// baselineOf is _baseline(key): nil when nothing was read in this scope.
func (s *Scope) baselineOf(repository *Repository, key string) (*jsonx.Object, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	s.bind(repository)
	entry := s.baselines[key]
	s.mu.Unlock()
	if entry.build != nil {
		return entry.build()
	}
	return entry.value, nil
}

// resolvedKey is a key of the resolved_profiles memo: an explicit id, or
// none (Python: None).
type resolvedKey struct {
	explicit bool
	id       string
}

// resolvedProfiles is resolved_profiles(): the validated ids of this
// request, nil inside a transaction (the working copy may have deleted a
// profile while the commit count has not advanced yet). The caller holds
// scope.mu.
func (s *Scope) resolvedProfiles(ctx context.Context, repository *Repository) map[resolvedKey]string {
	if repository.InTransaction(ctx) {
		return nil
	}
	s.bind(repository)
	if commits := repository.CommitCount(); s.resolved == nil || s.resolvedCommits != commits {
		s.resolved, s.resolvedCommits = map[resolvedKey]string{}, commits
	}
	return s.resolved
}

// remember is memo[memo_key] = memo[candidate] = candidate, into the memo
// taken before the profile was validated: when a commit replaced it in the
// meantime, the result is not trusted afterwards (as in Python).
func (s *Scope) remember(memo map[resolvedKey]string, key resolvedKey, profileID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	memo[key], memo[resolvedKey{explicit: true, id: profileID}] = profileID, profileID
}

// ResolveProfile is profile_context.resolve_profile_id(explicit, fallback):
// the explicit id, else (for a profile-bound request) the route argument,
// ?profile=, ?profile_id= or the X-ConfigFlow-Profile header, else fallback,
// else the default profile. The id is validated and must exist
// (ErrValidation, ErrNotFound). Results are remembered per request until
// the next commit.
func ResolveProfile(ctx context.Context, explicit, fallback *string) (string, error) {
	repository, err := GetRepository(ctx)
	if err != nil {
		return "", err
	}
	scope := ScopeOf(ctx)
	var request *RequestInfo
	if scope != nil {
		scope.mu.Lock()
		request = scope.request
		scope.mu.Unlock()
	}
	profileBound := request != nil && (request.Endpoint == "" || !request.Global)
	// Reuse what this request already validated. Global requests take the
	// switchable default profile and calls with a fallback depend on their
	// argument: neither is remembered.
	var memo map[resolvedKey]string
	var memoKey resolvedKey
	if request != nil && fallback == nil && (explicit != nil || profileBound) {
		if explicit != nil {
			memoKey = resolvedKey{explicit: true, id: *explicit}
		}
		scope.mu.Lock()
		memo = scope.resolvedProfiles(ctx, repository)
		resolved, ok := memo[memoKey]
		scope.mu.Unlock()
		if ok {
			return resolved, nil
		}
	}
	candidate := explicit
	if candidate == nil && profileBound {
		for _, source := range []*string{request.PathProfileID, request.QueryProfile, request.QueryProfileID, request.HeaderProfile} {
			if source != nil {
				candidate = source
				break
			}
		}
	}
	if candidate == nil {
		candidate = fallback
	}
	profileID := ""
	if candidate != nil {
		profileID = *candidate
	} else if profileID, err = repository.DefaultProfileID(ctx); err != nil {
		return "", err
	}
	if err := ValidateProfileID(profileID); err != nil {
		return "", err
	}
	if _, err := repository.ProfileMetadata(ctx, profileID); err != nil {
		return "", err
	}
	if memo != nil {
		scope.remember(memo, memoKey, profileID)
	}
	return profileID, nil
}

// ResolveProfileID is ResolveProfile without a fallback; an empty explicit
// id means "not given" (Python: None).
func ResolveProfileID(ctx context.Context, explicit string) (string, error) {
	if explicit == "" {
		return ResolveProfile(ctx, nil, nil)
	}
	return ResolveProfile(ctx, &explicit, nil)
}

// GetDefaultProfileID is get_default_profile_id().
func GetDefaultProfileID(ctx context.Context) (string, error) {
	repository, err := GetRepository(ctx)
	if err != nil {
		return "", err
	}
	return repository.DefaultProfileID(ctx)
}

// GetConfig is get_config(profile_id): the request-scoped compat view of a
// profile ("" = the request's profile). Within one Scope the same object is
// returned every time, so edits made to it are what SaveConfig(ctx, nil, id)
// stores, merged three-way against the state it was read from. Without a
// Scope every call returns a fresh view.
func GetConfig(ctx context.Context, profileID string) (*jsonx.Object, error) {
	resolvedID, err := ResolveProfileID(ctx, profileID)
	if err != nil {
		return nil, err
	}
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	scope := ScopeOf(ctx)
	if scope != nil {
		scope.mu.Lock()
		scope.bind(repository)
		cached := scope.cache[resolvedID]
		scope.mu.Unlock()
		if cached != nil {
			return cached, nil
		}
	}
	config, source, err := repository.GetCompatConfigWithSource(ctx, resolvedID)
	if err != nil || scope == nil {
		return config, err
	}
	entry := baseline{value: config.DeepCopy()}
	if source != nil {
		// A committed document is never modified in place: keeping the
		// reference yields the exact baseline at save time without copying.
		entry = baseline{build: func() (*jsonx.Object, error) { return repository.CompatConfigFrom(source, resolvedID) }}
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if cached := scope.cache[resolvedID]; cached != nil {
		return cached, nil
	}
	if scope.cache == nil {
		scope.cache = map[string]*jsonx.Object{}
	}
	if scope.baselines == nil {
		scope.baselines = map[string]baseline{}
	}
	scope.cache[resolvedID] = config
	scope.baselines[resolvedID] = entry
	return config, nil
}

// GetResourceConfig is get_resource_config(kind, resource_id, profile_id):
// the compat view with one shared resource (or, with resourceID nil, all of
// that kind) resolved in addition; nothing is cached or persisted.
func GetResourceConfig(ctx context.Context, kind string, resourceID *string, profileID string) (*jsonx.Object, error) {
	resolvedID, err := ResolveProfileID(ctx, profileID)
	if err != nil {
		return nil, err
	}
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	roots := ResourceRoots{kind: nil}
	if resourceID != nil {
		roots[kind] = []string{*resourceID}
	}
	return repository.GetCompatConfig(ctx, resolvedID, roots)
}

// LoadConfig is load_config(): startup initialisation. Outside a container
// an empty system_config.server_domain is set from the DefaultServerDomain
// hook; then the InitAgentManager hook runs and the default view is returned.
func LoadConfig(ctx context.Context) (*jsonx.Object, error) {
	ResetConfigContext(ctx)
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	h := currentHooks()
	_, dockerErr := os.Stat("/.dockerenv")
	_, podmanErr := os.Stat("/run/.containerenv")
	if dockerErr != nil && podmanErr != nil && h.DefaultServerDomain != nil {
		system, err := repository.GetSystem(ctx)
		if err != nil {
			return nil, err
		}
		if pyStrip(jsonx.Str(system.GetObject("system_config").GetOr("server_domain", ""))) == "" {
			domain := h.DefaultServerDomain()
			_, err := repository.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
				system.GetObject("system_config").Set("server_domain", domain)
				return nil, nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	if h.InitAgentManager != nil {
		h.InitAgentManager()
	}
	return GetConfig(ctx, "")
}

// SaveConfig is save_config(config, profile_id): store the profile fields
// of config (nil: the Scope's snapshot from GetConfig), merged against the
// baseline captured when the profile was read in this Scope.
func SaveConfig(ctx context.Context, config *jsonx.Object, profileID string) error {
	_, err := saveConfig(ctx, config, profileID, true)
	return err
}

// saveConfig is the body of save_config (snapshotDefault: a nil config is
// the Scope's snapshot) and save_config_with_publish_guard (a nil config is
// refused by save_profile, after the profile was resolved).
func saveConfig(ctx context.Context, config *jsonx.Object, profileID string, snapshotDefault bool) (*jsonx.Object, error) {
	resolvedID, err := ResolveProfileID(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if config == nil && snapshotDefault {
		if config, err = GetConfig(ctx, resolvedID); err != nil {
			return nil, err
		}
	}
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	base, err := ScopeOf(ctx).baselineOf(repository, resolvedID)
	if err != nil {
		return nil, err
	}
	_, revision, err := repository.SaveProfile(ctx, resolvedID, config, base)
	if err != nil {
		return nil, err
	}
	ResetConfigContext(ctx)
	return jsonx.Obj("expected_profile_id", resolvedID, "expected_config_revision", jsonx.Int(revision)), nil
}

// SaveConfigWithPublishGuard is save_config_with_publish_guard: save a draft
// and return the exact committed version for an optional push:
// {expected_profile_id, expected_service_type, expected_config_revision}.
func SaveConfigWithPublishGuard(ctx context.Context, config *jsonx.Object, serviceType, profileID string) (*jsonx.Object, error) {
	saved, err := saveConfig(ctx, config, profileID, false)
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("expected_profile_id", saved.Value("expected_profile_id"), "expected_service_type", serviceType,
		"expected_config_revision", saved.Value("expected_config_revision")), nil
}

// UpdateConfigTransaction is update_config_transaction(updater,
// profile_id): apply an incremental update to freshly locked profile data;
// it returns a copy of the stored profile. The Scope's snapshot of that
// profile is dropped.
func UpdateConfigTransaction(ctx context.Context, updater Updater, profileID string) (*jsonx.Object, error) {
	resolvedID, err := ResolveProfileID(ctx, profileID)
	if err != nil {
		return nil, err
	}
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	result, _, err := repository.UpdateProfileTransaction(ctx, resolvedID, updater)
	if err != nil {
		return nil, err
	}
	if scope := ScopeOf(ctx); scope != nil {
		scope.mu.Lock()
		delete(scope.cache, resolvedID)
		delete(scope.baselines, resolvedID)
		scope.mu.Unlock()
	}
	return result, nil
}

// SafeImportConfig is safe_import_config(new_data, profile_id).
func SafeImportConfig(ctx context.Context, newData *jsonx.Object, profileID string) error {
	resolvedID, err := ResolveProfileID(ctx, profileID)
	if err != nil {
		return err
	}
	repository, err := GetRepository(ctx)
	if err != nil {
		return err
	}
	if _, err := repository.ImportProfile(ctx, resolvedID, newData); err != nil {
		return err
	}
	ResetConfigContext(ctx)
	return nil
}

const (
	sharedBaselineKey = "@shared"
	systemBaselineKey = "@system"
)

func sectionBaseline(result, source *jsonx.Object) baseline {
	if source != nil {
		return baseline{build: func() (*jsonx.Object, error) { return source.DeepCopy(), nil }}
	}
	return baseline{value: result.DeepCopy()}
}

// GetSharedConfig is get_shared_config(): a copy of the shared section; the
// state it was read from becomes the baseline of SaveSharedConfig.
func GetSharedConfig(ctx context.Context) (*jsonx.Object, error) {
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	result, source, err := repository.GetSharedWithSource(ctx)
	if err != nil {
		return nil, err
	}
	ScopeOf(ctx).setBaseline(repository, sharedBaselineKey, sectionBaseline(result, source))
	return result, nil
}

// SaveSharedConfig is save_shared_config(config, baseline); a nil baseline
// uses the one captured by GetSharedConfig in this Scope, if any.
func SaveSharedConfig(ctx context.Context, config, base *jsonx.Object) error {
	repository, err := GetRepository(ctx)
	if err != nil {
		return err
	}
	if base == nil {
		if base, err = ScopeOf(ctx).baselineOf(repository, sharedBaselineKey); err != nil {
			return err
		}
	}
	if _, err := repository.SaveShared(ctx, config, base); err != nil {
		return err
	}
	ResetConfigContext(ctx)
	return nil
}

// UpdateSharedConfigTransaction is update_shared_config_transaction.
func UpdateSharedConfigTransaction(ctx context.Context, updater Updater) (*jsonx.Object, error) {
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	result, err := repository.UpdateSharedTransaction(ctx, updater)
	if err != nil {
		return nil, err
	}
	ResetConfigContext(ctx)
	return result, nil
}

// UpdateSharedAndConfigTransaction is update_shared_and_config_transaction:
// update shared resources and one or more profiles in a single commit. A nil
// profileIDs selects the request's profile.
func UpdateSharedAndConfigTransaction(ctx context.Context, sharedUpdater Updater, updater ProfileUpdater,
	profileIDs ProfileSelector) (shared, profiles *jsonx.Object, err error) {
	if profileIDs == nil {
		resolvedID, err := ResolveProfileID(ctx, "")
		if err != nil {
			return nil, nil, err
		}
		profileIDs = Profiles(resolvedID)
	}
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	shared, profiles, err = repository.UpdateSharedAndProfilesTransaction(ctx, sharedUpdater, updater, profileIDs)
	if err != nil {
		return nil, nil, err
	}
	ResetConfigContext(ctx)
	return shared, profiles, nil
}

// GetSystemConfig is get_system_config(): a copy of the system section
// (settings, backup, agents); the state it was read from becomes the
// baseline of SaveSystemConfig.
func GetSystemConfig(ctx context.Context) (*jsonx.Object, error) {
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	result, source, err := repository.GetSystemWithSource(ctx)
	if err != nil {
		return nil, err
	}
	ScopeOf(ctx).setBaseline(repository, systemBaselineKey, sectionBaseline(result, source))
	return result, nil
}

// SaveSystemConfig is save_system_config(config, baseline); a nil baseline
// uses the one captured by GetSystemConfig in this Scope, if any.
func SaveSystemConfig(ctx context.Context, config, base *jsonx.Object) error {
	repository, err := GetRepository(ctx)
	if err != nil {
		return err
	}
	if base == nil {
		if base, err = ScopeOf(ctx).baselineOf(repository, systemBaselineKey); err != nil {
			return err
		}
	}
	if _, err := repository.SaveSystem(ctx, config, base); err != nil {
		return err
	}
	ResetConfigContext(ctx)
	return nil
}
