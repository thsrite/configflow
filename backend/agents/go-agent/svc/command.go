package svc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Commands are the subcommand names Run accepts (configflow-agent <name>).
var Commands = map[string]bool{"svc": true, "svc-exec": true, "svc-finish": true, CompanionServ: true}

// Exit codes of `svc status`, as supervisorctl status: 0 running, 3 not
// running, 4 unknown program.
const (
	exitNotRunning = 3
	exitUnknown    = 4
)

// Run executes one subcommand and returns the process exit code.
//
//	svc status|start|stop|restart NAME  control a program of this container
//	svc-exec NAME                       s6 run script (replaces itself)
//	svc-finish NAME [EXIT SIGNAL]       s6 finish script
//	ha-companion-serve                  companion control socket
func Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) int {
	switch name {
	case "svc":
		if len(args) != 2 {
			fmt.Fprintln(stderr, msgUsage)
			return 2
		}
		return control(ctx, NewS6(), args[0], args[1], stdout, stderr)
	case "svc-exec":
		if len(args) != 1 {
			fmt.Fprintln(stderr, msgUsage)
			return 2
		}
		err := Exec(StateDir, args[0])
		fmt.Fprintf(stderr, msgExecFailed+"\n", args[0], err)
		return 111
	case "svc-finish":
		if len(args) < 1 {
			fmt.Fprintln(stderr, msgUsage)
			return 2
		}
		dir, err := os.Getwd()
		if err != nil {
			return 0
		}
		return NewFinisher().Finish(ctx, args[0], dir)
	case CompanionServ:
		return serveCompanion(ctx, stderr)
	}
	fmt.Fprintln(stderr, msgUsage)
	return 2
}

func control(ctx context.Context, m Manager, action, name string, stdout, stderr io.Writer) int {
	if action == "status" {
		status, err := m.Status(ctx, name)
		switch {
		case errors.Is(err, ErrUnknown):
			fmt.Fprintf(stdout, msgUnknownProgram+"\n", name)
			return exitUnknown
		case err != nil:
			fmt.Fprintf(stderr, msgStatusFailed+"\n", name, err)
			return 1
		}
		fmt.Fprintln(stdout, status.String())
		if status.State != StateRunning {
			return exitNotRunning
		}
		return 0
	}
	if action != "start" && action != "stop" && action != "restart" {
		fmt.Fprintln(stderr, msgUsage)
		return 2
	}
	err := Do(ctx, m, action, name)
	switch {
	case errors.Is(err, ErrUnknown):
		fmt.Fprintf(stderr, msgUnknownProgram+"\n", name)
		return exitUnknown
	case errors.Is(err, ErrFatal):
		fmt.Fprintf(stderr, msgSpawnFailed+"\n", name)
		return 1
	case err != nil:
		fmt.Fprintf(stderr, msgActionFailed+"\n", name, action, err)
		return 1
	}
	fmt.Fprintf(stdout, msgActionDone+"\n", name, map[string]string{"start": "已启动", "stop": "已停止", "restart": "已重启"}[action])
	return 0
}

// CompanionSocket is the control socket on the shared HA volume.
const CompanionSocket = "/var/lib/configflow-ha/run/control.sock"

func serveCompanion(ctx context.Context, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := os.MkdirAll(filepath.Dir(CompanionSocket), 0o700); err != nil {
		fmt.Fprintf(stderr, msgServeFailed+"\n", err)
		return 1
	}
	listener, err := Listen(CompanionSocket)
	if err != nil {
		fmt.Fprintf(stderr, msgServeFailed+"\n", err)
		return 1
	}
	if err := Serve(ctx, listener, NewS6()); err != nil {
		fmt.Fprintf(stderr, msgServeFailed+"\n", err)
		return 1
	}
	return 0
}
