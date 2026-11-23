package routes

import (
	"net/http"
)

// HealthHandler 处理健康检查请求
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	logger.Debugf("Health check requested from %s", r.RemoteAddr)

	// JsonResponse 是一个辅助函数，用于统一返回JSON响应
	JsonResponse(w, http.StatusOK, HealthResponse{Status: "ok"})
}
