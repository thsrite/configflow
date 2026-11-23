package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"agent/internal/logx"
)

var logger = logx.Get("agent/routes")

func sensitiveURLQueryKey(key string) bool {
	return logx.SensitiveURLQueryKey(key)
}

// redactURLForLog returns a URL suitable for logs without userinfo or query credentials.
func redactURLForLog(raw string) string {
	return logx.RedactURL(raw)
}

func safeURLFailure(action, rawURL string, cause error) error {
	return logx.SafeURLFailure(action, rawURL, cause)
}

func commandForLog(command string) string {
	if strings.HasPrefix(command, "http://") || strings.HasPrefix(command, "https://") {
		return redactURLForLog(command)
	}
	return "[COMMAND REDACTED]"
}

// JsonResponse 是一个辅助函数，用于统一返回JSON响应
func JsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		if statusCode >= 400 {
			logger.Warnf("Returning error response: %d", statusCode)
		}
		json.NewEncoder(w).Encode(data)
	}
}
