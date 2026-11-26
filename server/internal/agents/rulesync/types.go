// Package rulesync reconciles opted-in rule scopes against successful deployment
// receipts. It never publishes unrelated proxy or DNS drafts.
package rulesync

import (
	"configflow/internal/deploy"
	"context"
	"errors"
	"time"
)

// Worker timing and the minimum native hot-reload Agent version.
const (
	MinAgentVersion = "1.8.30-go"
	PollInterval    = 5 * time.Second
	// IdleFallback is the pause of the idle rule-sync loop (see loop).
	IdleFallback = time.Minute
	RetryAfter   = 60 * time.Second
	RecheckAfter = 600 * time.Second
)

// Settings independently opts in shared library and device rule changes.
type Settings struct {
	Library bool `json:"library"`
	Rules   bool `json:"rules"`
}

// Any reports whether a rule scope is enabled.
func (s Settings) Any() bool { return s.Library || s.Rules }

// Baseline combines a durable receipt with the verified live device files.
type Baseline struct {
	Receipt *deploy.Receipt
	Bundle  *deploy.Bundle
}

// PrepareFunc constructs an isolated candidate from a verified baseline.
type PrepareFunc func(context.Context, deploy.Target, *deploy.Source, Baseline, Settings, string, bool) (*deploy.Bundle, error)

// ErrAgentNotFound means the requested registry entry does not exist.
var ErrAgentNotFound = errors.New("agent not found")

// Error is a safe user-facing validation or isolation refusal.
type Error struct{ Message string }

func (e *Error) Error() string    { return e.Message }
func refuse(message string) error { return &Error{Message: message} }

// Unwrap classifies an isolation refusal for the shared publishing owner.
func (e *Error) Unwrap() error { return &deploy.Refusal{Message: e.Message} }
