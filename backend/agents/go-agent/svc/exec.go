package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Program returns the command of a program: the argv override written by
// Setup, or the Spec default.
func Program(stateDir, name string) ([]string, error) {
	spec, ok := Specs[name]
	if !ok {
		return nil, ErrUnknown
	}
	raw, err := os.ReadFile(argvFile(stateDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return spec.Argv, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s command: %w", name, err)
	}
	var argv []string
	if err := json.Unmarshal(raw, &argv); err != nil || len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return nil, fmt.Errorf("invalid %s command override", name)
	}
	return argv, nil
}

// Exec is the s6 run script of a program: it records the start time for
// Finish, merges stderr into the logged stdout and replaces itself with the
// program.
func Exec(stateDir, name string) error {
	argv, err := Program(stateDir, name)
	if err != nil {
		return err
	}
	stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := os.WriteFile(startedFile(stateDir, name), []byte(stamp), 0o600); err != nil {
		return fmt.Errorf("record %s start: %w", name, err)
	}
	if Specs[name].Logged {
		if err := syscall.Dup2(1, 2); err != nil {
			return fmt.Errorf("redirect %s stderr: %w", name, err)
		}
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}

// Finisher implements the s6 finish script that reproduces Supervisor's
// startsecs/startretries: an exit within the start period is a failed start;
// after StartRetries failed starts in a row the program is FATAL and s6 is
// told (exit code 125) not to restart it.
type Finisher struct {
	StateDir   string
	CommandDir string
	Run        Runner
	Now        func() time.Time
}

// NewFinisher returns the Finisher of the image.
func NewFinisher() *Finisher {
	return &Finisher{StateDir: StateDir, CommandDir: CommandDir, Run: execRunner, Now: time.Now}
}

// finishPermanent is the finish exit code that stops s6 from restarting.
const finishPermanent = 125

// Finish handles one exit of program name; serviceDir is the s6 service
// directory (the finish script's working directory). It returns the finish
// script's exit code.
func (f *Finisher) Finish(ctx context.Context, name, serviceDir string) int {
	spec, ok := Specs[name]
	if !ok {
		return 0
	}
	// A requested stop is not a failure.
	out, err := f.Run(ctx, filepath.Join(f.CommandDir, "s6-svstat"), "-o", "wantedup", serviceDir)
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return 0
	}
	raw, _ := os.ReadFile(startedFile(f.StateDir, name))
	started, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err == nil && f.Now().Sub(time.UnixMilli(started)) >= spec.StartSecs {
		// The program had started; this is an ordinary autorestart.
		os.Remove(failuresFile(f.StateDir, name))
		return 0
	}
	raw, _ = os.ReadFile(failuresFile(f.StateDir, name))
	failures, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	failures++
	if failures > spec.StartRetries {
		os.Remove(failuresFile(f.StateDir, name))
		if err := os.WriteFile(fatalFile(f.StateDir, name), nil, 0o600); err != nil {
			return 0
		}
		return finishPermanent
	}
	os.WriteFile(failuresFile(f.StateDir, name), []byte(strconv.Itoa(failures)), 0o600)
	return 0
}
