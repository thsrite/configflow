// Package webdav uploads complete ConfigFlow backups, applies retention and
// coalesces post-commit backup requests. It does not mutate configuration.
package webdav

import (
	"configflow/internal/jsondecode"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// Retention defaults and limits are shared with the settings API.
const (
	DefaultKeepLatest = 30
	DefaultKeepDaily  = 30
	MaxKeepLatest     = 1000
	MaxKeepDaily      = 3650
	DefaultPath       = "/config-flow-backup/"
)

// Settings is the WebDAV view of saved backup settings; unknown document fields
// stay in the repository and are never rewritten by the backup service.
type Settings struct {
	URL        string `json:"webdav_url"`
	Username   string `json:"webdav_username"`
	Password   string `json:"webdav_password"`
	Path       string `json:"webdav_path"`
	Auto       bool   `json:"auto_backup"`
	KeepLatest int    `json:"keep_latest"`
	KeepDaily  int    `json:"keep_daily"`
}

// UnmarshalJSON retains the historical fallback for invalid imported retention.
func (s *Settings) UnmarshalJSON(data []byte) error {
	type textFields struct {
		URL      string          `json:"webdav_url"`
		Username string          `json:"webdav_username"`
		Password string          `json:"webdav_password"`
		Path     *string         `json:"webdav_path"`
		Auto     bool            `json:"auto_backup"`
		Latest   json.RawMessage `json:"keep_latest"`
		Daily    json.RawMessage `json:"keep_daily"`
	}
	var fields textFields
	if err := jsondecode.Unmarshal(data, &fields); err != nil {
		return err
	}
	*s = Settings{URL: fields.URL, Username: fields.Username, Password: fields.Password, Path: DefaultPath, Auto: fields.Auto, KeepLatest: RetentionValue(fields.Latest, DefaultKeepLatest, MaxKeepLatest), KeepDaily: RetentionValue(fields.Daily, DefaultKeepDaily, MaxKeepDaily)}
	if fields.Path != nil {
		s.Path = *fields.Path
	}
	return nil
}

// RetentionValue accepts only JSON integers in the configured range.
func RetentionValue(raw json.RawMessage, fallback, limit int) int {
	var value int
	if json.Unmarshal(raw, &value) != nil || string(raw) == "null" || value < 0 || value > limit {
		return fallback
	}
	return value
}

// HTTPDoer is the transport used for WebDAV requests.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Service holds a transport and clock; requests use their own 30-second timeout.
// Service runs WebDAV backup tests, uploads, and automatic post-commit backups.
type Service struct {
	HTTP          HTTPDoer
	Now           func() time.Time
	Repo          *repo.Repository
	Tasks         *tasks.Registry
	Notifications *notifications.Service

	workerOnce sync.Once
	worker     *Worker
}

// NewService uses net/http without adding a WebDAV dependency.
func NewService(client HTTPDoer) *Service {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("redirect limit exceeded")
			}
			if len(via) > 0 && (req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme) {
				return errors.New("cross-origin WebDAV redirect refused")
			}
			return nil
		}}
	}
	return &Service{HTTP: client, Now: time.Now}
}

// StatusError reports an HTTP status without retaining a remote URL or body.
type StatusError struct{ StatusCode int }

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d", e.StatusCode) }

// DirectoryError identifies a failed local directory-creation step safely.
type DirectoryError struct {
	Path  string
	Cause error
}

func (e *DirectoryError) Error() string {
	return fmt.Sprintf(msgCreateDirectory, e.Path, DescribeError(e.Cause))
}
func (e *DirectoryError) Unwrap() error { return e.Cause }

// ErrIncompleteSettings means one of the required connection fields is empty.
var ErrIncompleteSettings = errors.New(msgIncomplete)

// ErrUnsafePath rejects ambiguous directory traversal before network I/O.
var ErrUnsafePath = errors.New(msgUnsafePath)

// ValidatePaths checks only path safety, allowing partially filled editor forms.
// URL userinfo and query fields are not directory components.
func (s Settings) ValidatePaths() error {
	if unsafePath(s.Path) {
		return ErrUnsafePath
	}
	u, err := url.Parse(s.URL)
	if err == nil && (unsafePath(u.Path) || unsafePath(u.Opaque)) {
		return ErrUnsafePath
	}
	if err != nil {
		// An invalid port/escape must not hide a plainly unsafe address path.
		address := s.URL
		if end := strings.IndexAny(address, "?#"); end >= 0 {
			address = address[:end]
		}
		if unsafePath(address) {
			return ErrUnsafePath
		}
	}
	return nil
}

func unsafePath(value string) bool {
	for range 8 {
		for _, component := range strings.Split(strings.ReplaceAll(value, `\`, "/"), "/") {
			if component == "." || component == ".." {
				return true
			}
		}
		decoded, err := url.PathUnescape(value)
		if err != nil {
			// Literal percent signs are valid directory data. If malformed escapes
			// coexist with encoded separators/dots, do not partially interpret them.
			lower := strings.ToLower(value)
			return strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%25")
		}
		if decoded == value {
			return false
		}
		value = decoded
	}
	return true // Excessive encoding layers are ambiguous, not ordinary directories.
}

// DescribeError never includes credentials, URLs, remote bodies or raw errors.
func DescribeError(err error) string {
	if errors.Is(err, ErrUnsafePath) {
		return msgUnsafePath
	}
	var status *StatusError
	if errors.As(err, &status) {
		return status.Error()
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "request timeout"
	}
	var invalid *url.Error
	if errors.As(err, &invalid) {
		return "network request failed"
	}
	return "backup operation failed"
}
