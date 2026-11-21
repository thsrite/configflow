package auth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/testjson"
)

type pyjwtResult struct {
	OK      bool            `json:"ok"`
	Claims  json.RawMessage `json:"claims"`
	Message string          `json:"message"`
	Expired bool            `json:"expired"`
}

type pyjwtCase struct {
	Name   string      `json:"name"`
	Token  string      `json:"token"`
	Result pyjwtResult `json:"result"`
}

type pyjwtDoc struct {
	Secret string      `json:"secret"`
	Cases  []pyjwtCase `json:"cases"`
}

func loadPyJWT(t *testing.T) pyjwtDoc {
	t.Helper()
	data, err := os.ReadFile("testdata/pyjwt_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc pyjwtDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// nativeNumericDate lists the PyJWT rows decided differently on purpose
// (O18 C17): iat/nbf/exp must be JSON numbers, so a numeric string or a
// boolean (int("…")/int(True) in PyJWT) makes the token invalid.
var nativeNumericDate = map[string]pyjwtResult{
	"exp_string":     {Message: "Expiration Time claim (exp) must be a number."},
	"exp_bad_string": {Message: "Expiration Time claim (exp) must be a number."},
	"exp_bool":       {Message: "Expiration Time claim (exp) must be a number."},
	"iat_bad":        {Message: "Issued At claim (iat) must be a number."},
}

// Every token PyJWT 2.8.0 decoded (or refused) decodes the same way here,
// with the same error text, except the nativeNumericDate rows.
func TestDecodeMatchesPyJWT(t *testing.T) {
	doc := loadPyJWT(t)
	now := time.Now()
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if native, ok := nativeNumericDate[c.Name]; ok {
				c.Result = native
			}
			claims, err := DecodeHS256(c.Token, doc.Secret, now)
			if c.Name == "junk_chars" {
				if err == nil {
					t.Fatal("native must reject junk token")
				}
				return
			}
			if c.Result.OK {
				if err != nil {
					t.Fatalf("PyJWT accepted, Go refused: %v", err)
				}
				if diff := testjson.Diff(claims, c.Result.Claims); diff != "" {
					t.Fatalf("claims mismatch:\n%s", diff)
				}
				return
			}
			if err == nil {
				t.Fatalf("PyJWT refused (%s), Go accepted", c.Result.Message)
			}
			te, ok := err.(*TokenError)
			if !ok {
				t.Fatalf("want TokenError, got %T %v", err, err)
			}
			if (c.Name != "header_bad_json" && c.Name != "payload_bad_json" && te.Message != c.Result.Message) || te.Expired != c.Result.Expired {
				t.Fatalf("got %q expired=%v, want %q expired=%v", te.Message, te.Expired,
					c.Result.Message, c.Result.Expired)
			}
		})
	}
}

// Issued tokens are validated by Python in the HTTP T29 differential. Token
// bytes deliberately follow encoding/json rather than PyJWT's ASCII encoder.
func TestIssuedTokensCarryExactClaims(t *testing.T) {
	previous := Configure(Config{SecretKey: "synthetic-key", Now: func() time.Time { return time.Unix(1800000000, 0) }})
	defer Configure(previous)
	token, err := GenerateToken("管理员")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := DecodeHS256(token, "synthetic-key", time.Unix(1800000000, 0))
	if err != nil || claimString(claims.Username) != "管理员" || string(claims.Expires) != "1800086400" {
		t.Fatalf("%+v %v", claims, err)
	}
}

func TestParseBearerTokenAcceptsExactPrintableASCIIToken(t *testing.T) {
	for name, c := range map[string][2]string{
		"printable-ascii": {"Bearer abc.DEF-_~+/=", "abc.DEF-_~+/="},
		"maximum-length":  {"Bearer " + strings.Repeat("x", MaxAuthTokenLength), strings.Repeat("x", MaxAuthTokenLength)},
	} {
		if got, ok := ParseBearerToken(c[0], true); !ok || got != c[1] {
			t.Errorf("%s: got %q %v", name, got, ok)
		}
	}
}

func TestParseBearerTokenRejectsMalformedHeaders(t *testing.T) {
	if _, ok := ParseBearerToken("", false); ok {
		t.Error("none")
	}
	for _, header := range []string{"", "Bearer ", "Bearer  token", "Bearer\ttoken", "Bearer token extra",
		" Bearer token", "Bearer token ", "bearer token", "Basic token", "Bearer café", "Bearer 令牌🔐",
		"Bearer token\tmore", "Bearer token\n", "Bearer " + strings.Repeat("x", MaxAuthTokenLength+1)} {
		if got, ok := ParseBearerToken(header, true); ok {
			t.Errorf("%q accepted as %q", header, got)
		}
	}
}

func TestIsTokenWithinLength(t *testing.T) {
	if IsTokenWithinLength("") || !IsTokenWithinLength("令") || IsTokenWithinLength(strings.Repeat("令", MaxAuthTokenLength+1)) ||
		!IsTokenWithinLength(strings.Repeat("令", MaxAuthTokenLength)) {
		t.Error("length bounds")
	}
}

func TestVerifyTokenRoundTripAndExpiry(t *testing.T) {
	clock := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	previous := Configure(Config{SecretKey: "s", Now: func() time.Time { return clock }})
	defer Configure(previous)
	token, _ := GenerateToken("admin")
	result, err := VerifyToken(token)
	if err != nil || result.Failed() || claimString(result.Claims.Username) != "admin" {
		t.Fatalf("%v %+v", err, result)
	}
	clock = clock.Add(24 * time.Hour)
	if result, _ := VerifyToken(token); result.Error != "expired" {
		t.Fatalf("expired token: %+v", result)
	}
	if result, _ := VerifyToken("abc"); result.Error != "invalid" || result.Detail != "Not enough segments" {
		t.Fatalf("garbage: %+v", result)
	}
}
