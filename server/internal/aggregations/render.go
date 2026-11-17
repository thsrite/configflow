// Package aggregations renders aggregation providers against one immutable
// main configuration and publishes them only after bundle validation.
package aggregations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// Proxy preserves protocol-specific fields without interpreting their schema.
type Proxy map[string]json.RawMessage

func (p Proxy) name() string { var s string; _ = json.Unmarshal(p["name"], &s); return s }

type resource struct {
	ID, Name string
	Enabled  bool
	raw      json.RawMessage
}
type aggregation struct {
	resource
	Subscriptions, Nodes []string
	Regex                json.RawMessage
}
type subscription struct {
	resource
	URL string
}
type node struct {
	resource
	SubscriptionID string
}
type sourceResult struct {
	proxies []Proxy
	nodes   []node
	updates []cacheUpdate
	err     error
}

// Result is a validated provider and the source statistics from that rendering.
type Result struct {
	Name                   string         `json:"name"`
	FilePath               *string        `json:"file_path"`
	Content                string         `json:"content"`
	SubscriptionNodeCounts map[string]int `json:"subscription_node_counts"`
	TotalCount             int            `json:"total_count"`
	Proxies                []Proxy        `json:"-"`
	updates                []cacheUpdate
}

// Options selects dry rendering or atomic publication. Supplied Main and Fetch
// are used by the provider bundle so a source is materialized only once.
type Options struct {
	Main           json.RawMessage
	Persist        bool
	SourcePrepared bool
	Format         string
	Fetch          Fetch
}

type formatKey struct{}

// WithFormat carries client format into nested bundle rendering.
func WithFormat(ctx context.Context, format string) context.Context {
	return context.WithValue(ctx, formatKey{}, format)
}

func render(ctx context.Context, agg aggregation, d *delivery, opts Options) (*Result, error) {
	result := &Result{Name: agg.Name, SubscriptionNodeCounts: map[string]int{}, Proxies: []Proxy{}}
	var selected []subscription
	selectedIDs := map[string]bool{}
	for _, id := range agg.Subscriptions {
		selectedIDs[id] = true
		for _, sub := range d.subscriptions {
			if sub.ID == id && sub.Enabled {
				selected = append(selected, sub)
				break
			}
		}
	}
	fetched := make([]sourceResult, len(selected))
	jobs := make(chan int, len(selected))
	for i := range selected {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(8, len(selected)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				func() {
					defer func() {
						if recover() != nil {
							fetched[i] = sourceResult{}
							logSourcePanic()
						}
					}()
					fetched[i] = d.fetchSource(ctx, selected[i], opts)
				}()
			}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allNodes := []node{}
	proxyByName := map[string]Proxy{}
	for i, sub := range selected {
		data := fetched[i]
		if data.err != nil {
			return nil, data.err
		}
		result.SubscriptionNodeCounts[sub.ID] = len(data.nodes)
		result.updates = append(result.updates, data.updates...)
		allNodes = append(allNodes, data.nodes...)
		for _, proxy := range data.proxies {
			proxyByName[proxy.name()] = proxy
		}
	}
	existingNames := map[string]bool{}
	for _, n := range allNodes {
		if n.Name != "" {
			existingNames[n.Name] = true
		}
	}
	manualIDs := map[string]bool{}
	for _, id := range agg.Nodes {
		manualIDs[id] = true
	}
	for _, n := range d.nodes {
		if manualIDs[n.ID] && n.Enabled && !existingNames[n.Name] {
			allNodes = append(allNodes, n)
			existingNames[n.Name] = true
		}
	}
	pattern := ""
	if len(agg.Regex) != 0 {
		var value *string
		if json.Unmarshal(agg.Regex, &value) != nil || value == nil {
			return nil, errors.New("aggregation regex must be a string")
		}
		pattern = strings.TrimSpace(*value)
	}
	if pattern != "" {
		regex, err := rules.CompilePyRegex(pattern, false)
		if err != nil {
			return nil, &rules.StrategyReferenceError{Message: msgInvalidRegex}
		}
		kept := allNodes[:0]
		for _, n := range allNodes {
			if regex.Search(n.Name) {
				kept = append(kept, n)
			}
		}
		allNodes = kept
	}
	if err := d.captureRecords(ctx, selectedIDs); err != nil {
		return nil, err
	}
	chains := opts.Format != "surge" && opts.Format != "loon"
	for _, n := range allNodes {
		proxy := proxyByName[n.Name]
		if proxy == nil {
			var err error
			proxy, err = d.convertNode(ctx, n)
			if err != nil {
				return nil, err
			}
		}
		if proxy == nil {
			continue
		}
		if selectedIDs[n.SubscriptionID] {
			var err error
			proxy, err = d.adapt(n.SubscriptionID, proxy, chains)
			if err != nil {
				return nil, err
			}
		}
		if proxy != nil {
			result.Proxies = append(result.Proxies, proxy)
		}
	}
	if err := d.validate(result.Proxies, agg.Name, opts.Format); err != nil {
		return nil, err
	}
	content, err := providerYAML(result.Proxies)
	if err != nil {
		return nil, err
	}
	result.Content, result.TotalCount = content, len(result.Proxies)
	return result, nil
}

func providerYAML(proxies []Proxy) (string, error) {
	raw, err := json.Marshal(struct {
		Proxies []Proxy `json:"proxies"`
	}{proxies})
	if err != nil {
		return "", err
	}
	root, err := yamlx.FromJSON(raw)
	if err != nil {
		return "", err
	}
	output, err := yamlx.Marshal(root)
	return string(output), err
}
