package repo

import (
	"testing"

	"configflow/internal/testjson"
)

func TestPublishConfigMatchesConfig(t *testing.T) {
	r := must(OpenDefault(t.TempDir()))(t)
	bare := must(r.Config(bg, DefaultProfileID, nil))(t)
	if bare.HasAgents || len(bare.Agents) != 0 {
		t.Fatal("Config included agents by default")
	}
	for _, agents := range []bool{true, false} {
		config, revision, err := r.PublishConfig(bg, DefaultProfileID, agents)
		noError(t, err)
		wantRevision := must(r.ConfigRevision(bg, DefaultProfileID))(t)
		if revision != wantRevision {
			t.Fatalf("agents=%v revision=%d want=%d", agents, revision, wantRevision)
		}
		if config.HasAgents != agents {
			t.Fatal("HasAgents")
		}
		gotObj := parseJSONObj(t, mustMarshalJSON(t, config))
		wantObj := parseJSONObj(t, mustMarshalJSON(t, bare))
		if agents {
			if _, has := gotObj.Get("agents"); !has {
				t.Fatal("PublishConfig(agents=true) omitted agents")
			}
			gotObj.Delete("agents")
		} else if _, has := gotObj.Get("agents"); has {
			t.Fatal("PublishConfig(agents=false) included agents")
		}
		if !testjson.Equal(mustMarshalJSON(t, wantObj), mustMarshalJSON(t, gotObj)) {
			t.Fatalf("agents=%v differs:\n%s", agents, testjson.Diff(mustMarshalJSON(t, wantObj), mustMarshalJSON(t, gotObj)))
		}
	}
	_, _, err := r.PublishConfig(bg, "missing", true)
	wantError(t, err, ErrNotFound, "missing")
}
