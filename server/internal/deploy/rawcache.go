package deploy

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"sync"
)

type rawEntry struct {
	url  string
	data []byte
}
type rawFlight struct {
	done chan struct{}
	data []byte
	err  error
}

// rawCache lives for one Build. L5 guards memory only; leaders fetch directly
// in one of the eight workers, never scheduling a second pool task.
type rawCache struct {
	mu      sync.Mutex
	lru     *list.List
	entries map[string]*list.Element
	flights map[string]*rawFlight
	size    int
}

func newRawCache() *rawCache {
	return &rawCache{lru: list.New(), entries: map[string]*list.Element{}, flights: map[string]*rawFlight{}}
}
func (c *rawCache) get(ctx context.Context, url string, fetch func(context.Context, string) ([]byte, error)) (data []byte, err error) {
	c.mu.Lock()
	if e := c.entries[url]; e != nil {
		c.lru.MoveToBack(e)
		data = bytes.Clone(e.Value.(rawEntry).data)
		c.mu.Unlock()
		return data, nil
	}
	if f := c.flights[url]; f != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			return bytes.Clone(f.data), f.err
		}
	}
	f := &rawFlight{done: make(chan struct{})}
	c.flights[url] = f
	c.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("rule download panic: %v", recovered)
			logger.Exception("rule download panic", err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err == nil && len(data) <= maxRawCacheBytes {
			for c.lru.Len() > 0 && (c.lru.Len() >= maxRawCacheEntries || c.size+len(data) > maxRawCacheBytes) {
				old := c.lru.Front()
				v := old.Value.(rawEntry)
				delete(c.entries, v.url)
				c.size -= len(v.data)
				c.lru.Remove(old)
			}
			v := rawEntry{url, bytes.Clone(data)}
			c.entries[url] = c.lru.PushBack(v)
			c.size += len(data)
		}
		f.data, f.err = data, err
		delete(c.flights, url)
		close(f.done)
	}()
	return fetch(ctx, url)
}
