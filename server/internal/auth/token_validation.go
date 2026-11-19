package auth

import (
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
	if config == nil {
		ctx := lifecycle.Detach(c.Context())
		if err := ctx.Err(); err != nil {
			return ValidateResult{}, err
		}
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return ValidateResult{}, err
		}
		config, err = r.System(ctx)
		if err != nil {
			return ValidateResult{}, err
		}
	}
	settings := &config.SystemConfig
	configToken := settings.ConfigToken
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
	if !IsAuthEnabled() && configToken == "" && !model.IssueUnder(model.Issues(settings), "config_token") {
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
