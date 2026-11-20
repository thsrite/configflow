package auth

import (
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// TODO(N8-11/W9): delete after the remaining resource consumers migrate.
// ValidateTokenOrJWT is the transitional document facade. New callers use
// ValidateSystemTokenOrJWT with the same captured system snapshot.
func ValidateTokenOrJWT(c *httpx.Ctx, config *jsonx.Object) (ValidateResult, error) {
	if config == nil {
		return ValidateSystemTokenOrJWT(c, nil)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return ValidateResult{}, err
	}
	var system model.System
	if err := json.Unmarshal(raw, &system); err != nil {
		return ValidateResult{}, err
	}
	return ValidateSystemTokenOrJWT(c, &system)
}
