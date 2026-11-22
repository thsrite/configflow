package provider

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"configflow/internal/clock"
	"configflow/internal/faultprobe"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/store"
)

var responseLog = logx.Get("backend.utils.provider_response")

// ResponseSeconds is RESPONSE_SECONDS: after this long a Mihomo-format
// provider download is answered with the last good content. Mihomo gives up
// on a proxy-provider download after 20 seconds.
const ResponseSeconds = 12

// PoolWorkers is the size of the provider-render pool: Mihomo refreshes
// every provider at once; none should queue behind another.
const PoolWorkers = 8

var (
	settingsMu      sync.Mutex
	responseSeconds float64 = ResponseSeconds

	pool = make(chan struct{}, PoolWorkers)

	flights = lockx.NewFlight[flightKey, *responseCopy](lifecycle.DefaultSpawner{}, "provider-render")
)

// SetResponseSeconds replaces RESPONSE_SECONDS (tests); it returns a
// restore function. A negative value waits for completed rendering without
// a deadline; successful responses are still saved as last-good content.
func SetResponseSeconds(seconds float64) (restore func()) {
	settingsMu.Lock()
	previous := responseSeconds
	responseSeconds = seconds
	settingsMu.Unlock()
	return func() { SetResponseSeconds(previous) }
}

func currentResponseSeconds() float64 {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return responseSeconds
}

// flightKey identifies one shared render: the provider, its request filters
// and the committed configuration it renders (head). A render of a working
// copy (no head) gets a unique solo number and is never shared.
type flightKey struct {
	kind, profileID, itemID, format, regex, excludeRegex string
	head                                                 store.Head
	solo                                                 uint64
}

// soloRenders numbers renders that must not join another flight.
var soloRenders atomic.Uint64

func newFlightKey(kind, profileID, itemID, format, regex, excludeRegex string, head *store.Head) flightKey {
	key := flightKey{kind: kind, profileID: profileID, itemID: itemID, format: format, regex: regex, excludeRegex: excludeRegex}
	if head != nil {
		key.head = *head
	} else {
		key.solo = soloRenders.Add(1)
	}
	return key
}

// rendered response shared by every request waiting on one flight; each
// waiter gets its own copy (_copy).
type responseCopy struct {
	status int
	header http.Header
	body   []byte
}

// Render renders a provider response. ctx outlives the request (the render
// may finish in the background after the fallback answered).
type Render func(ctx context.Context) (*httpx.Response, error)

func lastGoodPath(kind, itemID string) string {
	return "providers/served/" + kind + "-" + itemID + ".yaml"
}

// readLastGood is _read_last_good: the last content served for the
// provider, else (aggregations) the artifact the last bundle commit wrote.
// ok is false when neither can be read (OSError).
func readLastGood(ctx context.Context, profileID, kind, itemID string) (string, bool, error) {
	paths := []string{lastGoodPath(kind, itemID)}
	if kind == "aggregation" {
		paths = append(paths, "providers/"+itemID+".yaml")
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", false, err
	}
	for _, relative := range paths {
		path, err := r.ProfilePath(profileID, relative)
		if err != nil {
			return "", false, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue // OSError
		}
		if !utf8.Valid(data) {
			return "", false, &InputError{Field: "last-good UTF-8"}
		}
		// read_text: universal newlines.
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		return text, true, nil
	}
	return "", false, nil
}

func snapshotResponse(resp *httpx.Response) *responseCopy {
	body := resp.Body()
	return &responseCopy{status: resp.Status, header: resp.Header().Clone(), body: append([]byte(nil), body...)}
}

func (c *responseCopy) response() *httpx.Response {
	r := httpx.Bytes(c.status, "", append([]byte(nil), c.body...))
	for key, values := range c.header {
		if http.CanonicalHeaderKey(key) == "Content-Length" {
			continue
		}
		r.Header()[key] = append([]string(nil), values...)
	}
	return r
}

func mimetype(header http.Header) string {
	value, _, _ := strings.Cut(header.Get("Content-Type"), ";")
	return strings.ToLower(strings.TrimSpace(value))
}

// Serve is serve_provider(kind, item_id, profile_id, config_data, render):
// render's response, or the last good content once the client would time
// out. head is the committed head the caller resolved the configuration from
// (repo.ConfigAt); requests for the same provider, format and filters at the
// same head share one render on a pool of PoolWorkers. A nil head (working
// copy) renders alone. Only the Mihomo format (no ?format=) falls back; a
// successful text/yaml render of it is stored as the new last good content.
//
// Call after authentication and existence checks: the fallback must never
// answer a request that would otherwise be rejected.
func Serve(c *httpx.Ctx, kind, itemID, profileID string, head *store.Head, render Render) (*httpx.Response, error) {
	format, _ := c.Arg("format")
	fallback := format == ""
	regex, _ := c.Arg("regex")
	exclude, _ := c.Arg("exclude_regex")
	key := newFlightKey(kind, profileID, itemID, format, regex, exclude, head)
	renderCtx := lifecycle.Detach(c.Context())
	if err := renderCtx.Err(); err != nil {
		return nil, err
	}
	result := flights.DoChan(renderCtx, key, func(work context.Context) (*responseCopy, error) {
		return runFlight(work, fallback, profileID, kind, itemID, render)
	})
	clock := lifecycle.ClockFrom(c.Context())
	return awaitResponse(c.Context(), clock, result, fallback, profileID, kind, itemID)
}

func awaitResponse(ctx context.Context, source clock.Clock, result <-chan lockx.Result[*responseCopy], fallback bool, profileID, kind, itemID string) (*httpx.Response, error) {
	var timeout <-chan time.Time
	seconds := currentResponseSeconds()
	if fallback && seconds >= 0 {
		timer := source.NewTimer(time.Duration(seconds * float64(time.Second)))
		defer timer.Stop()
		timeout = timer.C()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-result:
			if r.Err != nil {
				return nil, r.Err
			}
			return r.Value.response(), nil
		case <-timeout:
			timeout = nil
			cached, ok, err := readLastGood(ctx, profileID, kind, itemID)
			if err != nil {
				return nil, err
			}
			// A missing fallback keeps waiting on this same channel: never admit a
			// replacement render when the first one has already finished.
			if ok {
				responseLog.Warningf(msgSlowProvider, kind, itemID, int64(seconds))
				return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(cached)).WithHeader("X-ConfigFlow-Provider", "last-good"), nil
			}
		}
	}
}

func runFlight(ctx context.Context, fallback bool, profileID, kind, itemID string, render Render) (*responseCopy, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case pool <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-pool }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	faultprobe.Point(ctx, faultprobe.ProviderRender)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rendered, err := render(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rendered == nil {
		return nil, errors.New("provider: render returned no response")
	}
	snapshot := snapshotResponse(rendered)
	if fallback && snapshot.status == 200 && mimetype(snapshot.header) == "text/yaml" {
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return nil, err
		}
		if _, err := r.WriteProfileText(ctx, profileID, lastGoodPath(kind, itemID), snapshot.body); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

// RequestedFilters is requested_filters(): the per-group text-provider
// filters (regex, exclude_regex) validated before fetching or publishing
// data; nil when the request has neither. Errors are
// *rules.CandidateValidationError (400).
func RequestedFilters(c *httpx.Ctx) (*[2]string, error) {
	_, hasRegex := c.Arg("regex")
	_, hasExclude := c.Arg("exclude_regex")
	if !hasRegex && !hasExclude {
		return nil, nil
	}
	if format, _ := c.Arg("format"); format != "surge" && format != "loon" {
		return nil, &rules.CandidateValidationError{Message: msgFiltersFormat}
	}
	filters := [2]string{c.ArgDefault("regex", ""), c.ArgDefault("exclude_regex", "")}
	if _, err := rules.Filter([]string{}, &filters[0], &filters[1], func(name string) string { return name }); err != nil {
		return nil, err
	}
	return &filters, nil
}
