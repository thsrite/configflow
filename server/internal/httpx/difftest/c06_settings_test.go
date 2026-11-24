package difftest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model/modeltest"
	"configflow/internal/repo/repotest"
	"configflow/internal/testjson"
)

type settingsCase struct {
	Name, Method, Path, Body, ContentType, Seed string
	Difference                                  string
	storedBackup                                json.RawMessage
}
type settingsResult struct {
	Status        int
	Body          json.RawMessage
	Before, After json.RawMessage
}

// versionOneMeta shows storage-version-2 meta rows as Python's version 1
// (O17: the settings tests store no Agent state, so only the version and the
// two state counters differ).
func versionOneMeta(rows [][]any) [][]any {
	kept := rows[:0]
	for _, row := range rows {
		switch row[0] {
		case "state_generation", "state_commit_id":
			continue
		case "storage_version":
			row[1] = float64(1) // decoded like the Python side (json.Unmarshal into any)
		}
		kept = append(kept, row)
	}
	return kept
}

func settingsSnapshot(t *testing.T, dir string) json.RawMessage {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "configflow.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tables := map[string][][]any{}
	for _, table := range []struct{ name, order string }{{"meta", "key"}, {"sections", "scope, owner"}, {"fields", "scope, owner, field"}} {
		rows, err := db.Query("SELECT * FROM " + table.name + " ORDER BY " + table.order)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		values := [][]any{}
		for rows.Next() {
			row := make([]any, len(columns))
			targets := make([]any, len(row))
			for i := range row {
				targets[i] = &row[i]
			}
			if err := rows.Scan(targets...); err != nil {
				t.Fatal(err)
			}
			for i, column := range columns {
				if column == "value" {
					text, ok := row[i].(string)
					if !ok {
						t.Fatalf("SQL value %T", row[i])
					}
					if err := json.Unmarshal([]byte(text), &row[i]); err != nil {
						t.Fatal(err)
					}
				}
			}
			values = append(values, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if table.name == "meta" {
			values = versionOneMeta(values)
		}
		tables[table.name] = values
	}
	files := map[string]any{}
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.Contains(entry.Name(), ".sqlite3") || strings.HasSuffix(path, ".lock") || strings.HasSuffix(path, ".log") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any = string(data)
		if strings.HasSuffix(path, ".json") {
			if err := json.Unmarshal(data, &value); err != nil {
				return err
			}
		}
		relative, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(relative)] = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"tables": tables, "files": files})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func normalizedSettingsState(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var normalize func(any) any
	normalize = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				if k == "created_at" || k == "updated_at" {
					x[k] = "<timestamp>"
				} else {
					x[k] = normalize(item)
				}
			}
		case []any:
			if len(x) == 2 && x[0] == "commit_id" {
				x[1] = "<random-commit-id>"
			}
			if len(x) >= 5 && (x[2] == "updated_at" || x[2] == "created_at") {
				x[4] = "<timestamp>"
			}
			if len(x) >= 5 {
				scope, scoped := x[0].(string)
				if field, ok := x[2].(string); ok && scoped {
					x[4] = storedRowThroughModel(scope, field, x[4])
				}
			}
			for i, item := range x {
				x[i] = normalize(item)
			}
		}
		return v
	}
	return normalize(value)
}

// storedRowThroughModel reads a stored row value as the plain records hold
// it (O18 T1c): undeclared and empty optional members are left out and stored
// values read with the stored-data rules. Go stores the subscription
// aggregation switch as its truth value where Python kept the request value;
// Python read it with truthiness and a missing value as off, so off is
// compared as absent.
func storedRowThroughModel(scope, field string, value any) any {
	value = testjson.WithoutEmptyMembers(modeltest.RowThroughModel(scope, field, value))
	if config, ok := value.(map[string]any); ok && scope == "system" && field == "system_config" {
		if config["subscription_aggregation_enabled"] == false {
			delete(config, "subscription_aggregation_enabled")
		}
	}
	return value
}

func TestC06SettingsFlaskStorageBoundary(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	const J = "application/json"
	var cases []settingsCase
	for _, path := range []string{"/api/settings/web-port", "/api/settings/rule-fetch-proxy", "/api/server-domain", "/api/config-token", "/api/backup/config", "/api/backup/test", "/api/backup/now", "/api/settings/subscription-aggregation", "/api/settings/dependencies/sub-store/mode"} {
		strict := path == "/api/server-domain" || path == "/api/config-token" || path == "/api/settings/subscription-aggregation"
		for _, body := range []string{"{", "[]", "null", "true", "42", `"text"`, "{}", `{"unrecognized":true}`, `{"Mode":"always","NEW_DOMAIN":"https://changed.example.com","TOKEN":"changed-token","ENABLED":true,"PORT":18082,"KEEP_LATEST":1}`} {
			difference := ""
			if strict && (body == "{" || !strings.HasPrefix(body, "{")) {
				difference = "native-binding"
			}
			cases = append(cases, settingsCase{Name: path + " body " + body, Method: "POST", Path: path, Body: body, ContentType: J, Difference: difference})
		}
		difference := ""
		if strict {
			difference = "native-media-type"
		}
		cases = append(cases, settingsCase{Name: path + " text MIME", Method: "POST", Path: path, Body: "{}", ContentType: "text/plain", Difference: difference})
		for _, token := range []string{"NaN", "Infinity", "-Infinity"} {
			body := fmt.Sprintf(`{"port":%s,"rule_fetch_proxy":%s,"new_domain":%s,"token":%s,"keep_latest":%s,"enabled":%s,"mode":%s}`, token, token, token, token, token, token, token)
			cases = append(cases, settingsCase{Name: path + " " + token, Method: "POST", Path: path, Body: body, ContentType: J, Difference: "native-nonfinite"})
		}
	}
	for _, tc := range []settingsCase{
		{Name: "save domain", Method: "POST", Path: "/api/server-domain", Body: `{"new_domain":" https://panel.example.com "}`},
		{Name: "save token", Method: "POST", Path: "/api/config-token", Body: `{"token":" pull-secret "}`},
		{Name: "delete token", Method: "DELETE", Path: "/api/config-token"},
		{Name: "save proxy", Method: "POST", Path: "/api/settings/rule-fetch-proxy", Body: `{"rule_fetch_proxy":" http://user:synthetic-secret@proxy.example.com:8080 "}`},
		{Name: "clear proxy", Method: "POST", Path: "/api/settings/rule-fetch-proxy", Body: `{"rule_fetch_proxy":""}`},
		{Name: "invalid proxy", Method: "POST", Path: "/api/settings/rule-fetch-proxy", Body: `{"rule_fetch_proxy":"socks5://user:synthetic-secret@proxy.example.com:1080"}`},
		{Name: "save backup", Method: "POST", Path: "/api/backup/config", Body: `{"webdav_url":"https://backup.example.com","webdav_username":"operator","webdav_password":"synthetic-secret","keep_latest":7,"auto_backup":false}`},
		{Name: "save aggregation", Method: "POST", Path: "/api/settings/subscription-aggregation", Body: `{"enabled":true}`},
		{Name: "legacy aggregation value", Method: "POST", Path: "/api/settings/subscription-aggregation", Body: `{"enabled":[1]}`},
	} {
		tc.ContentType = J
		cases = append(cases, tc)
	}
	for _, path := range []string{"/api/settings/web-port", "/api/settings/rule-fetch-proxy", "/api/server-domain", "/api/config-token", "/api/backup/config", "/api/settings/subscription-aggregation"} {
		cases = append(cases, settingsCase{Name: path + " HEAD readonly", Method: "HEAD", Path: path,
			Body: `{"port":18082,"rule_fetch_proxy":"http://proxy.example.com:8080","new_domain":"https://changed.example.com","token":"changed-token","webdav_url":"https://backup.example.com","enabled":true}`, ContentType: J, Difference: "head-readonly"})
	}
	runSettingsBoundary(t, cases)
}

// TestReviewC06SettingsMemberTypes checks the wire types and last-member
// semantics at the real Flask boundary, including the complete persisted state.
func TestReviewC06SettingsMemberTypes(t *testing.T) {
	var cases []settingsCase
	for _, field := range []struct{ path, key string }{
		{"/api/server-domain", "new_domain"},
		{"/api/config-token", "token"},
		{"/api/settings/rule-fetch-proxy", "rule_fetch_proxy"},
		{"/api/backup/config", "webdav_username"},
	} {
		for _, value := range []string{"null", "false", "17", "[]", "{}"} {
			cases = append(cases, settingsCase{Name: field.key + "=" + value, Method: "POST", Path: field.path,
				Body: fmt.Sprintf(`{"%s":%s}`, field.key, value), ContentType: "application/json"})
		}
	}
	for _, body := range []string{
		`{"keep_latest":1.0}`, `{"keep_latest":1e0}`, `{"keep_latest":true}`, `{"keep_latest":null}`,
		`{"keep_latest":1001}`, `{"keep_daily":3651}`, `{"keep_latest":0,"keep_daily":3650}`,
		`{"webdav_username":"first","webdav_username":null}`,
		`{"webdav_username":null,"webdav_username":"operator"}`,
		`{"keep_latest":1,"KEEP_LATEST":1001}`,
	} {
		cases = append(cases, settingsCase{Name: body, Method: "POST", Path: "/api/backup/config", Body: body, ContentType: "application/json"})
	}
	for _, body := range []string{
		`{"enabled":{"first":true},"enabled":{"second":false}}`,
		`{"enabled":true,"Enabled":false}`, `{"enabled":null}`, `{"enabled":"false"}`,
	} {
		cases = append(cases, settingsCase{Name: body, Method: "POST", Path: "/api/settings/subscription-aggregation", Body: body, ContentType: "application/json"})
	}
	runSettingsBoundary(t, cases)
}

func runSettingsBoundary(t *testing.T, cases []settingsCase) {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	const J = "application/json"
	var worlds []*httpxtest.Server
	for i := range cases {
		s := httpxtest.New(t, httpxtest.Options{})
		settings.NewHandler(s.Repo, nil, nil).Register(s.App.Router)
		worlds = append(worlds, s)
		if len(cases[i].storedBackup) > 0 {
			// The stored backup keeps values of the wrong type, as a hand
			// edit could; the plain record refuses them as input.
			repotest.WriteRow(t, s.Repo.DatabasePath(), "system", "", "backup", cases[i].storedBackup)
		}
		// Python copies its seed: give it the version-1 layout it reads (O17).
		seed := filepath.Join(t.TempDir(), "seed")
		if err := os.CopyFS(seed, os.DirFS(s.DataDir)); err != nil {
			t.Fatal(err)
		}
		repotest.PythonLayout(t, seed)
		cases[i].Seed = seed
	}
	output := c06Python(t, `
import contextlib, io, json, pathlib, shutil, sqlite3, sys, tempfile
from flask import Flask
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)
def state(root):
    tables = {}
    with sqlite3.connect(root/'configflow.sqlite3') as db:
        for table,order in [('meta','key'),('sections','scope, owner'),('fields','scope, owner, field')]:
            cursor = db.execute('SELECT * FROM '+table+' ORDER BY '+order)
            columns = [c[0] for c in cursor.description]
            tables[table] = [[json.loads(v) if columns[i]=='value' else v for i,v in enumerate(row)] for row in cursor.fetchall()]
    files = {}
    for path in root.rglob('*'):
        if not path.is_file() or '.sqlite3' in path.name or path.suffix in ('.lock','.log'): continue
        data = path.read_text()
        files[path.relative_to(root).as_posix()] = json.loads(data) if path.suffix=='.json' else data
    return {'tables':tables,'files':files}
rows = json.load(sys.stdin)
answers = []
with tempfile.TemporaryDirectory() as temporary:
    for i,row in enumerate(rows):
        root = pathlib.Path(temporary)/str(i)
        shutil.copytree(row['Seed'], root)
        repository = ProfileRepository(root)
        config.set_repository(repository)
        before = state(root)
        with contextlib.redirect_stdout(io.StringIO()):
            reply = app.test_client().open(row['Path'], method=row['Method'], data=row['Body'], content_type=row['ContentType'], headers={'Authorization':'Bearer '+__import__('jwt').encode({'username':'matrix-admin'}, 'matrix-secret', algorithm='HS256')})
        answers.append({'status':reply.status_code,'body':None if row['Method']=='HEAD' else reply.get_json(silent=True),'before':before,'after':state(root)})
        config.reset_config_context()
def finite_json(value):
    import math
    if isinstance(value,float) and not math.isfinite(value): return {'__nonfinite__':repr(value)}
    if isinstance(value,dict): return {k:finite_json(v) for k,v in value.items()}
    if isinstance(value,(list,tuple)): return [finite_json(v) for v in value]
    return value
json.dump(finite_json(answers), sys.stdout, ensure_ascii=False, allow_nan=False)
`, cases)
	var expected []settingsResult
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatalf("decode Python: %v: %s", err, output)
	}
	if len(expected) != len(cases) {
		t.Fatal("truncated settings oracle")
	}
	for i, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			s := worlds[i]
			want := expected[i]
			before := settingsSnapshot(t, s.DataDir)
			response := s.Do(tc.Method, tc.Path, httpxtest.Body([]byte(tc.Body), tc.ContentType))
			after := settingsSnapshot(t, s.DataDir)
			if tc.Difference == "head-readonly" {
				get := s.Get(tc.Path)
				if response.Status != get.Status || len(response.Body) != 0 || !reflect.DeepEqual(response.Header, get.Header) {
					t.Fatalf("HEAD differs from GET: %+v / %+v", response, get)
				}
				if !reflect.DeepEqual(normalizedSettingsState(t, before), normalizedSettingsState(t, after)) {
					t.Fatal("HEAD committed")
				}
				pythonStatus := 500
				if tc.Path == "/api/settings/rule-fetch-proxy" {
					pythonStatus = 200
				}
				if tc.Path == "/api/settings/web-port" {
					pythonStatus = 503
				}
				if want.Status != pythonStatus {
					t.Fatalf("unexpected Python HEAD status %d", want.Status)
				}
				if tc.Path == "/api/settings/rule-fetch-proxy" && reflect.DeepEqual(want.Before, want.After) {
					t.Fatal("Python HEAD no longer reproduced write bug")
				}
				return
			}
			if tc.Difference == "stored-backup-types" {
				// Python echoed stored values of the wrong type; Go reads
				// them with the stored-data rules (O18 T1c): a URL that is
				// not text is empty, numbers are text, null is absent (the
				// default path, empty text), auto_backup that is not a
				// boolean is off (coordinator ruling: switches that trigger
				// writes). Retention falls back to the default on both.
				var expected map[string]any
				if err := json.Unmarshal(want.Body, &expected); err != nil {
					t.Fatal(err)
				}
				for key, value := range expected {
					switch v := value.(type) {
					case float64:
						if key == "webdav_url" {
							expected[key] = ""
						} else if key != "keep_latest" && key != "keep_daily" {
							expected[key] = strconv.FormatFloat(v, 'f', -1, 64)
						}
					case nil:
						expected[key] = ""
						if key == "webdav_path" {
							expected[key] = "/config-flow-backup/"
						}
					}
					if key == "auto_backup" {
						if _, ok := value.(bool); !ok {
							expected[key] = false
						}
					}
				}
				var err error
				want.Body, err = json.Marshal(expected)
				if err != nil {
					t.Fatal(err)
				}
				tc.Difference = ""
			}
			if tc.Difference == "native-nonfinite" {
				pythonStatus := 400
				if tc.Path == "/api/server-domain" || tc.Path == "/api/config-token" {
					pythonStatus = 500
				}
				if tc.Path == "/api/settings/subscription-aggregation" {
					pythonStatus = 200
				}
				if want.Status != pythonStatus || response.Status != 400 {
					t.Fatalf("nonfinite status Python=%d Go=%d, want %d/400", want.Status, response.Status, pythonStatus)
				}
				control := s.Do(tc.Method, tc.Path, httpxtest.Body([]byte("{"), J)).WantStatus(400)
				if !reflect.DeepEqual(response.Object(), control.Object()) {
					t.Fatal("nonfinite body bypassed native malformed-body policy")
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("rejected nonfinite body committed")
				}
				if tc.Path == "/api/settings/subscription-aggregation" && reflect.DeepEqual(want.Before, want.After) {
					t.Fatal("Python nonfinite save no longer reproduced")
				}
				return
			}
			if tc.Difference != "" {
				status := 400
				if tc.Difference == "native-media-type" {
					status = 415
				}
				if want.Status != 500 || response.Status != status {
					t.Fatalf("%s: Python=%d Go=%d", tc.Difference, want.Status, response.Status)
				}
				body := response.Object()
				if body.Value("success") != false || body.GetString("message") == "" || body.Len() != 2 {
					t.Fatal("invalid binding error envelope")
				}
			} else if response.Status != want.Status {
				t.Fatalf("status Go=%d Python=%d body=%s Pythonbody=%s", response.Status, want.Status, response.Body, want.Body)
			} else {
				var gotBody, wantBody any
				if err := json.Unmarshal(response.Body, &gotBody); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(want.Body, &wantBody); err != nil {
					t.Fatal(err)
				}
				internalText := false
				if response.Status == 500 {
					g, gok := gotBody.(map[string]any)
					w, wok := wantBody.(map[string]any)
					if gok && wok && len(g) == 2 && len(w) == 2 && g["success"] == false && w["success"] == false {
						gm, gok := g["message"].(string)
						wm, wok := w["message"].(string)
						internalText = gok && wok && gm == "setting must be a string" && strings.Contains(wm, "object has no attribute 'strip'")
					}
				}
				if !internalText && !reflect.DeepEqual(gotBody, wantBody) {
					t.Fatalf("body Go=%s Python=%s", response.Body, want.Body)
				}
			}
			if !reflect.DeepEqual(normalizedSettingsState(t, before), normalizedSettingsState(t, want.Before)) {
				t.Fatal("different baseline")
			}
			goAfter, pyAfter := normalizedSettingsState(t, after), normalizedSettingsState(t, want.After)
			if tc.Name == "delete token" {
				// Without a saved token Python still rewrote system_config
				// with config_token "" (a new revision); the plain record
				// leaves the empty token out, so the value is unchanged and
				// Go keeps the row (O18 T1c). Values are still compared.
				sameRevision(goAfter, pyAfter, "system", "system_config")
			}
			if !reflect.DeepEqual(goAfter, pyAfter) {
				t.Fatalf("complete DB/files differ: %s", settingsDifference(goAfter, pyAfter, "state"))
			}
		})
	}
	t.Logf("%d C06 Flask request/SQLite/file comparisons", len(cases))
}

// sameRevision copies the revision of one fields row from want to got.
func sameRevision(got, want any, scope, field string) {
	rows := func(state any) []any {
		tables, _ := state.(map[string]any)["tables"].(map[string]any)
		list, _ := tables["fields"].([]any)
		return list
	}
	revisions := map[string]any{}
	for _, row := range rows(want) {
		if cells, ok := row.([]any); ok && len(cells) >= 6 && cells[0] == scope && cells[2] == field {
			revisions[fmt.Sprint(cells[1])] = cells[5]
		}
	}
	for _, row := range rows(got) {
		if cells, ok := row.([]any); ok && len(cells) >= 6 && cells[0] == scope && cells[2] == field {
			if revision, ok := revisions[fmt.Sprint(cells[1])]; ok {
				cells[5] = revision
			}
		}
	}
}

func settingsDifference(got, want any, path string) string {
	if reflect.DeepEqual(got, want) {
		return ""
	}
	switch g := got.(type) {
	case map[string]any:
		if w, ok := want.(map[string]any); ok {
			for k, v := range g {
				if diff := settingsDifference(v, w[k], path+"."+k); diff != "" {
					return diff
				}
			}
		}
	case []any:
		if w, ok := want.([]any); ok && len(g) == len(w) {
			for i, v := range g {
				if diff := settingsDifference(v, w[i], fmt.Sprintf("%s[%d]", path, i)); diff != "" {
					return diff
				}
			}
		}
	}
	return fmt.Sprintf("%s Go=%v Python=%v", path, got, want)
}

// c06ExpectedDifference is limited to the five named legacy differential rows.
// The full storage oracle and per-path HEAD tests independently check writes.
func c06ExpectedDifference(r request, py, got response) []string {
	pythonStatus, goStatus := 500, 400
	switch r.c06Difference {
	case "head-500":
		goStatus = 200
	case "head-400":
		pythonStatus, goStatus = 400, 200
	case "binding-415":
		goStatus = 415
	case "binding-400":
	default:
		return []string{"unknown C06 expected difference"}
	}
	if py.status != pythonStatus || got.status != goStatus {
		return []string{fmt.Sprintf("C06 %s: Python=%d Go=%d, want %d/%d", r.c06Difference, py.status, got.status, pythonStatus, goStatus)}
	}
	if strings.HasPrefix(r.c06Difference, "head-") {
		if len(py.body) != 0 || len(got.body) != 0 || got.header.Get("Content-Type") != "application/json" {
			return []string{"C06 HEAD response changed"}
		}
		return nil
	}
	message := "The browser (or proxy) sent a request that this server could not understand."
	if goStatus == 415 {
		message = "Did not attempt to load JSON data because the request Content-Type was not 'application/json'."
	}
	var body map[string]any
	if err := json.Unmarshal(got.body, &body); err != nil || !reflect.DeepEqual(body, map[string]any{"success": false, "message": message}) {
		return []string{"C06 native binding error envelope changed"}
	}
	return nil
}

func TestN811W5StoredBackupTypes(t *testing.T) {
	cases := []settingsCase{}
	for _, seed := range []string{
		`{"webdav_url":17,"webdav_username":17,"webdav_password":17,"webdav_path":17,"auto_backup":17,"keep_latest":1.0,"keep_daily":"3","opaque":{"value":9007199254740993}}`,
		`{"webdav_url":null,"webdav_username":null,"webdav_password":null,"webdav_path":null,"auto_backup":null}`,
		`{"WEBDAV_USERNAME":"not-a-canonical-key","Auto_Backup":true,"webdav_password":[]}`,
	} {
		difference := ""
		if strings.Contains(seed, `"webdav_url":`) {
			difference = "stored-backup-types"
		}
		cases = append(cases, settingsCase{Name: seed, Method: "GET", Path: "/api/backup/config", Difference: difference, storedBackup: json.RawMessage(seed)})
	}
	runSettingsBoundary(t, cases)
}
