package refresh

import (
	"bytes"
	"configflow/internal/agents/diag"
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
	var public *PublicFailure
	var stale *StaleRuleCache
	var remote *rulefetch.RemoteRuleValidationError
	var strategy *rules.StrategyReferenceError
	if errors.As(err, &public) || errors.As(err, &stale) || errors.As(err, &remote) || errors.As(err, &strategy) || errors.Is(err, repo.ErrValidation) {
		return diag.EventText(err.Error())
	}
	return fmt.Sprintf(msgRefreshFailed, strings.TrimPrefix(weburl.SafeErrorDetails(err), "exception_type="))
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
