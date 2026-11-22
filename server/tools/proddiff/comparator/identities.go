package main

import (
	"math"
	"regexp"
	"sync"
)

// agentComparisonRoutes carry independent content-check cache identities.
var agentComparisonRoutes = map[string]bool{"/api/agents": true, "/api/agents/<agent_id>": true,
	"/api/agents/<agent_id>/push-diff": true}

var (
	cacheVersion   = regexp.MustCompile(`^[a-f0-9]{16}$`)
	cacheCheckedAt = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{6})?Z$`)
)

// agentIdentities pairs each process's independent cache identities
// (checked_at, version), checking format, stability and uniqueness.
type agentIdentities struct {
	mu    sync.Mutex
	pairs map[string]map[string]string
}

func newAgentIdentities() *agentIdentities {
	return &agentIdentities{pairs: map[string]map[string]string{"checked_at": {}, "version": {}}}
}

func (p *agentIdentities) pair(key string, av, bv any) bool {
	a, aok := av.(string)
	b, bok := bv.(string)
	if !aok || !bok {
		return false
	}
	if key == "version" {
		if !cacheVersion.MatchString(a) || !cacheVersion.MatchString(b) {
			return false
		}
	} else {
		if !cacheCheckedAt.MatchString(a) || !cacheCheckedAt.MatchString(b) {
			return false
		}
		at, aok := fromISOFormat(a)
		bt, bok := fromISOFormat(b)
		if !aok || !bok || math.Abs(at.Sub(bt).Seconds()) > 600 {
			return false
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pairs := p.pairs[key]
	if old, ok := pairs[a]; ok && old != b {
		return false
	}
	for other, value := range pairs {
		if other != a && value == b {
			return false
		}
	}
	pairs[a] = b
	return true
}

func (p *agentIdentities) normalizeResult(a, b any) {
	am, aok := a.(*Map)
	bm, bok := b.(*Map)
	if !aok || !bok {
		return
	}
	for _, key := range []string{"checked_at", "version"} {
		av, inA := am.Get(key)
		bv, inB := bm.Get(key)
		if inA && inB && p.pair(key, av, bv) {
			am.Set(key, "<paired-cache-identity>")
			bm.Set(key, "<paired-cache-identity>")
		}
	}
}

func (p *agentIdentities) normalize(a, b any, route string) {
	switch route {
	case "/api/agents/<agent_id>/push-diff":
		p.normalizeResult(a, b)
	case "/api/agents":
		al, aok := a.([]any)
		bl, bok := b.([]any)
		if !aok || !bok {
			return
		}
		for i := 0; i < len(al) && i < len(bl); i++ {
			if _, ok := al[i].(*Map); !ok {
				continue
			}
			if _, ok := bl[i].(*Map); !ok {
				continue
			}
			p.normalizeResult(getOr(al[i], "content_sync", nil), getOr(bl[i], "content_sync", nil))
		}
	case "/api/agents/<agent_id>":
		if _, ok := a.(*Map); !ok {
			return
		}
		if _, ok := b.(*Map); !ok {
			return
		}
		p.normalizeResult(getOr(a, "content_sync", nil), getOr(b, "content_sync", nil))
	}
}
