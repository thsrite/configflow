package rulesync

import (
	"bytes"
	agentcore "configflow/internal/agents"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type syncFake struct {
	mu              sync.Mutex
	bundle          *deploy.Bundle
	snapshotFailure bool
	refreshes       int
	posts           int
	nativeStatus    string
	onNativeGet     func()
	requests        []agents.Request
	providerPaths   []string
	gate            chan struct{}
	entered         chan struct{}
}

func (f *syncFake) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
	return (agents.HTTPClient{}).Do(ctx, r)
}

type syncFixture struct {
	ctx      context.Context
	s        *Service
	fake     *syncFake
	agent    *agentfake.Agent
	now      time.Time
	source   *deploy.Source
	baseline *deploy.Bundle
}

func newWorkerFixture(t *testing.T, service string) *syncFixture {
	t.Helper()
	mihomoenv.Wire()
	ctx, s := fixtureService(t)
	fake := &syncFake{nativeStatus: "succeeded"}
	device := agentfake.New(agentfake.Options{AgentID: "device", Token: "synthetic-rule-token", ServiceType: service})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rule-sync/snapshot" {
			fake.mu.Lock()
			b, fail := fake.bundle, fake.snapshotFailure
			fake.mu.Unlock()
			if r.Method != "GET" || r.ContentLength > 0 || r.Header.Get("Authorization") != "Bearer synthetic-rule-token" || r.Header.Get("Accept") != "application/gzip" {
				t.Error("snapshot raw request contract")
			}
			if fail {
				http.Error(w, "unavailable", 503)
				return
			}
			raw, e := b.Archive()
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(raw)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/mihomo/providers/rules/") && r.Method == "PUT" {
			fake.mu.Lock()
			rawBody, _ := io.ReadAll(r.Body)
			if string(rawBody) != "{}" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Authorization") != "Bearer synthetic-rule-token" {
				t.Error("provider raw body/header contract")
			}
			fake.refreshes++
			fake.providerPaths = append(fake.providerPaths, r.URL.Path)
			gate, entered := fake.gate, fake.entered
			fake.mu.Unlock()
			if entered != nil {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
			if gate != nil {
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path == "/api/oxidns/rules/update" {
			fake.mu.Lock()
			if r.Method == "POST" {
				fake.posts++
				body, _ := io.ReadAll(r.Body)
				if !json.Valid(body) {
					t.Error("native body invalid")
				}
			}
			status := fake.nativeStatus
			onGet := fake.onNativeGet
			if r.Method == "GET" {
				fake.onNativeGet = nil
			} else {
				onGet = nil
			}
			taskID := "native-task"
			phase := "downloading"
			if r.Method == "GET" && fake.posts == 0 {
				status = "idle"
				taskID = ""
				phase = "idle"
			}
			if status == "succeeded" || status == "failed" {
				phase = "done"
			}
			fake.mu.Unlock()
			if onGet != nil {
				onGet()
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "task": map[string]any{"id": taskID, "status": status, "phase": phase, "message": "", "updated_at": 1, "downloaded": 0, "download_total": 0, "reloaded": 0, "reload_total": 0}})
			return
		}
		device.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		fake.mu.Lock()
		if fake.gate != nil {
			select {
			case <-fake.gate:
			default:
				close(fake.gate)
			}
		}
		fake.mu.Unlock()
		srv.Close()
	})
	u := strings.TrimPrefix(srv.URL, "http://")
	host, port, _ := net.SplitHostPort(u)
	n, _ := strconv.Atoi(port)
	e := s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.Agents[0].Host = host
		sys.Agents[0].Port = model.Int(n)
		sys.Agents[0].ServiceType = service
		sys.Agents[0].Version = "1.8.60-go"
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	e = s.m.Repository().Update(ctx, func(_ context.Context, d *model.Document) error {
		var item model.RuleLibraryItem
		if err := json.Unmarshal([]byte(`{"id":"lib-a","name":"Selected","source_type":"content","behavior":"domain","format":"text","content":"old.example.com"}`), &item); err != nil {
			return err
		}
		d.Shared.Nodes = []model.Node{}
		d.Shared.Subscriptions = []model.Subscription{}
		d.Shared.RuleLibrary = append(d.Shared.RuleLibrary, item)
		for _, p := range d.Profiles {
			if p == nil || p.ID != "default" {
				continue
			}
			p.ProxyGroups = []model.ProxyGroup{}
			if err := p.SetJSON("mosdns", json.RawMessage(`{}`)); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(`[{"id":"set-a","itemType":"ruleset","library_rule_id":"lib-a","policy":"DIRECT","enabled":true},{"id":"single","itemType":"rule","rule_type":"DOMAIN","value":"old-inline.example.com","policy":"DIRECT","enabled":true}]`), &p.RuleConfigs); err != nil {
				return err
			}
			if err := p.SetJSON("oxidns", json.RawMessage(`{"direct_rulesets":["set-a"],"direct_rules":["single"]}`)); err != nil {
				return err
			}
			if err := p.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mode: rule\n"}`)); err != nil {
				return err
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	source, e := publishSource(ctx, s.m, "default")
	if e != nil {
		t.Fatal(e)
	}
	record, e := s.m.ReadAgent(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	target, e := deploy.NewTypedTarget(record)
	if e != nil {
		t.Fatal(e)
	}
	main := `mode: rule
dns: {nameserver: [192.0.2.1]}
rule-providers:
 selected: {type: http, behavior: domain, format: text, url: 'https://panel.example.com/api/profiles/default/rule-library/content/lib-a', path: './ruleset/a.list'}
rules: ['RULE-SET,selected,DIRECT', 'DOMAIN,old-inline.example.com,DIRECT', 'MATCH,DIRECT']
`
	if service == "oxidns" {
		main = "plugins: []\n"
	}
	baseline, e := deploy.Build(ctx, deploy.BuildInput{Target: target, Source: source, ConfigContent: main, DeploymentID: "applied", BaseURL: "https://panel.example.com"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = deploy.PublishDirect(ctx, s.m, "device", baseline, deploy.PublishOptions{Activate: true, SourceSnapshot: source, ResourceConfig: source, BaseURL: "https://panel.example.com"})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		r, e := s.m.GetDeploymentModel(ctx, "device", "applied")
		if e != nil {
			t.Fatal(e)
		}
		if r.Status == "succeeded" {
			break
		}
	}
	record, e = s.m.ReadAgent(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	if record.AppliedContent == nil {
		t.Fatal("baseline was not applied")
	}
	fake.bundle = baseline
	s.m.SetClient(fake)
	f := &syncFixture{ctx: ctx, s: s, fake: fake, agent: device, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), source: source, baseline: baseline}
	s.now = func() time.Time { return f.now }
	_, e = s.m.ObserveHeartbeat(ctx, "device", agentModelForTest(t, map[string]any{"version": "1.8.60-go", "config_version": record.ConfigVersion}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Configure(ctx, "device", []byte(`{"library":true,"rules":false}`)); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *syncFixture) edit(t *testing.T, content string) {
	t.Helper()
	e := f.s.m.Repository().UpdateShared(f.ctx, func(_ context.Context, s *model.Shared) error {
		for i := range s.RuleLibrary {
			if s.RuleLibrary[i].ID == "lib-a" {
				c := content
				s.RuleLibrary[i].Content = &c
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func (f *syncFixture) state(t *testing.T) *model.RuleSyncState {
	t.Helper()
	a, e := f.s.m.ReadAgent(f.ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	return stateOf(a)
}
func TestWorkerDebounceVerifiedRefreshAndIdle(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	f.edit(t, "new.example.com")
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.state(t).Status != "pending" || f.fake.refreshes != 0 {
		t.Fatal("missing debounce")
	}
	f.now = f.now.Add(PollInterval - time.Millisecond)
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.fake.refreshes != 0 {
		t.Fatal("refreshed early")
	}
	f.now = f.now.Add(time.Millisecond)
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.state(t).Status != "succeeded" || f.fake.refreshes != 1 {
		t.Fatalf("state %+v refresh %d", f.state(t), f.fake.refreshes)
	}
	if f.fake.providerPaths[0] != "/api/mihomo/providers/rules/selected" {
		t.Fatal(f.fake.providerPaths)
	}
	record, e := f.s.m.ReadAgent(f.ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.s.receipts.LoadAgent(f.ctx, record, true)
	if e != nil || receipt == nil {
		t.Fatal(receipt, e)
	}
	if !bytes.Contains(receipt.Source, []byte("new.example.com")) {
		t.Fatal("verified source did not advance")
	}
	uploads := 0
	for _, call := range f.agent.Calls() {
		if call.Method == "POST" && strings.HasSuffix(call.Path, "/deployments") {
			uploads++
		}
	}
	if uploads > 1 {
		t.Fatal("content update published whole config")
	}
	for i := 0; i < 2; i++ {
		if e = f.s.Run(f.ctx, "device"); e != nil {
			t.Fatal(e)
		}
	}
	before := systemBytes(t, f.s)
	for i := 0; i < 3; i++ {
		if e = f.s.Run(f.ctx, "device"); e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(before, systemBytes(t, f.s)) || f.fake.refreshes != 1 {
		t.Fatal("unchanged device wrote or refreshed")
	}
	for _, r := range f.fake.requests {
		if strings.HasSuffix(r.URL, "/api/rule-sync/snapshot") {
			if r.Timeout != agents.Pair(3, 90) || !r.NoProxy || !r.NoRedirects || r.MaxBody != deploy.MaxTotalBytes+8<<20 || len(r.Body) != 0 {
				t.Fatal("snapshot production options changed")
			}
		}
	}
}
func TestWorkerRejectsUnverifiedSnapshotAndBacksOff(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	f.edit(t, "new.example.com")
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(PollInterval)
	f.fake.snapshotFailure = true
	if e := f.s.Run(f.ctx, "device"); e == nil {
		t.Fatal("unverified snapshot accepted")
	}
	state := f.state(t)
	if state.Status != "failed" || state.RetryAt == nil {
		t.Fatalf("%+v", state)
	}
	r, e := f.s.m.ReadAgent(f.ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.s.receipts.LoadAgent(f.ctx, r, false)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(receipt.Source, []byte("new.example.com")) {
		t.Fatal("failed verification acknowledged source")
	}
	calls := f.fake.refreshes
	f.now = f.now.Add(RetryAfter - time.Second)
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.fake.refreshes != calls {
		t.Fatal("retry before backoff")
	}
}

func TestNativeAcceptedTaskSurvivesRestartWithoutRepost(t *testing.T) {
	f := newWorkerFixture(t, "oxidns")
	f.fake.nativeStatus = "running"
	f.edit(t, "new.example.com")
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(PollInterval)
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.state(t).Status != "running" || f.fake.posts != 1 || value(f.state(t).NativeTask) != "native-task" {
		t.Fatalf("native state %+v posts %d", f.state(t), f.fake.posts)
	}
	// A fresh owner has no in-memory comparisons, but resumes the persisted task.
	resumed := New(f.s.m, nil, Options{Now: func() time.Time { return f.now }})
	if e := resumed.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.fake.posts != 1 {
		t.Fatal("reposted accepted native task")
	}
	f.fake.nativeStatus = "succeeded"
	if e := resumed.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if f.state(t).Status != "succeeded" || f.fake.posts != 1 {
		t.Fatalf("resumed state %+v posts %d", f.state(t), f.fake.posts)
	}
	for _, request := range f.fake.requests {
		if strings.HasSuffix(request.URL, "/api/oxidns/rules/update") {
			if request.Timeout != agents.Pair(3, 15) || !request.NoProxy || !request.NoRedirects {
				t.Fatal("native production options changed")
			}
			if request.Method == "GET" && len(request.Body) != 0 {
				t.Fatal("native GET body")
			}
			if request.Method == "POST" {
				var body struct {
					Paths []string `json:"paths"`
				}
				if e := json.Unmarshal(request.Body, &body); e != nil || len(body.Paths) != 1 || !strings.HasPrefix(body.Paths[0], "rules/rule_") {
					t.Fatalf("selective raw native body %s", request.Body)
				}
			}
		}
	}

}
func TestStructurePublicationPinsNonRuleConfiguration(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	if _, e := f.s.Configure(f.ctx, "device", []byte(`{"library":false,"rules":true}`)); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.m.Repository().UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		for i := range p.RuleConfigs {
			if p.RuleConfigs[i].ID == "single" {
				p.RuleConfigs[i].Value = "new-inline.example.com"
			}
		}
		p.Mihomo.CustomConfig = "mode: global\ndns: {nameserver: [192.0.2.99]}\n"
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	record, e := f.s.m.ReadAgent(f.ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	current, e := publishSource(f.ctx, f.s.m, "default")
	if e != nil {
		t.Fatal(e)
	}
	projected, e := ProjectRuleChanges(f.source, current, Settings{Rules: true}, "mihomo", nil)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.s.receipts.LoadAgent(f.ctx, record, true)
	if e != nil || receipt == nil {
		t.Fatal(receipt, e)
	}
	live, e := FetchSnapshot(f.ctx, f.s.m, record, Baseline{receipt, receipt.Bundle})
	if e != nil {
		t.Fatal(e)
	}
	target, e := deploy.NewTypedTarget(record)
	if e != nil {
		t.Fatal(e)
	}
	beforePreview := systemBytes(t, f.s)
	preview, e := PrepareCandidate(f.ctx, target, projected, Baseline{receipt, live}, Settings{Rules: true}, "preview", false)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(beforePreview, systemBytes(t, f.s)) {
		t.Fatal("preview wrote database")
	}
	previewFiles, e := preview.Contents()
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(PollInterval)
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, call := range f.agent.Calls() {
		if call.Manifest == nil || call.Manifest.DeploymentID == "applied" {
			continue
		}
		found = true
		if !call.Manifest.ReloadOnly {
			t.Fatal("not reload only")
		}
		b, e := deploy.Open(deploy.Manifest{ProtocolVersion: call.Manifest.ProtocolVersion, AgentID: call.Manifest.AgentID, DeploymentID: call.Manifest.DeploymentID, ProfileID: "default", ServiceType: "mihomo", ConfigPath: call.Manifest.ConfigPath, Files: func() []deploy.File {
			out := []deploy.File{}
			for _, entry := range call.Manifest.Files {
				out = append(out, deploy.File{Path: entry.Path, Role: deploy.Role(entry.Role), Size: entry.Size, SHA256: entry.SHA256})
			}
			return out
		}(), ReloadOnly: true}, call.Body, call.BodySHA256, "", nil)
		if e != nil {
			t.Fatal(e)
		}
		files, e := b.Contents()
		if e != nil {
			t.Fatal(e)
		}
		if len(files) != len(previewFiles) {
			t.Fatal("preview/execution file count differs")
		}
		for path, want := range previewFiles {
			got, ok := files[path]
			if !ok || got.Role != want.Role || !bytes.Equal(got.Data, want.Data) {
				t.Fatalf("preview/execution differs for %s", path)
			}
		}

		main := string(files[call.Manifest.ConfigPath].Data)
		if !strings.Contains(main, "new-inline.example.com") || strings.Contains(main, "192.0.2.99") || strings.Contains(main, "mode: global") {
			t.Fatal("rule boundary crossed", main)
		}
	}
	if !found {
		t.Fatalf("no rule publication, state %+v", f.state(t))
	}
}

func TestRuleSyncRuntimeDuringDeviceIO(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			f := newWorkerFixture(t, "mihomo")
			f.edit(t, "new.example.com")
			if e := f.s.Run(f.ctx, "device"); e != nil {
				t.Fatal(e)
			}
			f.now = f.now.Add(PollInterval)
			lifecycle.Stop(4 * time.Second)
			if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
				t.Fatal(e)
			}
			lifecycle.Start(f.ctx)
			t.Cleanup(func() {
				lifecycle.Stop(4 * time.Second)
				_ = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			})
			f.fake.gate = make(chan struct{})
			f.fake.entered = make(chan struct{}, 1)
			request, cancel := context.WithCancel(lifecycle.BindRequest(f.ctx))
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- f.s.Run(request, "device") }()
			select {
			case <-f.fake.entered:
			case <-time.After(30 * time.Second):
				t.Fatal("provider request did not start")
			}
			before := systemBytes(t, f.s)
			if shutdown {
				lifecycle.Stop(4 * time.Second)
				if e := <-done; e == nil {
					t.Fatal("shutdown device I/O succeeded")
				}
				if !bytes.Equal(before, systemBytes(t, f.s)) {
					t.Fatal("shutdown wrote state or acknowledged receipt")
				}
				r, e := f.s.m.ReadAgent(f.ctx, "device", false)
				if e != nil {
					t.Fatal(e)
				}
				baseline, e := f.s.receipts.LoadAgent(f.ctx, r, false)
				if e != nil {
					t.Fatal(e)
				}
				if bytes.Contains(baseline.Source, []byte("new.example.com")) {
					t.Fatal("shutdown acknowledged source")
				}
			} else {
				cancel()
				close(f.fake.gate)
				if e := <-done; e != nil {
					t.Fatal(e)
				}
				if f.state(t).Status != "succeeded" {
					t.Fatal("disconnect abandoned accepted work")
				}
			}
		})
	}
}
func TestEditDuringRefreshIsNotAcknowledged(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	f.edit(t, "new.example.com")
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(PollInterval)
	f.fake.gate = make(chan struct{})
	f.fake.entered = make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- f.s.Run(f.ctx, "device") }()
	select {
	case <-f.fake.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("no provider refresh")
	}
	f.edit(t, "concurrent.example.com")
	close(f.fake.gate)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	state := f.state(t)
	if state.Status != "pending" || state.Message != msgChangedDuring {
		t.Fatalf("%+v", state)
	}
	r, e := f.s.m.ReadAgent(f.ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	baseline, e := f.s.receipts.LoadAgent(f.ctx, r, false)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(baseline.Source, []byte("new.example.com")) || bytes.Contains(baseline.Source, []byte("concurrent.example.com")) {
		t.Fatal("unverified source advanced")
	}
}

func TestWorkerUsesDerivedOfflineStatus(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	f.edit(t, "new.example.com")
	entry := f.s.m.Runtime().Entries()["device"]
	entry.LastHeartbeat = time.Now().Add(-5 * time.Minute).Format("2006-01-02T15:04:05.000000")
	f.s.m.Runtime().SetEntry("device", entry)
	stored, e := f.s.m.ReadAgent(f.ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	if stored.Status != "online" {
		t.Fatal("negative control needs stored online status")
	}
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	state := f.state(t)
	if state.Status != "pending" || state.Message != msgOffline {
		t.Fatalf("stale heartbeat: %+v", state)
	}
	if len(f.fake.requests) != 0 {
		t.Fatal("offline worker made device requests")
	}
}

func TestBindingChangeDuringNativeStatusPreventsDownload(t *testing.T) {
	f := newWorkerFixture(t, "oxidns")
	r := f.s.m.Repository()
	if _, e := r.CreateProfileRecord(f.ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); e != nil {
		t.Fatal(e)
	}
	f.edit(t, "new.example.com")
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(PollInterval)
	var called atomic.Bool
	f.fake.mu.Lock()
	f.fake.onNativeGet = func() {
		called.Store(true)
		if _, e := f.s.m.EditAgent(f.ctx, "device", agentEditsForTest(t, map[string]any{"profile_id": "other"})); e != nil {
			t.Error(e)
		}
	}
	f.fake.mu.Unlock()
	if e := f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if !called.Load() {
		t.Fatal("negative control: status request hook did not run")
	}
	if f.fake.posts != 0 {
		t.Fatal("posted a download after profile changed")
	}
}

func agentModelForTest(t testing.TB, value any) *model.Agent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func agentEditsForTest(t testing.TB, value any) agentcore.AgentEdits {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	edits, err := agentcore.DecodeAgentEdits(raw)
	if err != nil {
		t.Fatal(err)
	}
	return edits
}
