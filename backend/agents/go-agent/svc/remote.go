package svc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

// The host-mode HA companion runs keepalived in its own container. The main
// Agent controls that one program through a Unix socket on the shared HA
// volume; the companion answers with its local S6 manager. One request per
// connection: a command line, then one JSON reply line.

// reply is the companion's answer.
type reply struct {
	State  State  `json:"state"`
	PID    int    `json:"pid,omitempty"`
	Uptime int64  `json:"uptime,omitempty"`
	Error  string `json:"error,omitempty"`
	Fatal  bool   `json:"fatal,omitempty"`
}

const (
	requestLimit   = 64
	requestTimeout = 5 * time.Second
	// actionTimeout bounds one start/stop/restart in the companion.
	actionTimeout = 90 * time.Second
)

// ErrUnavailable means the companion's control socket could not be reached.
var ErrUnavailable = errors.New("HA companion control socket is unavailable")

// Remote is the Manager of the companion's keepalived.
type Remote struct {
	Socket string
}

func (r Remote) call(ctx context.Context, name, command string) (Status, error) {
	if name != Keepalived {
		return Status{}, ErrUnknown
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", r.Socket)
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(actionTimeout + requestTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	if _, err := io.WriteString(conn, command+"\n"); err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 4096)).ReadBytes('\n')
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var answer reply
	if err := json.Unmarshal(line, &answer); err != nil {
		return Status{}, fmt.Errorf("invalid companion reply: %w", err)
	}
	status := Status{Name: name, State: answer.State, PID: answer.PID, Uptime: time.Duration(answer.Uptime) * time.Second}
	switch {
	case answer.Fatal:
		return status, ErrFatal
	case answer.Error != "":
		return status, errors.New(answer.Error)
	}
	return status, nil
}

// Start starts the companion's keepalived.
func (r Remote) Start(ctx context.Context, name string) error {
	_, err := r.call(ctx, name, "start")
	return err
}

// Stop stops the companion's keepalived.
func (r Remote) Stop(ctx context.Context, name string) error {
	_, err := r.call(ctx, name, "stop")
	return err
}

// Restart restarts the companion's keepalived.
func (r Remote) Restart(ctx context.Context, name string) error {
	_, err := r.call(ctx, name, "restart")
	return err
}

// Status reads the companion's keepalived state.
func (r Remote) Status(ctx context.Context, name string) (Status, error) {
	return r.call(ctx, name, "status")
}

// Listen opens the control socket (root-only), replacing a stale socket of
// a previous companion run.
func Listen(path string) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("restrict %s: %w", path, err)
	}
	return listener, nil
}

// Serve answers control requests for keepalived until ctx ends or the
// listener fails. Requests are handled one at a time, so start and stop
// never interleave.
func Serve(ctx context.Context, listener net.Listener, manager Manager) error {
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		handle(ctx, conn, manager)
	}
}

func handle(ctx context.Context, conn net.Conn, manager Manager) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(requestTimeout))
	line, err := bufio.NewReader(io.LimitReader(conn, requestLimit)).ReadString('\n')
	if err != nil {
		return // a liveness probe connects and closes without a request
	}
	command := strings.TrimSpace(line)
	answer := reply{}
	actionCtx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	switch command {
	case "status":
	case "start", "stop", "restart":
		if err := Do(actionCtx, manager, command, Keepalived); err != nil {
			answer.Error = err.Error()
			answer.Fatal = errors.Is(err, ErrFatal)
			log.Printf("ha companion: %s keepalived: %v", command, err)
		}
	default:
		answer.Error = "unsupported command"
	}
	if status, err := manager.Status(actionCtx, Keepalived); err == nil {
		answer.State, answer.PID, answer.Uptime = status.State, status.PID, int64(status.Uptime/time.Second)
	} else if answer.Error == "" {
		answer.Error = err.Error()
	}
	raw, _ := json.Marshal(answer)
	conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	conn.Write(append(raw, '\n'))
}
