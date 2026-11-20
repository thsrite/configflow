package agents

// Extension points for the packages that build on the registry. Python
// imported these modules lazily from manager.py and routes/agents.py; here the
// owning package registers its part from its own wire function. Every hook
// has the default described on its field: either the exact Python behaviour
// for the case the later package is not involved in, or a fail-closed error.

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"configflow/internal/model"
)

// InstallationError is gateway_installation.InstallationError (#30).
type InstallationError struct {
	Message string
	Status  int
}

func (e *InstallationError) Error() string { return e.Message }

// Hooks are the parts of other packages the registry calls.
type Hooks struct {
	// PinnedWithheld reports A16 baseline projection of pinned upstreams for
	// selected rules. Nil means unavailable; callers omit the best-effort field
	// after a committed save instead of rolling it back.
	PinnedWithheld func(ctx context.Context, profileID string, ruleIDs []string) (bool, error)
	// AddMetricsJSON is the context-aware native sample boundary. Installed
	// owners check Runtime shutdown again at the in-memory append point.
	AddMetricsJSON func(ctx context.Context, m *Manager, agentID string, sample json.RawMessage) error
	// DeleteMetricsHistoryContext is the native shutdown-aware history cleanup.
	DeleteMetricsHistoryContext func(context.Context, *Manager, string) error
	// DeleteTrafficHistoryContext keeps the work lifetime through auxiliary storage.
	DeleteTrafficHistoryContext func(context.Context, *Manager, string) error

	// PublicationComplete and PublicationCheckpoint are
	// oxidns_publication_stats.complete / checkpoint (#21). Default: nothing.
	PublicationComplete   func(ctx context.Context, m *Manager, agentID, deploymentID string, status any) error
	PublicationCheckpoint func(ctx context.Context, m *Manager, agentID, deploymentID string, configSHA256 any) error

	// AddressFollow and AddressConfirmed are address_follow.follow and
	// address_follow.confirmed (#28), used by the heartbeat route when the
	// Agent reports its host. Default: nothing followed, no confirmed_host.
	// AddressFollowTyped and AddressConfirmedTyped are the native address owners.
	AddressFollowTyped    func(context.Context, *Manager, *model.Agent, string) error
	AddressConfirmedTyped func(*model.Agent, string) string

	// ReceiptMatches is oxidns_validation_receipt.receipt_matches (#28),
	// checked before an OxiDNS candidate is activated. Default: false (fail
	// closed: activation is refused and the task fails as unchecked).
	ReceiptMatches func(*model.Agent, *model.Deployment) bool

	// HAServiceOperation, HADeployment and HAActivate are
	// oxidns_ha_publish.service_operation / deployment / activate (#29).
	// Defaults: the Python code path for Agents without an active pair
	// publication (conflict checks included, without the pair lock file);
	// an active pair publication fails closed with ErrHAOwnerMissing.
	// HAServiceOperationTyped fences native restart/uninstall operations with the
	// same HA lease. No legacy operation hook is accepted.
	HAServiceOperationTyped func(ctx context.Context, m *Manager, agentID string, operation func() (json.RawMessage, error), uninstall bool) (json.RawMessage, error)

	HADeployment func(ctx context.Context, m *Manager, agentID, deploymentID string) (*model.Deployment, error)
	HAActivate   func(ctx context.Context, m *Manager, agentID, deploymentID string) (*model.Deployment, error)

	// InstallationProfile and InstallationRegister are
	// gateway_installation.registration_profile / register (#30), used by
	// the registration route for gateway installs. Return *InstallationError
	// for the user-facing refusals. Default: an InstallationError (501).
	// InstallationProfileTyped validates native request bytes and returns the bound
	// profile and updated bytes. Argument order: data, installation ID, token, peer host.
	// Only the native owner is registered for this event.
	InstallationProfileTyped func(ctx context.Context, data json.RawMessage, installationID, installationToken, fallbackHost string) (profileID string, updated json.RawMessage, err error)
	// InstallationRegisterTyped commits native request bytes through the installation
	// owner. Argument order after manager: installation ID, token, data, existing token.
	InstallationRegisterTyped func(ctx context.Context, m *Manager, installationID, installationToken string, data json.RawMessage, existingToken string) (json.RawMessage, error)
}

// ErrNotPorted is returned where the registry would call a later package
// whose hook is not registered yet.
var ErrNotPorted = errors.New("agents: the package providing this operation is not ported yet")

var (
	hooksMu sync.RWMutex
	hooks   Hooks
)

// RegisterHooks installs the non-nil fields.
func RegisterHooks(h Hooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h.PinnedWithheld != nil {
		hooks.PinnedWithheld = h.PinnedWithheld
	}
	if h.AddMetricsJSON != nil {
		hooks.AddMetricsJSON = h.AddMetricsJSON
	}
	if h.DeleteMetricsHistoryContext != nil {
		hooks.DeleteMetricsHistoryContext = h.DeleteMetricsHistoryContext
	}
	if h.DeleteTrafficHistoryContext != nil {
		hooks.DeleteTrafficHistoryContext = h.DeleteTrafficHistoryContext
	}
	if h.PublicationComplete != nil {
		hooks.PublicationComplete = h.PublicationComplete
	}
	if h.PublicationCheckpoint != nil {
		hooks.PublicationCheckpoint = h.PublicationCheckpoint
	}
	if h.AddressFollowTyped != nil {
		hooks.AddressFollowTyped = h.AddressFollowTyped
	}
	if h.AddressConfirmedTyped != nil {
		hooks.AddressConfirmedTyped = h.AddressConfirmedTyped
	}
	if h.ReceiptMatches != nil {
		hooks.ReceiptMatches = h.ReceiptMatches
	}
	if h.HAServiceOperationTyped != nil {
		hooks.HAServiceOperationTyped = h.HAServiceOperationTyped
	}
	if h.HADeployment != nil {
		hooks.HADeployment = h.HADeployment
	}
	if h.HAActivate != nil {
		hooks.HAActivate = h.HAActivate
	}
	if h.InstallationProfileTyped != nil {
		hooks.InstallationProfileTyped = h.InstallationProfileTyped
	}
	if h.InstallationRegisterTyped != nil {
		hooks.InstallationRegisterTyped = h.InstallationRegisterTyped
	}
}

// ResetHooks clears every hook, delete function and init function (tests).
func ResetHooks() {
	hooksMu.Lock()
	hooks = Hooks{}
	hooksMu.Unlock()
	listMu.Lock()
	deleteHooks, initHooks = nil, nil
	listMu.Unlock()
}

func currentHooks() Hooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return hooks
}

// CurrentHooks returns the installed hooks (for routes in other packages).
func CurrentHooks() Hooks { return currentHooks() }

var (
	listMu      sync.Mutex
	deleteHooks []namedDelete
	initHooks   []func(ctx context.Context, m *Manager)
)

type namedDelete struct {
	name string
	fn   func(ctx context.Context, agentID string) error
}

// AgentDeletedStores are the per-Agent stores Python's DELETE
// /api/agents/<id> route always clears after the registry deleted the
// Agent, in its order. Each is registered with OnAgentDeleted under this
// name by the package that owns the store:
//
//	traffic_store  get_traffic_store().delete   A05 (traffic history and traffic store)
//	domain_probe   get_probe_store().delete     C16 (domain probe)
//	region_check   get_region_store().delete    C16 (region check)
//
// The manager-level deletes of delete_agent are Hooks.DeleteMetricsHistoryContext
// (A04) and Hooks.DeleteTrafficHistoryContext (A05).
var AgentDeletedStores = []string{"traffic_store", "domain_probe", "region_check"}

// OnAgentDeleted adds a store the DELETE /api/agents/<id> route clears after
// the registry deleted the Agent; an error answers 500 with its text. The
// stores of AgentDeletedStores run first and in that order whatever the
// order of registration; other names follow in registration order.
// Registering the same name again replaces the function in place.
func OnAgentDeleted(name string, fn func(ctx context.Context, agentID string) error) {
	listMu.Lock()
	defer listMu.Unlock()
	for i, item := range deleteHooks {
		if item.name == name {
			deleteHooks[i].fn = fn
			return
		}
	}
	deleteHooks = append(deleteHooks, namedDelete{name, fn})
}

func orderedDeleteHooks() []namedDelete {
	listMu.Lock()
	defer listMu.Unlock()
	rank := func(name string) int {
		for i, known := range AgentDeletedStores {
			if known == name {
				return i
			}
		}
		return len(AgentDeletedStores)
	}
	out := append([]namedDelete{}, deleteHooks...)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].name) < rank(out[j].name) })
	return out
}

// AgentDeletedHooks returns the registered delete functions in order.
func AgentDeletedHooks() []func(ctx context.Context, agentID string) error {
	hooks := orderedDeleteHooks()
	out := make([]func(ctx context.Context, agentID string) error, len(hooks))
	for i, item := range hooks {
		out[i] = item.fn
	}
	return out
}

// AgentDeletedHookNames returns the names of the registered delete
// functions in the order AgentDeletedHooks runs them.
func AgentDeletedHookNames() []string {
	hooks := orderedDeleteHooks()
	out := make([]string, len(hooks))
	for i, item := range hooks {
		out[i] = item.name
	}
	return out
}

// OnManagerInit adds work init_agent_manager does after creating the
// manager: dns_follow.start_loop (#28), oxidns_ha_publish.start_loop (#29),
// ipv6_verification.start_loop (#31), in that order of registration.
func OnManagerInit(fn func(ctx context.Context, m *Manager)) {
	listMu.Lock()
	initHooks = append(initHooks, fn)
	listMu.Unlock()
}

func managerInitHooks() []func(ctx context.Context, m *Manager) {
	listMu.Lock()
	defer listMu.Unlock()
	return append([]func(ctx context.Context, m *Manager){}, initHooks...)
}
