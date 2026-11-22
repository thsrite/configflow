package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// generatorKind is the static inventory: selector, output format and the HTTP
// template that takes the generator over once it is registered.
type generatorKind struct {
	name, selector, format string
	route                  any // string or nil
}

var generatorInventory = []generatorKind{
	{"mihomo", "profile", "yaml", "GET /api/config/<profile_id>/mihomo"},
	{"surge", "profile", "text", "GET /api/config/<profile_id>/surge"},
	{"loon", "profile", "text", "GET /api/config/<profile_id>/loon"},
	{"mosdns", "profile", "yaml", "GET /api/config/<profile_id>/mosdns"},
	{"oxidns", "profile", "yaml", "GET /api/config/<profile_id>/oxidns"},
	{"mosdns-files", "profile", "json", nil},
	{"mosdns-downloads", "profile", "json", nil},
	{"oxidns-files", "profile", "json", nil},
	{"oxidns-downloads", "profile", "json", nil},
	{"mihomo-providers", "profile", "json", nil},
	{"mihomo-rules", "profile", "json", nil},
	{"subscription-mihomo", "subscription", "yaml", "GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies"},
	{"subscription-surge", "subscription", "text", "GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies"},
	{"subscription-loon", "subscription", "text", "GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies"},
	{"aggregation-provider", "aggregation", "yaml", "GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider"},
	{"agent-mihomo", "agent", "yaml", "GET /api/agents/<agent_id>/config"},
	{"agent-mosdns", "agent", "yaml", "GET /api/agents/<agent_id>/config"},
	{"agent-oxidns", "agent", "yaml", "GET /api/agents/<agent_id>/config"},
	{"agent-surge", "agent", "text", "GET /api/agents/<agent_id>/config"},
	{"agent-deployment-bundle", "agent", "json", nil},
}

var missingGenerators = []struct{ name, route, reason string }{
	{"router-script", "POST /api/routers/<router_id>/plan", "No public Go router plan/script exporter exists."},
}

// routeKey is a (method, template) pair.
type routeKey [2]string

func splitRoute(route string) routeKey {
	method, template, _ := strings.Cut(route, " ")
	return routeKey{method, template}
}

// generatorJob is one private generator selector (written to the manifest only).
type generatorJob struct {
	index                                      int
	kind, profileID, subID, aggID, agentID, fm string
}

func (j generatorJob) value() *Map {
	m := newMap()
	m.Set("index", int64(j.index))
	m.Set("kind", j.kind)
	m.Set("profile_id", j.profileID)
	m.Set("sub_id", j.subID)
	m.Set("agg_id", j.aggID)
	m.Set("agent_id", j.agentID)
	m.Set("format", j.fm)
	return m
}

func serviceType(agent any) any { return getOr(agent, "service_type", "mihomo") }

// inventory lists generator jobs and the initial coverage rows (ordered).
func inventory(doc *Map, implemented map[routeKey]bool) ([]generatorJob, *Map) {
	var jobs []generatorJob
	coverage := newMap()
	profiles := section(doc, "profiles")
	system := section(doc, "system")
	shared := section(doc, "shared")
	agents := asList(getOr(system, "agents", []any{}))
	defaultProfile := getOr(getOr(system, "system_config", newMap()), "default_profile_id", nil)
	if !truthy(defaultProfile) {
		defaultProfile = "default"
	}
	for _, kind := range generatorInventory {
		mode := "generator"
		if route, ok := kind.route.(string); ok && implemented[splitRoute(route)] {
			mode = "http"
		}
		row := newMap()
		row.Set("mode", mode)
		row.Set("route", kind.route)
		row.Set("cases", int64(0))
		row.Set("success", int64(0))
		row.Set("both-error", int64(0))
		coverage.Set("GENERATOR "+kind.name, row)
		for i, profileKey := range profiles.keys {
			profile, _ := profiles.vals[i].(*Map)
			if profile == nil {
				profile = newMap()
			}
			config := mergeConfig(shared, profile)
			var entities []any
			switch kind.selector {
			case "profile":
				entities = []any{newMap()}
			case "subscription":
				entities = asList(getOr(config, "subscriptions", []any{}))
			case "aggregation":
				entities = asList(getOr(config, "subscription_aggregations", []any{}))
			default:
				for _, a := range agents {
					owner := getOr(a, "profile_id", nil)
					if !truthy(owner) {
						owner = defaultProfile
					}
					if !pyEqual(owner, profileKey) {
						continue
					}
					service := serviceType(a)
					bundle := kind.name == "agent-deployment-bundle" && (pyEqual(service, "mihomo") || pyEqual(service, "mosdns") || pyEqual(service, "oxidns"))
					if bundle || "agent-"+pyStr(service) == kind.name {
						entities = append(entities, a)
					}
				}
			}
			for _, entity := range entities {
				increment(row, "cases")
				if mode != "generator" {
					continue
				}
				job := generatorJob{index: len(jobs), kind: kind.name, profileID: pyStr(profileKey), fm: kind.format}
				id := pyStr(getOr(entity, "id", ""))
				switch kind.selector {
				case "subscription":
					job.subID = id
				case "aggregation":
					job.aggID = id
				case "agent":
					job.agentID = id
				}
				jobs = append(jobs, job)
			}
		}
	}
	var routers int64
	for _, profile := range profiles.vals {
		routers += int64(len(asList(getOr(getOr(profile, "routers", newMap()), "items", []any{}))))
	}
	for _, missing := range missingGenerators {
		mode := statusNotImplemented
		if implemented[splitRoute(missing.route)] {
			mode = "http"
		}
		row := newMap()
		row.Set("mode", mode)
		row.Set("route", missing.route)
		row.Set("cases", routers)
		row.Set("success", int64(0))
		row.Set("reason", missing.reason)
		coverage.Set("GENERATOR "+missing.name, row)
	}
	return jobs, coverage
}

// increment adds n (default 1) to an integer counter in an ordered row.
func increment(row *Map, key string, n ...int64) {
	by := int64(1)
	if len(n) > 0 {
		by = n[0]
	}
	current, _ := row.Get(key)
	value, _ := current.(int64)
	row.Set(key, value+by)
}

func counter(row any, key string) int64 {
	value, _ := get(row, key)
	switch n := value.(type) {
	case int64:
		return n
	case bool:
		return boolInt(n)
	case float64:
		return int64(n)
	}
	return 0
}

// workerEnv is the generator environment on top of the inherited one.
func workerEnv(overrides map[string]string) []string {
	var env []string
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if _, replaced := overrides[name]; !replaced {
			env = append(env, item)
		}
	}
	for name, value := range overrides {
		env = append(env, name+"="+value)
	}
	return env
}

// runCommand runs a worker with its own log; tests replace it.
var runCommand = func(command []string, env []string, logPath string, timeout time.Duration) error {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open worker log: %w", err)
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, log, log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generator worker: %w", err)
	}
	return nil
}

// copyTree is shutil.copytree: files and directories with mode and times.
var copyTree = func(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return errors.New("snapshot symlinks are not supported")
		case info.IsDir():
			return os.MkdirAll(destination, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(destination, data, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chtimes(destination, info.ModTime(), info.ModTime())
		}
		return nil
	})
}

// copyDirTimes restores directory times after their contents were copied.
func copyDirTimes(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return err
		}
		relative, _ := filepath.Rel(source, path)
		destination := filepath.Join(target, relative)
		if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(destination, info.ModTime(), info.ModTime())
	})
}

func copySnapshot(source, target string) error {
	if err := copyTree(source, target); err != nil {
		return fmt.Errorf("copy snapshot: %w", err)
	}
	if err := copyDirTimes(source, target); err != nil {
		return fmt.Errorf("copy snapshot times: %w", err)
	}
	return nil
}

// generatorResults compares the reference (base) generator with the candidate.
type generatorResults struct {
	found    map[[3]string]bool
	coverage *Map
	samples  map[string]reply
}

// runWorkers runs the baseline generator (left) and the candidate generator
// on their own snapshot copies and compares every successful output.
func (r *run) runWorkers(snapshot, copiesParent, report, candidate string, referenceCommand []string,
	doc *Map, implemented map[routeKey]bool) (generatorResults, error) {
	jobs, coverage := inventory(doc, implemented)
	manifest := filepath.Join(report, "generator-jobs.json")
	values := make([]any, len(jobs))
	for i, job := range jobs {
		values[i] = job.value()
	}
	if err := os.WriteFile(manifest, []byte(dumpJSON(values, dumpOptions{indent: -1, ensureASCII: true})), 0o600); err != nil {
		return generatorResults{}, fmt.Errorf("write generator manifest: %w", err)
	}
	var outputs [2][]*Map
	for i, label := range []string{leftLabel, "go"} {
		data := filepath.Join(copiesParent, "generator-"+label)
		if err := r.copySnapshot(snapshot, data); err != nil {
			return generatorResults{}, err
		}
		output := filepath.Join(report, "generator-"+label+".jsonl")
		logPath := filepath.Join(report, "generator-"+label+".log")
		env := workerEnv(map[string]string{"DATA_DIR": data, "LOG_FILE": logPath, "LOG_LEVEL": "ERROR",
			"SUB_STORE_BUILTIN_DIR": "/nonexistent", "CONFIGFLOW_BACKEND_HOST": "127.0.0.1", "CONFIGFLOW_BACKEND_PORT": "5001"})
		command := []string{candidate}
		if i == 0 {
			command = referenceCommand
		}
		command = append(append([]string{}, command...), manifest, output)
		if err := r.runCommand(command, env, logPath, 600*time.Second); err != nil {
			return generatorResults{}, err
		}
		results, err := readResults(output)
		if err != nil {
			return generatorResults{}, err
		}
		if len(results) != len(jobs) {
			return generatorResults{}, errors.New("generator worker result coverage mismatch")
		}
		for j, result := range results {
			if !pyEqual(getOr(result, "index", nil), int64(j)) {
				return generatorResults{}, errors.New("generator worker result coverage mismatch")
			}
		}
		outputs[i] = results
	}
	found := map[[3]string]bool{}
	samples := map[string]reply{}
	for j, job := range jobs {
		key := "GENERATOR " + job.kind
		rowValue, _ := coverage.Get(key)
		row := rowValue.(*Map)
		increment(row, "compared")
		left, right := outputs[0][j], outputs[1][j]
		leftOK := pyEqual(getOr(left, "status", nil), "ok")
		rightOK := pyEqual(getOr(right, "status", nil), "ok")
		if !leftOK || !rightOK {
			increment(row, leftLabel+"-error", boolInt(!leftOK))
			increment(row, "go-error", boolInt(!rightOK))
			if pyEqual(getOr(left, "status", nil), getOr(right, "status", nil)) {
				increment(row, "both-error")
				// Rejection agreement is separate from successful output coverage.
				if !pyEqual(getOr(left, "error", nil), getOr(right, "error", nil)) {
					found[[3]string{key, "error.message", "value"}] = true
				}
			} else if !rightOK {
				found[[3]string{key, "status", "generation-error-go"}] = true
			} else {
				found[[3]string{key, "status", "generation-error-" + leftLabel}] = true
			}
			continue
		}
		increment(row, "success")
		mime := map[string]string{"yaml": "text/yaml", "json": "application/json", "text": "text/plain"}[job.fm]
		a := bodyReply(200, mime, []byte(pyStr(getOr(left, "content", ""))))
		b := bodyReply(200, mime, []byte(pyStr(getOr(right, "content", ""))))
		if err := r.corpusOut.add("/generator/"+job.kind, a, b); err != nil {
			return generatorResults{}, fmt.Errorf("write corpus: %w", err)
		}
		diffs, err := r.cmp.responseDiffs(a, b, "/generator/"+job.kind, nil, nil, nil)
		if err != nil {
			return generatorResults{}, err
		}
		for _, d := range diffs {
			found[[3]string{key, d.Path, d.Kind}] = true
		}
		if _, ok := samples[job.fm]; !ok {
			samples[job.fm] = b
		}
	}
	return generatorResults{found: found, coverage: coverage, samples: samples}, nil
}

func readResults(path string) ([]*Map, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read generator results: %w", err)
	}
	defer file.Close()
	var out []*Map
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<30)
	for scanner.Scan() {
		value, err := parseJSON(scanner.Text(), true)
		if err != nil {
			return nil, fmt.Errorf("decode generator result: %w", err)
		}
		m, ok := value.(*Map)
		if !ok {
			return nil, errors.New("generator result is not an object")
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read generator results: %w", err)
	}
	return out, nil
}

// successfulOutputRegressions keeps a real output from silently becoming an agreed rejection.
func successfulOutputRegressions(expected, current *Map) []string {
	var lost []string
	for i, key := range expected.keys {
		name := pyStr(key)
		previous := expected.vals[i]
		now, _ := current.Get(name)
		if counter(now, "success") < counter(previous, "success") {
			lost = append(lost, name+" successful-output-count")
		}
		if counter(previous, "partial") != 0 &&
			counter(now, "success")+counter(now, "partial") < counter(previous, "success")+counter(previous, "partial") {
			lost = append(lost, name+" packaging-output-count")
		}
	}
	return lost
}
