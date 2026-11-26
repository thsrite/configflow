package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func signedSegments(header, payload, key string) string {
	input := b64(header) + "." + b64(payload)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestNativeJWTVerifiesReceivedBytes(t *testing.T) {
	const key = "synthetic-jwt-key"
	raw := `{ "extra": {"order": [2,1]}, "username": "\u7ba1\u7406\u5458", "exp": 2000000000.75, "iat": 1.5 }`
	token := signedSegments(`{"typ":"JWT", "alg":"HS256"}`, raw, key)
	claims, err := DecodeHS256(token, key, time.Unix(1900000000, 0))
	if err != nil || claimString(claims.Username) != "管理员" || string(claims.Expires) != "2000000000.75" {
		t.Fatalf("%+v %v", claims, err)
	}
	encoded, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[1] = b64(string(encoded))
	if _, err = DecodeHS256(strings.Join(parts, "."), key, time.Unix(1900000000, 0)); err == nil {
		t.Fatal("re-encoding payload without re-signing was accepted")
	}
	// A no-exp session and unknown fields survive; only HS256 is accepted.
	if _, err = DecodeHS256(signedSegments(`{"alg":"HS256"}`, `{"custom":[1,2]}`, key), key, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, alg := range []string{"none", "HS512", "RS256"} {
		if _, err = DecodeHS256(signedSegments(`{"alg":"`+alg+`"}`, `{}`, key), key, time.Now()); err == nil {
			t.Fatal(alg)
		}
	}
}

func TestNativeAudienceKeepsEmptyForms(t *testing.T) {
	for _, audience := range []string{`null`, `false`, `0`, `0.0`, `0e1`, `""`, `[ ]`, `{ }`} {
		token := signedSegments(`{"alg":"HS256"}`, `{"aud":`+audience+`}`, "synthetic-key")
		if _, err := DecodeHS256(token, "synthetic-key", time.Now()); err != nil {
			t.Fatalf("%s: %v", audience, err)
		}
	}
}

func FuzzNativeJWT(f *testing.F) {
	for _, token := range []string{"abc", signedSegments(`{"alg":"HS256"}`, `{"exp":2000000000.5,"unknown":[1,true]}`, "synthetic-key"), signedSegments(`{"alg":"none"}`, `{}`, "synthetic-key")} {
		f.Add(token)
	}
	f.Fuzz(func(t *testing.T, token string) {
		claims, err := DecodeHS256(token, "synthetic-key", time.Unix(1900000000, 0))
		if err == nil {
			data, err := json.Marshal(claims)
			if err != nil || !json.Valid(data) {
				t.Fatalf("%s %v", data, err)
			}
		}
	})
}

func TestNativeJWTAcceptsOptionalSegmentPadding(t *testing.T) {
	const key = "synthetic-padded-key"
	input := base64.URLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + base64.URLEncoding.EncodeToString([]byte(`{"username":"x"}`))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(input))
	token := input + "." + base64.URLEncoding.EncodeToString(mac.Sum(nil))
	claims, err := DecodeHS256(token, key, time.Now())
	if err != nil || claimString(claims.Username) != "x" {
		t.Fatalf("%+v %v", claims, err)
	}
}
