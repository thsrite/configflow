package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	core "configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func o9Server(t *testing.T) *httpxtest.Server {
	t.Helper()
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	core.ResetRuntime()
	t.Cleanup(core.ResetRuntime)
	s := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true, Register: httpxtest.Routes(Register)})
	t.Setenv("DATA_DIR", s.DataDir)
	return s
}
func o9Body() map[string]any {
	return map[string]any{
		"name": "new-device", "host": "192.0.2.30", "port": 8080,
		"service_type": "mihomo", "deployment_method": "shell", "version": "1.8.60-go",
	}
}

var o9Token = regexp.MustCompile(`"registration_token": "([A-Za-z0-9_-]{43})"`)

func o9Command(t *testing.T, s *httpxtest.Server, pairs ...any) (string, []string) {
	t.Helper()
	body := map[string]any{"name": "new-device", "agent_ip": "192.0.2.30", "server_url": "https://panel.example.com"}
	for i := 0; i+1 < len(pairs); i += 2 {
		body[pairs[i].(string)] = pairs[i+1]
	}
	script := mapStr(parseBodyMap(t, s.Post("/api/agents/install-command", s.Login(), httpxtest.JSON(body)).WantStatus(200)), "script")
	var tokens []string
	for _, match := range o9Token.FindAllStringSubmatch(script, -1) {
		tokens = append(tokens, match[1])
	}
	if len(tokens) == 0 {
		t.Fatal("no bootstrap credential in script")
	}
	return script, tokens
}

// This is the untagged O9-H2 reproducer: an anonymous identity must never get
// the token that would allow it to read the default profile's generated secrets.
func TestO9AnonymousAgentRegistrationCannotReadProfileSecrets(t *testing.T) {
	s, _, _ := newDeployHTTP(t)
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	if _, err := s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, profile *model.Profile) error {
		return profile.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mode: rule\nsecret: o9-synthetic-controller-secret\n"}`))
	}); err != nil {
		t.Fatal(err)
	}
	body := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(o9Body())).WantStatus(403))
	if mapHas(body, "token") || mapStr(body, "message") != RegistrationLocked {
		t.Fatalf("unsafe refusal: %v", body)
	}
	list, err := s.Repo.System(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Agents) != 1 {
		t.Fatal("anonymous Agent persisted")
	}
	s.Get("/api/agents/o9-rogue/config?token=wrong").WantStatus(401)
	legitimate := parseBodyMap(t, s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(200))
	config := s.Get("/api/agents/" + mapStr(legitimate, "id") + "/config?token=" + mapStr(legitimate, "token")).WantStatus(200)
	if !strings.Contains(config.Text(), "o9-synthetic-controller-secret") {
		t.Fatal("legitimate Agent lost configuration access")
	}
}
func TestO9BootstrapProofsAndExistingTokens(t *testing.T) {
	for _, proof := range []string{"key", "jwt", "credential"} {
		t.Run(proof, func(t *testing.T) {
			s := o9Server(t)
			body := o9Body()
			options := []httpxtest.Option{}
			switch proof {
			case "key":
				t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-registration-key")
				body["registration_key"] = "synthetic-registration-key"
			case "jwt":
				options = append(options, s.Login())
			case "credential":
				_, tokens := o9Command(t, s)
				body["registration_token"] = tokens[0]
			}
			options = append(options, httpxtest.JSON(body))
			answer := parseBodyMap(t, s.Post("/api/agents/register", options...).WantStatus(200))
			if mapStr(answer, "token") == "" {
				t.Fatal("missing token")
			}
			t.Setenv("AGENT_REGISTRATION_KEY", "rotated-registration-key")
			s.Post("/api/agents/register", httpxtest.Bearer(mapStr(answer, "token")), httpxtest.JSON(o9Body())).WantStatus(200)
			// A valid JWT/key cannot take over an already-owned identity.
			s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(401)
			s.Post("/api/agents/"+mapStr(answer, "id")+"/heartbeat", httpxtest.Bearer(mapStr(answer, "token")), httpxtest.JSON(map[string]any{"version": "1.8.60-go"})).WantStatus(200)
			stored, err := s.Repo.System(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(stored)
			if strings.Contains(string(encoded), "synthetic-registration-key") || strings.Contains(string(encoded), `"registration_token"`) {
				t.Fatal("plaintext bootstrap proof persisted")
			}
		})
	}
}
func TestO9CredentialRefusalsAndAtomicConsumption(t *testing.T) {
	for _, kind := range []string{"expired", "wrong", "wrong-host", "wrong-name", "wrong-service", "wrong-port", "wrong-method", "wrong-profile", "used", "leaked-second-host", "failed-register"} {
		t.Run(kind, func(t *testing.T) {
			s := o9Server(t)
			_, tokens := o9Command(t, s)
			stored := mustO9System(t, s)
			if len(stored.RegistrationSessions) != 1 || len(stored.RegistrationSessions[0].Hash) != 64 || stored.RegistrationSessions[0].Hash == tokens[0] {
				t.Fatal("invalid persisted credential digest")
			}
			if expires := int64(stored.RegistrationSessions[0].Expires); expires < time.Now().Add(23*time.Hour).Unix() || expires > time.Now().Add(24*time.Hour).Unix() {
				t.Fatal("incorrect capability lifetime")
			}
			encoded, _ := json.Marshal(stored)
			if strings.Contains(string(encoded), tokens[0]) {
				t.Fatal("plaintext capability stored")
			}
			body := o9Body()
			body["registration_token"] = tokens[0]
			status := 403
			switch kind {
			case "expired":
				if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
					system.RegistrationSessions[0].Expires = model.Int(time.Now().Unix())
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "wrong":
				body["registration_token"] = strings.Repeat("x", 43)
			case "wrong-host":
				body["host"] = "192.0.2.31"
			case "wrong-name":
				body["name"] = "another-device"
			case "wrong-service":
				body["service_type"] = "oxidns"
			case "wrong-port":
				body["port"] = 8082
			case "wrong-method":
				body["deployment_method"] = "docker"
			case "wrong-profile":
				if _, err := s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
					t.Fatal(err)
				}
				body["profile_id"] = "other"
			case "used", "leaked-second-host":
				registered := parseBodyMap(t, s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200))
				if kind == "used" {
					s.Delete("/api/agents/"+mapStr(registered, "id"), s.Login()).WantStatus(200)
				} else {
					body["host"] = "192.0.2.31"
				}
			case "failed-register":
				body["profile_id"] = "bad id!"
				status = 500
			}
			s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(status)
			if kind == "failed-register" {
				delete(body, "profile_id")
				s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
			}
		})
	}
}
func TestO9CredentialConcurrentSingleUseAndRestart(t *testing.T) {
	s := o9Server(t)
	before := mustO9System(t, s).Revision
	_, tokens := o9Command(t, s, "agent_ip", "")
	if mustO9System(t, s).Revision != before {
		t.Fatal("private credential issuance changed user configuration revision")
	}
	// Reopen from disk, without relying on a process-local capability registry.
	restarted := httpxtest.New(t, httpxtest.Options{Auth: true, DataDir: s.DataDir, DefaultFactories: true, Register: httpxtest.Routes(Register)})
	url := restarted.URL()
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, host := range []string{"192.0.2.31", "192.0.2.32"} {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			body := o9Body()
			body["host"] = host
			body["registration_token"] = tokens[0]
			b, _ := json.Marshal(body)
			resp, err := http.Post(url+"/api/agents/register", "application/json", strings.NewReader(string(b)))
			if err != nil {
				codes <- 0
				return
			}
			defer resp.Body.Close()
			codes <- resp.StatusCode
		}(host)
	}
	wg.Wait()
	close(codes)
	success, refused := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 403 {
			refused++
		}
	}
	if success != 1 || refused != 1 {
		t.Fatalf("success=%d refusal=%d", success, refused)
	}
}
func TestO9RegistrationCapacityExpiryAndShutdown(t *testing.T) {
	s := o9Server(t)
	m, err := core.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	identity := model.AgentRegistrationSession{Name: "new-device", Host: "192.0.2.30", Port: 8080, ServiceType: "mihomo", DeploymentMethod: "shell"}
	identities := make([]model.AgentRegistrationSession, core.MaxRegistrationSessions)
	for i := range identities {
		identities[i] = identity
	}
	if _, err := m.CreateRegistrationCredentials(s.Context(), identities); err != nil {
		t.Fatal(err)
	}
	s.Post("/api/agents/install-command", s.Login(), httpxtest.JSON(map[string]any{})).WantStatus(200)
	if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		for i := range system.RegistrationSessions {
			system.RegistrationSessions[i].Expires = 1
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, tokens := o9Command(t, s)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: time.Second})
	runtime.Start(context.Background())
	request, cancel := context.WithCancel(runtime.BindRequest(s.Context()))
	cancel()
	if _, err := m.CreateRegistrationCredentials(request, []model.AgentRegistrationSession{identity}); err != nil {
		t.Fatalf("disconnected credential issuance: %v", err)
	}
	proof := core.RegistrationAuthorization{Credential: tokens[0]}
	input, err := core.DecodeRegistrationInput(mustRawJSON(t, o9Body()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.RegisterIdentity(request, input, core.RegistrationOptions{Authorization: &proof}); err != nil {
		t.Fatalf("disconnected request: %v", err)
	}
	ctx := runtime.BindRequest(s.Context())
	runtime.Shutdown(context.Background())
	before, _ := json.Marshal(mustO9System(t, s))
	if _, err := m.CreateRegistrationCredentials(ctx, []model.AgentRegistrationSession{identity}); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown issue: %v", err)
	}
	input, err = core.DecodeRegistrationInput(mustRawJSON(t, o9Body()))
	if err != nil {
		t.Fatal(err)
	}
	allowNew := core.RegistrationAuthorization{AllowNew: true}
	if _, err := m.RegisterIdentity(ctx, input, core.RegistrationOptions{Authorization: &allowNew}); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown register: %v", err)
	}
	after, _ := json.Marshal(mustO9System(t, s))
	if string(before) != string(after) {
		t.Fatal("write after shutdown")
	}
	// No bootstrap plaintext in notifications or any other file in DATA_DIR.
	filepath.WalkDir(s.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && !strings.Contains(path, "install-commands") {
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), tokens[0]) {
				t.Errorf("credential leaked to %s", filepath.Base(path))
			}
		}
		return nil
	})
}
func mustO9System(t *testing.T, s *httpxtest.Server) *model.System {
	t.Helper()
	system, err := s.Repo.System(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	return system
}
func TestO9AIOAndDockerIssueIndependentServiceProofs(t *testing.T) {
	s := o9Server(t)
	_, tokens := o9Command(t, s, "type", "aio")
	if len(tokens) != 2 || tokens[0] == tokens[1] {
		t.Fatal("shared AIO capability")
	}
	for i, service := range []string{"mihomo", "oxidns"} {
		body := o9Body()
		body["name"] = "new-device-" + service
		body["service_type"] = service
		body["registration_token"] = tokens[i]
		if i == 1 {
			body["port"] = 8082
		}
		s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
	}
	for _, format := range []string{"compose", "run"} {
		text := s.Get("/api/agents/docker-agent-"+format+"?server_url=https://panel.example.com&agent_name=docker-device&agent_ip=192.0.2.30&enable_oxidns=true", s.Login()).WantStatus(200).Text()
		re := regexp.MustCompile(`AGENT_(MIHOMO|OXIDNS)_REGISTRATION_TOKEN=([A-Za-z0-9_-]{43})`)
		matches := re.FindAllStringSubmatch(text, -1)
		if len(matches) != 2 || matches[0][2] == matches[1][2] {
			t.Fatal("missing Docker service credentials")
		}
	}
}

func TestO9PublicScriptCannotMintProofAndCredentialsStayOutOfLogs(t *testing.T) {
	s := o9Server(t)
	logFile := filepath.Join(t.TempDir(), "panel.log")
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: logFile})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	anonymous := s.Get("/api/agents/install-script?name=new-device&type=mihomo").WantStatus(200)
	if o9Token.Match(anonymous.Body) {
		t.Fatal("public script minted credential")
	}
	authenticated := s.Get("/api/agents/install-script?name=new-device&type=mihomo&agent_ip=192.0.2.30", s.Login()).WantStatus(200)
	match := o9Token.FindSubmatch(authenticated.Body)
	if len(match) != 2 {
		t.Fatal("authenticated script omitted credential")
	}
	token := string(match[1])
	body := o9Body()
	body["registration_token"] = token
	body["name"] = token
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
	body = o9Body()
	body["registration_token"] = token
	body["profile_id"] = token
	failure := s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(500)
	if strings.Contains(failure.Text(), token) {
		t.Fatal("proof reflected by registration error")
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("bootstrap proof leaked in log metadata")
	}
	filepath.WalkDir(s.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), token) {
				t.Errorf("proof leaked to %s", filepath.Base(path))
			}
		}
		return nil
	})
}

func TestO9BackupDoesNotResurrectBootstrapSessions(t *testing.T) {
	s := o9Server(t)
	_, tokens := o9Command(t, s)
	backupRaw, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	backup := parseJSONMap(t, backupRaw)
	if mapHas(mapObj(backup, "system"), "agent_registration_sessions") {
		t.Fatal("backup exported installation capabilities")
	}
	// Even an old/full current-schema backup that contains the Go field is
	// stripped on import, rather than trusting a previously unconsumed hash.
	stored := mustO9System(t, s)
	storedMap := parseJSONMap(t, mustRawJSON(t, stored))
	mapObj(backup, "system")["agent_registration_sessions"] = storedMap["agent_registration_sessions"]
	if _, err := s.Repo.ImportAllJSON(s.Context(), mustRawJSON(t, backup)); err != nil {
		t.Fatal(err)
	}
	body := o9Body()
	body["registration_token"] = tokens[0]
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
}

func TestO9CredentialIdentityUsesRenderedPortAndReportedHost(t *testing.T) {
	s := o9Server(t)
	response := s.Get("/api/agents/install-script?name=new-device&type=mihomo&port=8_0&agent_ip=172.20.0.5", s.Login()).WantStatus(200)
	match := o9Token.FindSubmatch(response.Body)
	if len(match) != 2 {
		t.Fatal("no proof")
	}
	body := o9Body()
	body["port"] = 80
	body["host"] = "172.20.0.5"
	body["registration_token"] = string(match[1])
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
}

func TestO9LoginDisabledKeepsExplicitRegistrationKeyPolicy(t *testing.T) {
	s := o9Server(t)
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-key")
	script := mapStr(parseBodyMap(t, s.Post("/api/agents/install-command", httpxtest.JSON(map[string]any{"name": "new-device"})).WantStatus(200)), "script")
	if o9Token.MatchString(script) {
		t.Fatal("anonymous template bypassed explicit registration key policy")
	}
	s.Post("/api/agents/register", httpxtest.JSON(o9Body())).WantStatus(403)
	body := o9Body()
	body["registration_key"] = "synthetic-key"
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
}

func TestO9ValidBootstrapProofConsumedEvenWhenJWTAlsoPresent(t *testing.T) {
	s := o9Server(t)
	_, tokens := o9Command(t, s)
	body := o9Body()
	body["registration_token"] = tokens[0]
	registered := parseBodyMap(t, s.Post("/api/agents/register", s.Login(), httpxtest.JSON(body)).WantStatus(200))
	s.Delete("/api/agents/"+mapStr(registered, "id"), s.Login()).WantStatus(200)
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
}

func TestO9ValidBootstrapProofConsumedAlongsideExistingAgentToken(t *testing.T) {
	s := o9Server(t)
	registered := parseBodyMap(t, s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(200))
	_, tokens := o9Command(t, s)
	body := o9Body()
	body["registration_token"] = tokens[0]
	s.Post("/api/agents/register", httpxtest.Bearer(mapStr(registered, "token")), httpxtest.JSON(body)).WantStatus(200)
	s.Delete("/api/agents/"+mapStr(registered, "id"), s.Login()).WantStatus(200)
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
}

func TestO9InstallScriptRetainsRawQuerySemantics(t *testing.T) {
	s := o9Server(t)
	tests := []struct {
		name, query, want string
		status            int
	}{
		{"bad escape value", "config_path=/tmp/a%2", `"config_path": "/tmp/a%2"`, 200},
		{"semicolon value", "config_path=/tmp/a;b", `"config_path": "/tmp/a;b"`, 200},
		{"bad escape service", "type=mihomo%zz", "", 400},
		{"bad escape lifecycle", "restart_command=%2Fcustom%2", "", 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, authenticated := range []bool{false, true} {
				var options []httpxtest.Option
				if authenticated {
					options = append(options, s.Login())
				}
				result := s.Get("/api/agents/install-script?"+test.query, options...).WantStatus(test.status)
				if test.want != "" && !strings.Contains(result.Text(), test.want) {
					t.Fatalf("query parameter lost: %s", test.query)
				}
			}
		})
	}
}

// Review O9-fix: without any first-registration proof, an existing name/host
// pair must be refused exactly like an unknown one (no registry oracle).
func TestO9AnonymousRefusalDoesNotRevealExistingIdentity(t *testing.T) {
	s := o9Server(t)
	s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(200)
	unknown := o9Body()
	unknown["name"] = "unknown-device"
	garbage := o9Body()
	garbage["registration_token"] = strings.Repeat("A", 43)
	tests := []struct {
		name    string
		options []httpxtest.Option
	}{
		{"unknown", []httpxtest.Option{httpxtest.JSON(unknown)}},
		{"existing-anonymous", []httpxtest.Option{httpxtest.JSON(o9Body())}},
		{"existing-wrong-bearer", []httpxtest.Option{httpxtest.Bearer("synthetic-wrong-agent-token"), httpxtest.JSON(o9Body())}},
		{"existing-garbage-proof", []httpxtest.Option{httpxtest.JSON(garbage)}},
	}
	// The shared server is bound to the parent test, so cases run in sequence.
	var first string
	for _, test := range tests {
		response := s.Post("/api/agents/register", test.options...).WantStatus(403)
		if body := parseBodyMap(t, response); mapStr(body, "message") != RegistrationLocked {
			t.Fatalf("%s: distinguishable refusal: %v", test.name, body)
		}
		if first == "" {
			first = response.Text()
		} else if response.Text() != first {
			t.Fatalf("%s: refusal body differs: %s vs %s", test.name, response.Text(), first)
		}
	}
	// Proof holders still get the actionable ownership refusal, and keep their proof.
	s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(401)
	_, tokens := o9Command(t, s)
	proof := o9Body()
	proof["registration_token"] = tokens[0]
	s.Post("/api/agents/register", httpxtest.JSON(proof)).WantStatus(401)
	if sessions := mustO9System(t, s).RegistrationSessions; len(sessions) != 1 {
		t.Fatalf("ownership refusal consumed the proof: %d sessions", len(sessions))
	}
}

// Both formats are alternatives for one installation, not two issuances.
func TestO9DockerPairedCommandsShareOneProofPerService(t *testing.T) {
	s := o9Server(t)
	path := "/api/agents/docker-agent-compose?format=both&server_url=https://panel.example.com&agent_name=paired-device&agent_ip=192.0.2.30&enable_oxidns=true"
	s.Get(path).WantStatus(401)
	if len(mustO9System(t, s).RegistrationSessions) != 0 {
		t.Fatal("anonymous issuance")
	}
	response := s.Get(path, s.Login()).WantStatus(200)
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable credentials")
	}
	body := parseBodyMap(t, response)
	re := regexp.MustCompile(`AGENT_(MIHOMO|OXIDNS)_REGISTRATION_TOKEN=([A-Za-z0-9_-]{43})`)
	compose := re.FindAllStringSubmatch(mapStr(body, "compose"), -1)
	run := re.FindAllStringSubmatch(mapStr(body, "run"), -1)
	if len(compose) != 2 || len(run) != 2 || compose[0][2] == compose[1][2] {
		t.Fatal("missing independent service proofs")
	}
	if len(mustO9System(t, s).RegistrationSessions) != 2 {
		t.Fatal("duplicate issuance for alternative formats")
	}
	for i, match := range compose {
		if match[1] != run[i][1] || match[2] != run[i][2] {
			t.Fatal("formats use different credentials")
		}
		service := strings.ToLower(match[1])
		registration := o9Body()
		registration["name"] = "paired-device-" + service
		registration["deployment_method"] = "docker"
		registration["service_type"] = service
		registration["registration_token"] = match[2]
		if service == "oxidns" {
			registration["port"] = 8082
		}
		s.Post("/api/agents/register", httpxtest.JSON(registration)).WantStatus(200)
	}
	if len(mustO9System(t, s).RegistrationSessions) != 0 {
		t.Fatal("unspent duplicate credential")
	}
}

func TestO9RegistrationCapacityEvictsOldestCommand(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	core.ResetRuntime()
	t.Cleanup(core.ResetRuntime)
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(Register)})
	t.Setenv("DATA_DIR", s.DataDir)
	authorization := s.Login()
	path := "/api/agents/docker-agent-compose?format=both&server_url=https://panel.example.com&agent_name=new-device&agent_ip=192.0.2.30"
	re := regexp.MustCompile(`AGENT_MIHOMO_REGISTRATION_TOKEN=([A-Za-z0-9_-]{43})`)
	var oldest, latest string
	for i := 0; i <= core.MaxRegistrationSessions; i++ {
		body := parseBodyMap(t, s.Get(path, authorization).WantStatus(200))
		match := re.FindStringSubmatch(mapStr(body, "compose"))
		if len(match) != 2 {
			t.Fatal("missing credential")
		}
		latest = match[1]
		if i == 0 {
			oldest = latest
		}
	}
	if len(mustO9System(t, s).RegistrationSessions) != core.MaxRegistrationSessions {
		t.Fatal("unbounded sessions")
	}
	registration := o9Body()
	registration["name"] = "new-device-mihomo"
	registration["deployment_method"] = "docker"
	registration["registration_token"] = oldest
	wantBodyJSON(t, s.Post("/api/agents/register", httpxtest.JSON(registration)).WantStatus(403), map[string]any{"success": false, "message": RegistrationLocked})
	registration["registration_token"] = latest
	s.Post("/api/agents/register", httpxtest.JSON(registration)).WantStatus(200)
}

func TestO9RegistrationProofRejectsBooleanPort(t *testing.T) {
	s := o9Server(t)
	manager, err := core.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	issued, err := manager.CreateRegistrationCredentials(s.Context(), []model.AgentRegistrationSession{{Name: "new-device", Host: "192.0.2.30", Port: 1, ServiceType: "mihomo", DeploymentMethod: "shell"}})
	if err != nil {
		t.Fatal(err)
	}
	body := o9Body()
	body["registration_token"] = issued["mihomo"]
	body["port"] = true
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
	if len(mustO9System(t, s).RegistrationSessions) != 1 {
		t.Fatal("invalid JSON type consumed proof")
	}
	body["port"] = 1
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
	if len(mustO9System(t, s).RegistrationSessions) != 0 {
		t.Fatal("exact numeric identity did not consume proof")
	}
}
