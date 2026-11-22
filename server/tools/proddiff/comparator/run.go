package main

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"configflow/internal/clock"
)

// Framework failures; details go only to the private error.txt.
var (
	errStartup        = errors.New("server startup failed; inspect private logs")
	errStartupTimeout = errors.New("server startup timeout")
	errLogin          = errors.New("test login failed")
	errNoProfiles     = errors.New("snapshot has no profiles")
	errWritable       = errors.New("snapshot mount is writable")
	errSnapshotChange = errors.New("snapshot changed")
	errSymlink        = errors.New("snapshot symlinks are not supported")
	errBaselineReason = errors.New("baseline entry needs a reason")
	errBaselineLine   = errors.New("baseline line needs four tab-separated fields")
	errInterrupted    = errors.New("interrupted")
)

// process is one launched server.
type process interface {
	Exited() bool
	Kill()
	Wait()
}

type osProcess struct {
	cmd  *exec.Cmd
	log  *os.File
	mu   sync.Mutex
	done bool
}

// reap collects the child's exit status; block waits for it.
func (p *osProcess) reap(block bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return true
	}
	options := syscall.WNOHANG
	if block {
		options = 0
	}
	var status syscall.WaitStatus
	pid, err := syscall.Wait4(p.cmd.Process.Pid, &status, options, nil)
	if pid == p.cmd.Process.Pid || err != nil && !errors.Is(err, syscall.EINTR) {
		p.done = true
		p.log.Close()
	}
	return p.done
}

func (p *osProcess) Exited() bool { return p.reap(false) }

func (p *osProcess) Wait() {
	for !p.reap(true) {
	}
}

func (p *osProcess) Kill() {
	if p.Exited() {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// launch starts a server in its own session with its log as stdout/stderr.
func launch(command, env []string, logPath string) (process, error) {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open server log: %w", err)
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, fmt.Errorf("start server: %w", err)
	}
	return &osProcess{cmd: cmd, log: log}, nil
}

// run holds one comparison run and its replaceable process/network boundaries.
type run struct {
	root, parity, private string
	cmp                   *comparator
	stdout                io.Writer
	getenv                func(string) string

	send           sender
	launch         func(command, env []string, logPath string) (process, error)
	runCommand     func(command, env []string, logPath string, timeout time.Duration) error
	copySnapshot   func(source, target string) error
	assertNoEgress func() error
	readOnly       func(path string) (bool, error)
	digest         func(path string) ([]byte, error)
	lockRuns       func(private string) (func(), error)
	loadAvg        func() string
	sleep          func(time.Duration)
	selfCheck      func(real reply, samples map[string]reply) ([]string, error)
	enumerate      func(rows [][]string, readonlyPost map[string]bool, doc *Map, data string) ([]testCase, error)
	document       func(data string) (*Map, error)
	workers        func(snapshot, copiesParent, report, candidate string, referenceCommand []string, doc *Map, implemented map[routeKey]bool) (generatorResults, error)

	corpus    bool // PRODDIFF_CORPUS=1: write private reply pairs for crosscheck
	corpusOut *corpusWriter

	mu        sync.Mutex
	processes []process
	report    string
}

func newRun(root string) *run {
	r := &run{root: root, parity: filepath.Join(root, "server/parity"), stdout: os.Stdout, getenv: os.Getenv,
		send: send, launch: launch, runCommand: runCommand, copySnapshot: copySnapshot,
		assertNoEgress: assertNoEgress, readOnly: readOnlyMount, digest: digestTree, lockRuns: lockRuns,
		loadAvg: loadAvg, sleep: clockSleep, enumerate: enumerate, document: document}
	home, _ := os.UserHomeDir()
	r.private = filepath.Join(home, "cf-prod-snapshot/runs")
	r.selfCheck = func(real reply, samples map[string]reply) ([]string, error) { return r.cmp.selfCheck(real, samples) }
	r.workers = r.runWorkers
	return r
}

func (r *run) stopProcesses() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.processes {
		p.Kill()
	}
	for _, p := range r.processes {
		p.Wait()
	}
}

// clockSleep pauses between startup probes on the shared real clock.
func clockSleep(d time.Duration) {
	timer := clock.Real{}.NewTimer(d)
	defer timer.Stop()
	<-timer.C()
}

func loadAvg() string {
	data, _ := os.ReadFile("/proc/loadavg")
	return strings.TrimSpace(string(data))
}

// digestTree hashes every file below root; symlinks are refused.
func digestTree(root string) ([]byte, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errSymlink
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, file := range files {
		relative, _ := filepath.Rel(root, file)
		h.Write([]byte(relative))
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("digest snapshot: %w", err)
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("digest snapshot: %w", err)
		}
	}
	return h.Sum(nil), nil
}

// lockRuns holds a shared lock for the run; whoever gets it exclusively
// first removes copies left by killed runs.
func lockRuns(private string) (func(), error) {
	handle, err := os.OpenFile(filepath.Join(private, ".lock"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open run lock: %w", err)
	}
	fd := int(handle.Fd())
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) == nil {
		stale, _ := filepath.Glob(filepath.Join(private, "run-*", "copies-*"))
		for _, path := range stale {
			_ = os.RemoveAll(path)
		}
	}
	if err := syscall.Flock(fd, syscall.LOCK_SH); err != nil {
		handle.Close()
		return nil, fmt.Errorf("lock runs: %w", err)
	}
	return func() { handle.Close() }, nil
}

func readRows(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routes: %w", err)
	}
	var rows [][]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows, nil
}

func readRouteSet(path string) (map[routeKey]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read route manifest: %w", err)
	}
	out := map[routeKey]bool{}
	for _, line := range splitLines(string(data)) {
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 {
			out[routeKey{fields[0], strings.Join(fields[1:], "\t")}] = true
		} else {
			out[routeKey{line, ""}] = true
		}
	}
	return out, nil
}

func readWords(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lifecycle manifest: %w", err)
	}
	out := map[string]bool{}
	for _, word := range strings.Fields(string(data)) {
		out[word] = true
	}
	return out, nil
}

// phase aligns discovery cold/hit transitions before parallel reads and
// artifact POSTs; log readers run last so they observe every request.
func phase(c testCase) int {
	if c.Method == "GET" && c.Route == "/api/client-devices/discovery" {
		return -1
	}
	if strings.HasPrefix(c.Route, "/api/logs/") {
		return 2
	}
	if c.Method != "GET" {
		return 1
	}
	return 0
}

// comparisonResults runs the cases in phases and returns results in phase,
// then case order. Phases 0 and 1 use eight workers.
func comparisonResults[T any](cases []testCase, compare func(testCase) (T, error)) ([]T, error) {
	var out []T
	serial := func(p int) error {
		for _, c := range cases {
			if phase(c) == p {
				result, err := compare(c)
				if err != nil {
					return err
				}
				out = append(out, result)
			}
		}
		return nil
	}
	if err := serial(-1); err != nil {
		return nil, err
	}
	for _, p := range []int{0, 1} {
		var selected []testCase
		for _, c := range cases {
			if phase(c) == p {
				selected = append(selected, c)
			}
		}
		results := make([]T, len(selected))
		errs := make([]error, len(selected))
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					results[i], errs[i] = compare(selected[i])
				}
			}()
		}
		for i := range selected {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		for i := range selected {
			if errs[i] != nil {
				return nil, errs[i]
			}
		}
		out = append(out, results...)
	}
	if err := serial(2); err != nil {
		return nil, err
	}
	return out, nil
}

type caseResult struct {
	c           testCase
	implemented bool
	httpResult
}

var connectionFailure = regexp.MustCompile(`(?i)ConnectionError|Network is unreachable|No route to host|Connection refused|Failed to establish|connect: network|dial tcp|timed out`)

func domainOf(route string) string {
	switch {
	case strings.Contains(route, "/agents") || strings.HasPrefix(route, "GENERATOR agent-"):
		return "agents"
	case strings.Contains(route, "/routers") || strings.HasPrefix(route, "GENERATOR router-"):
		return "routers"
	}
	return "config"
}

// counts is an insertion-ordered counter.
type counts struct{ m *Map }

func newCounts() counts                  { return counts{newMap()} }
func (c counts) add(key string, n int64) { increment(c.m, key, n) }
func (c counts) get(key string) int64    { return counter(c.m, key) }

// main runs the whole comparison and returns the process exit code.
func (r *run) main(args []string) (code int, err error) {
	loadStart := r.loadAvg()
	if err := r.assertNoEgress(); err != nil {
		return 2, err
	}
	if len(args) < 2 {
		return 2, errors.New("usage: comparator run <snapshot> <candidate-binary> [--record-baseline]")
	}
	snapshot, binary := args[0], args[1]
	record := false
	for _, a := range args[2:] {
		if a == "--record-baseline" {
			record = true
		}
	}
	readOnly, err := r.readOnly(snapshot)
	if err != nil {
		return 2, err
	}
	if !readOnly {
		return 2, errWritable
	}
	before, err := r.digest(snapshot)
	if err != nil {
		return 2, err
	}
	unlock, err := r.lockRuns(r.private)
	if err != nil {
		return 2, err
	}
	report, err := os.MkdirTemp(r.private, "run-")
	if err != nil {
		unlock()
		return 2, fmt.Errorf("create run directory: %w", err)
	}
	r.mu.Lock()
	r.report = report
	r.mu.Unlock()
	defer func() {
		r.stopProcesses()
		unlock()
		after, digestErr := r.digest(snapshot)
		if digestErr == nil && string(after) != string(before) {
			digestErr = errSnapshotChange
		}
		if digestErr != nil {
			code, err = 2, digestErr
		}
	}()
	rows, err := readRows(filepath.Join(r.parity, "routes.tsv"))
	if err != nil {
		return 2, err
	}
	routes := map[string]bool{}
	for _, row := range rows {
		if len(row) >= 2 {
			routes[row[1]] = true
		}
	}
	rules, err := loadRules(filepath.Join(r.parity, "proddiff-normalize.json"), routes, "go")
	if err != nil {
		return 2, err
	}
	bracket := map[string]bool{}
	for _, rule := range rules {
		if rule.Bracket {
			bracket[rule.Route] = true
		}
	}
	if r.corpus {
		writer, err := newCorpusWriter(report)
		if err != nil {
			return 2, err
		}
		r.corpusOut = writer
		defer writer.close()
	}
	temporary, err := os.MkdirTemp(report, "copies-")
	if err != nil {
		return 2, fmt.Errorf("create copies: %w", err)
	}
	defer func() {
		r.stopProcesses()
		_ = os.RemoveAll(temporary)
	}()
	return r.compareAll(snapshot, binary, record, report, temporary, rows, rules, bracket, loadStart)
}

func (r *run) compareAll(snapshot, binary string, record bool, report, temporary string, rows [][]string,
	rules []rule, bracket map[string]bool, loadStart string) (int, error) {
	copies := []string{filepath.Join(temporary, leftLabel), filepath.Join(temporary, "go")}
	for _, data := range copies {
		if err := r.copySnapshot(snapshot, data); err != nil {
			return 2, err
		}
	}
	doc, err := r.document(copies[0])
	if err != nil {
		return 2, err
	}
	if section(doc, "profiles").Len() == 0 {
		return 2, errNoProfiles
	}
	leftRoutes := filepath.Join(report, leftLabel+"-routes.tsv")
	goRoutes := filepath.Join(report, "go-routes.tsv")
	for index, data := range copies {
		manifest := goRoutes
		command := []string{binary}
		logName := "go.log"
		if index == 0 {
			manifest, command, logName = leftRoutes, []string{r.getenv("PRODDIFF_BASE_BINARY")}, leftLabel+".log"
		}
		env := workerEnv(map[string]string{"DATA_DIR": data, "LOG_FILE": filepath.Join(data, "app.log"), "LOG_LEVEL": "ERROR",
			"ADMIN_USERNAME": "proddiff-admin", "ADMIN_PASSWORD": "proddiff-test-password",
			"JWT_SECRET_KEY": "proddiff-test-only-shared-secret-0123456789", "CONFIGFLOW_BACKEND_HOST": "127.0.0.1",
			"CONFIGFLOW_BACKEND_PORT": strconv.Itoa(15001 + index), "CONFIGFLOW_FRONTEND_DIR": "",
			"SUB_STORE_BUILTIN_DIR": "/nonexistent", "PRODDIFF_ROUTES": manifest})
		p, err := r.launch(command, env, filepath.Join(report, logName))
		if err != nil {
			return 2, err
		}
		r.mu.Lock()
		r.processes = append(r.processes, p)
		r.mu.Unlock()
	}
	var tokens [2]string
	ready := []string{leftRoutes + ".ready", goRoutes + ".ready"}
	for index := 0; index < 2; index++ {
		started := false
		for attempt := 0; attempt < 450; attempt++ {
			if r.processes[index].Exited() {
				return 2, errStartup
			}
			status, _, err := r.send(15001+index, "GET", "/api/auth/status", "", nil, 20*time.Second)
			if err != nil {
				return 2, err
			}
			// Go serves before lifecycle.Start; wait until its startup work finished.
			if status.Status == 200 {
				if _, err := os.Stat(ready[index]); err == nil {
					started = true
					break
				}
			}
			r.sleep(200 * time.Millisecond)
		}
		if !started {
			return 2, errStartupTimeout
		}
		login := newMap()
		login.Set("username", "proddiff-admin")
		login.Set("password", "proddiff-test-password")
		answer, _, err := r.send(15001+index, "POST", "/api/auth/login", "", login, 20*time.Second)
		if err != nil {
			return 2, err
		}
		if answer.Status != 200 {
			return 2, errLogin
		}
		value, err := loadJSONLoose(answer.Body)
		if err != nil {
			return 2, errLogin
		}
		token, ok := getOr(value, "token", nil).(string)
		if !ok {
			return 2, errLogin
		}
		tokens[index] = token
	}
	implemented, err := readRouteSet(goRoutes)
	if err != nil {
		return 2, err
	}
	leftImplemented, err := readRouteSet(leftRoutes)
	if err != nil {
		return 2, err
	}
	known := map[routeKey]bool{}
	for _, row := range rows {
		if len(row) >= 2 {
			known[routeKey{row[0], row[1]}] = true
		}
	}
	// A route registered under another template would be silently reported as not implemented.
	unmappedSet := map[string]bool{}
	for route := range implemented {
		if !known[route] {
			unmappedSet["UNMAPPED-GO-ROUTE\t"+route[0]+" "+route[1]] = true
		}
	}
	for route := range leftImplemented {
		if !known[route] {
			unmappedSet["UNMAPPED-"+strings.ToUpper(leftLabel)+"-ROUTE\t"+route[0]+" "+route[1]] = true
		}
	}
	// Routes the baseline lacks cannot be compared; routes the candidate lost
	// drop out of coverage and fail as coverage regressions.
	candidateOnly := 0
	for route := range implemented {
		if !leftImplemented[route] {
			candidateOnly++
			delete(implemented, route)
		}
	}
	goLoops, err := readWords(goRoutes + ".loops")
	if err != nil {
		return 2, err
	}
	goStartups, err := readWords(goRoutes + ".startups")
	if err != nil {
		return 2, err
	}
	generators, err := r.workers(snapshot, temporary, report, r.getenv("PRODDIFF_GENERATOR"),
		[]string{r.getenv("PRODDIFF_BASE_GENERATOR")}, doc, implemented)
	if err != nil {
		return 2, err
	}
	realStatus, _, err := r.send(15002, "GET", "/api/auth/status", tokens[1], nil, 20*time.Second)
	if err != nil {
		return 2, err
	}
	real := scrub(realStatus, copies[1])
	readonlyPost, err := r.readonlyPost()
	if err != nil {
		return 2, err
	}
	allCases, err := r.enumerate(rows, readonlyPost, doc, copies[0])
	if err != nil {
		return 2, err
	}
	identities := newAgentIdentities()
	proofs := newArtifactProofs(copies)
	trace, err := os.OpenFile(filepath.Join(report, "transport-attempts.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 2, fmt.Errorf("open transport trace: %w", err)
	}
	defer trace.Close()
	var traceMu sync.Mutex
	ask := func(index int, c testCase, timeout int) (reply, error) {
		if c.Method == "GET" && contentSyncRoutes[c.Route] && timeout < contentSyncSeconds {
			timeout = contentSyncSeconds
		}
		started := time.Now()
		answer, failure, err := r.send(15001+index, c.Method, c.Path, tokens[index], c.Body, time.Duration(timeout)*time.Second)
		if err != nil {
			return reply{}, err
		}
		// Schema-only diagnostics: never expanded paths, bodies, credentials or error texts.
		event := newMap()
		event.Set("side", []string{leftLabel, "go"}[index])
		event.Set("method", c.Method)
		event.Set("route", c.Route)
		event.Set("timeout_seconds", int64(timeout))
		event.Set("elapsed_seconds", time.Since(started).Seconds())
		event.Set("status", int64(answer.Status))
		if failure != nil {
			event.Set("error_type", failure.Type)
			if failure.Errno != nil {
				event.Set("errno", int64(*failure.Errno))
			} else {
				event.Set("errno", nil)
			}
		}
		traceMu.Lock()
		_, werr := trace.WriteString(dumpJSON(event, dumpOptions{indent: -1, ensureASCII: true}) + "\n")
		traceMu.Unlock()
		if werr != nil {
			return reply{}, fmt.Errorf("write transport trace: %w", werr)
		}
		return scrub(answer, copies[index]), nil
	}
	compare := func(c testCase) (caseResult, error) {
		if !implemented[routeKey{c.Method, c.Route}] {
			return caseResult{c: c}, nil
		}
		result, err := compareHTTP(
			func(timeout int) (reply, error) { return ask(0, c, timeout) },
			func(timeout int) (reply, error) { return ask(1, c, timeout) },
			func(a, b reply) ([]diff, error) { return r.cmp.responseDiffs(a, b, c.Route, rules, identities, proofs) },
			c.Method == "GET" && bracket[c.Route])
		if err != nil {
			return caseResult{}, err
		}
		return caseResult{c: c, implemented: true, httpResult: result}, nil
	}
	results, err := comparisonResults(allCases, compare)
	if err != nil {
		return 2, err
	}
	return r.summarize(summaryInput{record: record, report: report, rows: rows, rules: rules, results: results,
		generators: generators, real: real, doc: doc, identities: identities, proofs: proofs, cases: len(allCases),
		leftRoutes: leftRoutes, goLoops: goLoops, goStartups: goStartups, candidateOnly: candidateOnly,
		unmapped: unmappedSet, loadStart: loadStart})
}

func (r *run) readonlyPost() (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(r.parity, "proddiff-readonly-post.json"))
	if err != nil {
		return nil, fmt.Errorf("read read-only POST list: %w", err)
	}
	value, err := loadJSONLoose(data)
	if err != nil {
		return nil, fmt.Errorf("parse read-only POST list: %w", err)
	}
	out := map[string]bool{}
	if m, ok := value.(*Map); ok {
		for _, k := range m.keys {
			out[pyStr(k)] = true
		}
	}
	return out, nil
}

type summaryInput struct {
	record              bool
	report              string
	rows                [][]string
	rules               []rule
	results             []caseResult
	generators          generatorResults
	real                reply
	doc                 *Map
	identities          *agentIdentities
	proofs              *artifactProofs
	cases               int
	leftRoutes          string
	goLoops, goStartups map[string]bool
	candidateOnly       int
	unmapped            map[string]bool
	loadStart           string
}

func (r *run) summarize(in summaryInput) (int, error) {
	found := in.generators.found
	generatorCoverage := in.generators.coverage
	samples := in.generators.samples
	countsByKind := newCounts()
	coverage := newMap()
	failures := newCounts()
	httpSuccesses := map[string]map[string]bool{}
	var jsonSample *reply
	transport := [2]int64{}
	responses, err := os.OpenFile(filepath.Join(in.report, "responses.jsonl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 2, fmt.Errorf("open responses: %w", err)
	}
	defer responses.Close()
	writer := bufio.NewWriter(responses)
	agents := asList(getOr(section(in.doc, "system"), "agents", []any{}))
	for _, result := range in.results {
		c := result.c
		key := c.Method + " " + c.Route
		status := "compared"
		if !result.implemented {
			status = statusNotImplemented
		}
		rowValue, ok := coverage.Get(key)
		if !ok {
			rowValue = newMap()
			coverage.Set(key, rowValue)
		}
		row := rowValue.(*Map)
		increment(row, status)
		increment(row, "no-snapshot-parameter", boolInt(c.NoData))
		if !result.implemented {
			continue
		}
		left, right := result.left, result.right
		transport[0] += int64(result.retries[0])
		transport[1] += int64(result.retries[1])
		countsByKind.add("bracket-rescued", boolInt(result.rescued > 0))
		line := newMap()
		line.Set("method", c.Method)
		line.Set("route", c.Route)
		line.Set("path", c.Path)
		line.Set(leftLabel, replyValue(left))
		line.Set("go", replyValue(right))
		if _, err := writer.WriteString(dumpJSON(line, dumpOptions{indent: -1}) + "\n"); err != nil {
			return 2, fmt.Errorf("write responses: %w", err)
		}
		countsByKind.add("compared", 1)
		if err := r.corpusOut.add(c.Route, left, right); err != nil {
			return 2, fmt.Errorf("write corpus: %w", err)
		}
		if right.Status >= 200 && right.Status < 300 {
			if strings.Contains(right.ContentType, "yaml") {
				if _, ok := samples["yaml"]; !ok {
					samples["yaml"] = right
				}
			}
			if strings.Contains(right.ContentType, "text/plain") && (strings.Contains(c.Route, "/config/") ||
				strings.Contains(c.Route, "/generate/") || strings.HasSuffix(c.Route, "/install-script")) {
				if _, ok := samples["text"]; !ok {
					samples["text"] = right
				}
			}
			if strings.Contains(right.ContentType, "json") && (jsonSample == nil || len(right.Body) > len(jsonSample.Body)) {
				sample := right
				jsonSample = &sample
			}
			for name := range right.Headers {
				if !ignoredHeaders[name] {
					if _, ok := samples["headers"]; !ok {
						samples["headers"] = right
					}
					break
				}
			}
		}
		rawPath, rawQuery, _ := strings.Cut(c.Path, "?")
		queryValues := parseQS(rawQuery)
		for i, generatorKey := range generatorCoverage.keys {
			grow := generatorCoverage.vals[i].(*Map)
			name := strings.TrimPrefix(pyStr(generatorKey), "GENERATOR ")
			if !pyEqual(getOr(grow, "mode", nil), "http") || !pyEqual(getOr(grow, "route", nil), key) {
				continue
			}
			if strings.HasPrefix(name, "subscription-") {
				format := "mihomo"
				if values := queryValues["format"]; len(values) > 0 {
					format = values[0]
				}
				if name != "subscription-"+format {
					continue
				}
			}
			if name == "agent-mihomo" || name == "agent-mosdns" || name == "agent-oxidns" || name == "agent-surge" {
				segments := strings.Split(rawPath, "/")
				agentID := ""
				if len(segments) > 3 {
					agentID, _ = url.PathUnescape(segments[3])
				}
				var agent any = newMap()
				for _, a := range agents {
					if pyStr(getOr(a, "id", nil)) == agentID {
						agent = a
						break
					}
				}
				if name != "agent-"+pyStr(serviceType(agent)) {
					continue
				}
			}
			increment(grow, "compared")
			if left.Status >= 200 && left.Status < 300 && right.Status >= 200 && right.Status < 300 {
				identity := rawPath
				if strings.HasPrefix(name, "subscription-") {
					profile := ""
					if values := queryValues["profile_id"]; len(values) > 0 {
						profile = values[0]
					}
					identity += "?" + profile
				}
				if httpSuccesses[pyStr(generatorKey)] == nil {
					httpSuccesses[pyStr(generatorKey)] = map[string]bool{}
				}
				httpSuccesses[pyStr(generatorKey)][identity] = true
				grow.Set("success", int64(len(httpSuccesses[pyStr(generatorKey)])))
			}
		}
		for i, sideReply := range []reply{left, right} {
			label := []string{leftLabel, "go"}[i]
			if sideReply.Status == 0 || connectionFailure.Match(sideReply.Body) {
				failures.add(label+"-connection-failed", 1)
			}
		}
		if left.Status == 0 || right.Status == 0 {
			if left.Status == 0 && right.Status == 0 {
				failures.add("both-transport-failure", 1)
			} else {
				failures.add("one-sided-transport-failure", 1)
			}
		}
		diffs, err := r.cmp.responseDiffs(left, right, c.Route, in.rules, in.identities, in.proofs)
		if err != nil {
			return 2, err
		}
		for _, d := range diffs {
			found[[3]string{key, d.Path, d.Kind}] = true
			countsByKind.add(domainOf(c.Route)+":"+d.Kind, 1)
		}
	}
	if err := writer.Flush(); err != nil {
		return 2, fmt.Errorf("write responses: %w", err)
	}
	if jsonSample != nil {
		samples["http-json"] = *jsonSample
	}
	checks, err := r.selfCheck(in.real, samples)
	if err != nil {
		return 2, err
	}
	passed := make([]string, len(checks))
	checkMap := newMap()
	for i, name := range checks {
		passed[i] = name + "=PASS"
		checkMap.Set(name, "PASS")
	}
	fmt.Fprintln(r.stdout, "self-check: "+strings.Join(passed, " "))
	for i, k := range generatorCoverage.keys {
		coverage.Set(k, generatorCoverage.vals[i])
	}
	baselinePath := filepath.Join(r.parity, "proddiff-baseline-go.txt")
	baselineText, err := os.ReadFile(baselinePath)
	if err != nil {
		return 2, fmt.Errorf("read baseline: %w", err)
	}
	baseline := map[[3]string]bool{}
	reasons := map[[3]string]string{}
	for _, line := range strings.Split(string(baselineText), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			return 2, errBaselineLine
		}
		reason := strings.TrimSpace(parts[3])
		if !in.record && (reason == "" || reason == "open") {
			return 2, errBaselineReason
		}
		entry := [3]string{parts[0], parts[1], parts[2]}
		baseline[entry] = true
		reasons[entry] = parts[3]
	}
	sortedFound := sortedEntries(found)
	var formatted strings.Builder
	for i, entry := range sortedFound {
		if i > 0 {
			formatted.WriteByte('\n')
		}
		reason, ok := reasons[entry]
		if !ok {
			reason = "open"
		}
		formatted.WriteString(strings.Join(entry[:], "\t") + "\t" + reason)
	}
	formatted.WriteByte('\n')
	if err := os.WriteFile(filepath.Join(in.report, "differences.tsv"), []byte(formatted.String()), 0o600); err != nil {
		return 2, fmt.Errorf("write differences: %w", err)
	}
	if in.record {
		header := "# Route template\tkey path\tdifference type\treason (entries marked open must be triaged before commit)\n"
		if err := os.WriteFile(baselinePath, []byte(header+formatted.String()), 0o644); err != nil {
			return 2, fmt.Errorf("record baseline: %w", err)
		}
		baseline = found
	}
	var newEntries, staleEntries [][3]string
	for _, entry := range sortedFound {
		if !baseline[entry] {
			newEntries = append(newEntries, entry)
		}
	}
	for _, entry := range sortedEntries(baseline) {
		if !found[entry] {
			staleEntries = append(staleEntries, entry)
		}
	}
	current := map[string]bool{}
	for i, k := range coverage.keys {
		name := pyStr(k)
		row := coverage.vals[i]
		if counter(row, "compared") != 0 {
			current[name] = true
		}
		// A route that loses every snapshot-derived parameter keeps only its error branch.
		if !strings.HasPrefix(name, "GENERATOR ") && counter(row, "compared") > counter(row, "no-snapshot-parameter") {
			current[name+" snapshot-parameter"] = true
		}
	}
	for i, k := range generatorCoverage.keys {
		if counter(generatorCoverage.vals[i], "success") != 0 {
			current[pyStr(k)+" success"] = true
		}
	}
	coveragePath := filepath.Join(r.parity, "proddiff-coverage.txt")
	coverageText, err := os.ReadFile(coveragePath)
	if err != nil {
		return 2, fmt.Errorf("read coverage: %w", err)
	}
	previous := map[string]bool{}
	for _, line := range splitLines(string(coverageText)) {
		if line != "" && !strings.HasPrefix(line, "#") {
			previous[line] = true
		}
	}
	if in.record {
		keys := sortedStrings(current)
		text := "# Previously compared route templates; removals fail the gate.\n" + strings.Join(keys, "\n") + "\n"
		if err := os.WriteFile(coveragePath, []byte(text), 0o644); err != nil {
			return 2, fmt.Errorf("record coverage: %w", err)
		}
		previous = current
	}
	lost := map[string]bool{}
	for line := range previous {
		if !current[line] {
			lost[line] = true
		}
	}
	floorPath := filepath.Join(r.parity, "proddiff-generator-coverage.json")
	if in.record {
		if err := os.WriteFile(floorPath, []byte(dumpJSON(generatorCoverage, dumpOptions{indent: 2})+"\n"), 0o644); err != nil {
			return 2, fmt.Errorf("record generator coverage: %w", err)
		}
	}
	floorText, err := os.ReadFile(floorPath)
	if err != nil {
		return 2, fmt.Errorf("read generator coverage: %w", err)
	}
	floorValue, err := loadJSONLoose(floorText)
	if err != nil {
		return 2, fmt.Errorf("parse generator coverage: %w", err)
	}
	floors, ok := floorValue.(*Map)
	if !ok {
		return 2, errors.New("generator coverage is not an object")
	}
	for _, line := range successfulOutputRegressions(floors, generatorCoverage) {
		lost[line] = true
	}
	unique := map[string]int64{}
	for entry := range found {
		unique[domainOf(entry[0])+":"+entry[2]]++
	}
	uniqueMap := newMap()
	for _, k := range sortedStrings(boolKeys(unique)) {
		uniqueMap.Set(k, unique[k])
	}
	var getCompared, getErrorOnly, getNotImplemented, noSnapshot, routesCompared, notImplemented int64
	httpRows := 0
	for i, k := range coverage.keys {
		name := pyStr(k)
		row := coverage.vals[i]
		if counter(row, statusNotImplemented) > 0 {
			notImplemented++
		}
		if strings.HasPrefix(name, "GENERATOR ") {
			continue
		}
		httpRows++
		compared, missing := counter(row, "compared"), counter(row, "no-snapshot-parameter")
		get := strings.HasPrefix(name, "GET ")
		if get && compared > 0 {
			getCompared++
		}
		if get && compared > 0 && compared <= missing {
			getErrorOnly++
		}
		if get && counter(row, statusNotImplemented) > 0 {
			getNotImplemented++
		}
		noSnapshot += missing
		if compared > 0 {
			routesCompared++
		}
	}
	getTotal := 0
	for _, row := range in.rows {
		if row[0] == "GET" {
			getTotal++
		}
	}
	leftLoops, err := readWords(in.leftRoutes + ".loops")
	if err != nil {
		return 2, err
	}
	leftStartups, err := readWords(in.leftRoutes + ".startups")
	if err != nil {
		return 2, err
	}
	retries := newMap()
	retries.Set(leftLabel, transport[0])
	retries.Set("go", transport[1])
	load := newMap()
	load.Set("start", in.loadStart)
	load.Set("end", r.loadAvg())
	startups := sortedStrings(in.goStartups)
	startupList := make([]any, len(startups))
	for i, s := range startups {
		startupList[i] = s
	}
	summary := newMap()
	summary.Set("GET_routes_compared", getCompared)
	summary.Set("GET_routes_error_branch_only", getErrorOnly)
	summary.Set("GET_Go_not_implemented", getNotImplemented)
	summary.Set("mode", "go")
	summary.Set(leftLabel+"_disabled_background", int64(len(difference(leftLoops, leftStartups))))
	summary.Set("go_disabled_background", int64(len(difference(in.goLoops, in.goStartups))))
	summary.Set("go_startup_once", startupList)
	summary.Set("candidate_only_routes", int64(in.candidateOnly))
	summary.Set("unique_differences_by_domain_type", uniqueMap)
	summary.Set("no_snapshot_parameter_cases", noSnapshot)
	summary.Set("GET_routes_total", int64(getTotal))
	summary.Set("routes_total", int64(httpRows))
	summary.Set("routes_compared", routesCompared)
	summary.Set("Go_not_implemented", notImplemented)
	summary.Set("generators", generatorCoverage)
	summary.Set("self_checks", checkMap)
	summary.Set("cases", int64(in.cases))
	summary.Set("counts", countsByKind.m)
	summary.Set("connection_failures", failures.m)
	summary.Set("transport_retries", retries)
	summary.Set("loadavg", load)
	summary.Set("coverage_regressions", int64(len(lost)))
	summary.Set("unmapped_routes", int64(len(in.unmapped)))
	summary.Set("unique_differences", int64(len(found)))
	summary.Set("new", int64(len(newEntries)))
	summary.Set("stale", int64(len(staleEntries)))
	summary.Set("self_check", "PASS")
	pretty := dumpJSON(summary, dumpOptions{indent: 2})
	if err := os.WriteFile(filepath.Join(in.report, "summary.json"), []byte(pretty), 0o600); err != nil {
		return 2, fmt.Errorf("write summary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(in.report, "coverage.json"), []byte(dumpJSON(coverage, dumpOptions{indent: 2})), 0o600); err != nil {
		return 2, fmt.Errorf("write coverage: %w", err)
	}
	phase2 := []string{
		"POST /api/profiles", "POST /api/profiles/<profile_id>/clone",
		"POST /api/config/import", "POST /api/subscriptions/<sub_id>/fetch",
		"POST /api/rules/compose", "PUT /api/agents/<agent_id>",
		"POST /api/agents/<agent_id>/heartbeat", "POST /api/agents/<agent_id>/push-config",
		"POST /api/routers/<router_id>/apply", "POST /api/routers/<router_id>/routes/apply",
	}
	var md strings.Builder
	md.WriteString("# Production differential\n\n```json\n" + pretty + "\n```\n\nDifferences: route template / schema path / type only.\n\n")
	md.WriteString(formatted.String() + "\nPhase two: replay writes with DB/file/device-effect comparison.\n\n")
	for i, route := range phase2 {
		if i > 0 {
			md.WriteByte('\n')
		}
		md.WriteString("- " + route)
	}
	md.WriteByte('\n')
	if err := os.WriteFile(filepath.Join(in.report, "report.md"), []byte(md.String()), 0o600); err != nil {
		return 2, fmt.Errorf("write report: %w", err)
	}
	// A safe report is deliberately stored separately from private server logs.
	fmt.Fprintln(r.stdout, dumpJSON(summary, dumpOptions{indent: -1}))
	staleNote := ""
	if len(staleEntries) > 0 {
		staleNote = "; update baseline"
	}
	fmt.Fprintf(r.stdout, "baseline: new=%d stale=%d%s\n", len(newEntries), len(staleEntries), staleNote)
	for _, entry := range newEntries {
		fmt.Fprintln(r.stdout, "NEW\t"+strings.Join(entry[:], "\t"))
	}
	for _, line := range sortedStrings(lost) {
		fmt.Fprintln(r.stdout, "COVERAGE-REGRESSION\t"+line)
	}
	for _, line := range sortedStrings(in.unmapped) {
		fmt.Fprintln(r.stdout, line)
	}
	if len(newEntries) > 0 || len(staleEntries) > 0 || len(lost) > 0 || len(in.unmapped) > 0 ||
		failures.get("one-sided-transport-failure") > 0 {
		return 1, nil
	}
	return 0, nil
}

func replyValue(r reply) []any {
	headers := newMap()
	for _, name := range sortedStrings(stringKeys(r.Headers)) {
		headers.Set(name, r.Headers[name])
	}
	return []any{int64(r.Status), r.ContentType, strings.ToValidUTF8(string(r.Body), "�"), headers}
}

func sortedEntries(set map[[3]string]bool) [][3]string {
	out := make([][3]string, 0, len(set))
	for entry := range set {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		for k := 0; k < 3; k++ {
			if out[i][k] != out[j][k] {
				return out[i][k] < out[j][k]
			}
		}
		return false
	})
	return out
}

func sortedStrings[V any](set map[string]V) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stringKeys(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func boolKeys(m map[string]int64) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func difference(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	return out
}

// parseQS is urllib.parse.parse_qs: blank values are dropped.
func parseQS(raw string) map[string][]string {
	values, _ := url.ParseQuery(raw)
	out := map[string][]string{}
	for k, list := range values {
		for _, v := range list {
			if v != "" {
				out[k] = append(out[k], v)
			}
		}
	}
	return out
}
