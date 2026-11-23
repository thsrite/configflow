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
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
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
	// the native parser/policy failure, or nil for reference errors. Positions
	// are one-based and absent when the parser supplied none.
	SourceError(message, text string, line, column *int, cause error, sourceLabel string) string
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
func (PassthroughLocations) SourceError(message, _ string, line, _ *int, _ error, sourceLabel string) string {
	if line != nil && sourceLabel != "" {
		return message + fmt.Sprintf(msgContentSource, sourceLabel)
	}
	return message
}

// AggregationRenderer prepares one typed source against a frozen configuration.
type AggregationRenderer func(context.Context, model.Aggregation, *model.Config, *yamlx.Node, *DeliverySnapshot, refresh.FetchSourceFn) (*Artifact, error)

// ErrAggregationProviderMissing is returned by the default
// AggregationProvider (fail closed) until C31 registers the real one.
var ErrAggregationProviderMissing = errors.New("provider: generate_aggregation_provider is not registered (C31)")

func missingAggregationProvider(context.Context, model.Aggregation, *model.Config, *yamlx.Node, *DeliverySnapshot, refresh.FetchSourceFn) (*Artifact, error) {
	return nil, ErrAggregationProviderMissing
}

// SetLocations registers A11's deployment_locations on the default service;
// nil restores the default. It returns a function restoring the previous value.
func SetLocations(l Locations) (restore func()) { return Default().setLocations(l) }

// SetAggregationRenderer registers the aggregation owner on the default service;
// nil restores the fail-closed default. It returns a restore function.
func SetAggregationRenderer(fn AggregationRenderer) (restore func()) {
	return Default().setAggregationRenderer(fn)
}

func currentLocations() Locations { return Default().Locations() }

func currentAggregationRenderer() AggregationRenderer { return Default().AggregationRenderer() }

// DeliveryEnv supplies typed source and node conversion collaborators.
type DeliveryEnv struct {
	// Fetch is sub_store_client.get_subscription_proxies_yaml, the default
	// fetch of prepare_subscription_provider.
	Fetch refresh.FetchSourceFn
	// SubscriptionFetch is routes.subscriptions.get_subscription_proxies_yaml
	// (sub_store_client.get_subscription_proxies_yaml(sub_id, url)).
	SubscriptionFetch refresh.FetchSourceFn
	// AggregationFetch is routes.aggregations.get_subscription_proxies_yaml
	// (the same function, patched separately in Python tests).
	AggregationFetch refresh.FetchSourceFn
	// Converter converts proxy strings of cached nodes (Sub-Store).
	Converter convert.NodeConverter
	// NodeID is f"node_{uuid.uuid4().hex[:8]}".
	NodeID func() string
}

func defaultFetch(ctx context.Context, subID, url string) (*refresh.SourceResult, error) {
	result, err := substore.Default().Client.Fetch(ctx, subID, url, "", substore.FetchOptions{})
	if err != nil || result == nil {
		return nil, err
	}
	var traffic *refresh.Traffic
	if result.Traffic != nil {
		data, err := model.Marshal(result.Traffic)
		if err != nil {
			return nil, err
		}
		traffic = &refresh.Traffic{}
		if err := json.Unmarshal(data, traffic); err != nil {
			return nil, err
		}
	}
	return &refresh.SourceResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}, nil
}

func randomNodeID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "node_" + strconv.FormatUint(nodeFallback.Add(1), 16)
	}
	return "node_" + hex.EncodeToString(b[:])
}

var nodeFallback atomic.Uint64

// DefaultDeliveryEnv is the production environment.
func DefaultDeliveryEnv() *DeliveryEnv {
	return &DeliveryEnv{Fetch: defaultFetch, SubscriptionFetch: defaultFetch, AggregationFetch: defaultFetch, NodeID: randomNodeID}
}

// SetDeliveryEnv replaces the environment on the default service
// (nil = DefaultDeliveryEnv) and returns a restore function.
func SetDeliveryEnv(e *DeliveryEnv) (restore func()) { return Default().setDeliveryEnv(e) }

func currentDeliveryEnv() *DeliveryEnv { return Default().DeliveryEnv() }

func (e *DeliveryEnv) converter() convert.NodeConverter {
	if e.Converter != nil {
		return e.Converter
	}
	return convert.OptionalNodeConverter(substore.NativeConverter())
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
