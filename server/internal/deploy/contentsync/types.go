// Package contentsync compares desired and successfully applied Agent contents.
// It owns bounded in-memory caches and lifecycle-tracked jobs, not publication.
package contentsync

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/deploy/pushdiff"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/redact"
)

// Production cache and worker bounds.
const (
	Workers      = 2
	SuccessTTL   = 15 * time.Minute
	ErrorTTL     = 30 * time.Second
	MaxEntries   = 512
	MaxDetails   = 8
	MaxListBytes = 48 * 1024 * 1024
)

// ErrNoEntries means the current comparison has no such retained file/group.
var ErrNoEntries = errors.New("content comparison has no retained entries")

// PrepareFunc prepares immutable desired contents, without publication.
type PrepareFunc func(context.Context, deploy.Target, *deploy.Source, string, string) (*deploy.Bundle, error)

// Options configures a Manager-owned checker; zero limits use production values.
// Clock nil uses lifecycle.ClockFrom on the originating owner context.
type Options struct {
	Clock                                lifecycle.Clock
	Spawner                              lockx.Spawner
	Workers                              int
	SuccessTTL, ErrorTTL                 time.Duration
	MaxEntries, MaxDetails, MaxListBytes int
	Prepare                              PrepareFunc
	LoadReceipt                          func(context.Context, deploy.Target) (*deploy.Receipt, error)
	DefaultProfileID                     func(context.Context) (string, error)
}

// Result is a content status, with a secret-free reason or completion timestamp.
type Result struct {
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"`
}

// Answer is an immutable itemized comparison or a simple status result.
type Answer struct {
	Result
	raw json.RawMessage
}

// MarshalJSON writes the original typed comparison without exposing its cache.
func (a Answer) MarshalJSON() ([]byte, error) {
	if a.raw != nil {
		return append([]byte(nil), a.raw...), nil
	}
	return json.Marshal(a.Result)
}
func (a Answer) clone() Answer { a.raw = append(json.RawMessage(nil), a.raw...); return a }

// EntriesQuery names a page of one immutable comparison version.
type EntriesQuery struct {
	Version, Path, Change string
	Offset, Limit         int
	Query                 pushdiff.Query
	Find                  func(string) []redact.Range
}

// EntriesResult is either a current page or a stale-version explanation.
type EntriesResult struct {
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Version string `json:"version,omitempty"`
	File    string `json:"file,omitempty"`
	Change  string `json:"change,omitempty"`
	*pushdiff.Page
}
