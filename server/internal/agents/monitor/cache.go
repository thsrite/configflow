package monitor

// Port of backend/agents/oxidns_cache.py (A07): flush DNS caches of one
// OxiDNS Agent's core. The Agent discovers `type: cache` plugins in its own
// deployed configuration and accepts only those tags. This bridge targets
// exactly the registered Agent named in the request; core addresses and
// credentials never leave the Agent.

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"configflow/internal/agents"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// Limits of oxidns_cache.py.
const (
	CacheMinAgentVersion   = "1.8.36-go"
	CacheMaxResponseBytes  = 16 * 1024
	CacheMaxTags           = 64
	cacheTagMaxLength      = 64
	cacheSegment           = `[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?`
	cacheRequestTimeoutSec = 40
)

// cacheTagRE is _TAG without its length lookahead (checked separately):
// the Agent's oxMonitorSafeTag.
var cacheTagRE = regexp.MustCompile(`^` + cacheSegment + `(?:\.` + cacheSegment + `)*$`)

// CacheFail is oxidns_cache._fail(code, status).
func CacheFail(code string, status int) *Error {
	if status == 0 {
		status = 502
	}
	message, ok := CacheMessages[code]
	if !ok {
		message = CacheMessages["CACHE_FLUSH_FAILED"]
	}
	return &Error{Code: code, Status: status, Message: message}
}

// CacheResult contains the public cache list or completed flush.
type CacheResult struct {
	flushing bool
	Success  bool     `json:"success"`
	Caches   []string `json:"caches,omitempty"`
	Flushed  []string `json:"flushed,omitempty"`
}

// CacheOperationError includes only validated tags from a partial operation.
type CacheOperationError struct {
	Err     *Error
	Flushed []string
}

// Error returns the public failure message.
func (e *CacheOperationError) Error() string { return e.Err.Message }

// Unwrap exposes the structured monitor error.
func (e *CacheOperationError) Unwrap() error { return e.Err }

func validCacheTag(value any) bool {
	tag, ok := value.(string)
	return ok && tag != "" && len(tag) <= cacheTagMaxLength && cacheTagRE.MatchString(tag)
}

func validTags(tags []string, nonempty bool) bool {
	if tags == nil || len(tags) > CacheMaxTags || nonempty && len(tags) == 0 {
		return false
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !validCacheTag(tag) || seen[tag] {
			return false
		}
		seen[tag] = true
	}
	return true
}

func answerTags(fields model.Fields, name string) ([]string, error) {
	raw, _ := fields.Get(name)
	var tags []string
	if json.Unmarshal(raw, &tags) != nil || !validTags(tags, false) {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	return tags, nil
}

// CallCache lists or flushes validated cache tags through one registered Agent.
// A nil tag slice denotes GET; POST requires at least one unique safe tag.
func CallCache(ctx context.Context, m Registry, agentID, method string, tags []string) (*CacheResult, error) {
	if method != "GET" && method != "POST" || method == "GET" && tags != nil || method == "POST" && !validTags(tags, true) {
		return nil, CacheFail("INVALID_REQUEST", 400)
	}
	if method == "POST" {
		ctx = lifecycle.Detach(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	agent, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, CacheFail("AGENT_NOT_FOUND", 404)
	}
	if originalText(agent.MemberJSON("service_type")) != "oxidns" {
		return nil, CacheFail("WRONG_SERVICE", 400)
	}
	if !modelVersionAtLeast(agent, CacheMinAgentVersion) {
		return nil, CacheFail("UPGRADE_REQUIRED", 409)
	}
	token, valid := originalString(agent.MemberJSON("token"))
	if !valid {
		return nil, agents.ErrAgentEndpoint
	}
	origin, err := Origin(agent)
	if err != nil {
		return nil, err
	}
	header, err := monitorHeaders(token)
	if err != nil {
		return nil, CacheFail("AGENT_UNREACHABLE", 0)
	}
	path := "/api/oxidns/cache"
	var body []byte
	if method == "POST" {
		path += "/flush"
		header.Set("Content-Type", "application/json")
		body, err = jsonresp.EncodeDevice(struct {
			Tags []string `json:"tags"`
		}{tags})
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response, err := m.Client().Do(ctx, agents.Request{Method: method, URL: origin + path, Header: header, Body: body,
		Timeout: agents.Pair(3, cacheRequestTimeoutSec), NoProxy: true, NoRedirects: true, MaxBody: CacheMaxResponseBytes})
	if err != nil {
		return nil, CacheFail("AGENT_UNREACHABLE", 0)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	answer, err := protocolAnswer(response, CacheMaxResponseBytes, CacheFail)
	if err != nil {
		return nil, err
	}
	status := response.StatusCode
	success, present, valid := answerBool(answer, "success")
	if present && !valid {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	if status >= 400 || present && !success {
		rawCode, _ := answer.Get("code")
		if status == 404 && len(rawCode) > 0 && (rawCode[0] == '[' || rawCode[0] == '{') {
			return nil, errors.New("cache error code must be a string")
		}
		code := fieldText(answer, "code")
		if _, known := CacheMessages[code]; !known {
			if status == 404 {
				return nil, CacheFail("UPGRADE_REQUIRED", 409)
			}
			code = "CACHE_FLUSH_FAILED"
		}
		errStatus := 502
		switch status {
		case 400, 409, 503, 504:
			errStatus = status
		}
		flushed, _ := answerTags(answer, "flushed")
		if flushed == nil {
			flushed = []string{}
		}
		return nil, &CacheOperationError{Err: CacheFail(code, errStatus), Flushed: flushed}
	}
	if status != 200 || !present || !success {
		return nil, CacheFail("INVALID_RESPONSE", 0)
	}
	result := &CacheResult{Success: true, flushing: method == "POST"}
	if method == "GET" {
		result.Caches, err = answerTags(answer, "caches")
	} else {
		result.Flushed, err = answerTags(answer, "flushed")
		if err == nil && !slices.Equal(result.Flushed, tags) {
			err = CacheFail("INVALID_RESPONSE", 0)
		}
	}
	return result, err
}

// MarshalJSON retains an explicit empty list for a successful list/flush.
func (r CacheResult) MarshalJSON() ([]byte, error) {
	if r.flushing {
		return json.Marshal(struct {
			Success bool     `json:"success"`
			Flushed []string `json:"flushed"`
		}{r.Success, r.Flushed})
	}
	return json.Marshal(struct {
		Success bool     `json:"success"`
		Caches  []string `json:"caches"`
	}{r.Success, r.Caches})
}
