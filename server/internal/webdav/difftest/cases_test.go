package difftest

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/jsonx"
)

const connection = `{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"******","webdav_path":"one/two"}`

func TestBackupRoutesAgainstFlask(t *testing.T) {
	w := newWorld(t)
	w.call("GET", "/api/backup/config", "", true, false)
	for _, endpoint := range []string{"config", "test", "now"} {
		for _, body := range []string{`null`, `[]`, `"text"`, `{}`, `{"unknown":true}`, `{"webdav_url":null}`, `{"auto_backup":"false"}`, `{"keep_latest":true}`, `{"keep_latest":1001}`, `{"keep_latest":-1}`, `{"keep_daily":3651}`, `{"keep_latest":1.5}`, `{"keep_latest":null}`, `{"webdav_username":3}`} {
			w.call("POST", "/api/backup/"+endpoint, body, true, false)
		}
	}
	w.call("POST", "/api/backup/config", `{"keep_latest":0,"keep_daily":3650,"webdav_password":"******","webdav_path":"/new/"}`, true, false)
	w.call("POST", "/api/backup/test", connection, false, false)
	w.call("POST", "/api/backup/now", connection, false, false)
	w.call("POST", "/api/backup/test", connection, true, false)
	w.call("POST", "/api/backup/now", connection, true, false)
	w.call("POST", "/api/backup/test", `{"auto_backup":false}`, true, false)
	w.call("POST", "/api/backup/now", `{"auto_backup":false}`, true, false)
}
func TestWebDAVFailuresAndRetentionAgainstFlask(t *testing.T) {
	for _, method := range []string{"", "MKCOL", "PUT", "PROPFIND", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			w := newWorld(t)
			for _, d := range []*dav{w.pythonDAV, w.goDAV} {
				for i := range 4 {
					d.files[fmt.Sprintf("/one/two/config_backup_20200101_10000%d_000000.json", i)] = []byte(`{}`)
				}
				d.files["/one/two/foreign.txt"] = []byte(`"keep"`)
				d.setFailure(method, 403)
			}
			body := `{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"******","webdav_path":"one/two","keep_latest":1,"keep_daily":0}`
			w.call("POST", "/api/backup/test", body, true, false)
			w.call("POST", "/api/backup/now", body, true, false)
		})
	}
}

func TestConnectionRefusedByDAVMatchesFlask(t *testing.T) {
	w := newWorld(t)
	for _, d := range []*dav{w.pythonDAV, w.goDAV} {
		d.setFailure("HEAD", 403)
	}
	w.call("POST", "/api/backup/test", connection, true, false)
}
func TestAutomaticBackupFollowsCommittedConfig(t *testing.T) {
	w := newWorld(t)
	w.call("POST", "/api/backup/config", `{"auto_backup":true,"keep_latest":1,"keep_daily":0}`, true, true)
	w.call("POST", "/api/backup/config", `{"webdav_path":"/second/"}`, true, true)
	for _, d := range []*dav{w.pythonDAV, w.goDAV} {
		d.setFailure("PUT", 403)
	}
	w.call("POST", "/api/backup/config", `{"webdav_path":"/failure/"}`, true, true)
	w.call("POST", "/api/backup/config", `{"auto_backup":false}`, true, false)
}

func TestAutomaticChangesCoalesceAndPendingDisableCancels(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprint(disable), func(t *testing.T) {
			w := newWorld(t)
			w.call("POST", "/api/backup/config", `{"auto_backup":true}`, true, false)
			for i := range 5 {
				w.call("POST", "/api/backup/config", fmt.Sprintf(`{"webdav_path":"/pending-%d/"}`, i), true, false)
			}
			last := `{"keep_latest":2}`
			if disable {
				last = `{"auto_backup":false}`
			}
			w.call("POST", "/api/backup/config", last, true, true)
			want := 1
			if disable {
				want = 0
			}
			w.goDAV.mu.Lock()
			count := w.goDAV.uploads
			w.goDAV.mu.Unlock()
			if count != want {
				t.Fatalf("upload count %d want %d", count, want)
			}
		})
	}
}
func TestImportedRetentionFallbackMatchesFlask(t *testing.T) {
	w := newWorldSaved(t, jsonx.Obj("keep_latest", "5", "keep_daily", -3))
	for _, d := range []*dav{w.pythonDAV, w.goDAV} {
		for i := range 31 {
			d.files[fmt.Sprintf("/one/two/config_backup_20200101_%06d_000000.json", i)] = []byte(`{}`)
		}
	}
	w.call("GET", "/api/backup/config", "", true, false)
	w.call("POST", "/api/backup/now", connection, true, false)
}

func TestApprovedEditorSecurityCorrections(t *testing.T) {
	t.Run("URL userinfo", func(t *testing.T) {
		w := newWorld(t)
		w.call("POST", "/api/backup/config", `{"webdav_url":"https://uri-user:uri-private-secret@example.com/dav/"}`, true, false)
		w.callApproved("GET", "/api/backup/config", "", true, false, func(py *answer) {
			body := py.Body.(map[string]any)
			if body["webdav_url"] != "https://uri-user:uri-private-secret@example.com/dav/" {
				t.Fatal("Python credential exposure contract changed; revisit approved correction")
			}
			body["webdav_url"] = "https://example.com/dav/"
		})
	})
	t.Run("blank password", func(t *testing.T) {
		w := newWorld(t)
		w.call("POST", "/api/backup/config", `{"webdav_password":"******"}`, true, false)
		w.callApproved("POST", "/api/backup/config", `{"webdav_password":""}`, true, false, func(py *answer) {
			state := py.State.(map[string]any)
			doc := state["document"].(map[string]any)
			backup := doc["system"].(map[string]any)["backup"].(map[string]any)
			if backup["webdav_password"] != "" {
				t.Fatal("Python blank-password behavior changed; revisit approved correction")
			}
			backup["webdav_password"] = "fixture-password"
		})
	})
}

func TestManualRequestOverridesMalformedSavedConnection(t *testing.T) {
	w := newWorldSaved(t, jsonx.Obj("webdav_username", 42, "webdav_path", 17))
	body := `{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"fixture-password","webdav_path":"one/two"}`
	w.call("POST", "/api/backup/now", body, true, false)
}

func TestWebDAVDirectoryVariantsAgainstFlask(t *testing.T) {
	for _, path := range []string{"", "/", "one//two", "/directory with spaces/", "/备份/", "/a%2Fb/"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t)
			encodedPath, _ := json.Marshal(path)
			body := fmt.Sprintf(`{"webdav_url":"<DAV>","webdav_username":"fixture-user","webdav_password":"******","webdav_path":%s}`, encodedPath)
			w.call("POST", "/api/backup/test", string(body), true, false)
			w.call("POST", "/api/backup/now", string(body), true, false)
		})
	}
}
