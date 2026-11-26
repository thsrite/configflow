package routes

import (
	"encoding/json"
	"net/http"

	"agent/internal/logx"
)

var logger = logx.Get("agent/routes")

func sensitiveURLQueryKey(key string) bool {
	return logx.SensitiveURLQueryKey(key)
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
