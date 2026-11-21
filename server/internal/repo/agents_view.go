package repo

import (
	"context"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/store"
)

type agentSnapshot struct {
	head    store.Head
	records []model.Agent
}

// ViewAgents borrows the original typed Agent registry from one generation.
// The callback must not mutate or retain any reachable value. A full-document
// cache hit is reused; otherwise only the agents row is decoded and remembered
// for one generation, including rows above the document cache budget. Large unrelated profiles never
// force a whole-document decode for each Agent or heartbeat.
// Write transactions and locked read snapshots always use their own document.
func (r *Repository) ViewAgents(ctx context.Context, fn func([]model.Agent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	records, _, err := r.BorrowAgents(ctx)
	if err != nil {
		return err
	}
	return r.visitAgents(records, fn)
}

func (r *Repository) visitAgents(records []model.Agent, fn func([]model.Agent) error) error {
	guard := r.currentViewGuard()
	if guard == nil {
		return fn(records)
	}
	before, err := model.Marshal(records)
	if err != nil {
		return err
	}
	result := fn(records)
	after, err := model.Marshal(records)
	if err != nil || string(before) != string(after) {
		guard()
	}
	return result
}

func (r *Repository) cachedAgentRecords(head store.Head) ([]model.Agent, bool) {
	if document := r.cached(); document != nil && document.Head == head {
		return document.Model.System.Agents, true
	}
	r.agentsCacheMu.Lock()
	defer r.agentsCacheMu.Unlock()
	if r.agentsCache != nil && r.agentsCache.head == head {
		return r.agentsCache.records, true
	}
	return nil, false
}

func (r *Repository) agentRecords(ctx context.Context) ([]model.Agent, store.Head, error) {
	var records []model.Agent
	var head store.Head
	hit := false
	err := r.store.Reading(func(connection *store.Conn) error {
		var err error
		head, err = connection.Head()
		if err != nil {
			return err
		}
		records, hit = r.cachedAgentRecords(head)
		return nil
	})
	if err != nil {
		return nil, store.Head{}, r.agentReadFailure(ctx, err)
	}
	if hit {
		return records, head, nil
	}
	// Coalesce cold registry loads. No reader callback runs under this mutex.
	r.agentsLoadMu.Lock()
	defer r.agentsLoadMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, store.Head{}, err
	}
	err = r.store.Reading(func(connection *store.Conn) error {
		var err error
		head, err = connection.Head()
		if err != nil {
			return err
		}
		if records, hit = r.cachedAgentRecords(head); hit {
			return nil
		}
		raw, present, err := connection.Field("system", "", "agents")
		if err != nil {
			return err
		}
		var system model.System
		if present {
			if err := system.SetJSON("agents", raw); err != nil {
				return fmt.Errorf(msgModelRowInvalid, "system", "", "agents", err)
			}
		}
		r.agentLoads.Add(1)
		records = system.Agents
		r.reportAgentIssues(head, &system)
		// A byte cap here would decode an oversized registry on every heartbeat.
		// Retain only this row for one generation instead of the whole document.
		r.agentsCacheMu.Lock()
		r.agentsCache = &agentSnapshot{head: head, records: records}
		r.agentsCacheMu.Unlock()
		return nil
	})
	if err != nil {
		return nil, store.Head{}, r.agentReadFailure(ctx, err)
	}
	return records, head, nil
}

// agentReadFailure retains the repository's established error classification
// on exceptional reads. The full load is only a recovery/error path.
func (r *Repository) agentReadFailure(ctx context.Context, original error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := r.readEntry(ctx, false); err != nil {
		return err
	}
	return original
}

func (r *Repository) reportAgentIssues(head store.Head, system *model.System) {
	r.cacheMu.Lock()
	fullReported := r.modelIssueHeadSet && r.modelIssueHead == head
	r.cacheMu.Unlock()
	r.agentsCacheMu.Lock()
	already := r.agentsIssueHeadSet && r.agentsIssueHead == head
	r.agentsIssueHead, r.agentsIssueHeadSet = head, true
	r.agentsCacheMu.Unlock()
	if fullReported || already {
		return
	}
	for _, issue := range model.Issues(system) {
		logger.Warningf(msgModelFieldIssue, "system."+issue.Path, issue.Kind)
	}
}

func (r *Repository) agentIssuesReported(head store.Head, path string) bool {
	if !strings.HasPrefix(path, "system.agents") {
		return false
	}
	r.agentsCacheMu.Lock()
	defer r.agentsCacheMu.Unlock()
	return r.agentsIssueHeadSet && r.agentsIssueHead == head
}

type agentDefaultSnapshot struct {
	head store.Head
	id   string
}

// AgentDefaultProfileID shares one validated default binding per generation.
// Cold reads retain the repository's full structural validation; Agent loops
// never reload an oversized document for each optional binding. Transaction
// proposals and pinned read snapshots keep their own default.
func (r *Repository) AgentDefaultProfileID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if document, working, err := r.workingModel(ctx); working || err != nil {
		if err != nil {
			return "", err
		}
		return defaultProfileIn(document), nil
	}
	if snapshot := r.lockedSnapshot(ctx); snapshot != nil {
		return defaultProfileIn(snapshot.entry.Model), nil
	}
	var head store.Head
	if err := r.store.Reading(func(c *store.Conn) error { var err error; head, err = c.Head(); return err }); err != nil {
		return "", r.agentReadFailure(ctx, err)
	}
	r.agentDefaultMu.Lock()
	defer r.agentDefaultMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if cached := r.agentDefault; cached != nil && cached.head == head {
		return cached.id, nil
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	id := defaultProfileIn(entry.Model)
	r.agentDefault = &agentDefaultSnapshot{head: entry.Head, id: id}
	return id, nil
}
