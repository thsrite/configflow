package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	// The program keeps this PID (exec) and leads its own process group
	// (s6-supervise starts it with setsid); Finish reaps what it leaves.
	if err := os.WriteFile(pidFile(stateDir, name), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("record %s pid: %w", name, err)
	}
	if Specs[name].Logged {
		if err := syscall.Dup2(1, 2); err != nil {
			return fmt.Errorf("redirect %s stderr: %w", name, err)
		}
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}

// Finisher implements the s6 finish script that reproduces Supervisor's
// startsecs/startretries: an exit within the start period is a failed start,
// retried after a delay growing by one second per failure (Supervisor's
// backoff); after StartRetries failed starts in a row the program is FATAL
// and s6 is told (exit code 125) not to restart it. For programs with
// KillGroup it also ends what the program left in its process group
// (Supervisor's stopasgroup/killasgroup).
type Finisher struct {
	StateDir   string
	CommandDir string
	ProcDir    string
	Run        Runner
	Now        func() time.Time
	Sleep      func(time.Duration)
	Kill       func(pid int, signal syscall.Signal) error
}

// NewFinisher returns the Finisher of the image.
func NewFinisher() *Finisher {
	return &Finisher{StateDir: StateDir, CommandDir: CommandDir, ProcDir: "/proc", Run: execRunner,
		Now: time.Now, Sleep: time.Sleep, Kill: syscall.Kill}
}

// finishPermanent is the finish exit code that stops s6 from restarting.
const finishPermanent = 125

// finishTimeout is the timeout-finish of every program: the longest backoff
// (StartRetries seconds) plus the process group cleanup, with margin.
const finishTimeout = 15 * time.Second

// Backoff steps and the grace period of leftover group members.
const (
	backoffPoll = 250 * time.Millisecond
	groupGrace  = 3 * time.Second
)

// Finish handles one exit of program name; serviceDir is the s6 service
// directory (the finish script's working directory). It returns the finish
// script's exit code.
func (f *Finisher) Finish(ctx context.Context, name, serviceDir string) int {
	spec, ok := Specs[name]
	if !ok {
		return 0
	}
	if spec.KillGroup {
		f.endGroup(name)
	}
	// A requested stop is not a failure.
	if !f.wantedUp(ctx, serviceDir) {
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
	f.backoff(ctx, serviceDir, time.Duration(failures)*time.Second)
	return 0
}

func (f *Finisher) wantedUp(ctx context.Context, serviceDir string) bool {
	out, err := f.Run(ctx, filepath.Join(f.CommandDir, "s6-svstat"), "-o", "wantedup", serviceDir)
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// backoff waits before s6 restarts the program (Supervisor waited 1, 2, 3
// seconds after the first, second, third failed start), ending early when a
// stop is requested meanwhile.
func (f *Finisher) backoff(ctx context.Context, serviceDir string, delay time.Duration) {
	deadline := f.Now().Add(delay)
	for {
		left := deadline.Sub(f.Now())
		if left <= 0 || ctx.Err() != nil {
			return
		}
		f.Sleep(min(left, backoffPoll))
		if !f.wantedUp(ctx, serviceDir) {
			return
		}
	}
}

// endGroup ends the processes the program left in its process group (for
// keepalived: a VRRP child that outlived its parent, which would keep the
// virtual address): SIGTERM first, so it can release its addresses, then
// SIGKILL after groupGrace. Only processes named like the program are
// signalled, so a reused process group number is never hit.
func (f *Finisher) endGroup(name string) {
	raw, err := os.ReadFile(pidFile(f.StateDir, name))
	if err != nil {
		return
	}
	group, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || group <= 1 {
		return
	}
	command := filepath.Base(Specs[name].Argv[0])
	deadline := f.Now().Add(groupGrace)
	signal := syscall.SIGTERM
	for first := true; ; first = false {
		members := f.groupMembers(group, command)
		if len(members) == 0 {
			return
		}
		if first {
			log.Printf("%s exited and left %d process(es) %v in its process group; ending them", name, len(members), members)
		}
		if f.Now().After(deadline) {
			signal = syscall.SIGKILL
		}
		for _, pid := range members {
			f.Kill(pid, signal)
		}
		if signal == syscall.SIGKILL {
			return
		}
		f.Sleep(backoffPoll)
	}
}

// groupMembers lists the processes of process group group whose command
// name is command.
func (f *Finisher) groupMembers(group int, command string) []int {
	entries, err := os.ReadDir(f.ProcDir)
	if err != nil {
		return nil
	}
	var members []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(f.ProcDir, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// pid (comm) state ppid pgrp ...; comm may contain spaces and ")".
		lp, rp := strings.IndexByte(string(raw), '('), strings.LastIndexByte(string(raw), ')')
		if lp < 0 || rp < lp {
			continue
		}
		fields := strings.Fields(string(raw[rp+1:]))
		if len(fields) < 3 || string(raw[lp+1:rp]) != command || fields[2] != strconv.Itoa(group) {
			continue
		}
		members = append(members, pid)
	}
	return members
}
