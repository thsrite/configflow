package auth

import (
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// TODO(N8-5): replace this repository adapter with typed system credentials.
// ValidateTokenOrJWT is validate_token_or_jwt(request, config): accept an
// internal call, a valid Bearer JWT, or ?token= equal to the configured
// config token; with auth disabled and no config token everything passes.
// The internal rule proxy tokens (current and retired) never authenticate.
// config is the system document (get_system_config()); nil reads it.
func ValidateTokenOrJWT(c *httpx.Ctx, config *jsonx.Object) (ValidateResult, error) {
	if c.IsInternalCall() {
		return ValidateResult{Valid: true}, nil
	}
	if config == nil {
		var err error
		if config, err = repo.GetSystemConfig(c.Context()); err != nil {
			return ValidateResult{}, err
		}
	}
	systemConfig, _ := config.GetOr("system_config", jsonx.NewObject()).(*jsonx.Object)
	if !jsonx.Truthy(systemConfig) {
		systemConfig = jsonx.NewObject()
	}
	configToken := systemConfig.GetOr("config_token", "")
	ruleProxyToken := systemConfig.GetOr("rule_proxy_token", "")
	retired, _ := systemConfig.GetOr("retired_rule_proxy_tokens", []any{}).([]any)
	bearer, hasBearer := bearerOf(c)
	urlToken, _ := c.Arg("token")
	if !IsTokenWithinLength(urlToken) {
		urlToken = ""
	}
	internalMatch := 0
	for _, token := range append([]any{ruleProxyToken}, retired...) {
		if text, ok := token.(string); ok && text != "" {
			// Compare fixed-size digests so credential contents and lengths do not
			// short-circuit secret comparisons. Check every retired capability.
			internalMatch |= equalCredential(bearer, text) | equalCredential(urlToken, text)
		}
	}
	if internalMatch != 0 {
		return ValidateResult{Message: invalidOrMissingMsg}, nil
	}
	if !IsAuthEnabled() && !jsonx.Truthy(configToken) {
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
	if expected, ok := configToken.(string); ok && urlToken != "" && equalCredential(urlToken, expected) == 1 {
		return ValidateResult{Valid: true}, nil
	}
	return ValidateResult{Message: invalidOrMissingMsg}, nil
}
