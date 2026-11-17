package agents

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
)

// TestReviewA24ShutdownRejectsWrites checks every owner against a stopped Runtime.
func TestReviewA24ShutdownRejectsWrites(t *testing.T) {
	for _, kind := range []string{"note", "policy", "domain", "ip", "toggle", "clear"} {
		t.Run(kind, func(t *testing.T) {
			s := a24Fixture(t)
			r := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
			r.Start(s.Context())
			ctx := r.BindRequest(s.Context())
			r.Shutdown(context.Background())
			before := a24State(t, s)
			var err error
			switch kind {
			case "note":
				err = (clientTools{}).SaveNote(ctx, "m", noteEdit{"office", "192.0.2.5", "changed", "旧电脑"})
			case "policy":
				err = (clientTools{}).SavePolicies(ctx, "d", policyEdit{ProfileID: "office", Policies: json.RawMessage(`[]`)})
			case "domain":
				_, err = (clientTools{}).ApplyConnection(ctx, "m", connectionEdit{"office", "domains", "direct", json.RawMessage(`"shutdown.example"`), "DOMAIN"})
			case "ip":
				_, err = (clientTools{}).ApplyConnection(ctx, "m", connectionEdit{"office", "ip", "direct", json.RawMessage(`"198.51.100.8"`), "IP-CIDR"})
			case "toggle":
				_, err = (clientTools{}).SetDiscovery(ctx, "m", true)
			case "clear":
				_, err = (clientTools{}).ClearDiscovery(ctx, "m")
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("shutdown result: %v", err)
			}
			if !reflect.DeepEqual(before, a24State(t, s)) {
				t.Error("shutdown changed repository or discovery files")
			}
		})
	}
}

// TestReviewA24PublicCredentials checks legacy metadata without mutating its source.
func TestReviewA24PublicCredentials(t *testing.T) {
	for _, text := range []string{
		`{"id":"legacy","host":"operator:secret-password@192.0.2.9","token":"secret-token"}`,
		`{"id":"legacy","description":"old secret-registration and secret-password","registration_key":"secret-registration","password":"secret-password","private_key":"secret-key","future":{"keep":true}}`,
		`{"id":"device","token":"secret-token","latest_upgrade":{"status":"failed","registration_key":"secret-registration","message":"old secret-token"}}`,
		`{"id":"device","latest_deployment":{"status":"ready","api_key":"secret-api","message":"password=secret-password"},"deployments":{"old":{"status":"failed","password":"secret-old"}}}`,
		`{"id":"device","oxidns_ha_publication":{"status":"failed","message":"password=secret-ha","members":["a","b"]}}`,
		`{"id":"device","future":{"message":"Bearer secret-bearer","authorization":"Basic secret-basic"}}`,
		`{"id":"device","description":"-----BEGIN PRIVATE KEY-----\nsecret-pem\n-----END PRIVATE KEY-----"}`,
	} {
		v, err := jsonx.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		record := v.(*jsonx.Object)
		before := record.DeepCopy()
		public := publicAgent(record)
		raw, err := json.Marshal(public)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "secret-") {
			t.Errorf("credentials escaped: %s", raw)
		}
		if !jsonx.Equal(record, before) {
			t.Error("source was mutated")
		}
		if public.GetString("id") != record.GetString("id") {
			t.Error("public identity lost")
		}
	}
}

// TestReviewA24PublicHTTPDoesNotExposeLegacyCredentials covers both public HTTP exits.
func TestReviewA24PublicHTTPDoesNotExposeLegacyCredentials(t *testing.T) {
	s := a24Fixture(t)
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		for _, item := range system.GetList("agents") {
			agent := item.(*jsonx.Object)
			if agent.GetString("id") == "m" {
				agent.Set("host", "operator:secret-password@192.0.2.8")
				agent.Set("registration_key", "secret-registration")
			}
			if agent.GetString("id") == "d" {
				agent.Set("latest_deployment", jsonx.Obj("status", "failed", "password", "secret-deployment", "message", "password=secret-message"))
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	login := s.Post("/api/auth/login", httpxtest.JSON(jsonx.Obj("username", "a24-admin", "password", "a24-password"))).WantStatus(200).Object()
	auth := httpxtest.Bearer(login.GetString("token"))
	for _, path := range []string{"/api/agents/m/domain-discovery", "/api/agents/d/oxidns/device-policies"} {
		var options = []httpxtest.Option{auth}
		method := "GET"
		if strings.HasSuffix(path, "domain-discovery") {
			method = "PUT"
			options = append(options, httpxtest.JSON(jsonx.Obj("enabled", true)))
		}
		reply := s.Do(method, path, options...).WantStatus(200)
		if strings.Contains(string(reply.Body), "secret-") {
			t.Fatalf("%s leaked: %s", path, reply.Body)
		}
	}
}

// TestReviewA24TargetsIgnoreUnrelatedLegacySettings checks normalized legacy settings.
func TestReviewA24TargetsIgnoreUnrelatedLegacySettings(t *testing.T) {
	w := newClientWorldSetup(t, func(s *httpxtest.Server) {
		_, _, err := s.Repo.UpdateProfileTransaction(s.Context(), "office", func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
			settings := profile.GetObject("domain_discovery")
			settings.Set("history", []any{"old entry", nil})
			settings.Set("region_probe_port", "17999")
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	w.call("GET", "/api/agents/m/mihomo/rule-targets", nil, 200, "admin")
}
