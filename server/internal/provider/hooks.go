// Package provider is the port of backend/utils/provider_delivery.py and
// backend/utils/provider_response.py: request-local validation of the
// proxy-provider payloads (subscriptions, aggregations) before node caches or
// artifacts are published, and the single-flight answer of provider downloads
// with the last good content once Mihomo would time out.
//
// The provider endpoints themselves (/api/subscriptions/<id>/proxies,
// /api/subscriptions/proxies, /api/aggregations/<id>/provider) live in the
// route modules (C30, C31); they call ServeProvider, RequestedFilters,
// Capture, ParseProviderProxies, PrepareProviderBundle and
// CommitProviderBundle from here.
//
// Calls into code of other modules that Python imports lazily go through
// hooks registered by their owners (coordinator decision 3):
//   - SetLocations: agents/deployment_locations.py (A11) preparation_context
//     and source_error.
//   - SetAggregationProvider: routes/aggregations.py
//     generate_aggregation_provider (C31).
package provider

import (
	"configflow/internal/agents/diag"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pyyaml"
	"configflow/internal/refresh"
	"configflow/internal/substore"
)

// Locations is what provider_delivery calls in
// backend/agents/deployment_locations.py (A11). Both methods return str() of
// the DeploymentPreparationError Python builds.
type Locations interface {
	// PreparationContext is str(preparation_context(exc, name=, path=,
	// kind=)) where str(exc) is message. name and path are already str() of
	// the Python values, or "" when those were falsy.
	PreparationContext(message, name, path, kind string) string
	// SourceError is str(source_error(message, text, line=, column=,
	// cause=, source_label=)). line and column are nil for None. cause is
	// the yaml.YAMLError (a *pyyaml.Error; Class "ReaderError" has no
	// context/problem/marks) or nil for None.
	SourceError(message, text string, line, column *int, cause *pyyaml.Error, sourceLabel string) string
}

// PassthroughLocations is the default Locations until A11 registers the
// real one: it retains owning-artifact and source labels; coordinates,
// file locations, excerpts and parser diagnostics remain A11's responsibility.
// It never changes which errors are raised, only their text.
type PassthroughLocations struct{}

// PreparationContext identifies the owning provider without source positions.
func (PassthroughLocations) PreparationContext(message, name, _ string, kind string) string {
	label := kind
	if name != "" {
		label = fmt.Sprintf(msgPreparationName, kind, strings.ReplaceAll(diag.FailureText(name, nil, 512), "\n", `\n`))
	}
	return fmt.Sprintf(msgPreparationFailed, label, message)
}

// SourceError returns message.
func (PassthroughLocations) SourceError(message, _ string, line, _ *int, _ *pyyaml.Error, sourceLabel string) string {
	if line != nil && sourceLabel != "" {
		return message + fmt.Sprintf(msgContentSource, sourceLabel)
	}
	return message
}

// AggregationProvider is routes/aggregations.py
// generate_aggregation_provider(agg, config=config, main_config=main,
// persist=False, subscription_fetch=fetch, source_prepared=True) (C31).
// The result needs Content and CacheUpdates; other keys of the Python dict
// go to Extra.
type AggregationProvider func(ctx context.Context, agg, config, main *jsonx.Object, fetch refresh.SourceFetch) (*Rendered, error)

// ErrAggregationProviderMissing is returned by the default
// AggregationProvider (fail closed) until C31 registers the real one.
var ErrAggregationProviderMissing = errors.New("provider: generate_aggregation_provider is not registered (C31)")

func missingAggregationProvider(context.Context, *jsonx.Object, *jsonx.Object, *jsonx.Object, refresh.SourceFetch) (*Rendered, error) {
	return nil, ErrAggregationProviderMissing
}

var (
	hooksMu             sync.RWMutex
	locations           Locations           = PassthroughLocations{}
	aggregationProvider AggregationProvider = missingAggregationProvider
)

// SetLocations registers A11's deployment_locations; nil restores the
// default. It returns a function restoring the previous value (tests).
func SetLocations(l Locations) (restore func()) {
	if l == nil {
		l = PassthroughLocations{}
	}
	hooksMu.Lock()
	previous := locations
	locations = l
	hooksMu.Unlock()
	return func() { SetLocations(previous) }
}

// SetAggregationProvider registers C31's generate_aggregation_provider;
// nil restores the fail-closed default. It returns a restore function.
func SetAggregationProvider(fn AggregationProvider) (restore func()) {
	if fn == nil {
		fn = missingAggregationProvider
	}
	hooksMu.Lock()
	previous := aggregationProvider
	aggregationProvider = fn
	hooksMu.Unlock()
	return func() { SetAggregationProvider(previous) }
}

func currentLocations() Locations {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return locations
}

func currentAggregationProvider() AggregationProvider {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return aggregationProvider
}

// Env holds what Python reaches through module globals that tests patch.
type Env struct {
	// Fetch is sub_store_client.get_subscription_proxies_yaml, the default
	// fetch of prepare_subscription_provider.
	Fetch refresh.SourceFetch
	// SubscriptionFetch is routes.subscriptions.get_subscription_proxies_yaml
	// (sub_store_client.get_subscription_proxies_yaml(sub_id, url)).
	SubscriptionFetch refresh.SourceFetch
	// AggregationFetch is routes.aggregations.get_subscription_proxies_yaml
	// (the same function, patched separately in Python tests).
	AggregationFetch refresh.SourceFetch
	// Converter converts proxy strings of cached nodes (Sub-Store).
	Converter convert.ProxyStringConverter
	// NodeID is f"node_{uuid.uuid4().hex[:8]}".
	NodeID func() string
}

func defaultFetch(ctx context.Context, subID, url string) (*substore.SubscriptionResult, error) {
	return substore.Default().Client.GetSubscriptionProxiesYAML(ctx, subID, url, "", substore.FetchOptions{})
}

func randomNodeID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "node_" + hex.EncodeToString(b[:])
}

// DefaultEnv is the production environment.
func DefaultEnv() *Env {
	return &Env{Fetch: defaultFetch, SubscriptionFetch: defaultFetch, AggregationFetch: defaultFetch, NodeID: randomNodeID}
}

var (
	envMu sync.Mutex
	env   *Env
)

// SetEnv replaces the environment (nil = DefaultEnv) and returns a restore
// function. Missing fields fall back to the defaults.
func SetEnv(e *Env) (restore func()) {
	envMu.Lock()
	previous := env
	env = e
	envMu.Unlock()
	return func() {
		envMu.Lock()
		env = previous
		envMu.Unlock()
	}
}

func currentEnv() *Env {
	envMu.Lock()
	defer envMu.Unlock()
	e := DefaultEnv()
	if env != nil {
		if env.Fetch != nil {
			e.Fetch = env.Fetch
		}
		if env.SubscriptionFetch != nil {
			e.SubscriptionFetch = env.SubscriptionFetch
		}
		if env.AggregationFetch != nil {
			e.AggregationFetch = env.AggregationFetch
		}
		e.Converter = env.Converter
		if env.NodeID != nil {
			e.NodeID = env.NodeID
		}
	}
	return e
}

func (e *Env) converter() convert.ProxyStringConverter {
	if e.Converter != nil {
		return e.Converter
	}
	return substore.Converter()
}

// SequentialNodeIDs returns a NodeID generator node_00000001, node_00000002,
// ... (tests and reference corpora).
func SequentialNodeIDs() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		s := strconv.FormatInt(int64(n), 16)
		for len(s) < 8 {
			s = "0" + s
		}
		return "node_" + s
	}
}
