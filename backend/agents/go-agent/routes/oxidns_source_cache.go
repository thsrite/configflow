package routes

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	oxSourceHeader     = "X-ConfigFlow-Query-Sources"
	oxSourceEntries    = 2000
	oxSourceStreams    = 4
	oxSourceEventLimit = 1 << 20
	oxSourceIdle       = time.Minute
	oxSourceSilence    = 45 * time.Second
	oxSourceWarmup     = 200 * time.Millisecond
)

// One manager per Agent HTTP handler, shared by all viewers. It never opens the
// core database or forwards credentials outside the configured local endpoint.
type oxSourceCache struct {
	mu      sync.Mutex
	scope   string
	streams map[string]*oxSourceStream
	closed  bool
	// Fixed after construction; tests shorten these without waiting a minute.
	idleTimeout, silenceTimeout time.Duration
}

type oxSourceStream struct {
	owner         *oxSourceCache
	config        oxMonitorConfig
	tag, instance string
	ctx           context.Context
	cancel        context.CancelFunc
	ready         chan struct{}
	readyOnce     sync.Once
	lastUsed      time.Time   // protected by owner.mu, as are rows/order/next
	idleTimer     *time.Timer // protected by owner.mu
	rows          map[[32]byte]string
	order         [][32]byte
	next          int
}

func newOxSourceCache() *oxSourceCache {
	return &oxSourceCache{streams: make(map[string]*oxSourceStream), idleTimeout: oxSourceIdle, silenceTimeout: oxSourceSilence}
}

// Caller holds owner.mu. Closing idle subscriptions does not depend on the
// upstream ending its response, receiving a new event, or a reconnect loop.
func (stream *oxSourceStream) stopLocked() {
	stream.cancel()
	if stream.idleTimer != nil {
		stream.idleTimer.Stop()
	}
}

func (cache *oxSourceCache) clearLocked() {
	for _, stream := range cache.streams {
		stream.stopLocked()
	}
	cache.streams = make(map[string]*oxSourceStream)
}

func (cache *oxSourceCache) close() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.closed = true
	cache.clearLocked()
}

func (cache *oxSourceCache) reset() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.clearLocked()
	cache.scope = ""
}

func oxSourceInstance(data []byte) string {
	var health struct {
		Instance string `json:"instance_id"`
	}
	if json.Unmarshal(data, &health) != nil || len(health.Instance) > 128 {
		return ""
	}
	return health.Instance
}

func (cache *oxSourceCache) subscribe(config *oxMonitorConfig, tag, instance string) *oxSourceStream {
	if instance == "" {
		return nil
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.closed {
		return nil
	}
	scope := config.sourceRevision + ":" + instance
	if cache.scope != scope {
		cache.clearLocked()
		cache.scope = scope
	}
	if stream := cache.streams[tag]; stream != nil {
		stream.lastUsed = time.Now()
		stream.idleTimer.Reset(cache.idleTimeout)
		return stream
	}
	if len(cache.streams) >= oxSourceStreams {
		var oldest *oxSourceStream
		for _, stream := range cache.streams {
			if oldest == nil || stream.lastUsed.Before(oldest.lastUsed) {
				oldest = stream
			}
		}
		oldest.stopLocked()
		delete(cache.streams, oldest.tag)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream := &oxSourceStream{owner: cache, config: *config, tag: tag, instance: instance, ctx: ctx, cancel: cancel,
		ready: make(chan struct{}), lastUsed: time.Now(), rows: make(map[[32]byte]string)}
	cache.streams[tag] = stream
	stream.idleTimer = time.AfterFunc(cache.idleTimeout, func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		// A renewed subscription may have reset the timer while this callback
		// was waiting for the lock. It must not close an active viewer's stream.
		if cache.streams[tag] == stream && time.Since(stream.lastUsed) >= cache.idleTimeout {
			stream.stopLocked()
			delete(cache.streams, tag)
		}
	})
	go stream.run()
	return stream
}

func oxSourceKey(record oxSourceRecord) ([32]byte, bool) {
	if record.ID <= 0 || record.Created < 0 || len(record.Questions) == 0 || record.Client == "" {
		return [32]byte{}, false
	}
	// A recorder can be cleared and IDs reused without restarting the core.
	identity, err := json.Marshal([]any{record.ID, record.Created, record.RequestID, record.Client, record.Questions})
	if err != nil {
		return [32]byte{}, false
	}
	return sha256.Sum256(identity), true
}

func (stream *oxSourceStream) put(data []byte) {
	var record oxSourceRecord
	if json.Unmarshal(data, &record) != nil || record.Steps == nil || len(record.Steps) > 2048 {
		return
	}
	key, ok := oxSourceKey(record)
	if !ok {
		return
	}
	source := oxQuerySource(record)
	cache := stream.owner
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.streams[stream.tag] != stream || stream.ctx.Err() != nil {
		return
	}
	if _, exists := stream.rows[key]; !exists {
		if len(stream.order) < oxSourceEntries {
			stream.order = append(stream.order, key)
		} else {
			delete(stream.rows, stream.order[stream.next])
			stream.order[stream.next] = key
			stream.next = (stream.next + 1) % oxSourceEntries
		}
	}
	stream.rows[key] = source
}

func (stream *oxSourceStream) initialDone() { stream.readyOnce.Do(func() { close(stream.ready) }) }

func oxReadSourceEvents(reader io.Reader, record func([]byte), ready func()) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), oxSourceEventLimit)
	var event string
	var data []byte
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if event == "record" && len(data) > 0 {
				record(data)
			}
			event, data = "", nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			ready()
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if len(data)+len(value)+1 > oxSourceEventLimit {
				return errors.New("source event too large")
			}
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, value...)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (stream *oxSourceStream) connect() error {
	ctx, cancel := context.WithCancel(stream.ctx)
	defer cancel()
	client := oxMonitorClient()
	client.Transport.(*http.Transport).ResponseHeaderTimeout = oxMonitorTimeout
	defer client.CloseIdleConnections()
	health, err := stream.config.fetch(ctx, client, "/api/health", nil, oxMonitorJSONLimit, true)
	if err != nil || oxSourceInstance(health) != stream.instance {
		return errors.New("core instance changed or unavailable")
	}
	client.Timeout = 0 // Healthy SSE connections have no fixed maximum lifetime.
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, stream.config.endpoint+"/api/plugins/"+stream.tag+"/stream?tail=2000", nil)
	if requestErr != nil {
		return requestErr
	}
	request.Header.Set("Accept", "text/event-stream")
	if stream.config.basicAuth {
		request.SetBasicAuth(stream.config.username, stream.config.password)
	}
	response, requestErr := client.Do(request)
	if requestErr != nil {
		return requestErr
	}
	defer response.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || mediaType != "text/event-stream" {
		return errors.New("source stream unavailable")
	}
	// The core sends a heartbeat every 15 seconds. Reconnect only when the
	// stream ends/fails or stops sending both records and heartbeats.
	silence := time.AfterFunc(stream.owner.silenceTimeout, cancel)
	defer silence.Stop()
	return oxReadSourceEvents(response.Body, func(data []byte) {
		silence.Reset(stream.owner.silenceTimeout)
		stream.put(data)
	}, func() {
		silence.Reset(stream.owner.silenceTimeout)
		stream.initialDone()
	})
}

func (stream *oxSourceStream) run() {
	defer stream.initialDone()
	defer func() {
		stream.owner.mu.Lock()
		defer stream.owner.mu.Unlock()
		stream.stopLocked()
		if stream.owner.streams[stream.tag] == stream {
			delete(stream.owner.streams, stream.tag)
		}
	}()
	delay := time.Second
	for stream.ctx.Err() == nil {
		stream.owner.mu.Lock()
		idle := time.Since(stream.lastUsed) > stream.owner.idleTimeout
		stream.owner.mu.Unlock()
		if idle {
			return
		}
		started := time.Now()
		_ = stream.connect() // Optional optimization; normal list/detail APIs remain the fallback.
		stream.initialDone()
		if time.Since(started) > 15*time.Second {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-stream.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 15*time.Second {
			delay = min(delay*2, 15*time.Second)
		}
	}
}

func (stream *oxSourceStream) enrich(ctx context.Context, data []byte) []byte {
	if stream == nil {
		return data
	}
	// The core emits its initial tail before the first heartbeat. Wait briefly on
	// first use only; never delay a list indefinitely for an optional stream.
	timer := time.NewTimer(oxSourceWarmup)
	defer timer.Stop()
	select {
	case <-stream.ready:
	case <-timer.C:
	case <-ctx.Done():
		return data
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(data, &response) != nil {
		return data
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(response["records"], &rows) != nil {
		return data
	}
	changed := false
	stream.owner.mu.Lock()
	defer stream.owner.mu.Unlock()
	if stream.owner.streams[stream.tag] != stream || stream.ctx.Err() != nil {
		return data
	}
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		var record oxSourceRecord
		if json.Unmarshal(raw, &record) != nil {
			continue
		}
		key, ok := oxSourceKey(record)
		if !ok {
			continue
		}
		if source, found := stream.rows[key]; found {
			row["resolution_source"], _ = json.Marshal(source)
			changed = true
		}
	}
	if !changed {
		return data
	}
	response["records"], _ = json.Marshal(rows)
	result, err := json.Marshal(response)
	if err != nil || len(result) > oxMonitorJSONLimit {
		return data
	}
	return result
}
