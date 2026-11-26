package dnsha

import (
	"context"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
)

// Review A18 F1: while a first enable is in flight the journal is active on
// both records but neither record is enabled yet. Python's publish() finds no
// pair and falls through to _publish_deployment, whose begin_deployment answers
// the HA conflict (409, Chinese text, ha_publication). The A14 fail-closed
// guard for a missing pair publisher must not fire once A18 is wired.
func TestPublishDuringFirstEnableAnswersPythonConflict(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	out, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example")
	if e != nil {
		t.Fatal(e)
	}
	if out.Publication == nil || terminal(out.Publication.Status) {
		t.Fatal("enable journal not active", out.Publication)
	}
	for _, id := range []string{"primary", "secondary"} {
		t.Run(id, func(t *testing.T) {
			a, e := f.s.get(f.ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			src, rev, e := f.s.source(f.ctx, a, true)
			if e != nil {
				t.Fatal(e)
			}
			b := f.bundle(t, id, "during-enable-"+id, false)
			dp := deploy.New(deploy.Options{Pair: func(ctx context.Context, _ *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*model.Deployment, bool, error) {
				return f.s.PublishPair(ctx, id, b, o)
			}})
			r, e := dp.Publish(f.ctx, f.m, id, b, deploy.PublishOptions{Activate: true, SourceSnapshot: src, ResourceConfig: src, ConfigRevision: rev})
			if e != nil {
				t.Fatalf("push during enable must be Python's 409 conflict, got error %v", e)
			}
			if (r.Success != nil && *r.Success) || (r.HTTPStatus == nil || *r.HTTPStatus != 409) || *r.Message != "DNS 主备正在同步，请等待两台发布完成" || r.HAPublication.ID != out.Publication.ID {
				t.Fatalf("conflict answer: %v", r)
			}
		})
	}
	if len(f.uploads()) != 0 {
		t.Fatal("refused publication reached a device")
	}
}
