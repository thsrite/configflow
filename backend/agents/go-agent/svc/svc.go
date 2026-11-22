// Package svc controls the long-running programs of the Agent Docker image
// (the Agent, the DNS/proxy cores and keepalived) through a small Manager
// interface. The image implementation drives s6 (s6-overlay); the HA
// companion implementation forwards keepalived control over a Unix socket to
// the companion container. Programs and their supervision parameters are
// described once in Specs, from which the s6-rc source tree is generated at
// container start (Setup).
//
// The package does not decide which programs a container runs (the image
// entrypoint does) and does not manage native systemd/OpenRC services.
package svc

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// State is a program state in Supervisor's vocabulary, which the Agent, the
// panel's generated scripts and the supervisorctl compatibility script
// already understand.
type State string

// Program states.
const (
	StateRunning  State = "RUNNING"
	StateStarting State = "STARTING"
	StateBackoff  State = "BACKOFF"
	StateStopping State = "STOPPING"
	StateStopped  State = "STOPPED"
	StateFatal    State = "FATAL"
)

// Transitional reports a state that will change without a new command.
func (s State) Transitional() bool {
	return s == StateStarting || s == StateBackoff || s == StateStopping
}

// Status is the observed state of one program.
type Status struct {
	Name   string
	State  State
	PID    int
	Uptime time.Duration
}

// String renders the Supervisor-style status line
// ("oxidns RUNNING pid 12, uptime 0:01:02").
func (s Status) String() string {
	if s.State == StateRunning && s.PID > 0 {
		total := int(s.Uptime / time.Second)
		return fmt.Sprintf("%-32s %-8s pid %d, uptime %d:%02d:%02d", s.Name, s.State, s.PID, total/3600, total/60%60, total%60)
	}
	return fmt.Sprintf("%-32s %s", s.Name, s.State)
}

// ErrUnknown means the program is not supervised in this container.
var ErrUnknown = errors.New("program is not supervised in this container")

// ErrFatal means the program exited too quickly too often and is no longer
// restarted (Supervisor's FATAL after startretries).
var ErrFatal = errors.New("program failed to stay up and is no longer restarted")

// Manager starts, stops and inspects supervised programs by name. Start and
// Restart return once the program has stayed up for its start period (or
// failed for good); Stop returns once the program is down.
type Manager interface {
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Status(ctx context.Context, name string) (Status, error)
}

// Do runs one of the actions start, stop or restart.
func Do(ctx context.Context, m Manager, action, name string) error {
	switch action {
	case "start":
		return m.Start(ctx, name)
	case "stop":
		return m.Stop(ctx, name)
	case "restart":
		return m.Restart(ctx, name)
	}
	return fmt.Errorf("unsupported action %q", action)
}

// ContainerManager is the service_manager value of programs supervised by
// the Agent Docker image (s6).
const ContainerManager = "s6"

// LegacyContainerManager is the value images before 1.9.3-go wrote while
// Supervisor ran the programs. It is only read, as a synonym of
// ContainerManager: HA journals and Agent configurations written by those
// images, and Docker Agents not yet moved to the new image, still carry it.
// Planned for removal in the next major release.
const LegacyContainerManager = "supervisor"

// NormalizeManager maps the legacy container value to ContainerManager and
// returns every other value unchanged.
func NormalizeManager(manager string) string {
	if manager == LegacyContainerManager {
		return ContainerManager
	}
	return manager
}

// IsContainerManager reports a container-supervised value, new or legacy.
func IsContainerManager(manager string) bool {
	return NormalizeManager(manager) == ContainerManager
}
