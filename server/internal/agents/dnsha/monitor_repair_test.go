package dnsha

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/model"
)

type repairedRuleClient struct{ calls int }

func (c *repairedRuleClient) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	c.calls++
	return &agents.Response{StatusCode: 202, Body: []byte(`{"success":true,"task":{"id":"task-1","status":"running","phase":"downloading","downloaded":0,"download_total":1,"reloaded":0,"reload_total":1}}`)}, nil
}

func TestMonitorDamagedPublicationCanBeDisabledAndRulesResume(t *testing.T) {
	for _, raw := range []string{`""`, `0`, `"damaged"`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			f := fixtureNew(t, Options{}, false, nil)
			err := f.m.EditAgents(f.ctx, "", func(_ context.Context, rows *[]model.Agent) error {
				for i := range *rows {
					a := &(*rows)[i]
					if a.ID != "primary" {
						continue
					}
					if err := a.SetJSON("oxidns_ha_state", json.RawMessage(`{"status":"failed","members":["primary"]}`)); err != nil {
						return err
					}
					return a.SetJSON("oxidns_ha_publication", json.RawMessage(raw))
				}
				t.Fatal("missing primary")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			before := len(f.client.calls())
			_, _, err = monitor.CallRuleUpdate(f.ctx, monitor.TypedFromAgents(f.m), "primary", "POST", nil)
			if err == nil || !strings.Contains(err.Error(), "OxiDNS HA 发布记录损坏") || len(f.client.calls()) != before {
				t.Fatalf("damaged guard admitted update: %v", err)
			}
			if _, err = f.s.Disable(f.ctx, "primary", true); err != nil {
				t.Fatal(err)
			}
			a, err := f.m.ReadAgent(f.ctx, "primary", false)
			if err != nil {
				t.Fatal(err)
			}
			if _, present, err := a.MemberJSON("oxidns_ha_publication"); err != nil || present {
				t.Fatalf("disable retained damaged journal: %v", err)
			}
			client := &repairedRuleClient{}
			f.m.SetClient(client)
			result, status, err := monitor.CallRuleUpdate(f.ctx, monitor.TypedFromAgents(f.m), "primary", "POST", nil)
			if err != nil || status != 202 || result.Task.Status != "running" || client.calls != 1 {
				t.Fatalf("repaired update: %+v status=%d calls=%d err=%v", result, status, client.calls, err)
			}
		})
	}
}
