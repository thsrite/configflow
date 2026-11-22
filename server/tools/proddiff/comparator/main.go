// Command comparator is the offline production-snapshot differential in
// Go-vs-Go mode: a Go baseline build (left, "base") and the Go candidate
// (right) each load a private copy of the same read-only snapshot inside the
// network-less namespace that server/scripts/proddiff.sh creates. It compares
// every GET, the listed read-only POSTs and the generator outputs, applies
// server/parity/proddiff-normalize.json, and checks the Go baseline file,
// route/generator coverage floors and the 23 mutation self-checks.
//
// Console output and recorded files contain schema only (route templates,
// public key paths, difference types, counts). Responses, logs and errors stay
// in the private run directory under ~/cf-prod-snapshot/runs.
//
// It does not start Flask or read backend/; the Python reference mode remains
// in run.py until the switch removes it.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	os.Exit(command(os.Args[1:]))
}

func command(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: comparator run <snapshot> <candidate-binary> [--record-baseline] | crosscheck <corpus> <out> | keys")
		return 2
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "proddiff: cannot read working directory")
		return 2
	}
	keys, err := loadKeys(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "proddiff: cannot read public key names")
		return 2
	}
	cmp := &comparator{keys: keys}
	switch args[0] {
	case "run":
		if mode := os.Getenv("PRODDIFF_MODE"); mode != "go" {
			fmt.Println("proddiff: the Go comparator only supports PRODDIFF_MODE=go")
			return 2
		}
		return runCommandLine(root, cmp, args[1:])
	case "crosscheck":
		if len(args) != 3 {
			return 2
		}
		if err := crosscheck(root, cmp, args[1], args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "crosscheck:", err)
			return 2
		}
		return 0
	case "keys":
		for _, key := range sortedStrings(keys.keys) {
			fmt.Println(key)
		}
		return 0
	}
	return 2
}

func runCommandLine(root string, cmp *comparator, args []string) int {
	r := newRun(root)
	r.cmp = cmp
	if os.Getenv("PRODDIFF_CORPUS") == "1" {
		r.corpus = true
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	finished := make(chan struct{})
	watcher := make(chan struct{})
	go func() {
		defer close(watcher)
		select {
		case <-signals:
			r.stopProcesses()
			r.removeCopies()
			r.fail(errInterrupted)
			os.Exit(2)
		case <-finished:
		}
	}()
	defer func() {
		signal.Stop(signals)
		close(finished)
		<-watcher
	}()
	code, err := r.main(args)
	if err != nil {
		r.fail(err)
		return 2
	}
	return code
}

// removeCopies deletes this run's snapshot copies after an interruption.
func (r *run) removeCopies() {
	r.mu.Lock()
	report := r.report
	r.mu.Unlock()
	if report == "" {
		return
	}
	copies, _ := filepath.Glob(filepath.Join(report, "copies-*"))
	for _, path := range copies {
		_ = os.RemoveAll(path)
	}
}

// publicFailures are fixed texts without snapshot data; anything else is
// reported only by kind, its details only in the private error.txt.
var publicFailures = []error{errStartup, errStartupTimeout, errLogin, errNoProfiles, errWritable, errSnapshotChange,
	errSymlink, errBaselineReason, errBaselineLine, errInterrupted, errObserverTimeout, errRule, errSelfCheckSample}

func (r *run) fail(err error) {
	r.mu.Lock()
	report := r.report
	r.mu.Unlock()
	if report != "" {
		_ = os.WriteFile(filepath.Join(report, "error.txt"), []byte(fmt.Sprintf("%+v\n", err)), 0o600)
	}
	kind := "error"
	for _, known := range publicFailures {
		if errors.Is(err, known) {
			kind = known.Error()
		}
	}
	if errors.Is(err, errSelfCheckFailed) {
		kind = err.Error() // check names only
	}
	fmt.Println("proddiff: infrastructure failure (" + kind + "); inspect private run directory")
}
