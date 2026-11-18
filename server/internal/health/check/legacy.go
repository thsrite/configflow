package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// KeyError is the legacy missing-key error expected by unmigrated callers of
// ProviderHealthCheck (removed when convert drops legacy health helpers).
type KeyError struct{ Key string }

// Error returns the native missing-health configuration message.
func (e *KeyError) Error() string { return msgMissingHealthCheck }

// Unwrap preserves the native missing-member cause for transitional callers.
func (e *KeyError) Unwrap() error { return ErrMissingHealthCheck }

func decodeLegacy[T any](value any, dst *T) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func encodeLegacyValue(value any) (any, error) {
	raw, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}

func encodeLegacyObject(value any) (*jsonx.Object, error) {
	parsed, err := encodeLegacyValue(value)
	if err != nil {
		return nil, err
	}
	obj, ok := parsed.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", parsed)
	}
	return obj, nil
}

func encodeLegacySlice(value any) ([]any, error) {
	parsed, err := encodeLegacyValue(value)
	if err != nil {
		return nil, err
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON array, got %T", parsed)
	}
	return list, nil
}

func replaceObjectInPlace(target, next *jsonx.Object) {
	for _, key := range target.Keys() {
		target.Delete(key)
	}
	for key, item := range next.All() {
		target.Set(key, item)
	}
}

// ProviderDefaults returns DefaultProviderHealth as a legacy *jsonx.Object.
// Removed when api/proxygroups migrates off jsonx.
func ProviderDefaults() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultProviderHealth())
	return obj
}

// GroupDefaults returns DefaultGroupHealth as a legacy *jsonx.Object.
// Removed when api/proxygroups migrates off jsonx.
func GroupDefaults() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultGroupHealth())
	return obj
}

// InitializeGroupHealth mutates group in place via InitGroupHealth.
// Removed when api/proxygroups migrates off jsonx.
func InitializeGroupHealth(group *jsonx.Object) {
	if group == nil {
		return
	}
	var rawObj rawJSONObject
	if err := decodeLegacy(group, &rawObj); err != nil {
		return
	}
	if err := InitGroupHealth(&rawObj.Fields); err != nil {
		return
	}
	next, err := encodeLegacyObject(rawObj)
	if err != nil {
		return
	}
	replaceObjectInPlace(group, next)
}

// InitializeProviderHealth mutates source in place via InitProviderHealth.
// Removed when api/subscriptions and api/aggregations migrate off jsonx.
func InitializeProviderHealth(source *jsonx.Object) {
	if source == nil {
		return
	}
	var rawObj rawJSONObject
	if err := decodeLegacy(source, &rawObj); err != nil {
		return
	}
	if err := InitProviderHealth(&rawObj.Fields); err != nil {
		return
	}
	next, err := encodeLegacyObject(rawObj)
	if err != nil {
		return
	}
	replaceObjectInPlace(source, next)
}

// ValidateHealth validates legacy jsonx values via ValidateHealthJSON.
// Removed when api/proxygroups and repo/legacy.go migrate off jsonx.
func ValidateHealth(values any, provider bool) error {
	if _, ok := values.(*jsonx.Object); !ok || values == (*jsonx.Object)(nil) {
		return valueError(msgHealthMustBeObject)
	}
	raw, err := jsonx.Compact(values)
	if err != nil {
		return valueError(msgHealthMustBeObject)
	}
	return ValidateHealthJSON(raw, provider)
}

// ProviderHealthCheck resolves source's health_check via ResolveProviderHealth.
// Removed when convert migrates off legacy ProviderHealthCheck.
func ProviderHealthCheck(source *jsonx.Object) (*jsonx.Object, error) {
	if source == nil {
		return nil, &KeyError{Key: "health_check"}
	}
	var rawObj rawJSONObject
	if err := decodeLegacy(source, &rawObj); err != nil {
		return nil, err
	}
	// Transitional callers still expose the raw request validator contract.
	if health, present := rawObj.Fields.Get("health_check"); present {
		if err := ValidateHealthJSON(health, true); err != nil {
			return nil, err
		}
	}
	resolved, err := ResolveProviderHealth(&rawObj.Fields)
	if err != nil {
		if errors.Is(err, ErrMissingHealthCheck) {
			return nil, &KeyError{Key: "health_check"}
		}
		return nil, err
	}
	return encodeLegacyObject(resolved)
}

// ControlTargets returns DefaultControlTargets as a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls DefaultControlTargets directly.
func ControlTargets() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultControlTargets())
	return obj
}

// Judge adapts legacy jsonx path statistics to JudgePath.
// Removed in N8-11 when discovery/bridge.go calls JudgePath directly.
func Judge(direct, proxy any, options JudgeOptions) (*jsonx.Object, error) {
	var d, p *PathStats
	if direct != nil {
		var parsed PathStats
		if err := decodeLegacy(direct, &parsed); err != nil {
			return nil, err
		}
		d = &parsed
	}
	if proxy != nil {
		var parsed PathStats
		if err := decodeLegacy(proxy, &parsed); err != nil {
			return nil, err
		}
		p = &parsed
	}
	return encodeLegacyObject(JudgePath(d, p, options))
}

// Save adapts legacy jsonx probe results to ProbeStore.SaveEntries.
// Removed in N8-11 when discovery/bridge.go calls SaveEntries directly.
func (s *ProbeStore) Save(agentID string, results *jsonx.Object, now time.Time) error {
	return s.SaveContext(context.Background(), agentID, results, now)
}

// SaveContext adapts legacy probe entries while preserving the owner's lifetime.
func (s *ProbeStore) SaveContext(ctx context.Context, agentID string, results *jsonx.Object, now time.Time) error {
	if results == nil {
		return valueError(msgInvalidProbeEntry)
	}
	var entries model.Map[TimedRecord]
	if err := decodeLegacy(results, &entries); err != nil {
		return err
	}
	return s.SaveEntriesContext(ctx, agentID, entries, now)
}

// Load adapts ProbeStore.LoadEntries to a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls LoadEntries directly.
func (s *ProbeStore) Load(agentID string, now time.Time) (*jsonx.Object, error) {
	return encodeLegacyObject(s.LoadEntries(agentID, now))
}

// LatestProbes adapts LatestProbeEntries to a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls LatestProbeEntries directly.
func LatestProbes(agentIDs []string, store *ProbeStore) (*jsonx.Object, error) {
	entries, err := LatestProbeEntries(agentIDs, store)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(entries)
}

// ExitRequest returns DefaultExitRequest as a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls DefaultExitRequest directly.
func ExitRequest() *jsonx.Object {
	obj, _ := encodeLegacyObject(DefaultExitRequest())
	return obj
}

// ServiceRequests returns BuildServiceRequests as a legacy []any.
// Removed in N8-11 when discovery/bridge.go calls BuildServiceRequests directly.
func ServiceRequests() []any {
	list, _ := encodeLegacySlice(BuildServiceRequests())
	return list
}

// DomainRequest returns BuildDomainRequest as a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls BuildDomainRequest directly.
func DomainRequest(requestID, url string) *jsonx.Object {
	obj, _ := encodeLegacyObject(BuildDomainRequest(requestID, url))
	return obj
}

// PublicServices returns PublicServiceList as a legacy []any.
// Removed in N8-11 when discovery/bridge.go calls PublicServiceList directly.
func PublicServices() []any {
	list, _ := encodeLegacySlice(PublicServiceList())
	return list
}

// PickTargets adapts legacy jsonx targets to ChooseTargets.
// Removed in N8-11 when discovery/bridge.go calls ChooseTargets directly.
func PickTargets(targets []any, preferredGroup any) ([]any, error) {
	var typedTargets []AvailableTarget
	if err := decodeLegacy(targets, &typedTargets); err != nil {
		return nil, err
	}
	var preferred Text
	if preferredGroup != nil {
		if err := decodeLegacy(preferredGroup, &preferred); err != nil {
			return nil, err
		}
	}
	return encodeLegacySlice(ChooseTargets(typedTargets, string(preferred)))
}

// ExitInfo adapts legacy jsonx results to SummarizeExit.
// Removed in N8-11 when discovery/bridge.go calls SummarizeExit directly.
func ExitInfo(results []any) (*jsonx.Object, error) {
	var typedResults []RegionResult
	if err := decodeLegacy(results, &typedResults); err != nil {
		return nil, err
	}
	summary, err := SummarizeExit(typedResults)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(summary)
}

// EvaluateServices adapts legacy jsonx results to EvaluateServiceList.
// Removed in N8-11 when discovery/bridge.go calls EvaluateServiceList directly.
func EvaluateServices(results []any) (*jsonx.Object, error) {
	var typedResults []RegionResult
	if err := decodeLegacy(results, &typedResults); err != nil {
		return nil, err
	}
	verdicts, err := EvaluateServiceList(typedResults)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(verdicts)
}

// ClassifyDomainResult adapts a legacy jsonx result to ClassifyDomain.
// Removed in N8-11 when discovery/bridge.go calls ClassifyDomain directly.
func ClassifyDomainResult(result any, originalURL string) (*jsonx.Object, error) {
	var typedResult *RegionResult
	if result != nil {
		var parsed RegionResult
		if err := decodeLegacy(result, &parsed); err != nil {
			return nil, err
		}
		typedResult = &parsed
	}
	classified, err := ClassifyDomain(typedResult, originalURL)
	if err != nil {
		return nil, err
	}
	return encodeLegacyObject(classified)
}

// SummarizeDomain adapts legacy jsonx per-target states to SummarizeTargets.
// Removed in N8-11 when discovery/bridge.go calls SummarizeTargets directly.
func SummarizeDomain(perTarget *jsonx.Object) (*jsonx.Object, error) {
	var typedTargets model.Map[TargetState]
	if perTarget != nil {
		if err := decodeLegacy(perTarget, &typedTargets); err != nil {
			return nil, err
		}
	}
	return encodeLegacyObject(SummarizeTargets(typedTargets))
}

// Load adapts RegionStore.LoadData to a legacy *jsonx.Object.
// Removed in N8-11 when discovery/bridge.go calls LoadData directly.
func (s *RegionStore) Load(agentID string) *jsonx.Object {
	obj, _ := encodeLegacyObject(s.LoadData(agentID))
	return obj
}

// Update adapts a legacy *jsonx.Object mutator to RegionStore.UpdateData.
// Removed in N8-11 when discovery/bridge.go calls UpdateData directly.
func (s *RegionStore) Update(agentID string, mutate func(*jsonx.Object) error, now time.Time) error {
	return s.UpdateContext(context.Background(), agentID, mutate, now)
}

// UpdateContext adapts a legacy region mutator while preserving the owner's lifetime.
func (s *RegionStore) UpdateContext(ctx context.Context, agentID string, mutate func(*jsonx.Object) error, now time.Time) error {
	return s.UpdateDataContext(ctx, agentID, func(data *RegionData) error {
		obj, err := encodeLegacyObject(data)
		if err != nil {
			return err
		}
		if err := mutate(obj); err != nil {
			return err
		}
		var rawCheck rawJSONObject
		if err := decodeLegacy(obj, &rawCheck); err != nil {
			return err
		}
		if !rawCheck.Fields.Has("domains") || !rawCheck.Fields.Has("websites") {
			return valueError(msgInvalidRegionStore)
		}
		var next RegionData
		if err := decodeLegacy(obj, &next); err != nil {
			return err
		}
		*data = next
		return nil
	}, now)
}
