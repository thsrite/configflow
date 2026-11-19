package installer

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

const nativeInstallRequest = `{"server_url":"https://panel.example.com","agent_name":"native-dns","agent_ip":"192.0.2.7","oxidns_port":8082,"data_dir":"/srv/native-dns","network_mode":"host"}`

type nativeRegistry struct {
	records      []model.Agent
	calls        int
	beforeCommit func()
	originValue  any
}

func (r *nativeRegistry) EditAgents(ctx context.Context, profile string, edit func(context.Context, *[]model.Agent) error) error {
	r.calls++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.originValue = ctx.Value(nativeContextKey{})
	raw, err := model.Marshal(r.records)
	if err != nil {
		return err
	}
	var working []model.Agent
	if err := json.Unmarshal(raw, &working); err != nil {
		return err
	}
	if err := edit(ctx, &working); err != nil {
		return err
	}
	if r.beforeCommit != nil {
		r.beforeCommit()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.records = working
	return nil
}

type nativeContextKey struct{}

func TestN810CapabilityCorruptSessionIDsNeverWrite(t *testing.T) {
	tests := []struct {
		name        string
		replacement json.RawMessage
		index       string
	}{
		{"null", json.RawMessage(`null`), ""},
		{"parent", json.RawMessage(`{"id":"../escape"}`), ""},
		{"absolute", json.RawMessage(`{"id":"/srv/escape"}`), ""},
		{"mismatch", json.RawMessage(`{"id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), ""},
		{"numeric", json.RawMessage(`{"id":7}`), ""},
		{"uppercase", json.RawMessage(`{"id":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`), ""},
		{"index parent", nil, "../escape"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			capability, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
			if err != nil {
				t.Fatal(err)
			}
			id := capability.URL[strings.LastIndexByte(capability.URL, '/')+1:]
			root := filepath.Join(dir, "docker-ha-installations")
			if test.replacement != nil {
				if err := os.WriteFile(filepath.Join(root, id+".json"), test.replacement, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if test.index != "" {
				indexes, err := filepath.Glob(filepath.Join(root, "*.index"))
				if err != nil || len(indexes) != 1 {
					t.Fatal("index missing")
				}
				if err := os.WriteFile(indexes[0], []byte(test.index), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := canonical(readTree(dir))
			got, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
			var refusal *EnrollmentError
			if got != nil || !errors.As(err, &refusal) || refusal.Status != 409 || refusal.Message != enrollmentUnreadable {
				t.Fatalf("issue: %v", err)
			}
			if test.replacement != nil {
				registry := &nativeRegistry{}
				identity, err := EnrollIdentity(context.Background(), dir, registry, id, capability.Token, true)
				if identity != (AgentIdentity{}) || !errors.As(err, &refusal) || refusal.Status != 401 || refusal.Message != enrollmentInvalid || registry.calls != 0 {
					t.Fatalf("enroll: %v, calls %d", err, registry.calls)
				}
			}
			if canonical(readTree(dir)) != before {
				t.Fatal("corrupt session refusal wrote files")
			}
		})
	}
}

func TestN810CapabilityOriginalStringsAndReplay(t *testing.T) {
	dir := t.TempDir()
	capability, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
	if err != nil {
		t.Fatal(err)
	}
	id := capability.URL[strings.LastIndexByte(capability.URL, '/')+1:]
	registry := &nativeRegistry{}
	first, err := EnrollIdentity(context.Background(), dir, registry, id, capability.Token, true)
	if err != nil || first.AgentID == "" || first.Token == "" {
		t.Fatalf("first: %v", err)
	}
	second, err := EnrollIdentity(context.Background(), dir, registry, id, capability.Token, true)
	if err != nil || first != second || len(registry.records) != 1 {
		t.Fatalf("replay: %v", err)
	}
	if err := registry.records[0].SetJSON("id", []byte(`7`)); err != nil {
		t.Fatal(err)
	}
	before, err := model.Marshal(registry.records)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EnrollIdentity(context.Background(), dir, registry, id, capability.Token, true)
	var refusal *EnrollmentError
	if result != (AgentIdentity{}) || !errors.As(err, &refusal) || refusal.Status != 409 {
		t.Fatalf("numeric identity authorized: %v", err)
	}
	after, _ := model.Marshal(registry.records)
	if string(after) != string(before) {
		t.Fatal("failed replay changed records")
	}
	if err := registry.records[0].SetJSON("id", []byte(`"7"`)); err != nil {
		t.Fatal(err)
	}
	result, err = EnrollIdentity(context.Background(), dir, registry, id, capability.Token, true)
	if err != nil || result.AgentID != "7" {
		t.Fatalf("original string positive control: %v", err)
	}
}

func TestN810CapabilityDisconnectAndShutdown(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, cancel := context.WithCancel(context.WithValue(context.Background(), nativeContextKey{}, "origin"))
	bound := runtime.BindRequest(request)
	cancel()
	dir := t.TempDir()
	capability, err := IssueCapability(bound, dir, []byte(nativeInstallRequest), "default")
	if err != nil {
		t.Fatalf("disconnect canceled issue: %v", err)
	}
	id := capability.URL[strings.LastIndexByte(capability.URL, '/')+1:]
	registry := &nativeRegistry{beforeCommit: func() { runtime.Shutdown(context.Background()) }}
	identity, err := EnrollIdentity(bound, dir, registry, id, capability.Token, true)
	if identity != (AgentIdentity{}) || !errors.Is(err, context.Canceled) || len(registry.records) != 0 || registry.originValue != "origin" {
		t.Fatalf("shutdown committed identity: %v", err)
	}
	stopped := filepath.Join(t.TempDir(), "never-created")
	if _, err := IssueCapability(bound, stopped, []byte(nativeInstallRequest), "default"); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped issue: %v", err)
	}
	if _, err := os.Stat(stopped); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shutdown created directories")
	}
}

func TestN810CapabilityEntropyFailureNeverCreatesIdentity(t *testing.T) {
	original := randomBytes
	sentinel := errors.New("synthetic entropy failure")
	randomBytes = func(int) ([]byte, error) { return nil, sentinel }
	t.Cleanup(func() { randomBytes = original })
	dir := t.TempDir()
	result, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
	if result != nil || !errors.Is(err, sentinel) {
		t.Fatalf("entropy failure: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "docker-ha-installations"))
	if err != nil || len(entries) != 0 {
		t.Fatal("failed entropy created capability files")
	}
}

func TestN810AgentTimeoutBoundsAndNativeConfig(t *testing.T) {
	tests := []struct {
		name    string
		timeout *big.Int
		reject  bool
	}{
		{"normal", big.NewInt(30), false},
		{"negative", big.NewInt(-5), false},
		{"duration overflow", big.NewInt(math.MaxInt64/int64(time.Second) + 1), true},
		{"integer overflow", new(big.Int).Add(big.NewInt(math.MaxInt64), big.NewInt(1)), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			script, err := RenderAgentInstall(AgentInstallOptions{ServerURL: "https://panel.example.com", AgentName: "native-dns", ServiceType: "oxidns", AgentPort: 8082, DeploymentHealthTimeout: test.timeout})
			if test.reject {
				if err == nil || script != "" {
					t.Fatal("overflow was serialized")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, rest, ok := strings.Cut(script, "<<'CONFIGFLOW_JSON_EOF'\n")
			if !ok {
				t.Fatal("missing config")
			}
			raw, _, ok := strings.Cut(rest, "\nCONFIGFLOW_JSON_EOF\n")
			if !ok {
				t.Fatal("unterminated config")
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["deployment_health_timeout"]) != test.timeout.String() {
				t.Fatal("integer changed")
			}
		})
	}
}

func TestN810CapabilityRejectsCaseAliasesAndDuplicateProof(t *testing.T) {
	data := []byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","ID":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","token":"opaque","profile_id":"default","name":"native-dns","host":"192.0.2.7","port":8082,"expires":2000000000}`)
	session, err := decodeInstallationSession(data)
	if err != nil || session.id != strings.Repeat("a", 32) {
		t.Fatal("case alias changed ownership")
	}
	if _, err := decodeInstallationSession([]byte(`{"ID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)); err == nil {
		t.Fatal("alias proved identity")
	}
	if _, err := decodeInstallationSession([]byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","\u0069d":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)); err == nil {
		t.Fatal("duplicate proved identity")
	}

}

func TestN810InstallCommandStoreKeepsOriginShutdown(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, cancel := context.WithCancel(context.Background())
	bound := runtime.BindRequest(request)
	cancel()
	dir := t.TempDir()
	store := Store{DataDir: dir, Token: func() string { return strings.Repeat("a", 22) }}
	token, _, err := store.SaveInstallScript(bound, "#!/bin/sh\nexit 0\n", nil)
	if err != nil {
		t.Fatalf("disconnect canceled save: %v", err)
	}
	script, found, err := store.LoadInstallScript(bound, token)
	if err != nil || !found || script != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("disconnected load: %v", err)
	}
	before := canonical(readTree(dir))
	runtime.Shutdown(context.Background())
	if _, _, err := store.SaveInstallScript(bound, "exit 1", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped save: %v", err)
	}
	if _, _, err := store.LoadInstallScript(bound, token); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped cleanup/load: %v", err)
	}
	if canonical(readTree(dir)) != before {
		t.Fatal("stopped store wrote files")
	}
	runtime.Start(context.Background())
	if _, _, err := store.SaveInstallScript(bound, "exit 2", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("old request moved to restarted generation")
	}
}

func TestN810InstallCommandStoreShutdownDuringTokenDoesNotOpenDatabase(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{})
	runtime.Start(context.Background())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	bound := runtime.BindRequest(context.Background())
	root := filepath.Join(t.TempDir(), "not-created")
	store := Store{DataDir: root, Token: func() string { runtime.Shutdown(context.Background()); return strings.Repeat("a", 22) }}
	if _, _, err := store.SaveInstallScript(bound, "#!/bin/sh\nexit 0\n", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shutdown opened database or created directories")
	}
}

func TestN810NativeIntegersHaveNoInterpreterDigitLimit(t *testing.T) {
	text, err := GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "https://panel.example.com", AgentName: "native-dns", ServiceType: "oxidns", AgentPort: strings.Repeat("0", 5000) + "8082", DeploymentHealthTimeout: strings.Repeat("0", 5000) + "30"})
	if err != nil || !strings.Contains(text, `"agent_port": 8082`) || !strings.Contains(text, `"deployment_health_timeout": 30`) {
		t.Fatalf("native leading-zero input: %v", err)
	}
}

func TestN810LinuxPathAliasesCannotBypassInstallerGuards(t *testing.T) {
	for _, method := range []string{"compose_compact", "compose_direct"} {
		for _, directory := range []string{"//", "//etc", "//opt", "/etc/", "/./etc", "/opt/../etc"} {
			t.Run(method+directory, func(t *testing.T) {
				var fields model.Fields
				if err := json.Unmarshal([]byte(nativeInstallRequest), &fields); err != nil {
					t.Fatal(err)
				}
				if err := setSessionField(&fields, "install_method", method); err != nil {
					t.Fatal(err)
				}
				if err := setSessionField(&fields, "data_dir", directory); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				result, err := GenerateGuideJSON(raw, nil)
				if result != nil || err == nil {
					t.Fatal("dangerous alias generated an installer")
				}
				if strings.HasPrefix(directory, "//") && err.Error() != composeDataDirMessage {
					t.Fatalf("wrong alias refusal: %v", err)
				}
			})
		}
		t.Run(method+"valid-double-slash", func(t *testing.T) {
			raw := strings.Replace(nativeInstallRequest, `"/srv/native-dns"`, `"//srv/native"`, 1)
			var fields model.Fields
			if err := json.Unmarshal([]byte(raw), &fields); err != nil {
				t.Fatal(err)
			}
			if err := setSessionField(&fields, "install_method", method); err != nil {
				t.Fatal(err)
			}
			request, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			result, err := GenerateGuideJSON(request, nil)
			if err != nil || result == nil || result.Compose == nil || !strings.Contains(*result.Compose, "//srv/native/agent") {
				t.Fatalf("valid raw path changed or refused: %v", err)
			}
			expectedProject := map[string]string{"compose_compact": "cf-oxidns-native-dns-54d32a3a", "compose_direct": "cf-oxidns-8c4cf53f1e947907"}[method]
			if result.ComposeDeployment == nil || result.ComposeDeployment.ProjectName != expectedProject {
				t.Fatal("path comparison changed published identity")
			}

		})
	}
}

func TestN810IdentityAndMigrationPathAliasesCannotOverlap(t *testing.T) {
	tests := []struct {
		name, method, identity string
		extra                  map[string]any
		message                string
	}{
		{"bundle identity", "compose", "//srv/native/agent/host-id", nil, msgBundleIdentityLocation},
		{"direct identity", "compose_direct", "//srv/native/host-id", nil, msgDirectIdentityLocation},
		{"migration bind overlap", "script", "/srv/host-id", map[string]any{"mode": "migrate", "container_name": "old-oxidns", "existing_agent_name": "native-dns-oxidns", "ha_data_dir": "/srv/new-ha", "backup_dir": "/srv/backup", "agent_mount": map[string]string{"type": "bind", "source": "//srv/shared"}, "core_mount": map[string]string{"type": "bind", "source": "/srv/shared/oxidns"}}, msgMigrationDirectories},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fields model.Fields
			if err := json.Unmarshal([]byte(nativeInstallRequest), &fields); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{"data_dir": "/srv/native", "ha_host_id_path": test.identity, "install_method": test.method} {
				if err := setSessionField(&fields, name, value); err != nil {
					t.Fatal(err)
				}
			}
			for name, value := range test.extra {
				if err := setSessionField(&fields, name, value); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			result, err := GenerateGuideJSON(raw, nil)
			if result != nil || err == nil || err.Error() != test.message {
				t.Fatalf("alias generated output or wrong refusal: %v", err)
			}
		})
	}
}

func TestN810CapabilityRenewalPreservesUnknownJSON(t *testing.T) {
	dir := t.TempDir()
	first, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
	if err != nil {
		t.Fatal(err)
	}
	id := first.URL[strings.LastIndexByte(first.URL, '/')+1:]
	path := filepath.Join(dir, "docker-ha-installations", id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	future := json.RawMessage(`{"huge":900719925474099312345678901,"mixed":[null,false,{"future_key":"原值"}]}`)
	if err := fields.Set("future", future); err != nil {
		t.Fatal(err)
	}
	if err := fields.Set("expires", json.RawMessage(`0`)); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	second, err := IssueCapability(context.Background(), dir, []byte(nativeInstallRequest), "default")
	if err != nil || second.URL != first.URL || second.Token == first.Token {
		t.Fatalf("renewal: %v", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	preserved, ok := fields.Get("future")
	if !ok || string(preserved) != string(future) {
		t.Fatal("renewal changed opaque JSON or rounded its integer")
	}
}

func TestN810PortIdentityNeverRounds(t *testing.T) {
	for _, test := range []struct {
		raw   string
		equal bool
	}{
		{"8082", true}, {"8082.0", true}, {"8.082e3", true},
		{`"8082"`, false}, {"true", false}, {"8082.000000000000000001", false},
	} {
		if numericPortEqual([]byte(test.raw), "8082") != test.equal {
			t.Fatalf("wrong identity proof for %s", test.raw)
		}
	}
}
