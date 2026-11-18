package refresh

import (
	"bytes"
	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/store"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// PublicFailure retains a collaborator's controlled reason and native cause.
// Source text, transport URLs and credentials are redacted before display/logging.
type PublicFailure struct {
	Reason string
	Cause  error
}

func (e *PublicFailure) Error() string { return e.Reason }
func (e *PublicFailure) Unwrap() error { return e.Cause }

// FailureReason renders typed failure categories without exception emulation.
func FailureReason(err error) string {
	var subscription *substore.SubscriptionFetchError
	if errors.As(err, &subscription) {
		if subscription.DirectError != nil {
			err = subscription.DirectError
		} else if subscription.SubStoreError != nil {
			err = subscription.SubStoreError
		}
	}
	var status interface{ HTTPStatus() int }
	if errors.As(err, &status) && status.HTTPStatus() != 0 {
		return fmt.Sprintf(msgUpstreamStatus, status.HTTPStatus())
	}
	var aggregate *rulefetch.RuleFetchError
	if errors.As(err, &aggregate) {
		return fmt.Sprintf(msgRefreshFailed, "RuleFetchError")
	}
	kind := ""
	var fetch *rulefetch.FetchError
	var request *substore.RequestError
	if errors.As(err, &fetch) {
		kind = fetch.Class
	} else if errors.As(err, &request) {
		kind = request.Class
	}
	switch kind {
	case "MissingSchema", "InvalidSchema", "InvalidURL", "InvalidProxyURL":
		return msgInvalidSourceURL
	case "SSLError":
		return msgSourceTLS
	case "Timeout", "ConnectTimeout", "ReadTimeout":
		return msgSourceTimeout
	case "ConnectionError", "ProxyError":
		return msgSourceConnection
	}
	var dialer *convert.DialerReferenceError
	if errors.As(err, &dialer) {
		return fmt.Sprintf(msgRefreshFailed, "DialerReferenceError")
	}
	var public *PublicFailure
	var stale *StaleRuleCache
	var remote *rulefetch.RemoteRuleValidationError
	var strategy *rules.StrategyReferenceError
	var invalid *SourceValidationError
	if errors.As(err, &public) || errors.As(err, &stale) || errors.As(err, &remote) || errors.As(err, &strategy) || errors.As(err, &invalid) {
		return diag.EventText(err.Error())
	}
	return fmt.Sprintf(msgRefreshFailed, strings.TrimSuffix(strings.TrimPrefix(weburl.SafeErrorDetails(err), "exception_type="), " status=0"))
}
func safeName(name, id string) string {
	if name == "" {
		name = id
	}
	return logx.LogText(diag.EventText(name), 120)
}
func isOSError(err error) bool {
	var path *fs.PathError
	var link *os.LinkError
	var errno syscall.Errno
	return errors.Is(err, store.ErrStorage) || errors.As(err, &path) || errors.As(err, &link) || errors.As(err, &errno)
}
func isRepositoryOrOSError(err error) bool {
	return errors.Is(err, repo.ErrRepository) || isOSError(err)
}
func equalStoredMembers(a, b model.RuleLibraryItem, fields []string) bool {
	for _, field := range fields {
		left, right := rawMember(a, field), rawMember(b, field)
		if string(left) == "null" {
			left = nil
		}
		if string(right) == "null" {
			right = nil
		}
		if !bytes.Equal(left, right) {
			return false
		}
	}
	return true
}

// SourceValidationError reports a controlled source/cache validation failure.
type SourceValidationError struct {
	Message    string
	Cause      error
	CacheShape bool
}

func (e *SourceValidationError) Error() string { return e.Message }
func (e *SourceValidationError) Unwrap() error { return errors.Join(repo.ErrValidation, e.Cause) }
