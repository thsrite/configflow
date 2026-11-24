package httpx_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
)

type c05Reference struct {
	Function      string               `json:"fn"`
	Name          string               `json:"test"`
	Path          string               `json:"path"`
	Headers       map[string]string    `json:"headers"`
	JWT           bool                 `json:"jwt"`
	Status        int                  `json:"status"`
	ProfileHeader string               `json:"profile_header"`
	Endpoint      string               `json:"endpoint"`
	Blueprint     string               `json:"blueprint"`
	HasPath       bool                 `json:"has_path"`
	Request       *string              `json:"request"`
	Explicit      *string              `json:"explicit"`
	Fallback      *string              `json:"fallback"`
	Config        json.RawMessage      `json:"config"`
	URL           string               `json:"url"`
	Params        [][2]json.RawMessage `json:"params"`
	Error         string               `json:"error"`
	Out           json.RawMessage      `json:"out"`
}

func c05References(t *testing.T) []c05Reference {
	t.Helper()
	file, err := os.Open("testdata/review_c05.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var rows []c05Reference
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var row c05Reference
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 1000 {
		t.Fatalf("incomplete C05 references: %d", len(rows))
	}
	return rows
}

func c05EqualJSON(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %s; want %s", data, want)
	}
}

func c05Observe(c *httpx.Ctx) (*httpx.Response, error) {
	if strings.Contains(c.Path(), "/proxies") || strings.HasSuffix(c.Path(), "/provider") {
		r := c.App().Repo
		result, err := auth.ValidateSystemTokenOrJWT(c, r, &model.System{SystemConfig: model.SystemConfig{ConfigToken: "c05-config-token"}})
		if err != nil {
			return nil, err
		}
		if !result.Valid {
			return httpx.JSONStatus(401, map[string]any{"valid": false, "message": result.Message}), nil
		}
	}
	arg := func(name string) *string {
		value, present := c.Arg(name)
		if !present {
			return nil
		}
		return &value
	}
	r := c.App().Repo
	profile, err := r.ResolveProfile(c.Context(), arg("explicit"), arg("fallback"))
	if err != nil {
		return nil, err
	}
	first, err := r.ScopedConfig(c.Context(), profile)
	if err != nil {
		return nil, err
	}
	second, err := r.ScopedConfig(c.Context(), profile)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{
		"profile":            profile,
		"middleware_profile": c.ProfileID,
		"config_profile":     first.ProfileID,
		"reused":             first == second,
	}), nil
}

// The views observe selection only. The real App, repository, JWT gate and
// provider credential check run in their production order, independently of
// the business handlers owned by C30 and the other route modules.
func TestC05ReviewProfileHTTPReferences(t *testing.T) {
	rows := c05References(t)
	server := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(func(router *httpx.Router) {
		for _, row := range rows {
			if row.Function != "classify" {
				continue
			}
			path := row.Path
			if row.HasPath {
				path = strings.Replace(path, "/profiles/path/", "/profiles/<profile_id>/", 1)
			}
			router.GET(path, row.Endpoint, c05Observe)
		}
	})})
	for _, id := range []string{"path", "query", "query-id", "header", "explicit", "fallback"} {
		if _, err := server.Repo.CreateProfileRecord(server.Context(), repo.ProfileCreation{ID: id, Name: id}, ""); err != nil {
			t.Fatal(err)
		}
	}
	token, err := auth.GenerateToken("c05-admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Function != "http" {
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			var options []httpxtest.Option
			for key, value := range row.Headers {
				options = append(options, httpxtest.Header(key, value))
			}
			if row.JWT {
				options = append(options, httpxtest.Bearer(token))
			}
			response := server.Get(row.Path, options...)
			// O9-L6: the frozen Python reference resolves profiles first. Only anonymous
			// protected requests migrate to the exact login-required envelope, no profile header.
			parsed, err := url.Parse(row.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !row.JWT && !auth.IsPublicPath(parsed.Path) {
				row.Status, row.ProfileHeader = 401, ""
				row.Out = json.RawMessage(`{"success":false,"message":"请先登录后再操作。"}`)
			}
			if response.Status != row.Status {
				t.Fatalf("status %d, want %d; body %s", response.Status, row.Status, response.Body)
			}
			if header := response.Header.Get("X-ConfigFlow-Profile"); header != row.ProfileHeader {
				t.Fatalf("profile header %q, want %q", header, row.ProfileHeader)
			}
			var body any
			if err := json.Unmarshal(response.Body, &body); err != nil {
				t.Fatal(err)
			}
			c05EqualJSON(t, body, row.Out)
		})
	}
}

func TestC05ReviewProfileHelperReferences(t *testing.T) {
	server := httpxtest.New(t, httpxtest.Options{})
	for _, id := range []string{"query", "explicit", "fallback"} {
		if _, err := server.Repo.CreateProfileRecord(server.Context(), repo.ProfileCreation{ID: id, Name: id}, ""); err != nil {
			t.Fatal(err)
		}
	}
	contexts := map[string]context.Context{}
	for _, row := range c05References(t) {
		if row.Function == "http" {
			continue
		}
		t.Run(row.Function+"/"+row.Name, func(t *testing.T) {
			switch row.Function {
			case "classify":
				c05EqualJSON(t, repo.IsGlobalRequest(repo.RequestRoute{HasPathProfileID: row.HasPath, Endpoint: row.Endpoint, Blueprint: row.Blueprint, Path: row.Path}), row.Out)
			case "resolve":
				key := "background"
				if row.Request != nil {
					key = *row.Request
				}
				ctx := contexts[key]
				if ctx == nil {
					ctx = server.Context()
					if row.Request != nil {
						parsed, err := url.Parse(*row.Request)
						if err != nil {
							t.Fatal(err)
						}
						info := repo.RequestInfo{Global: true}
						switch parsed.Path {
						case "/api/rules":
							info.Endpoint, info.Global = "rules.get_rules", false
						case "/api/nodes":
							info.Endpoint = "nodes.get_nodes"
						}
						if values, ok := parsed.Query()["profile"]; ok {
							info.QueryProfile = &values[0]
						}
						repo.SetRequest(ctx, info)
					}
					contexts[key] = ctx
				}
				got, err := server.Repo.ResolveProfile(ctx, row.Explicit, row.Fallback)
				if row.Error != "" {
					kind := repo.ErrValidation
					if row.Error == "ProfileNotFound" {
						kind = repo.ErrNotFound
					}
					if !errors.Is(err, kind) {
						t.Fatalf("got %v; want %s", err, row.Error)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					c05EqualJSON(t, got, row.Out)
				}
			case "paths":
				// ProfileAPIPath/ConfigAPIPath had no production callers and were retired in N8-11 W9.
				return
			case "url":
				// The frozen recorder used dynamic urlencode arguments. Project its
				// archived call shape only in this test; production accepts typed
				// repeated string pairs and no longer implements Python coercion.
				var pairs [][2]string
				for _, pair := range row.Params {
					key := c05PyStr(c05DecodeJSON(t, pair[0]))
					val := c05DecodeJSON(t, pair[1])
					switch value := val.(type) {
					case []any:
						for _, item := range value {
							pairs = append(pairs, [2]string{key, c05PyStr(item)})
						}
					case *c05OrderedObj:
						for _, name := range value.keys {
							pairs = append(pairs, [2]string{key, name})
						}
					default:
						pairs = append(pairs, [2]string{key, c05PyStr(value)})
					}
				}
				got, err := convert.AppendQuery(row.URL, pairs)
				if row.Error != "" {
					if err == nil {
						t.Fatalf("accepted URL; want %s", row.Error)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					// Pair order, repeated keys, blanks and decoded values matter;
					// percent-encoding spelling is not a generated-config difference.
					var want string
					if err := json.Unmarshal(row.Out, &want); err != nil {
						t.Fatal(err)
					}
					a, err := url.Parse(got)
					if err != nil {
						t.Fatal(err)
					}
					b, err := url.Parse(want)
					if err != nil {
						t.Fatal(err)
					}
					queryEqual := reflect.DeepEqual(httpx.ParseQuery(a.RawQuery), httpx.ParseQuery(b.RawQuery))
					a.RawQuery, b.RawQuery = "", ""
					a.RawPath, b.RawPath = "", ""
					a.RawFragment, b.RawFragment = "", ""
					if !queryEqual || !reflect.DeepEqual(a, b) {
						t.Fatalf("URL %q, want %q", got, want)
					}
				}
			default:
				t.Fatalf("unknown reference function %q", row.Function)
			}
		})
	}
}

type c05OrderedObj struct {
	keys []string
}

func c05DecodeJSON(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	return c05ReadJSON(t, dec)
}

func c05ReadJSON(t *testing.T, dec *json.Decoder) any {
	t.Helper()
	tok, err := dec.Token()
	if err != nil {
		t.Fatal(err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok
	}
	if delim == '{' {
		obj := &c05OrderedObj{}
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				t.Fatal(err)
			}
			_ = c05ReadJSON(t, dec)
			name := k.(string)
			if !seen[name] {
				seen[name] = true
				obj.keys = append(obj.keys, name)
			}
		}
		if _, err := dec.Token(); err != nil {
			t.Fatal(err)
		}
		return obj
	}
	var list []any
	for dec.More() {
		list = append(list, c05ReadJSON(t, dec))
	}
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	return list
}

func c05PyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}
