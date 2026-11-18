package difftest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/deploy"
)

var artifactID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func artifactRequest(w *world, method, path string, body *string) request {
	r := request{Method: method, Path: path, Body: body, Headers: map[string]string{"Authorization": w.token}}
	if body != nil {
		r.Headers["Content-Type"] = "application/json"
	}
	return r
}

func decodeArtifact(t *testing.T, a answer, profile, service string) deploy.Artifact {
	t.Helper()
	if a.Status != 200 {
		t.Fatalf("artifact status %d: %v", a.Status, a.Body)
	}
	if headers(a)["Cache-Control"] != "no-store" {
		t.Fatal("artifact cache header")
	}
	raw, err := json.Marshal(a.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v deploy.Artifact
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if !v.Success || !artifactID.MatchString(v.ID) || !artifactID.MatchString(v.DeploymentID) || v.AgentID != "manual" || v.ProfileID != profile || v.ServiceType != service || len(v.TargetIDs) != 0 || v.ExpiresAt < time.Now().Unix()+3500 || v.ExpiresAt > time.Now().Unix()+3601 {
		t.Fatal("invalid frozen artifact metadata")
	}
	return v
}

// artifactState checks private ownership/manifest consistency before excluding
// random directory names and compressed archive encoding from cross-language
// comparison. The complete database and every other persisted file still compare.
func artifactState(t *testing.T, a answer, v deploy.Artifact) (any, any) {
	t.Helper()
	state := a.State.(map[string]any)
	files := state["files"].(map[string]any)
	path := ".generated-previews/" + v.ID + "/record.json"
	record, ok := files[path].(map[string]any)
	if !ok {
		t.Fatal("missing persisted preview")
	}
	for key, want := range map[string]any{"id": v.ID, "agent_id": "manual", "profile_id": v.ProfileID, "service_type": v.ServiceType, "deployment_id": v.DeploymentID, "expires_at": float64(v.ExpiresAt)} {
		compare(t, "persisted "+key, record[key], want)
	}
	meta := record["bundle_meta"].(map[string]any)
	manifest := meta["manifest"].(map[string]any)
	for key, want := range map[string]any{"agent_id": "manual", "profile_id": v.ProfileID, "service_type": v.ServiceType, "deployment_id": v.DeploymentID, "config_path": v.ConfigPath} {
		compare(t, "manifest "+key, manifest[key], want)
	}
	compare(t, "persisted manifest files", manifest["files"], v.Files)
	source := record["source"].(map[string]any)
	compare(t, "source profile", source["profile_id"], v.ProfileID)
	if _, exists := source["agents"]; exists {
		t.Fatal("manual source captured Agent credentials")
	}
	if record["config_revision"] == nil || record["input_digest"] == "" {
		t.Fatal("missing snapshot generation")
	}
	if _, ok := files[".generated-previews/"+v.ID+"/bundle.tar.gz"]; !ok {
		t.Fatal("missing frozen archive")
	}
	for path := range files {
		if strings.HasPrefix(path, ".generated-previews/") {
			delete(files, path)
		}
	}
	return state, map[string]any{"source": source, "config_revision": record["config_revision"]}
}

func checkArtifactFiles(t *testing.T, a answer, v deploy.Artifact) map[string]any {
	t.Helper()
	if a.Status != 200 || headers(a)["Cache-Control"] != "no-store" || headers(a)["Content-Disposition"] != "attachment; filename="+v.ServiceType+"-final-config.zip" {
		t.Fatalf("download protocol: %d %v", a.Status, headers(a))
	}
	files, ok := a.Body.(map[string]any)
	if !ok || len(files) != len(v.Files) {
		t.Fatal("download file count")
	}
	seen := map[string]bool{}
	for _, file := range v.Files {
		data, ok := files[file.Path].(string)
		if !ok || seen[file.Path] {
			t.Fatal("missing/duplicate file")
		}
		seen[file.Path] = true
		if a.ZIPModes[file.Path] != 0600 {
			t.Fatal("frozen ZIP file permissions differ from private configuration contract")
		}
		sum := sha256.Sum256([]byte(data))
		if int64(len(data)) != file.Size || hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatal("digest/size do not describe frozen bytes")
		}
		if file.Path == v.ConfigPath && (data != v.Content || file.Role != deploy.RoleConfig) {
			t.Fatal("preview differs from final main file")
		}
	}
	return files
}

func TestArtifactsFlaskSuccessfulGenerationAndReads(t *testing.T) {
	for _, prefix := range []string{"/api/generate", "/api/profiles/office/generate"} {
		for _, service := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			t.Run(prefix+"/"+service, func(t *testing.T) {
				w := newWorld(t)
				profile := "default"
				path := prefix + "/" + service + "/artifact"
				if strings.Contains(prefix, "office") {
					profile = "office"
					path += "?profile_id=default"
				}
				r := artifactRequest(w, "POST", path, body(map[string]string{"base_url": "http://localhost"}))
				py := w.py.send(t, r)
				goAnswer := sendGo(t, w.srv, r)
				gp, pp := decodeArtifact(t, goAnswer, profile, service), decodeArtifact(t, py, profile, service)
				gstate, gsource := artifactState(t, goAnswer, gp)
				pstate, psource := artifactState(t, py, pp)
				compare(t, "persisted non-artifact state", gstate, pstate)
				compare(t, "captured source generation", gsource, psource)
				compare(t, "generation headers", headers(goAnswer), headers(py))
				compare(t, "content", gp.Content, pp.Content)
				compare(t, "hosts", gp.SubscriptionHosts, pp.SubscriptionHosts)
				compare(t, "dialers", gp.SubscriptionDialers, pp.SubscriptionDialers)
				if gp.ConfigPath != pp.ConfigPath || len(gp.Files) != len(pp.Files) {
					t.Fatal("different file layout")
				}
				gbase, pbase := "/api/generate/artifacts/"+gp.ID, "/api/generate/artifacts/"+pp.ID
				gd := sendGo(t, w.srv, artifactRequest(w, "GET", gbase+"/download", nil))
				pd := w.py.send(t, artifactRequest(w, "GET", pbase+"/download", nil))
				gfiles, pfiles := checkArtifactFiles(t, gd, gp), checkArtifactFiles(t, pd, pp)
				for i, file := range gp.Files {
					if file.Path != pp.Files[i].Path || file.Role != pp.Files[i].Role {
						t.Fatal("manifest order/role differs")
					}
					compare(t, "download "+file.Path, gfiles[file.Path], pfiles[file.Path])
					gr := sendGo(t, w.srv, artifactRequest(w, "GET", gbase+"/file?path="+url.QueryEscape(file.Path), nil))
					pr := w.py.send(t, artifactRequest(w, "GET", pbase+"/file?path="+url.QueryEscape(file.Path), nil))
					compare(t, "read status", gr.Status, pr.Status)
					compare(t, "read headers", headers(gr), headers(pr))
					if gr.Body != gfiles[file.Path] || pr.Body != pfiles[file.Path] {
						t.Fatal("file read diverges from exported bytes")
					}
				}
				for _, action := range []string{"file?path=../record.json", "file?path=missing", "unknown"} {
					gr := sendGo(t, w.srv, artifactRequest(w, "GET", gbase+"/"+action, nil))
					pr := w.py.send(t, artifactRequest(w, "GET", pbase+"/"+action, nil))
					compare(t, "invalid read status", gr.Status, pr.Status)
					compare(t, "invalid read headers", headers(gr), headers(pr))
					compare(t, "invalid read body", gr.Body, pr.Body)
				}
			})
		}
	}
}

func TestArtifactsFlaskErrorClasses(t *testing.T) {
	w := newWorld(t)
	for _, r := range []request{
		post("/api/generate/mihomo/artifact", []string{"non-object"}),
		post("/api/generate/unknown/artifact", map[string]any{}),
		post("/api/profiles/missing/generate/mihomo/artifact", map[string]any{}),
		{Method: "GET", Path: "/api/generate/artifacts/invalid/download"},
		{Method: "GET", Path: "/api/generate/artifacts/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/file?path=config.yaml"},
		{Method: "GET", Path: "/api/generate/artifacts/invalid/unknown"},
		{Method: "POST", Path: "/api/generate/surge/artifact", Body: raw(`{}`), Headers: map[string]string{"Authorization": "", "Content-Type": "application/json"}},
		{Method: "GET", Path: "/api/generate/artifacts/invalid/download", Headers: map[string]string{"Authorization": ""}},
	} {
		w.check(r)
	}
}

// These are C34's already-approved boundary differences: strict malformed
// JSON and text-only base_url, without adding coercion or exception emulation.
func TestArtifactsApprovedRequestBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, raw              string
		goStatus, pythonStatus int
	}{
		{"malformed", `{`, 400, 500},
		{"base-type", `{"base_url":true}`, 400, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			before := snapshot(t, w.srv)
			r := artifactRequest(w, "POST", "/api/generate/mihomo/artifact", raw(tc.raw))
			py := w.py.send(t, r)
			got := sendGo(t, w.srv, r)
			if got.Status != tc.goStatus || py.Status != tc.pythonStatus {
				t.Fatalf("request boundary Go=%d Flask=%d", got.Status, py.Status)
			}
			compare(t, "rejected request writes nothing", got.State, before)
			if tc.name == "base-type" {
				v := decodeArtifact(t, py, "default", "mihomo")
				artifactState(t, py, v)
				message := got.Body.(map[string]any)["message"]
				if message != "base_url 必须为文本" {
					t.Fatal("wrong validation message")
				}
			} else {
				compare(t, "malformed reference writes nothing", py.State, before)
			}
		})
	}
}

func TestArtifactsFlaskExpiredAndForeignOwnership(t *testing.T) {
	for _, kind := range []string{"expired", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			w := newWorld(t)
			r := artifactRequest(w, "POST", "/api/generate/surge/artifact", body(map[string]string{}))
			py := w.py.send(t, r)
			got := sendGo(t, w.srv, r)
			gp, pp := decodeArtifact(t, got, "default", "surge"), decodeArtifact(t, py, "default", "surge")
			pythonRoot := ""
			for _, env := range w.py.cmd.Env {
				if strings.HasPrefix(env, "DATA_DIR=") {
					pythonRoot = strings.TrimPrefix(env, "DATA_DIR=")
				}
			}
			if pythonRoot == "" {
				t.Fatal("missing private oracle root")
			}
			for _, item := range []struct{ root, id string }{{w.srv.DataDir, gp.ID}, {pythonRoot, pp.ID}} {
				path := filepath.Join(item.root, ".generated-previews", item.id, "record.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var record map[string]json.RawMessage
				if err := json.Unmarshal(raw, &record); err != nil {
					t.Fatal(err)
				}
				if kind == "expired" {
					record["expires_at"] = json.RawMessage(`1`)
				} else {
					record["agent_id"] = json.RawMessage(`"foreign-agent"`)
				}
				raw, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshot(t, w.srv)
			var pythonState any
			for _, action := range []string{"file?path=surge.conf", "download"} {
				g := sendGo(t, w.srv, artifactRequest(w, "GET", "/api/generate/artifacts/"+gp.ID+"/"+action, nil))
				p := w.py.send(t, artifactRequest(w, "GET", "/api/generate/artifacts/"+pp.ID+"/"+action, nil))
				if g.Status != 409 || p.Status != 409 {
					t.Fatalf("ownership/expiry Go=%d Flask=%d", g.Status, p.Status)
				}
				compare(t, "conflict response", g.Body, p.Body)
				compare(t, "conflict headers", headers(g), headers(p))
				compare(t, "Go conflict read writes nothing", g.State, before)
				if pythonState != nil {
					compare(t, "Flask conflict read writes nothing", p.State, pythonState)
				}
				pythonState = p.State
			}
		})
	}
}

func TestArtifactsFlaskGenerationFailureDoesNotSavePreview(t *testing.T) {
	w := newWorld(t)
	w.check(post("/api/profiles/default/import", map[string]any{"mihomo": map[string]string{"custom_config": "proxies: ["}}))
	before := snapshot(t, w.srv)
	r := artifactRequest(w, "POST", "/api/generate/mihomo/artifact", body(map[string]string{}))
	py := w.py.send(t, r)
	got := sendGo(t, w.srv, r)
	if got.Status != 500 || py.Status != 500 {
		t.Fatalf("generation error Go=%d Flask=%d", got.Status, py.Status)
	}
	compare(t, "generation failure headers", headers(got), headers(py))
	// Parser-internal diagnostics belong to the native YAML implementation;
	// status, rejection envelope, and all durable effects remain comparable.
	for _, a := range []answer{got, py} {
		object := a.Body.(map[string]any)
		message, ok := object["message"].(string)
		if !ok || message == "" || object["success"] != false || len(object) != 2 {
			t.Fatal("invalid generation failure envelope")
		}
		compare(t, "generation failure leaves no artifact", a.State, before)
	}
}
