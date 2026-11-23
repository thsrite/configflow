package routes

import (
	"crypto/subtle"
	"net/http"
)

const (
	bearerPrefix         = "Bearer "
	maxBearerTokenLength = 4096
)

func parseBearerToken(authHeader string) ([]byte, bool) {
	if len(authHeader) <= len(bearerPrefix) || len(authHeader) > len(bearerPrefix)+maxBearerTokenLength {
		return nil, false
	}
	if authHeader[:len(bearerPrefix)] != bearerPrefix {
		return nil, false
	}

	token := []byte(authHeader[len(bearerPrefix):])
	for _, char := range token {
		if char < 0x21 || char > 0x7e {
			return nil, false
		}
	}
	return token, true
}

// AuthMiddleware 验证请求的 Bearer Token
func AuthMiddleware(cfg *Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, valid := parseBearerToken(r.Header.Get("Authorization"))
		if !valid || subtle.ConstantTimeCompare(token, []byte(cfg.Token)) != 1 {
			logger.Warnf("Authorization rejected")
			JsonResponse(w, http.StatusUnauthorized, StringStatusMessageResponse{Message: "Unauthorized", Success: "false"})
			return
		}
		next(w, r)
	}
}
