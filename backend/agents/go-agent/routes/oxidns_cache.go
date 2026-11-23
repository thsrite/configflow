package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Cache flushing addresses only `type: cache` plugins of this Agent's deployed
// OxiDNS configuration. Requests name a discovered tag; the core address,
// credentials and plugin paths never come from the caller.

const (
	oxCacheRequestLimit = 16 * 1024
	oxCacheMaxTags      = 64
	oxCacheRoundTimeout = 30 * time.Second
)

func oxCacheParseBody(body []byte) ([]string, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || len(object) != 1 {
		return nil, false
	}
	raw, ok := object["tags"]
	var tags []string
	if !ok || json.Unmarshal(raw, &tags) != nil || len(tags) == 0 || len(tags) > oxCacheMaxTags {
		return nil, false
	}
	seen := make(map[string]bool)
	for _, tag := range tags {
		if !oxMonitorSafeTag(tag) || seen[tag] {
			return nil, false
		}
		seen[tag] = true
	}
	return tags, true
}

func oxCacheFlush(ctx context.Context, client *http.Client, config *oxMonitorConfig, tag string) *oxMonitorError {
	// The core exposes flushing as GET; it is still a state change and runs
	// only from the authenticated POST below.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.endpoint+"/api/plugins/"+tag+"/flush", nil)
	if err != nil {
		return oxMonitorConfigError()
	}
	req.Header.Set("Accept", "application/json")
	if config.basicAuth {
		req.SetBasicAuth(config.username, config.password)
	}
	response, err := client.Do(req)
	if err != nil {
		return oxMonitorRequestError(ctx, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, oxMonitorJSONLimit))
	switch response.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return oxMonitorFail(http.StatusConflict, "CACHE_FLUSH_UNSUPPORTED", "The running OxiDNS core does not expose cache flushing")
	case http.StatusUnauthorized, http.StatusForbidden:
		return oxMonitorFail(http.StatusBadGateway, "CORE_AUTH_FAILED", "OxiDNS API authentication failed")
	case http.StatusConflict:
		return oxMonitorFail(http.StatusConflict, "CORE_BUSY", "The OxiDNS cache is busy")
	default:
		return oxMonitorFail(http.StatusBadGateway, "CACHE_FLUSH_FAILED", "OxiDNS rejected the cache flush")
	}
}

func oxCacheWriteFailure(w http.ResponseWriter, err *oxMonitorError, flushed []string) {
	w.Header().Set("Cache-Control", "no-store")
	JsonResponse(w, err.status, map[string]any{"success": false, "code": err.code, "message": err.message, "flushed": flushed})
}

func OxiDNSCacheHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_REQUEST", "Cache operations do not accept parameters"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, oxCacheRequestLimit+1))
		if err != nil || len(body) > oxCacheRequestLimit {
			oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_REQUEST", "Invalid cache request"))
			return
		}
		switch r.URL.Path {
		case "/api/oxidns/cache":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				oxMonitorWriteError(w, oxMonitorFail(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use GET"))
				return
			}
			if len(bytes.TrimSpace(body)) != 0 {
				oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_REQUEST", "GET does not accept a body"))
				return
			}
			config, configErr := loadOxiDNSMonitorConfig(cfg)
			if configErr != nil {
				oxMonitorWriteError(w, configErr)
				return
			}
			JsonResponse(w, http.StatusOK, map[string]any{"success": true, "caches": config.capabilities.Caches})
		case "/api/oxidns/cache/flush":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				oxMonitorWriteError(w, oxMonitorFail(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use POST"))
				return
			}
			tags, ok := oxCacheParseBody(body)
			if !ok {
				oxMonitorWriteError(w, oxMonitorFail(http.StatusBadRequest, "INVALID_REQUEST", "POST requires {\"tags\": [...]}"))
				return
			}
			// Never flush a core that is being replaced, restarted or recovered.
			release, lockErr := AcquireServiceOperation(cfg)
			if lockErr != nil {
				oxMonitorWriteError(w, oxMonitorFail(http.StatusConflict, "SERVICE_BUSY", "A service operation is already running"))
				return
			}
			defer release()
			if deploymentBlocked(cfg) != nil {
				oxMonitorWriteError(w, oxMonitorFail(http.StatusConflict, "SERVICE_BUSY", "Complete deployment recovery before flushing caches"))
				return
			}
			config, configErr := loadOxiDNSMonitorConfig(cfg)
			if configErr != nil {
				oxMonitorWriteError(w, configErr)
				return
			}
			available := make(map[string]bool)
			for _, tag := range config.capabilities.Caches {
				available[tag] = true
			}
			for _, tag := range tags {
				if !available[tag] {
					oxMonitorWriteError(w, oxMonitorFail(http.StatusConflict, "CACHE_UNKNOWN", "A requested cache is not configured on this Agent"))
					return
				}
			}
			ctx, cancel := context.WithTimeout(r.Context(), oxCacheRoundTimeout)
			defer cancel()
			client := oxMonitorClient()
			flushed := []string{}
			for _, tag := range tags {
				if flushErr := oxCacheFlush(ctx, client, config, tag); flushErr != nil {
					oxCacheWriteFailure(w, flushErr, flushed)
					return
				}
				flushed = append(flushed, tag)
			}
			JsonResponse(w, http.StatusOK, map[string]any{"success": true, "flushed": flushed})
		default:
			oxMonitorWriteError(w, oxMonitorFail(http.StatusNotFound, "NOT_FOUND", "Unknown cache operation"))
		}
	}
}
