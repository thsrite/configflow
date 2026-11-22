// Package dockerha implements the one-shot commands that the panel's Docker
// OxiDNS HA guide, migration script and Compose installations run inside the
// Agent image: first enrollment and its proofs, the compact and direct
// Compose installation checks (ha-compact, ha-compose), the HA companion's
// mount and identity check, the migration check of an old single-OxiDNS
// container, the read-only inspection and the host DNS port probe. It does
// not render those scripts (the panel does) and never starts services
// itself; the entry commands hand over to the image entrypoint.
package dockerha

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
)

// Commands are the subcommand names Run accepts (configflow-agent <name>).
var Commands = map[string]bool{
	"ha-bootstrap": true, "ha-enroll": true, "ha-enroll-check": true,
	"ha-migration-check": true, "ha-inspect": true, "ha-port-check": true,
	"ha-compose": true, "ha-compact": true, "ha-companion-check": true,
}

const (
	defaultAgentDir   = "/opt/configflow-agent"
	defaultEntrypoint = "/entrypoint.sh"
	oxidnsConfigPath  = "/etc/oxidns/config.yaml"
	dnsPort           = 53
)

// entrypointArgs start the image's normal service path (s6-overlay) after
// ha-bootstrap, ha-compose run and ha-compact.
var entrypointArgs = []string{"/init"}

// Run executes one subcommand and returns the process exit code. Refusals
// are printed to stderr; status lines to stdout.
func Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch name {
	case "ha-compose":
		return runDirect(ctx, args, stdout, stderr)
	case "ha-compact":
		return runCompact(ctx, args, stdout, stderr)
	case "ha-companion-check":
		if len(args) != 0 {
			fmt.Fprintln(stderr, msgUsage)
			return 2
		}
		return report(name, CheckCompanion(ctx, DefaultCompanionPaths, os.Getenv), stderr)
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	agentDir := flags.String("agent-dir", defaultAgentDir, "Agent persistent directory")
	paths := DefaultBootstrapPaths
	entrypoint := defaultEntrypoint
	if name == "ha-bootstrap" {
		flags.StringVar(&paths.Settings, "settings", paths.Settings, "prepared enrollment settings")
		flags.StringVar(&paths.Setup, "setup", paths.Setup, "prepared owner file")
		flags.StringVar(&entrypoint, "entrypoint", entrypoint, "program started after a successful bootstrap")
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	operands := flags.Args()
	want := map[string]int{"ha-enroll-check": 1, "ha-migration-check": 3}[name]
	if !Commands[name] || len(operands) != want {
		fmt.Fprintln(stderr, msgUsage)
		return 2
	}
	var err error
	switch name {
	case "ha-bootstrap":
		paths.AgentDir = *agentDir
		if err = Bootstrap(ctx, paths, BundleClient(), stdout); err == nil {
			return handOver(entrypoint, entrypointArgs, os.Environ(), stderr)
		}
	case "ha-enroll":
		err = Enroll(ctx, *agentDir, stdin, EnrollClient(), stdout)
	case "ha-enroll-check":
		err = CheckEnrollment(*agentDir, operands[0], stdout)
	case "ha-migration-check":
		err = CheckMigration(MigrationCheck{AgentDir: *agentDir, AgentName: operands[0], ServerURL: operands[1],
			AgentPort: operands[2], OxiDNSPath: oxidnsConfigPath}, stdin, stdout)
	case "ha-inspect":
		err = Inspect(*agentDir, stdout)
	case "ha-port-check":
		err = CheckPort(dnsPort)
	}
	return report(name, err, stderr)
}

// report prints a refusal verbatim (other errors with the command name) and
// returns the exit code.
func report(name string, err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var refusal *Failure
	if errors.As(err, &refusal) {
		fmt.Fprintln(stderr, refusal.Message)
	} else {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
	}
	return 1
}

// handOver replaces this process with the image entrypoint.
func handOver(entrypoint string, args, environ []string, stderr io.Writer) int {
	argv := append([]string{entrypoint}, args...)
	err := syscall.Exec(entrypoint, argv, environ)
	fmt.Fprintf(stderr, "start %s: %v\n", entrypoint, err)
	return 1
}

// ioFailure turns an unexpected error into the installation's I/O refusal,
// as the former scripts did for every OSError.
func ioFailure(err error, message string) error {
	var refusal *Failure
	if err == nil || errors.As(err, &refusal) {
		return err
	}
	return fail(message)
}

// runDirect is `ha-compose prepare|run` (compose_direct installations).
func runDirect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	settings, err := directSettings(os.Getenv("CONFIGFLOW_COMPOSE_INSTALL"))
	if err != nil {
		return report("ha-compose", err, stderr)
	}
	switch {
	case len(args) == 1 && args[0] == "prepare":
		return report("ha-compose", ioFailure(PrepareDirect(settings, DefaultDirectPaths, stdout), msgDirectIO), stderr)
	case len(args) == 1 && args[0] == "run":
		err := ioFailure(RunDirect(ctx, settings, DefaultDirectPaths, BundleClient(), stdout), msgDirectIO)
		if err != nil {
			return report("ha-compose", err, stderr)
		}
		// This does not remove the short-lived value from docker inspect.
		return handOver(defaultEntrypoint, entrypointArgs, withoutVariables(os.Environ(), "CONFIGFLOW_COMPOSE_INSTALL"), stderr)
	}
	return report("ha-compose", fail(msgDirectAction), stderr)
}

// runCompact is `ha-compact [COMMAND...]` (compose_compact installations);
// COMMAND defaults to the image's service supervisor.
func runCompact(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	settings, err := compactSettings(os.LookupEnv)
	if err == nil {
		err = RunCompact(ctx, settings, DefaultCompactPaths, BundleClient(), stdout)
	}
	if err = ioFailure(err, msgCompactIO); err != nil {
		return report("ha-compact", err, stderr)
	}
	command := args
	if len(command) == 0 {
		command = entrypointArgs
	}
	return handOver(defaultEntrypoint, command, CompactEnvironment(os.Environ(), settings.Network), stderr)
}

func withoutVariables(environ []string, names ...string) []string {
	var out []string
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(names, key) {
			out = append(out, entry)
		}
	}
	return out
}

// CheckPort binds UDP and TCP 0.0.0.0:port (without SO_REUSEADDR and
// without listening) and releases both; any failure means the host DNS port
// is taken or not permitted.
func CheckPort(port int) error {
	for _, kind := range []int{syscall.SOCK_DGRAM, syscall.SOCK_STREAM} {
		if err := bindOnce(kind, port); err != nil {
			return fail(msgPortUnavailable)
		}
	}
	return nil
}

func bindOnce(kind, port int) error {
	fd, err := syscall.Socket(syscall.AF_INET, kind|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Port: port}); err != nil {
		return fmt.Errorf("bind: %w", err)
	}
	return nil
}
