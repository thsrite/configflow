package auth

import (
	"bytes"
	"encoding/json"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// ValidateSystemTokenOrJWT accepts internal calls, Bearer JWTs and configuration
// capabilities, while rejecting current and retired internal rule-proxy tokens.
// A nil system reads the request's repository snapshot.
func ValidateSystemTokenOrJWT(c *httpx.Ctx, config *model.System) (ValidateResult, error) {
	if c.IsInternalCall() {
		return ValidateResult{Valid: true}, nil
	}
	ctx := lifecycle.Detach(c.Context())
	if err := ctx.Err(); err != nil {
		return ValidateResult{}, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return ValidateResult{}, err
	}
	var settings *model.SystemConfig
	if config != nil {
		settings = &config.SystemConfig
	} else {
		// Only the settings record: System would copy every Agent's history.
		settings, err = r.SystemConfig(ctx)
		if err != nil {
			return ValidateResult{}, err
		}
	}
	// Stored settings that are not an object read as empty settings (O18
	// T1a); their token is damaged, not unconfigured. Also checked when the
	// caller passes its own snapshot (live configs, Linux gateway providers):
	// damaged settings are only repaired by an explicit settings save, so a
	// generation read in between holds settings a user chose.
	damagedSettings, err := r.SystemSettingsDamaged(ctx)
	if err != nil {
		return ValidateResult{}, err
	}
	configToken, malformedToken, err := storedConfigToken(settings)
	if err != nil {
		return ValidateResult{}, err
	}
	malformedToken = malformedToken || damagedSettings
	tokens := []string{settings.RuleProxyToken}
	// Imported credentials may contain malformed neighbours. Decode each retired
	// capability independently so a valid internal token never becomes public.
	raw, _, err := settings.MemberJSON("retired_rule_proxy_tokens")
	if err != nil {
		return ValidateResult{}, err
	}
	var retired []json.RawMessage
	if json.Unmarshal(raw, &retired) == nil {
		for _, member := range retired {
			var token string
			if json.Unmarshal(member, &token) == nil && token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	bearer, hasBearer := bearerOf(c)
	urlToken, _ := c.Arg("token")
	if !IsTokenWithinLength(urlToken) {
		urlToken = ""
	}
	internalMatch := 0
	for _, text := range tokens {
		if text != "" {
			// Compare fixed-size digests so credential contents and lengths do not
			// short-circuit secret comparisons. Check every retired capability.
			internalMatch |= equalCredential(bearer, text) | equalCredential(urlToken, text)
		}
	}
	if internalMatch != 0 {
		return ValidateResult{Message: invalidOrMissingMsg}, nil
	}
	if !IsAuthEnabled() && configToken == "" && !malformedToken {
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
	if urlToken != "" && equalCredential(urlToken, configToken) == 1 {
		return ValidateResult{Valid: true}, nil
	}
	return ValidateResult{Message: invalidOrMissingMsg}, nil
}

// storedConfigToken returns the configured public credential. Only a stored
// JSON string (or null/absence, meaning unconfigured) is a credential; any
// other stored type, including numbers that the model's scalar rules would
// read as text, is a damaged credential that fails closed.
func storedConfigToken(settings *model.SystemConfig) (token string, malformed bool, err error) {
	raw, present, err := settings.MemberJSON("config_token")
	if err != nil {
		return "", false, err
	}
	raw = bytes.TrimSpace(raw)
	switch {
	case !present || len(raw) == 0 || string(raw) == "null":
		return "", false, nil
	case raw[0] != '"':
		return "", true, nil
	}
	return settings.ConfigToken, false, nil
}
