package yamlx

import "sync"

// Marshal asks yaml.v3 how it would write some scalars (preserveScalarTag,
// stringNeedsQuotes). The answer depends only on the scalar's tag, value and
// context, so it is remembered: a generated configuration has thousands of
// scalars, and each probe used to build a new yaml.v3 encoder and decoder
// (O10 R9). Long values are not remembered; the table is emptied when full.

const (
	probeValueLimit = 256
	probeTableLimit = 1 << 14
)

type scalarProbe struct {
	tag, value string
	flow, str  bool
}

type probeTable struct {
	mu      sync.Mutex
	answers map[scalarProbe]bool
}

var probes probeTable

func (p *probeTable) get(key scalarProbe) (answer, ok bool) {
	if len(key.value) > probeValueLimit {
		return false, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	answer, ok = p.answers[key]
	return answer, ok
}

func (p *probeTable) put(key scalarProbe, answer bool) {
	if len(key.value) > probeValueLimit {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.answers == nil || len(p.answers) >= probeTableLimit {
		p.answers = make(map[scalarProbe]bool, 256)
	}
	p.answers[key] = answer
}
