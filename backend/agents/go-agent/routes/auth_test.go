package routes

import (
	"agent/internal/logx"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddlewareStrictAuthorizationGrammar(t *testing.T) {
	const token = "valid-auth-token-7f3c"
	longToken := strings.Repeat("a", 4097)
	tests := []struct {
		name        string
		header      string
		configToken string
		wantStatus  int
	}{
		{name: "valid", header: "Bearer " + token, configToken: token, wantStatus: http.StatusNoContent},
		{name: "missing", configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "empty token", header: "Bearer ", configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "lowercase scheme", header: "bearer " + token, configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "tab separator", header: "Bearer\t" + token, configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "two spaces", header: "Bearer  " + token, configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "trailing field", header: "Bearer " + token + " extra", configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "trailing tab", header: "Bearer " + token + "\textra", configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "unicode token", header: "Bearer 令牌", configToken: "令牌", wantStatus: http.StatusUnauthorized},
		{name: "unicode scheme", header: "Beáer " + token, configToken: token, wantStatus: http.StatusUnauthorized},
		{name: "overlong token", header: "Bearer " + longToken, configToken: longToken, wantStatus: http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if test.header != "" {
				request.Header.Set("Authorization", test.header)
			}
			response := httptest.NewRecorder()
			called := false
			handler := AuthMiddleware(&Config{Token: test.configToken}, func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})

			handler(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if called != (test.wantStatus == http.StatusNoContent) {
				t.Fatalf("protected handler called = %v", called)
			}
		})
	}
}

func TestParseBearerTokenFuzzLikeByteCases(t *testing.T) {
	for value := 0; value <= 255; value++ {
		candidate := "left" + string([]byte{byte(value)}) + "right"
		parsed, valid := parseBearerToken("Bearer " + candidate)
		wantValid := value >= 0x21 && value <= 0x7e
		if valid != wantValid {
			t.Fatalf("byte 0x%02x validity = %v, want %v", value, valid, wantValid)
		}
		if valid && string(parsed) != candidate {
			t.Fatalf("byte 0x%02x parsed token = %q, want %q", value, parsed, candidate)
		}
	}

	validBoundary := strings.Repeat("z", maxBearerTokenLength)
	if _, valid := parseBearerToken("Bearer " + validBoundary); !valid {
		t.Fatal("maximum-length ASCII token was rejected")
	}
	if _, valid := parseBearerToken("Bearer " + validBoundary + "z"); valid {
		t.Fatal("overlong token was accepted")
	}

	for index := range bearerPrefix {
		mutated := []byte(bearerPrefix + "token")
		mutated[index] ^= 1
		if _, valid := parseBearerToken(string(mutated)); valid {
			t.Fatalf("mutated prefix at byte %d was accepted", index)
		}
	}
}

func TestAuthMiddlewareRejectsInvalidTokenWithoutLoggingOrEchoingSecrets(t *testing.T) {
	const expectedToken = "expected-auth-token-7f3c"
	const providedToken = "provided-auth-token-a91e"

	var logs bytes.Buffer
	originalWriter := logx.SetOutput(&logs)
	defer logx.SetOutput(originalWriter)

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+providedToken)
	response := httptest.NewRecorder()
	handler := AuthMiddleware(&Config{Token: expectedToken}, func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid token reached protected handler")
	})

	handler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	body := response.Body.String()
	if strings.Contains(body, expectedToken) || strings.Contains(body, providedToken) {
		t.Fatalf("response echoed an authentication token: %q", body)
	}
	output := logs.String()
	if strings.Contains(output, expectedToken) || strings.Contains(output, providedToken) {
		t.Fatalf("authentication failure log leaked a token: %q", output)
	}
}
