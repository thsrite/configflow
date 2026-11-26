package dnsha

import (
	"context"

	"configflow/internal/agents"
)

type repairedRuleClient struct{ calls int }

func (c *repairedRuleClient) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	c.calls++
	return &agents.Response{StatusCode: 202, Body: []byte(`{"success":true,"task":{"id":"task-1","status":"running","phase":"downloading","downloaded":0,"download_total":1,"reloaded":0,"reload_total":1}}`)}, nil
}
