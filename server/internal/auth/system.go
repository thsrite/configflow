package auth

import (
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// ValidateSystemTokenOrJWT authenticates a live configuration request against native system credentials.
func ValidateSystemTokenOrJWT(c *httpx.Ctx, system *model.System) (ValidateResult, error) {
	if c.IsInternalCall() {
		return ValidateResult{Valid: true}, nil
	}
	if system == nil {
		r, err := repo.GetRepository(c.Context())
		if err != nil {
			return ValidateResult{}, err
		}
		system, err = r.System(lifecycle.Detach(c.Context()))
		if err != nil {
			return ValidateResult{}, err
		}
	}
	raw, present, err := system.SystemConfig.MemberJSON("config_token")
	if err != nil {
		return ValidateResult{}, err
	}
	var configToken string
	validToken := json.Unmarshal(raw, &configToken) == nil && string(raw) != "null"
	tokenConfigured := present && string(raw) != "null" && string(raw) != `""` && string(raw) != "false" && string(raw) != "0" && string(raw) != "[]" && string(raw) != "{}"
	// Invalid stored token types must never authenticate through their model projection.
	// Empty legacy values still mean that no configuration token is set.
	if tokenConfigured && !validToken {
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			if value, err := number.Float64(); err == nil && value == 0 {
				tokenConfigured = false
			}
		}
		var list []json.RawMessage
		if len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &list) == nil && len(list) == 0 {
			tokenConfigured = false
		}
		var fields map[string]json.RawMessage
		if len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, &fields) == nil && len(fields) == 0 {
			tokenConfigured = false
		}
	}
	tokens, _ := repo.SanitizationTokens(*system)
	bearer, hasBearer := bearerOf(c)
	urlToken, _ := c.Arg("token")
	if !IsTokenWithinLength(urlToken) {
		urlToken = ""
	}
	internalMatch := 0
	for token := range tokens {
		if text := token; text != "" {
			// Compare fixed-size digests so credential contents and lengths do not
			// short-circuit secret comparisons. Check every retired capability.
			internalMatch |= equalCredential(bearer, text) | equalCredential(urlToken, text)
		}
	}
	if internalMatch != 0 {
		return ValidateResult{Message: invalidOrMissingMsg}, nil
	}
	if !IsAuthEnabled() && !tokenConfigured {
		return ValidateResult{Valid: true}, nil
	}
	if hasBearer {
		result, err := VerifyToken(bearer)
		if err != nil {
			return ValidateResult{}, err
		}
		if !result.Failed() {
			return ValidateResult{Valid: true}, nil
		}
	}
	if validToken && urlToken != "" && equalCredential(urlToken, configToken) == 1 {
		return ValidateResult{Valid: true}, nil
	}
	return ValidateResult{Message: invalidOrMissingMsg}, nil
}
