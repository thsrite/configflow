package difftest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
)

type settingsCase struct {
	Name, Method, Path, Body, ContentType, Seed string
	Difference                                  string
}
type settingsResult struct {
	Status        int
	Body          json.RawMessage
	Before, After json.RawMessage
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
			for i, item := range x {
				x[i] = normalize(item)
			}
		}
		return v
	}
	return normalize(value)
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
		s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(settings.Register)})
		worlds = append(worlds, s)
		cases[i].Seed = s.DataDir
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
			if !reflect.DeepEqual(normalizedSettingsState(t, after), normalizedSettingsState(t, want.After)) {
				t.Fatalf("complete DB/files differ: %s", settingsDifference(normalizedSettingsState(t, after), normalizedSettingsState(t, want.After), "state"))
			}
		})
	}
	t.Logf("%d C06 Flask request/SQLite/file comparisons", len(cases))
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
