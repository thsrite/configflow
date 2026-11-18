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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
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
func o9Body() *jsonx.Object {
	return jsonx.Obj("name", "new-device", "host", "192.0.2.30", "port", 8080, "service_type", "mihomo", "deployment_method", "shell", "version", "1.8.60-go")
}

var o9Token = regexp.MustCompile(`"registration_token": "([A-Za-z0-9_-]{43})"`)

func o9Command(t *testing.T, s *httpxtest.Server, pairs ...any) (string, []string) {
	t.Helper()
	body := jsonx.Obj("name", "new-device", "agent_ip", "192.0.2.30", "server_url", "https://panel.example.com")
	body.Update(jsonx.Obj(pairs...))
	script := s.Post("/api/agents/install-command", s.Login(), httpxtest.JSON(body)).WantStatus(200).Object().GetString("script")
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
	if _, _, err := s.Repo.UpdateProfileFields(s.Context(), "default", jsonx.Obj("mihomo", jsonx.Obj("custom_config", "mode: rule\nsecret: o9-synthetic-controller-secret\n")), nil); err != nil {
		t.Fatal(err)
	}
	body := s.Post("/api/agents/register", httpxtest.JSON(o9Body())).WantStatus(403).Object()
	if body.Has("token") || body.GetString("message") != RegistrationLocked {
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
	legitimate := s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(200).Object()
	config := s.Get("/api/agents/" + legitimate.GetString("id") + "/config?token=" + legitimate.GetString("token")).WantStatus(200)
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
				body.Set("registration_key", "synthetic-registration-key")
			case "jwt":
				options = append(options, s.Login())
			case "credential":
				_, tokens := o9Command(t, s)
				body.Set("registration_token", tokens[0])
			}
			options = append(options, httpxtest.JSON(body))
			answer := s.Post("/api/agents/register", options...).WantStatus(200).Object()
			if answer.GetString("token") == "" {
				t.Fatal("missing token")
			}
			t.Setenv("AGENT_REGISTRATION_KEY", "rotated-registration-key")
			s.Post("/api/agents/register", httpxtest.Bearer(answer.GetString("token")), httpxtest.JSON(o9Body())).WantStatus(200)
			// A valid JWT/key cannot take over an already-owned identity.
			s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(401)
			s.Post("/api/agents/"+answer.GetString("id")+"/heartbeat", httpxtest.Bearer(answer.GetString("token")), httpxtest.JSON(jsonx.Obj("version", "1.8.60-go"))).WantStatus(200)
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
			body.Set("registration_token", tokens[0])
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
				body.Set("registration_token", strings.Repeat("x", 43))
			case "wrong-host":
				body.Set("host", "192.0.2.31")
			case "wrong-name":
				body.Set("name", "another-device")
			case "wrong-service":
				body.Set("service_type", "oxidns")
			case "wrong-port":
				body.Set("port", 8082)
			case "wrong-method":
				body.Set("deployment_method", "docker")
			case "wrong-profile":
				if _, err := s.Repo.CreateProfile(s.Context(), jsonx.Obj("id", "other", "name", "Other"), ""); err != nil {
					t.Fatal(err)
				}
				body.Set("profile_id", "other")
			case "used", "leaked-second-host":
				registered := s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200).Object()
				if kind == "used" {
					s.Delete("/api/agents/"+registered.GetString("id"), s.Login()).WantStatus(200)
				} else {
					body.Set("host", "192.0.2.31")
				}
			case "failed-register":
				body.Set("profile_id", "bad id!")
				status = 500
			}
			s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(status)
			if kind == "failed-register" {
				body.Pop("profile_id")
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
			body.Set("host", host)
			body.Set("registration_token", tokens[0])
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
	s.Post("/api/agents/install-command", s.Login(), httpxtest.JSON(jsonx.Obj())).WantStatus(503)
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
	if _, err := m.RegisterAuthorizedAgent(request, o9Body(), nil, proof); err != nil {
		t.Fatalf("disconnected request: %v", err)
	}
	ctx := runtime.BindRequest(s.Context())
	runtime.Shutdown(context.Background())
	before, _ := json.Marshal(mustO9System(t, s))
	if _, err := m.CreateRegistrationCredentials(ctx, []model.AgentRegistrationSession{identity}); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown issue: %v", err)
	}
	if _, err := m.RegisterAuthorizedAgent(ctx, o9Body(), nil, core.RegistrationAuthorization{AllowNew: true}); !errors.Is(err, context.Canceled) {
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
		body.Set("name", "new-device-"+service)
		body.Set("service_type", service)
		body.Set("registration_token", tokens[i])
		if i == 1 {
			body.Set("port", 8082)
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
	body.Set("registration_token", token)
	body.Set("name", token)
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
	body = o9Body()
	body.Set("registration_token", token)
	body.Set("profile_id", token)
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
	backup, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if backup.GetObject("system").Has("agent_registration_sessions") {
		t.Fatal("backup exported installation capabilities")
	}
	// Even an old/full current-schema backup that contains the Go field is
	// stripped on import, rather than trusting a previously unconsumed hash.
	stored, err := s.Repo.GetSystem(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	backup.GetObject("system").Set("agent_registration_sessions", stored.Value("agent_registration_sessions"))
	if _, err := s.Repo.ImportAll(s.Context(), backup); err != nil {
		t.Fatal(err)
	}
	body := o9Body()
	body.Set("registration_token", tokens[0])
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
	body.Set("port", 80)
	body.Set("host", "172.20.0.5")
	body.Set("registration_token", string(match[1]))
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
}

func TestO9LoginDisabledKeepsExplicitRegistrationKeyPolicy(t *testing.T) {
	s := o9Server(t)
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-key")
	script := s.Post("/api/agents/install-command", httpxtest.JSON(jsonx.Obj("name", "new-device"))).WantStatus(200).Object().GetString("script")
	if o9Token.MatchString(script) {
		t.Fatal("anonymous template bypassed explicit registration key policy")
	}
	s.Post("/api/agents/register", httpxtest.JSON(o9Body())).WantStatus(403)
	body := o9Body()
	body.Set("registration_key", "synthetic-key")
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
}

func TestO9ValidBootstrapProofConsumedEvenWhenJWTAlsoPresent(t *testing.T) {
	s := o9Server(t)
	_, tokens := o9Command(t, s)
	body := o9Body()
	body.Set("registration_token", tokens[0])
	registered := s.Post("/api/agents/register", s.Login(), httpxtest.JSON(body)).WantStatus(200).Object()
	s.Delete("/api/agents/"+registered.GetString("id"), s.Login()).WantStatus(200)
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
}

func TestO9ValidBootstrapProofConsumedAlongsideExistingAgentToken(t *testing.T) {
	s := o9Server(t)
	registered := s.Post("/api/agents/register", s.Login(), httpxtest.JSON(o9Body())).WantStatus(200).Object()
	_, tokens := o9Command(t, s)
	body := o9Body()
	body.Set("registration_token", tokens[0])
	s.Post("/api/agents/register", httpxtest.Bearer(registered.GetString("token")), httpxtest.JSON(body)).WantStatus(200)
	s.Delete("/api/agents/"+registered.GetString("id"), s.Login()).WantStatus(200)
	s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
}
