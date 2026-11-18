// Package setupflow reads existing first-use wizard records. Mutating wizard
// steps and HTTP routes remain the future C36 module's responsibility.
package setupflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// ErrInvalidSession refuses a corrupt wizard record instead of treating it as no ownership evidence.
// ErrReadSession identifies unavailable or malformed wizard evidence.
var ErrReadSession = errors.New(msgReadSession)

var ErrInvalidSession = errors.New("invalid setup session JSON")

var sessionName = regexp.MustCompile(`^[a-f0-9]{32}\.json$`)

// MaxSessionBytes bounds both read-only evidence and the C36 write owner.
const MaxSessionBytes = 16 * 1024 * 1024

// ReadSession reads one existing session without locks or directory creation.
// It shares the ProfileSessions byte/JSON policy and leaves typed ownership
// validation to the write owner. Missing files retain os.ErrNotExist for races.
func ReadSession(ctx context.Context, dataDir, sessionID string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !sessionName.MatchString(sessionID + ".json") {
		return nil, fmt.Errorf("%w: %w", ErrReadSession, ErrInvalidSession)
	}
	file, err := os.Open(filepath.Join(dataDir, "setup-sessions", sessionID+".json"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadSession, err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, MaxSessionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadSession, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > MaxSessionBytes || !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, fmt.Errorf("%w: %w", ErrReadSession, ErrInvalidSession)
	}
	return raw, nil
}

// Session is an immutable stored wizard record and its sorting/binding fields.
// Raw preserves every unknown member for C36's later read-modify-write owner.
type Session struct {
	Raw       json.RawMessage
	ProfileID string
	UpdatedAt string
}

// ProfileSessions reads one profile's sessions, newest updated_at first, without
// taking wizard locks. Atomic file replacement makes each read a whole record.
func ProfileSessions(ctx context.Context, dataDir, profileID string) ([]Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, e := os.ReadDir(filepath.Join(dataDir, "setup-sessions"))
	if errors.Is(e, os.ErrNotExist) {
		return []Session{}, nil
	}
	if e != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadSession, e)
	}
	out := []Session{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if entry.IsDir() || !sessionName.MatchString(entry.Name()) {
			continue
		}
		raw, e := ReadSession(ctx, dataDir, strings.TrimSuffix(entry.Name(), ".json"))
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}

		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			continue
		}
		var profile string
		if rawID, ok := object["profile_id"]; ok {
			if string(rawID) == "null" || json.Unmarshal(rawID, &profile) != nil {
				return nil, fmt.Errorf("%w: %w", ErrReadSession, ErrInvalidSession)
			}
		}
		if profile != profileID {
			continue
		}
		// These members decide whether disabling a gateway interrupts routing.
		// Wrong-typed evidence is never treated as an absent wizard purpose.
		for _, key := range []string{"goal", "updated_at"} {
			if raw, ok := object[key]; ok && string(raw) != "null" {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					return nil, fmt.Errorf("%w: %w", ErrReadSession, ErrInvalidSession)
				}
			}
		}
		for _, key := range []string{"abandoned", "completed"} {
			if raw, ok := object[key]; ok && string(raw) != "null" {
				var value bool
				if json.Unmarshal(raw, &value) != nil {
					return nil, fmt.Errorf("%w: %w", ErrReadSession, ErrInvalidSession)
				}
			}
		}
		var updated string
		_ = json.Unmarshal(object["updated_at"], &updated)
		out = append(out, Session{Raw: raw, ProfileID: profile, UpdatedAt: updated})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
