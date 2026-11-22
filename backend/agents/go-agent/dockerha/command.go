// Package dockerha implements the one-shot commands that the panel's Docker
// OxiDNS HA guide, migration script and Compose bundle run inside the Agent
// image: first enrollment and its proofs, the migration check of an old
// single-OxiDNS container, the read-only inspection and the host DNS port
// probe. It does not render those scripts (the panel does) and never starts
// services itself; ha-bootstrap hands over to the image entrypoint.
package dockerha

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"syscall"
)

// Commands are the subcommand names Run accepts (configflow-agent <name>).
var Commands = map[string]bool{
	"ha-bootstrap": true, "ha-enroll": true, "ha-enroll-check": true,
	"ha-migration-check": true, "ha-inspect": true, "ha-port-check": true,
}

const (
	defaultAgentDir   = "/opt/configflow-agent"
	defaultEntrypoint = "/entrypoint.sh"
	oxidnsConfigPath  = "/etc/oxidns/config.yaml"
	dnsPort           = 53
)

// entrypointArgs start the image's normal service path after ha-bootstrap.
var entrypointArgs = []string{"/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"}

// Run executes one subcommand and returns the process exit code. Refusals
// are printed to stderr; status lines to stdout.
func Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
			return handOver(entrypoint, stderr)
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

// handOver replaces this process with the image entrypoint, keeping the
// environment, exactly like the container's default command.
func handOver(entrypoint string, stderr io.Writer) int {
	argv := append([]string{entrypoint}, entrypointArgs...)
	err := syscall.Exec(entrypoint, argv, os.Environ())
	fmt.Fprintf(stderr, "ha-bootstrap: start %s: %v\n", entrypoint, err)
	return 1
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
