package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendRegisterRequestAddsExistingBearerToken(t *testing.T) {
	const token = "existing-go-token"
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"id":"agent-1"}`))
	}))
	defer server.Close()

	config := &Config{ServerURL: server.URL, Token: token}
	response, err := config.sendRegisterRequest(RegisterRequest{Name: "probe", Host: "10.0.0.8"})

	if err != nil {
		t.Fatalf("sendRegisterRequest returned error: %v", err)
	}
	if response.ID != "agent-1" {
		t.Fatalf("unexpected response ID: %q", response.ID)
	}
	if gotAuthorization != "Bearer "+token {
		t.Fatalf("Authorization = %q, want configured bearer", gotAuthorization)
	}
}

func TestSendRegisterRequestWithoutTokenDoesNotAddAuthorization(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"id":"agent-new","token":"new-token"}`))
	}))
	defer server.Close()

	config := &Config{ServerURL: server.URL}
	_, err := config.sendRegisterRequest(RegisterRequest{Name: "new", Host: "10.0.0.9"})

	if err != nil {
		t.Fatalf("sendRegisterRequest returned error: %v", err)
	}
	if gotAuthorization != "" {
		t.Fatalf("Authorization = %q, want empty", gotAuthorization)
	}
}

func TestHandleRegisterResponsePreservesExistingTokenWhenResponseOmitsToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := &Config{AgentID: "agent-1", Token: "keep-token", filePath: path}

	if err := config.handleRegisterResponse(&RegisterResponse{Success: true, ID: "agent-1"}); err != nil {
		t.Fatalf("handleRegisterResponse returned error: %v", err)
	}
	if config.Token != "keep-token" {
		t.Fatalf("Token = %q, want existing token", config.Token)
	}
	assertSavedToken(t, path, "keep-token")
}

func TestHandleRegisterResponseStoresTokenForFirstRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := &Config{filePath: path}

	if err := config.handleRegisterResponse(&RegisterResponse{
		Success: true,
		ID:      "agent-new",
		Token:   "issued-token",
	}); err != nil {
		t.Fatalf("handleRegisterResponse returned error: %v", err)
	}
	if config.AgentID != "agent-new" || config.Token != "issued-token" {
		t.Fatalf("unexpected registration state: id=%q token=%q", config.AgentID, config.Token)
	}
	assertSavedToken(t, path, "issued-token")
}

func TestRedactURLForLogRemovesCredentialsAndTokenFragments(t *testing.T) {
	const token = "main-agent-secret-token"
	raw := "https://user:password@example.test/api?config_token=" + token + "&ok=visible"

	got := redactURLForLog(raw)

	for _, secret := range []string{"user", "password", token, token[:8]} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted URL leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "config_token=[REDACTED]") || !strings.Contains(got, "ok=visible") {
		t.Fatalf("unexpected redacted URL: %s", got)
	}
}

func TestRedactURLForLogDropsEncodedAndNestedFragments(t *testing.T) {
	for _, raw := range []string{
		"https://example.test/path#token=plain-fragment-secret",
		"https://example.test/path#token=encoded%2Dfragment%2Dsecret",
		"https://example.test/path#outer=https%253A%252F%252Fexample.test%252F%253Ftoken%253Dnested-secret",
		"https://example.test/path#safe=ok&api_key=mixed-fragment-secret",
	} {
		got := redactURLForLog(raw)
		if strings.Contains(got, "#") || strings.Contains(got, "fragment-secret") || strings.Contains(got, "nested-secret") {
			t.Fatalf("fragment credential leaked: raw=%q got=%q", raw, got)
		}
	}
}

func TestRedactURLForLogRedactsActualReviewNestedCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://example.test/p?next=https%253A%252F%252Fnested.test%252Fp%253Ftoken%253Dnested-url-secret",
		"https://example.test/p?next=token%253Dnested-expression-secret",
		"https://example.test/p?next=api%252Bkey%253Dplus-layer-secret",
	} {
		got := redactURLForLog(raw)
		if !strings.Contains(got, "next=[REDACTED]") {
			t.Fatalf("nested credential value was not redacted: raw=%q got=%q", raw, got)
		}
		for _, secret := range []string{"nested-url-secret", "nested-expression-secret", "plus-layer-secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("nested credential leaked %q: raw=%q got=%q", secret, raw, got)
			}
		}
	}
}

func TestRedactURLForLogFailsClosedWhenDecodingRevealsC0(t *testing.T) {
	for _, encodedControl := range []string{"%250d%250a", "%2500", "%251f", "%257f"} {
		raw := "https://example.test/p?next=ok" + encodedControl + "token=secret"
		if got := redactURLForLog(raw); got != "[INVALID URL REDACTED]" {
			t.Fatalf("decoded control did not fail closed: raw=%q got=%q", raw, got)
		}
	}
}

func TestRegisterTransportErrorPreservesIdentityWithoutLeakingError(t *testing.T) {
	want := errors.New("register transport secret")
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, want })
	t.Cleanup(func() { http.DefaultTransport = old })

	_, err := (&Config{ServerURL: "https://example.test", Token: "token"}).sendRegisterRequest(RegisterRequest{Name: "probe"})
	if !errors.Is(err, want) {
		t.Fatalf("errors.Is(err, want) = false: %v", err)
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("errors.As(err, *url.Error) = false: %v", err)
	}
}

func TestHeartbeatTransportErrorPreservesIdentityWithoutLeakingError(t *testing.T) {
	want := errors.New("heartbeat transport secret")
	old := heartbeatHTTPClient
	heartbeatHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, want })}
	t.Cleanup(func() { heartbeatHTTPClient = old })

	err := (&Config{ServerURL: "https://example.test", AgentID: "agent", Token: "secret"}).sendHeartbeatRequest([]byte(`{}`))
	if !errors.Is(err, want) {
		t.Fatalf("errors.Is(err, want) = false: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func assertSavedToken(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var saved Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if saved.Token != expected {
		t.Fatalf("saved token = %q, want %q", saved.Token, expected)
	}
}

func TestRegisterRejectionCarriesServerReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"message":"托管接口需要独立配置空间，\n请先分开绑定"}`))
	}))
	defer server.Close()
	cfg := &Config{ServerURL: server.URL}
	_, err := cfg.sendRegisterRequest(RegisterRequest{Name: "second-dns"})
	if err == nil || !strings.Contains(err.Error(), "409: 托管接口需要独立配置空间， 请先分开绑定") {
		t.Fatalf("error = %v", err)
	}
	if got := registerRejection(strings.NewReader("<html>bad gateway</html>")); got != "" {
		t.Fatalf("non-JSON body = %q", got)
	}
	if got := registerRejection(strings.NewReader(`{"message":"` + strings.Repeat("长", 400) + `"}`)); len([]rune(got)) != 303 {
		t.Fatalf("bounded message has %d runes", len([]rune(got)))
	}
}
