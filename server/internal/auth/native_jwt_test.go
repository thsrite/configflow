package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestNativeJWTInteroperatesWithPython(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	const key = "synthetic-interop-key"
	token, err := EncodeHS256(struct {
		Username string  `json:"username"`
		Expires  float64 `json:"exp"`
		IssuedAt float64 `json:"iat"`
	}{"管理员", float64(time.Now().Add(time.Hour).Unix()) + 0.5, 1.5}, key)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-c", `import jwt,sys; p=jwt.decode(sys.argv[1],sys.argv[2],algorithms=['HS256']); assert p['username']=='管理员'; assert p['iat']==1.5; print(jwt.encode({'username':'Python','custom':{'x':1},'iat':1.5},sys.argv[2],algorithm='HS256'))`, token, key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python interop: %v %s", err, output)
	}
	claims, err := DecodeHS256(strings.TrimSpace(string(output)), key, time.Now())
	if err != nil || claimString(claims.Username) != "Python" || string(claims.Extra["custom"]) != `{"x":1}` {
		t.Fatalf("%+v %v", claims, err)
	}
	// Python's permissive decoder accepts discarded junk in the signature;
	// the native URL alphabet rejects it. The signed input itself is unchanged.
	cmd = exec.Command(python, "-c", `import jwt,sys; t=jwt.encode({'username':'Python'},sys.argv[1],algorithm='HS256'); p=t.rsplit('.',1); print(p[0]+'.!!!!'+p[1]); jwt.decode(p[0]+'.!!!!'+p[1],sys.argv[1],algorithms=['HS256'])`, key)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python junk evidence: %v %s", err, output)
	}
	if _, err = DecodeHS256(strings.TrimSpace(string(output)), key, time.Now()); err == nil {
		t.Fatal("illegal signature alphabet accepted")
	}
}
