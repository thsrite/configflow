package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// 共用的 /connections 轮询：域名发现和流量记录都订阅它，同时开启时也只轮询一次。
// 没有订阅者时停止轮询。

// 连接轮询间隔；存活不到一个间隔的连接可能漏采
var discoveryPollInterval = 2 * time.Second

type mihomoConnSnapshot struct {
	At            time.Time
	Conns         []mihomoConn
	UploadTotal   int64
	DownloadTotal int64
	// 本次读取用的控制接口；Err 不为空时可能为 nil
	Controller *mihomoController
	Err        error
}

type mihomoConnFeed struct {
	mu     sync.Mutex
	cfg    *Config
	subs   map[int]chan mihomoConnSnapshot
	next   int
	cancel context.CancelFunc
	client *http.Client
}

var mihomoConns = &mihomoConnFeed{}

// subscribe 返回只保留最新一次快照的通道；订阅者处理慢时跳过中间的快照，不阻塞其他订阅者。
func (f *mihomoConnFeed) subscribe(cfg *Config) (<-chan mihomoConnSnapshot, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs == nil {
		f.subs = map[int]chan mihomoConnSnapshot{}
	}
	id := f.next
	f.next++
	ch := make(chan mihomoConnSnapshot, 1)
	f.subs[id] = ch
	f.cfg = cfg
	if f.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		f.cancel = cancel
		go f.loop(ctx)
	}
	return ch, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.subs, id)
		if len(f.subs) == 0 && f.cancel != nil {
			f.cancel()
			f.cancel = nil
		}
	}
}

func (f *mihomoConnFeed) loop(ctx context.Context) {
	if f.client == nil {
		f.client = &http.Client{Timeout: 5 * time.Second}
	}
	var controller *mihomoController
	ticker := time.NewTicker(discoveryPollInterval)
	defer ticker.Stop()
	for {
		f.mu.Lock()
		cfg := f.cfg
		f.mu.Unlock()
		snapshot := mihomoConnSnapshot{At: time.Now()}
		if controller == nil {
			controller, snapshot.Err = resolveMihomoController(cfg.ConfigPath)
		}
		if controller != nil {
			snapshot.Controller = controller
			snapshot.Conns, snapshot.UploadTotal, snapshot.DownloadTotal, snapshot.Err = fetchMihomoConnections(ctx, f.client, controller)
			if snapshot.Err != nil {
				// 配置可能被重新部署，下次重新读取地址与 secret
				controller = nil
			}
		}
		if ctx.Err() != nil {
			return
		}
		f.publish(snapshot)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (f *mihomoConnFeed) publish(snapshot mihomoConnSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.subs {
		select {
		case <-ch: // 丢掉还没处理的旧快照
		default:
		}
		ch <- snapshot
	}
}

func fetchMihomoConnections(ctx context.Context, client *http.Client, controller *mihomoController) ([]mihomoConn, int64, int64, error) {
	req, err := controller.request(ctx, http.MethodGet, "/connections")
	if err != nil {
		return nil, 0, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, 0, 0, errControllerAuth
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var body struct {
		DownloadTotal int64        `json:"downloadTotal"`
		UploadTotal   int64        `json:"uploadTotal"`
		Connections   []mihomoConn `json:"connections"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&body); err != nil {
		return nil, 0, 0, err
	}
	if body.Connections == nil {
		body.Connections = []mihomoConn{}
	}
	return body.Connections, body.UploadTotal, body.DownloadTotal, nil
}
