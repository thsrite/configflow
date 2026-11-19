package agents

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
)

type clientBindingError struct{ cause error }

func (e *clientBindingError) Error() string { return e.cause.Error() }
func (e *clientBindingError) Unwrap() error { return e.cause }

func clientBindingHandler(next httpx.HandlerFunc) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		response, err := next(c)
		var binding *clientBindingError
		if errors.As(err, &binding) {
			if result := httpx.RepositoryErrorHandler(c, binding.cause); result != nil {
				return result.NoStore(), nil
			}
		}
		return response, err
	}
}

type peerWarningTimes struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (w *peerWarningTimes) due(id string, at time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.last == nil {
		w.last = map[string]time.Time{}
	}
	if last, ok := w.last[id]; ok && at.Sub(last) >= 0 && at.Sub(last) < 10*time.Minute {
		return false
	}
	// Drop expired entries so removed imported Agents do not accumulate forever.
	for key, last := range w.last {
		if at.Sub(last) >= 10*time.Minute {
			delete(w.last, key)
		}
	}
	w.last[id] = at
	return true
}

var clientPeerWarnings peerWarningTimes

func warnClientPeer(a *model.Agent, at time.Time) {
	if !clientPeerWarnings.due(a.ID, at) {
		return
	}
	raw, err := model.Marshal(a)
	if err != nil {
		return
	}
	text := msgPolicyCorruptPeer + logx.LogText(a.ID, 120)
	logger.Warning(redact.DiagnosticText(text, redact.CollectCredentials(json.RawMessage(raw), nil), 512))
}
