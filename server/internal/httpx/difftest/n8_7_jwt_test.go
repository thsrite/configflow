package difftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/testjson"
)

// T29 checks both directions of actual login and preserved issued-token types.
// Only the illegal signature alphabet has a named, fully asserted difference.
func TestN87JWTBoundaryAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	root := t.TempDir()
	pyDir, goDir := filepath.Join(root, "python"), filepath.Join(root, "go")
	for _, dir := range []string{pyDir, goDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		seed(t, dir)
	}
	pyBase, goBase := startPython(t, pyDir), startGo(t, goDir)
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	noExp, _ := auth.EncodeHS256(map[string]any{"username": username, "extra": map[string]any{"nested": true}}, secret)
	fractional, _ := auth.EncodeHS256(map[string]any{"username": username, "exp": float64(time.Now().Add(time.Hour).Unix()) + 0.5, "iat": 1.5}, secret)
	parts := strings.Split(strings.TrimPrefix(goToken, "Bearer "), ".")
	parts[2] = "!!!!" + parts[2]
	junk := "Bearer " + strings.Join(parts, ".")
	for _, row := range []struct {
		name, pyToken, goToken string
		pyStatus, goStatus     int
	}{
		{"cross-login", goToken, pyToken, 200, 200},
		{"no-exp", "Bearer " + noExp, "Bearer " + noExp, 200, 200},
		{"fractional-dates", "Bearer " + fractional, "Bearer " + fractional, 200, 200},
		{"junk-signature", junk, junk, 200, 401},
	} {
		t.Run(row.name, func(t *testing.T) {
			req := request{name: row.name, method: "GET", path: "/api/auth/verify", auth: "session"}
			py := send(t, pyBase, req, map[string]string{"session": row.pyToken})
			goRes := send(t, goBase, req, map[string]string{"session": row.goToken})
			if py.status != row.pyStatus || goRes.status != row.goStatus {
				t.Fatalf("Python %d %s; Go %d %s", py.status, py.body, goRes.status, goRes.body)
			}
			if row.name == "junk-signature" {
				wantPy := map[string]any{"success": true, "message": "登录状态有效。"}
				wantGo := map[string]any{"success": false, "message": "登录已失效，请重新登录。"}
				pv, _ := testjson.Parse(py.body)
				gv, _ := testjson.Parse(goRes.body)
				if !testjson.Equal(pv, wantPy) || !testjson.Equal(gv, wantGo) {
					t.Fatalf("Python %s; Go %s", py.body, goRes.body)
				}
			} else if diffs := compare(req, py, goRes); len(diffs) > 0 {
				t.Fatal(diffs)
			}
		})
	}
}
