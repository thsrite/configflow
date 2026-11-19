package monitor

// Compatibility codecs for callers whose API surface has not completed N8.
// All cache validation, transport and result selection belong to CallCache.
import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

type legacyRegistry struct{ Manager }

func (m legacyRegistry) ReadAgent(ctx context.Context, id string, history bool) (*model.Agent, error) {
	raw, err := m.GetAgentByID(ctx, id, history)
	if err != nil || raw == nil {
		return nil, err
	}
	data, err := jsonx.Compact(raw)
	if err != nil {
		return nil, err
	}
	var a model.Agent
	if err := jsondecode.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
func registryFor(m Manager) Registry {
	if typed, ok := m.(Registry); ok {
		return typed
	}
	return legacyRegistry{m}
}

func nativeTags(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	tags := make([]string, len(list))
	for i, value := range list {
		tag, ok := value.(string)
		if !ok {
			return nil, false
		}
		tags[i] = tag
	}
	return tags, true
}

// ValidRequestTags validates the legacy request representation.
func ValidRequestTags(value any) (bool, error) {
	if list, ok := value.([]any); ok {
		for _, tag := range list {
			if _, ok := tag.([]any); ok {
				return false, errors.New("cache tags must be strings")
			}
			if _, ok := tag.(*jsonx.Object); ok {
				return false, errors.New("cache tags must be strings")
			}
		}
	}
	tags, ok := nativeTags(value)
	return ok && validTags(tags, true), nil
}

// CacheCall converts the legacy request and response at the package boundary.
func CacheCall(ctx context.Context, m Manager, agentID, method string, value any) (*jsonx.Object, error) {
	var tags []string
	if value != nil {
		valid, err := ValidRequestTags(value)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, CacheFail("INVALID_REQUEST", 400)
		}
		tags, _ = nativeTags(value)
	}
	result, err := CallCache(ctx, registryFor(m), agentID, method, tags)
	if err != nil {
		var partial *CacheOperationError
		if errors.As(err, &partial) {
			flushed := make([]any, len(partial.Flushed))
			for i, tag := range partial.Flushed {
				flushed[i] = tag
			}
			return nil, &FlushError{Err: partial.Err, Flushed: flushed}
		}
		return nil, err
	}
	key, values := "caches", result.Caches
	if method == "POST" {
		key, values = "flushed", result.Flushed
	}
	out := make([]any, len(values))
	for i, tag := range values {
		out[i] = tag
	}
	return jsonx.Obj("success", result.Success, key, out), nil
}

func (m legacyRegistry) monitorReply(ctx context.Context, a *model.Agent, endpoint string, params query, minimum string) (model.Fields, error) {
	var fields model.Fields
	if err := ctx.Err(); err != nil {
		return fields, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return fields, err
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return fields, err
	}
	answer, err := doRequest(ctx, m.Manager, decoded.(*jsonx.Object), endpoint, params, minimum)
	if err != nil {
		return fields, err
	}
	raw, err = jsonx.Compact(answer)
	if err != nil {
		return fields, err
	}
	if err := ctx.Err(); err != nil {
		return fields, err
	}
	raw, _ = jsonresp.NormalizeNonFinite(raw)
	err = jsondecode.Unmarshal(raw, &fields)
	if err != nil {
		return fields, NewError("INVALID_RESPONSE", 0, "")
	}
	return fields, nil
}

// FlushError is the legacy partial-flush response codec.
type FlushError struct {
	Err     *Error
	Flushed []any
}

func (e *FlushError) Error() string { return e.Err.Message }
func (e *FlushError) Unwrap() error { return e.Err }
