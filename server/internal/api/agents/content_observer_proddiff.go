//go:build configflow_proddiff

package agents

import (
	"configflow/internal/jsondecode"
	"context"
	"errors"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
)

const contentObserverHeader = "X-ConfigFlow-Proddiff-Error"
const contentObserverTimeout = 120 * time.Second

var errContentObservation = errors.New("proddiff content sync did not settle")

// InstallProddiffContentObserver installs the proddiff content-sync observer
// backed by h's handlers.
func (h *Handler) InstallProddiffContentObserver(app *httpx.App) {
	app.AfterRequest(func(c *httpx.Ctx, response *httpx.Response) {
		if c.Request.Method != "GET" {
			return
		}
		var read func(*httpx.Ctx) (*httpx.Response, error)
		switch c.Endpoint {
		case "agents.handle_agents":
			read = h.registry.createAgent
		case "agents.handle_agent_item":
			read = h.registry.handleAgentItem
		case "agents.agent_push_diff":
			read = h.content.agentPushDiff
		default:
			return
		}
		original := c.Context()
		bounded, cancel := context.WithTimeout(original, contentObserverTimeout)
		c.SetContext(bounded)
		defer c.SetContext(original)
		defer cancel()
		settled, err := observeContent(c.Context(), response, contentObserverTimeout, func() (*httpx.Response, error) { return read(c) })
		if err != nil {
			*response = *contentObservationFailure()
			return
		}
		*response = *settled
	})
}

func contentObservationFailure() *httpx.Response {
	return httpx.JSONStatus(503, map[string]string{"error": errContentObservation.Error()}).NoStore().WithHeader(contentObserverHeader, "content-sync-timeout")
}

func contentChecking(response *httpx.Response) bool {
	if response.Status != 200 || !response.IsJSON() {
		return false
	}
	type state struct {
		Status string `json:"status"`
	}
	type item struct {
		Status      string `json:"status"`
		ContentSync state  `json:"content_sync"`
	}
	body := response.Body()
	if len(body) > 0 && body[0] == '[' {
		var list []item
		if jsondecode.Unmarshal(body, &list) == nil {
			for _, row := range list {
				if row.ContentSync.Status == "checking" {
					return true
				}
			}
		}
		return false
	}
	var row item
	return jsondecode.Unmarshal(body, &row) == nil && (row.Status == "checking" || row.ContentSync.Status == "checking")
}

func observeContent(ctx context.Context, response *httpx.Response, limit time.Duration, read func() (*httpx.Response, error)) (*httpx.Response, error) {
	clock := lifecycle.ClockFrom(ctx)
	deadline := clock.Now().Add(limit)
	for contentChecking(response) {
		remaining := deadline.Sub(clock.Now())
		if remaining <= 0 {
			return nil, errContentObservation
		}
		timer := clock.NewTimer(min(remaining, 100*time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errContentObservation
		case <-timer.C():
		}
		var err error
		response, err = read()
		if err != nil || response == nil || ctx.Err() != nil || !clock.Now().Before(deadline) {
			return nil, errContentObservation
		}
	}
	return response, nil
}
