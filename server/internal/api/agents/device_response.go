package agents

import (
	"errors"
	"net/http"

	core "configflow/internal/agents"
	"configflow/internal/httpx"
)

func deviceResponseFailure(err error) (*httpx.Response, bool) {
	var upstream *core.DeviceResponseError
	if !errors.As(err, &upstream) {
		return nil, false
	}
	body := struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{false, upstream.Code, upstream.Message}
	return httpx.JSONStatus(http.StatusBadGateway, body), true
}
