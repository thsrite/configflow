package main

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite" // read-only snapshot copies
)

// baseURL is the public URL both servers see in requests.
const baseURL = "http://127.0.0.1:5001"

// paramPattern matches Flask-style route parameters, with an optional converter.
var paramPattern = regexp.MustCompile(`<(?:[^:>]+:)?([^>]+)>`)

// testCase is one enumerated request.
type testCase struct {
	Method, Route, Path string
	Body                *Map // nil: no request body
	NoData              bool // a route parameter had no snapshot value
}

// document reads the configuration SQLite of a data copy into
// {system, shared, profiles}.
func document(data string) (*Map, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(data, "configflow.sqlite3")+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open configuration: %w", err)
	}
	defer db.Close()
	result := newMap()
	result.Set("system", newMap())
	result.Set("shared", newMap())
	profiles := newMap()
	result.Set("profiles", profiles)
	rows, err := db.Query(`SELECT scope, owner, field, value FROM fields`)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var scope, owner, name, value string
		if err := rows.Scan(&scope, &owner, &name, &value); err != nil {
			return nil, fmt.Errorf("scan configuration: %w", err)
		}
		var section *Map
		if scope == "profile" {
			existing, ok := profiles.Get(owner)
			if !ok {
				existing = newMap()
				profiles.Set(owner, existing)
			}
			section = existing.(*Map)
		} else {
			existing, ok := result.Get(scope)
			if !ok {
				existing = newMap()
				result.Set(scope, existing)
			}
			m, ok := existing.(*Map)
			if !ok {
				return nil, fmt.Errorf("configuration scope %q is not a section", scope)
			}
			section = m
		}
		parsedValue, err := parseJSON(value, true)
		if err != nil {
			return nil, fmt.Errorf("decode configuration field: %w", err)
		}
		section.Set(name, parsedValue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	return result, nil
}

func section(doc *Map, name string) *Map {
	if v, ok := doc.Get(name); ok {
		if m, ok := v.(*Map); ok {
			return m
		}
	}
	return newMap()
}

// ids is [str(row[field]) for row in rows if row is a mapping with a non-null field].
func ids(rows any, field string) []string {
	list, _ := rows.([]any)
	var out []string
	for _, row := range list {
		if value, ok := get(row, field); ok && value != nil {
			out = append(out, pyStr(value))
		}
	}
	return out
}

func asList(v any) []any {
	list, _ := v.([]any)
	return list
}

// mergeConfig is dict(shared, **profile).
func mergeConfig(shared, profile *Map) *Map {
	out := newMap()
	for i, k := range shared.keys {
		out.Set(k, shared.vals[i])
	}
	for i, k := range profile.keys {
		out.Set(k, profile.vals[i])
	}
	return out
}

// storedIDs reads opaque job/artifact IDs from local metadata only.
type storedIDs struct {
	byName       map[string][]string
	previewFiles map[string][]string
}

func readStored(agents []any, data string) (storedIDs, error) {
	s := storedIDs{byName: map[string][]string{}, previewFiles: map[string][]string{}}
	for _, agent := range agents {
		deployments, _ := get(agent, "deployments")
		if !truthy(deployments) {
			continue
		}
		switch d := deployments.(type) {
		case *Map:
			for _, k := range d.keys {
				s.byName["deployments"] = append(s.byName["deployments"], pyStr(k))
			}
		case []any:
			for _, k := range d {
				s.byName["deployments"] = append(s.byName["deployments"], pyStr(k))
			}
		}
	}
	commandDB := filepath.Join(data, "install-commands/snapshots.sqlite3")
	if _, err := os.Stat(commandDB); err == nil {
		tokens, err := readTokens(commandDB)
		if err != nil {
			return s, err
		}
		s.byName["tokens"] = tokens
	}
	for _, name := range []string{".generated-previews", "deployments", "setup-sessions", "gateway-installations", "probes"} {
		folder := filepath.Join(data, name)
		if _, err := os.Stat(folder); err != nil {
			continue
		}
		_ = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			value, err := loadJSONLoose(raw)
			if err != nil {
				return nil
			}
			id, _ := get(value, "id")
			if !truthy(id) {
				return nil
			}
			key := pyStr(id)
			s.byName[name] = append(s.byName[name], key)
			if name == ".generated-previews" {
				var files []string
				meta, _ := get(value, "bundle_meta")
				manifest, _ := get(meta, "manifest")
				list, _ := get(manifest, "files")
				for _, entry := range asList(list) {
					if p, ok := get(entry, "path"); ok {
						files = append(files, pyStr(p))
					}
				}
				if len(files) == 0 {
					files = []string{"surge.conf"}
				}
				s.previewFiles[key] = files
			}
			return nil
		})
	}
	return s, nil
}

func readTokens(path string) ([]string, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open install commands: %w", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT token FROM snapshots`)
	if err != nil {
		return nil, fmt.Errorf("read install commands: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var token any
		if err := rows.Scan(&token); err != nil {
			return nil, fmt.Errorf("scan install commands: %w", err)
		}
		switch t := token.(type) {
		case []byte:
			out = append(out, string(t))
		case string:
			out = append(out, t)
		default:
			out = append(out, fmt.Sprint(t))
		}
	}
	return out, rows.Err()
}

// query is an ordered urlencode() argument list.
type query []struct {
	key   string
	value any
}

func (q query) with(key string, value any) query {
	out := append(query{}, q...)
	for i := range out {
		if out[i].key == key {
			out[i].value = value
			return out
		}
	}
	return append(out, struct {
		key   string
		value any
	}{key, value})
}

func (q query) without(key string) query {
	var out query
	for _, item := range q {
		if item.key != key {
			out = append(out, item)
		}
	}
	return out
}

// encode is urllib.parse.urlencode (quote_plus, str() of values).
func (q query) encode() string {
	parts := make([]string, len(q))
	for i, item := range q {
		parts[i] = quotePlus(item.key) + "=" + quotePlus(pyStr(item.value))
	}
	return strings.Join(parts, "&")
}

func quote(s, safe string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_.-~", c) >= 0 ||
			strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func quotePlus(s string) string { return strings.ReplaceAll(quote(s, " "), " ", "+") }

// enumerate expands every GET and every listed read-only POST over each
// configuration space and every saved entity.
func enumerate(rows [][]string, readonlyPost map[string]bool, doc *Map, data string) ([]testCase, error) {
	profiles := section(doc, "profiles")
	system := section(doc, "system")
	shared := section(doc, "shared")
	agents := asList(getOr(system, "agents", []any{}))
	stored, err := readStored(agents, data)
	if err != nil {
		return nil, err
	}
	var out []testCase
	for _, row := range rows {
		method, route := row[0], row[1]
		generate := method == "POST" && readonlyPost[route]
		if method != "GET" && !generate {
			continue
		}
		for profileIndex, profileKey := range profiles.keys {
			profileID := pyStr(profileKey)
			profile, _ := profiles.vals[profileIndex].(*Map)
			if profile == nil {
				profile = newMap()
			}
			config := mergeConfig(shared, profile)
			resourceSource := "subscriptions"
			if strings.Contains(route, "/aggregations/") {
				resourceSource = "subscription_aggregations"
			}
			var names []string
			for _, x := range asList(getOr(config, "rule_library", []any{})) {
				if m, ok := x.(*Map); ok {
					fallback, _ := m.Get("id")
					names = append(names, pyStr(getOr(m, "name", fallback)))
				}
			}
			operation := []string{"preview"}
			if method == "GET" {
				operation = []string{"status"}
			}
			filename := []string{"configflow-agent-linux-amd64"}
			if strings.Contains(route, "/loon/") {
				filename = []string{"loon.lcf"}
			}
			sessions := stored.byName["setup-sessions"]
			if strings.Contains(route, "/gateway-installations/") {
				sessions = stored.byName["gateway-installations"]
			}
			params := map[string][]string{
				"profile_id":    {profileID},
				"agent_id":      ids(agents, "id"),
				"router_id":     ids(getOr(getOr(config, "routers", newMap()), "items", []any{}), "id"),
				"sub_id":        ids(getOr(config, "subscriptions", []any{}), "id"),
				"agg_id":        ids(getOr(config, "subscription_aggregations", []any{}), "id"),
				"resource_id":   ids(getOr(config, resourceSource, []any{}), "id"),
				"rule_id":       ids(getOr(config, "rule_library", []any{}), "id"),
				"name":          names,
				"service":       {"mihomo", "surge", "loon", "mosdns", "oxidns"},
				"operation":     operation,
				"resource":      {"configs", "proxies", "rules", "connections", "version"},
				"filename":      filename,
				"action":        {"file", "download"},
				"preview_id":    stored.byName[".generated-previews"],
				"deployment_id": stored.byName["deployments"],
				"job_id":        stored.byName["probes"],
				"session_id":    sessions,
				"record_id":     nil,
				"token":         stored.byName["tokens"],
			}
			var paramNames []string
			for _, m := range paramPattern.FindAllStringSubmatch(route, -1) {
				paramNames = append(paramNames, m[1])
			}
			noData := false
			choices := make([][]string, len(paramNames))
			for i, name := range paramNames {
				values := params[name]
				if len(values) == 0 {
					noData = true
					values = []string{"proddiff-missing"}
				}
				choices[i] = values
			}
			for _, combo := range product(choices) {
				mapping := map[string]string{}
				for i, name := range paramNames {
					mapping[name] = combo[i]
				}
				path := paramPattern.ReplaceAllStringFunc(route, func(match string) string {
					return quote(mapping[paramPattern.FindStringSubmatch(match)[1]], "")
				})
				q := query{}.with("profile_id", profileID)
				var agent any = newMap()
				for _, a := range agents {
					if id, _ := get(a, "id"); pyStr(id) == mapping["agent_id"] {
						if _, has := mapping["agent_id"]; has {
							agent = a
							break
						}
					}
				}
				if strings.HasSuffix(route, "/config") && strings.Contains(route, "/agents/") {
					q = q.with("token", getOr(agent, "token", ""))
				}
				var body *Map
				if method == "POST" {
					body = requestBody(route, profileID, agent, config)
				}
				emit := func(variant query) {
					out = append(out, testCase{method, route, path + "?" + variant.encode(), body, noData})
					// Once per route, also let each server resolve the default profile itself.
					if profileIndex == 0 && !strings.Contains(route, "<profile_id>") && method == "GET" {
						rest := variant.without("profile_id")
						p := path
						if len(rest) > 0 {
							p += "?" + rest.encode()
						}
						out = append(out, testCase{method, route, p, body, noData})
					}
				}
				switch {
				case strings.Contains(route, "/subscriptions/") && strings.HasSuffix(route, "/proxies"):
					for _, format := range []string{"mihomo", "surge", "loon"} {
						emit(q.with("format", format))
					}
				case strings.HasSuffix(route, "/file") || mapping["action"] == "file":
					files, ok := stored.previewFiles[mapping["preview_id"]]
					if _, has := mapping["preview_id"]; !has || !ok {
						files = []string{"config.yaml"}
					}
					for _, file := range files {
						emit(q.with("path", file))
					}
				case route == "/api/agents/install-script" || route == "/api/agents/docker-agent-compose" || route == "/api/agents/docker-agent-run":
					targets := agents
					if len(targets) == 0 {
						targets = []any{newMap()}
					}
					serverURL := getOr(getOr(system, "system_config", newMap()), "server_domain", nil)
					if !truthy(serverURL) {
						serverURL = baseURL
					}
					for _, t := range targets {
						name := getOr(t, "name", "proddiff")
						emit(q.with("server_url", serverURL).with("name", name).with("agent_name", name).
							with("agent_ip", getOr(t, "host", "")).with("type", getOr(t, "service_type", "mihomo")).
							with("port", getOr(t, "port", int64(8080))).with("enable_mihomo", "true").with("enable_oxidns", "true"))
					}
				default:
					emit(q)
				}
			}
		}
	}
	return out, nil
}

func requestBody(route, profileID string, agent any, config *Map) *Map {
	body := newMap()
	body.Set("base_url", baseURL)
	switch {
	case strings.HasSuffix(route, "/docker-oxidns-ha-guide"):
		body.Set("action", "inspect")
	case strings.HasSuffix(route, "/generate-config"):
		expected := getOr(agent, "profile_id", nil)
		if !truthy(expected) {
			expected = profileID
		}
		body.Set("expected_profile_id", expected)
		body.Set("expected_service_type", getOr(agent, "service_type", nil))
	case strings.Contains(route, "/oxidns/"):
		oxidns := getOr(config, "oxidns", newMap())
		body.Set("settings", oxidns)
		body.Set("changes", newMap())
		body.Set("native_config", getOr(oxidns, "native_config", ""))
	case strings.Contains(route, "/linux-gateway/"):
		body.Set("settings", getOr(config, "linux_gateway", newMap()))
	case strings.HasSuffix(route, "/preview-regex"):
		body.Set("source", "subscription")
		source := ""
		if subs := ids(getOr(config, "subscriptions", []any{}), "id"); len(subs) > 0 {
			source = subs[0]
		}
		body.Set("source_id", source)
		body.Set("regex", ".*")
	}
	return body
}

// product is itertools.product over string choices.
func product(choices [][]string) [][]string {
	out := [][]string{{}}
	for _, values := range choices {
		var next [][]string
		for _, prefix := range out {
			for _, v := range values {
				combo := append(append([]string{}, prefix...), v)
				next = append(next, combo)
			}
		}
		out = next
	}
	return out
}
