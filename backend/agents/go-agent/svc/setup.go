package svc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Layout is where Setup writes. DefaultLayout is the image.
type Layout struct {
	SourceDir string
	BundleDir string
	StateDir  string
	LogRoot   string
}

// DefaultLayout is the s6-overlay layout of the Agent image.
var DefaultLayout = Layout{SourceDir: SourceDir, BundleDir: BundleDir, StateDir: StateDir, LogRoot: LogRoot}

// Plan is the set of programs of one container.
type Plan struct {
	// Defined programs are supervised; only Enabled ones start at boot.
	Defined []string
	Enabled []string
	// Argv overrides the default command of a program.
	Argv map[string][]string
	// Release is the oneshot that stops keepalived first at shutdown.
	Release string
}

// MainPlan is an Agent container: the given programs start at boot and
// keepalived is defined for the Agent to start. A macvlan HA container also
// stops keepalived before its Agent and core at shutdown.
func MainPlan(enabled []string, argv map[string][]string, macvlanHA bool) Plan {
	plan := Plan{Enabled: enabled, Argv: argv}
	for name := range Specs {
		if name != CompanionServ {
			plan.Defined = append(plan.Defined, name)
		}
	}
	sort.Strings(plan.Defined)
	if macvlanHA {
		plan.Release = releaseMain
	}
	return plan
}

// CompanionPlan is the host-mode HA companion: the control socket starts at
// boot; keepalived waits for the Agent's start command.
func CompanionPlan() Plan {
	return Plan{Defined: []string{Keepalived, CompanionServ}, Enabled: []string{CompanionServ}, Release: releaseCompanion}
}

// Setup writes the s6-rc source definitions and the boot bundle of plan,
// clears the per-boot state directory and writes argv overrides. It runs at
// every container start before s6-overlay compiles the sources.
func Setup(layout Layout, plan Plan) error {
	defined := map[string]bool{}
	for _, name := range plan.Defined {
		if _, ok := Specs[name]; !ok {
			return fmt.Errorf("unknown program %q", name)
		}
		defined[name] = true
	}
	for _, name := range plan.Enabled {
		if !defined[name] {
			return fmt.Errorf("program %q is enabled but not defined", name)
		}
	}
	if err := os.RemoveAll(layout.StateDir); err != nil {
		return fmt.Errorf("clear service state: %w", err)
	}
	if err := os.MkdirAll(layout.StateDir, 0o700); err != nil {
		return fmt.Errorf("create service state: %w", err)
	}
	// The image owns both directories: definitions of an earlier start
	// (another SERVICE_TYPE, an older image) must not survive.
	for _, dir := range []string{layout.SourceDir, layout.BundleDir} {
		if err := resetDir(dir); err != nil {
			return err
		}
	}
	for _, name := range plan.Defined {
		if err := writeLongrun(layout, Specs[name], defined); err != nil {
			return err
		}
	}
	bundle := append([]string{}, plan.Enabled...)
	for _, name := range plan.Enabled {
		if Specs[name].Logged {
			bundle = append(bundle, name+"-log")
		}
	}
	if plan.Release != "" {
		var after []string
		if plan.Release == releaseMain {
			after = []string{"agent-oxidns"}
		}
		down := Binary + " svc stop " + Keepalived + "\n"
		if err := writeService(filepath.Join(layout.SourceDir, plan.Release), map[string]string{
			"type": "oneshot\n", "up": "/bin/true\n", "down": down}, after); err != nil {
			return err
		}
		bundle = append(bundle, plan.Release)
	}
	for _, name := range bundle {
		if err := os.WriteFile(filepath.Join(layout.BundleDir, name), nil, 0o644); err != nil {
			return fmt.Errorf("enable %s: %w", name, err)
		}
	}
	for name, argv := range plan.Argv {
		if !defined[name] {
			return fmt.Errorf("command override for undefined program %q", name)
		}
		raw, err := json.Marshal(argv)
		if err != nil {
			return fmt.Errorf("encode %s command: %w", name, err)
		}
		if err := os.WriteFile(argvFile(layout.StateDir, name), raw, 0o600); err != nil {
			return fmt.Errorf("write %s command: %w", name, err)
		}
	}
	return nil
}

func resetDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return nil
}

func writeLongrun(layout Layout, spec Spec, defined map[string]bool) error {
	files := map[string]string{
		"type":           "longrun\n",
		"run":            "#!/bin/sh\nexec " + CommandDir + "/with-contenv " + Binary + " svc-exec " + spec.Name + "\n",
		"finish":         "#!/bin/sh\nexec " + Binary + " svc-finish " + spec.Name + "\n",
		"timeout-kill":   strconv.FormatInt(spec.StopTimeout.Milliseconds(), 10) + "\n",
		"timeout-finish": strconv.FormatInt(finishTimeout.Milliseconds(), 10) + "\n",
	}
	var after []string
	for _, name := range spec.After {
		if defined[name] {
			after = append(after, name)
		}
	}
	logName := spec.Name + "-log"
	if spec.Logged {
		files["producer-for"] = logName + "\n"
	}
	if err := writeService(filepath.Join(layout.SourceDir, spec.Name), files, after); err != nil {
		return err
	}
	if !spec.Logged {
		return nil
	}
	logDir := filepath.Join(layout.LogRoot, spec.Name)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create %s log directory: %w", spec.Name, err)
	}
	return writeService(filepath.Join(layout.SourceDir, logName), map[string]string{
		"type": "longrun\n",
		"run": fmt.Sprintf("#!/bin/sh\nexec %s/s6-log n%d s%d %s\n",
			CommandDir, logBackups, logMaxBytes, logDir),
		"consumer-for": spec.Name + "\n",
	}, nil)
}

func writeService(dir string, files map[string]string, after []string) error {
	if err := resetDir(dir); err != nil {
		return err
	}
	for name, content := range files {
		mode := os.FileMode(0o644)
		if name == "run" || name == "finish" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
			return fmt.Errorf("write %s/%s: %w", filepath.Base(dir), name, err)
		}
	}
	if len(after) == 0 {
		return nil
	}
	deps := filepath.Join(dir, "dependencies.d")
	if err := os.Mkdir(deps, 0o755); err != nil {
		return fmt.Errorf("create %s dependencies: %w", filepath.Base(dir), err)
	}
	for _, name := range after {
		if err := os.WriteFile(filepath.Join(deps, name), nil, 0o644); err != nil {
			return fmt.Errorf("write %s dependency: %w", filepath.Base(dir), err)
		}
	}
	return nil
}
