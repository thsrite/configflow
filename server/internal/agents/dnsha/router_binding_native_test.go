package dnsha

import (
	"context"
	"testing"

	"configflow/internal/model"
)

func TestRouterBindingsUseOriginalAgentIDStrings(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	for _, tt := range []struct {
		raw, alias string
		want       int
	}{{`"7"`, ``, 1}, {`"other"`, `"7"`, 0}, {`null`, `"7"`, 0}} {
		if err := f.r.Update(f.ctx, func(_ context.Context, document *model.Document) error {
			router := model.Router{ID: "router", Name: "Synthetic router"}
			if err := router.SetJSON("dns_agent_id", []byte(tt.raw)); err != nil {
				return err
			}
			_ = tt.alias // a differently cased member cannot be stored on a plain record
			document.Profiles[0].Routers.Items = []model.Router{router}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		before, err := f.r.Store().Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		result, err := f.s.routersUsing(f.ctx, []string{"7"})
		if err != nil || len(result) != tt.want {
			t.Fatalf("raw=%s alias=%s bindings=%v err=%v", tt.raw, tt.alias, result, err)
		}
		if after, err := f.r.Store().Fingerprint(); err != nil || after != before {
			t.Fatal("router binding read committed", err)
		}
	}
}
