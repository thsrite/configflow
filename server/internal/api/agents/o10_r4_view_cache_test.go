package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/store"
)

var viewCachePaths = []string{"/api/agents", "/api/agents?history=0", "/api/agents?summary=1", "/api/agents/content-agent"}

// withoutContentSync drops content_sync (computed per request outside the
// cache, and timing dependent) so cached and direct bodies compare bytewise.
func withoutContentSync(t *testing.T, body []byte) []byte {
	t.Helper()
	strip := func(raw json.RawMessage) json.RawMessage {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		delete(object, "content_sync")
		out, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if bytes.HasPrefix(body, []byte("[")) {
		var list []json.RawMessage
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatal(err)
		}
		for i := range list {
			list[i] = strip(list[i])
		}
		out, _ := json.Marshal(list)
		return out
	}
	return strip(body)
}

// expectCacheMatchesDirect reads every view twice with the cache on (fill,
// hit) and once with it off, and requires identical bodies.
func expectCacheMatchesDirect(t *testing.T, f *contentHTTP, step string) map[string][]byte {
	t.Helper()
	bodies := map[string][]byte{}
	for _, path := range viewCachePaths {
		restore := setAgentViewCache(true)
		fill := withoutContentSync(t, f.get(path).WantStatus(200).Body)
		hit := withoutContentSync(t, f.get(path).WantStatus(200).Body)
		restore()
		off := setAgentViewCache(false)
		direct := withoutContentSync(t, f.get(path).WantStatus(200).Body)
		off()
		if !bytes.Equal(fill, direct) || !bytes.Equal(hit, direct) {
			t.Fatalf("%s %s: cached view differs from direct\nfill   %s\nhit    %s\ndirect %s", step, path, fill, hit, direct)
		}
		bodies[path] = direct
	}
	return bodies
}

func agentObject(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(body, &list); err != nil || len(list) != 1 {
		t.Fatalf("%v %s", err, body)
	}
	return list[0]
}

// TestAgentViewCacheFollowsEveryInput covers each input of the derived Agent
// view: heartbeat overlay (liveness, metrics), Agent writes, configuration
// writes, the registration key and the cache switch. Every step compares the
// cached views with the direct computation.
func TestAgentViewCacheFollowsEveryInput(t *testing.T) {
	f := newContentHTTP(t, true, false)
	restore := setAgentViewCache(true)
	defer restore()
	f.settle(t)
	initial := expectCacheMatchesDirect(t, f, "initial")
	if got := string(agentObject(t, initial["/api/agents"])["status"]); got != `"offline"` {
		t.Fatalf("initial status %s", got)
	}

	heartbeat := func(usage float64) {
		t.Helper()
		f.s.Post("/api/agents/content-agent/heartbeat", httpxtest.Bearer("stored-agent-token"), httpxtest.JSON(map[string]any{
			"service_status": "running", "system_metrics": map[string]any{"cpu": map[string]any{"usage_percent": usage}},
		})).WantStatus(200)
	}
	heartbeat(3.5)
	// A heartbeat changes no store head: the very next read must show it.
	afterBeat := expectCacheMatchesDirect(t, f, "heartbeat")
	for _, path := range []string{"/api/agents", "/api/agents?summary=1"} {
		object := agentObject(t, afterBeat[path])
		if string(object["status"]) != `"online"` || !strings.Contains(string(object["cpu"]), "3.5") {
			t.Fatalf("%s did not follow the heartbeat: %s", path, afterBeat[path])
		}
	}
	heartbeat(7.25)
	afterMetrics := expectCacheMatchesDirect(t, f, "metrics")
	if !strings.Contains(string(agentObject(t, afterMetrics["/api/agents"])["cpu"]), "7.25") {
		t.Fatalf("metrics overlay not reflected: %s", afterMetrics["/api/agents"])
	}

	// Agent write.
	if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
		return model.SetStoredMember(&system.Agents[0], "name", json.RawMessage(`"Renamed"`))
	}); err != nil {
		t.Fatal(err)
	}
	renamed := expectCacheMatchesDirect(t, f, "agent write")
	if string(agentObject(t, renamed["/api/agents?summary=1"])["name"]) != `"Renamed"` {
		t.Fatalf("Agent write not reflected: %s", renamed["/api/agents?summary=1"])
	}

	// Configuration write: the content source belongs to the new head.
	if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
		return system.SystemConfig.SetJSON("server_domain", json.RawMessage(`"panel.example.test"`))
	}); err != nil {
		t.Fatal(err)
	}
	expectCacheMatchesDirect(t, f, "config write")
	f.get("/api/agents").WantStatus(200)
	head, err := f.s.Repo.Head(f.s.Context())
	if err != nil {
		t.Fatal(err)
	}
	views.mu.Lock()
	cachedHead, sources := views.head, len(views.sources)
	views.mu.Unlock()
	if cachedHead != head || sources == 0 {
		t.Fatalf("view cache generation %v (%d sources), store head %v", cachedHead, sources, head)
	}

	// The registration key is collected as a credential by every projection.
	t.Setenv("AGENT_REGISTRATION_KEY", "Renamed")
	redacted := expectCacheMatchesDirect(t, f, "registration key")
	if strings.Contains(string(redacted["/api/agents?summary=1"]), "Renamed") {
		t.Fatalf("registration key not applied: %s", redacted["/api/agents?summary=1"])
	}
}

// TestAgentViewCacheKeysLiveMembers checks the cache key directly: liveness
// decided at read time (offline after the heartbeat ages), runtime fields,
// generation, position and the switch each force a new projection.
func TestAgentViewCacheKeysLiveMembers(t *testing.T) {
	restore := setAgentViewCache(true)
	defer restore()
	f := newContentHTTP(t, false, false)
	repository := f.s.Repo
	gen := core.Generation{Head: store.Head{StoreID: "s", Generation: 1}, Shared: true}
	record := model.Agent{ID: "a", Status: "online", LastHeartbeat: "2026-01-01T00:00:00"}
	calls := 0
	compute := func() (map[string]json.RawMessage, error) {
		calls++
		return map[string]json.RawMessage{"n": json.RawMessage(`1`)}, nil
	}
	read := func(gen core.Generation, key viewKey, record *model.Agent) {
		t.Helper()
		public, err := views.public(repository, gen, key, record, compute)
		if err != nil || string(public["n"]) != "1" {
			t.Fatal(public, err)
		}
		public["n"] = json.RawMessage(`2`) // callers own the returned map
	}
	key := viewKey{0, "a", viewFull}
	read(gen, key, &record)
	read(gen, key, &record)
	if calls != 1 {
		t.Fatalf("hit recomputed: %d", calls)
	}
	offline := record
	if err := model.SetStoredMember(&offline, "status", json.RawMessage(`"offline"`)); err != nil {
		t.Fatal(err)
	}
	metrics := record
	if err := model.SetStoredMember(&metrics, "cpu", json.RawMessage(`{"usage_percent":1}`)); err != nil {
		t.Fatal(err)
	}
	beat := record
	beat.LastHeartbeat = "2026-01-01T00:00:30"
	for _, step := range []struct {
		name   string
		gen    core.Generation
		key    viewKey
		record *model.Agent
	}{
		{"offline at read time", gen, key, &offline},
		{"runtime field", gen, key, &metrics},
		{"heartbeat time", gen, key, &beat},
		{"new head", core.Generation{Head: store.Head{StoreID: "s", Generation: 2}, Shared: true}, key, &record},
		{"other position", core.Generation{Head: store.Head{StoreID: "s", Generation: 2}, Shared: true}, viewKey{1, "a", viewFull}, &record},
		{"other view", core.Generation{Head: store.Head{StoreID: "s", Generation: 2}, Shared: true}, viewKey{1, "a", viewSummary}, &record},
		{"write transaction", core.Generation{}, key, &record},
	} {
		before := calls
		read(step.gen, step.key, step.record)
		if calls != before+1 {
			t.Fatalf("%s served a cached projection", step.name)
		}
	}
	off := setAgentViewCache(false)
	defer off()
	before := calls
	read(gen, key, &record)
	read(gen, key, &record)
	if calls != before+2 {
		t.Fatal("disabled cache served a projection")
	}
}

// TestAgentViewMasksItsOwnGenerationCredentials: the registry and the stored
// credentials used to be read from the store separately, so a commit landing
// between the two reads projected a borrowed record with a later generation's
// credentials. A credential only a private member carries (the token, dropped
// from the projection, so the sanitizer cannot discover it) and that the
// commit rotated was then no longer masked in the record's free text. The
// credentials now come from the read generation itself.
func TestAgentViewMasksItsOwnGenerationCredentials(t *testing.T) {
	f := newContentHTTP(t, false, false)
	ctx := f.s.Context()
	const old = "old-agent-token-7f3a"
	set := func(token, remark string) {
		t.Helper()
		if err := f.s.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
			token, _ := json.Marshal(token)
			remark, _ := json.Marshal(remark)
			if err := model.SetStoredMember(&system.Agents[0], "token", token); err != nil {
				return err
			}
			return model.SetStoredMember(&system.Agents[0], "name", remark)
		}); err != nil {
			t.Fatal(err)
		}
	}
	set(old, "registered with "+old)
	m, err := core.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	records, gen, err := m.ReadAgentsAt(ctx, true, nil)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	// The commit lands after the registry read, before the credentials read.
	set("new-agent-token-91c2", "rotated")
	secrets, err := liveAgentSecrets(ctx, m, &contentSources{gen: gen}, &records[0])
	if err != nil {
		t.Fatal(err)
	}
	public, err := publicAgentView(&records[0], secrets)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := public["token"]; present {
		t.Fatal("token projected")
	}
	if strings.Contains(string(public["name"]), old) {
		t.Fatalf("own generation token leaked: %s", public["name"])
	}
}
