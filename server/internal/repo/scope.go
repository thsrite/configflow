package repo

// Module-level access of backend/common/config.py and the profile selection
// rules of backend/common/profile_context.py.
//
// Python keeps request-scoped state in ContextVars. Here it lives in a Scope
// carried by the context: the HTTP layer installs one per request with
// WithScope (and describes the request with SetRequest), background loops
// install one per task or run without. Without a Scope nothing is cached:
// Config reads the repository every time and saves have no merge baseline.

import (
	"context"
	"os"
	"strings"
	"sync"

	"configflow/internal/ctxx"
	"configflow/internal/model"
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

// Scope holds the request-scoped state of common/config.py: the resolved
// snapshots handed out by Config, the three-way merge baselines captured when
// shared data was read, and the profile ids already validated for the request.
// It is safe for concurrent use.
type Scope struct {
	mu              sync.Mutex
	repository      *Repository
	configs         map[string]*model.Config
	recordBaselines map[string]model.Fields
	request         *RequestInfo
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
		scope.configs, scope.recordBaselines, scope.resolved = nil, nil, nil
		scope.mu.Unlock()
	}
}

// bind drops everything when the scope was used with another repository
// (set_repository clears the ContextVars); the caller holds scope.mu.
func (s *Scope) bind(repository *Repository) {
	if s.repository != repository {
		s.repository = repository
		s.configs, s.recordBaselines, s.resolved = nil, nil, nil
	}
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
func (r *Repository) ResolveProfile(ctx context.Context, explicit, fallback *string) (string, error) {
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
		memo = scope.resolvedProfiles(ctx, r)
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
	var err error
	if candidate != nil {
		profileID = *candidate
	} else if profileID, err = r.DefaultProfileID(ctx); err != nil {
		return "", err
	}
	if err := ValidateProfileID(profileID); err != nil {
		return "", err
	}
	// Request preparation must finish before detached handlers (such as
	// provider rendering) start, even if the HTTP waiter already disconnected;
	// work shutdown carried by ctxx.WithParent is still honored.
	if _, err := r.ProfileMetadataRecord(ctxx.Detach(ctx), profileID); err != nil {
		return "", err
	}
	if memo != nil {
		scope.remember(memo, memoKey, profileID)
	}
	return profileID, nil
}

// ResolveProfileID is ResolveProfile without a fallback; an empty explicit
// id means "not given" (Python: None).
func (r *Repository) ResolveProfileID(ctx context.Context, explicit string) (string, error) {
	if explicit == "" {
		return r.ResolveProfile(ctx, nil, nil)
	}
	return r.ResolveProfile(ctx, &explicit, nil)
}

// ScopedConfig returns the request-scoped resolved view of a profile ("" = the
// request's profile). Within one Scope the same *model.Config snapshot is
// returned until invalidated by a write or ResetConfigContext. Without a
// Scope every call returns a fresh view.
func (r *Repository) ScopedConfig(ctx context.Context, profileID string) (*model.Config, error) {
	resolvedID, err := r.ResolveProfileID(ctx, profileID)
	if err != nil {
		return nil, err
	}
	scope := ScopeOf(ctx)
	if scope != nil {
		scope.mu.Lock()
		scope.bind(r)
		cached := scope.configs[resolvedID]
		scope.mu.Unlock()
		if cached != nil {
			return cached, nil
		}
	}
	config, err := r.Config(ctx, resolvedID, nil)
	if err != nil || scope == nil {
		return config, err
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	scope.bind(r)
	if cached := scope.configs[resolvedID]; cached != nil {
		return cached, nil
	}
	if scope.configs == nil {
		scope.configs = map[string]*model.Config{}
	}
	scope.configs[resolvedID] = config
	return config, nil
}

// PrepareConfig performs configuration startup preparation on r.
func (r *Repository) PrepareConfig(ctx context.Context) (*model.Config, error) {
	ctx = ctxx.Detach(ctx)
	if err := r.prepareConfig(ctx); err != nil {
		return nil, err
	}
	return r.ScopedConfig(ctx, "")
}

func (r *Repository) prepareConfig(ctx context.Context) error {
	ResetConfigContext(ctx)
	domainFn := r.options.DefaultServerDomain
	_, dockerErr := os.Stat("/.dockerenv")
	_, podmanErr := os.Stat("/run/.containerenv")
	if dockerErr != nil && podmanErr != nil && domainFn != nil {
		settings, err := r.SystemConfig(ctx)
		if err != nil {
			return err
		}
		if trimText(settings.ServerDomain) == "" {
			domain := domainFn()
			if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
				s.SystemConfig.ServerDomain = domain
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

const sharedBaselineKey = "@shared"
