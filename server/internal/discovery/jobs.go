package discovery

import (
	"context"
	"errors"
	"slices"
	"sync"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

var logger = logx.Get("backend.utils.domain_discovery_service")

// ProbeError is a user-facing failure to run a discovery operation.
type ProbeError struct{ Message string }

// Error returns the user-facing failure message.
func (e *ProbeError) Error() string { return e.Message }

// Busy identifies the exclusive job preventing another start.
type Busy struct{ JobID string }

// Error returns the user-facing failure message.
func (e *Busy) Error() string { return msgBusy }

// Job is bounded polling metadata. Probe payloads are never retained here.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Total      int        `json:"total"`
	Done       int        `json:"done"`
	Errors     []JobError `json:"errors"`
	Message    string     `json:"message"`
	StartedAt  string     `json:"started_at"`
	FinishedAt *string    `json:"finished_at"`
	ProfileID  string     `json:"-"`
}

// JobError describes a partial failure of an Agent, target or host.
type JobError struct {
	AgentID string `json:"agent_id,omitempty"`
	Target  string `json:"target,omitempty"`
	Host    string `json:"host,omitempty"`
	Message string `json:"message"`
}
type outcome struct {
	Results map[string]Probe
	Errors  []JobError
}

var jobState = struct {
	sync.Mutex
	jobs []*Job
}{}

func cloneJob(j *Job) *Job {
	if j == nil {
		return nil
	}
	v := *j
	if j.FinishedAt != nil {
		v.FinishedAt = new(*j.FinishedAt)
	}
	v.Errors = append([]JobError{}, j.Errors...)
	return &v
}
func runningJob(id string) *Job {
	jobState.Lock()
	defer jobState.Unlock()
	for _, j := range jobState.jobs {
		if j.Status == "running" && (id == "" || id == j.ProfileID) {
			return cloneJob(j)
		}
	}
	return nil
}

// GetJob returns an immutable metadata snapshot scoped to its profile.
func GetJob(id, profile string) *Job {
	jobState.Lock()
	defer jobState.Unlock()
	for _, j := range jobState.jobs {
		if j.ID == id && j.ProfileID == profile {
			return cloneJob(j)
		}
	}
	return nil
}

// BusyPayload prevents disclosure of a different profile's running job.
func BusyPayload(b *Busy, profile string) BusyView {
	j := GetJob(b.JobID, profile)
	msg := msgBusy
	if j == nil {
		msg = msgOtherBusy
	}
	return BusyView{Message: msg, Job: j}
}

var jobLock = lockx.NewMutex(lockx.LevelJob, "domain-discovery")

// jobFailure retains the internal cause while exposing the existing safe message.
type jobFailure struct {
	user  *ProbeError
	cause error
}

func (e *jobFailure) Error() string   { return e.user.Error() }
func (e *jobFailure) Unwrap() []error { return []error{e.user, e.cause} }

func startJob(ctx context.Context, profile, kind string, wait bool, run func(context.Context, func(int, int)) (outcome, error)) (*Job, map[string]Probe, error) {
	return startJobOn(ctx, profile, kind, wait, run, lifecycle.DefaultSpawner{})
}

func startJobOn(ctx context.Context, profile, kind string, wait bool, run func(context.Context, func(int, int)) (outcome, error), spawner lockx.Spawner) (*Job, map[string]Probe, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// Async workers retain the repository and their held lock, not request
	// values/Scope. The spawner supplies Runtime lifetime and a fresh Scope.
	base := context.Background()
	if wait {
		base = lifecycle.Detach(ctx)
	}
	jobState.Lock()
	for _, j := range jobState.jobs {
		if j.Status == "running" {
			jobState.Unlock()
			return nil, nil, &Busy{j.ID}
		}
	}
	// L1 is tried, never waited for; metadata and admission are published
	// together. release below does no I/O and completes before metadata unlock.
	held, release, ok := jobLock.TryLock(base)
	if !ok {
		jobState.Unlock()
		return nil, nil, &Busy{}
	}
	j := &Job{ID: uniqueID(""), ProfileID: profile, Kind: kind, Status: "running", Errors: []JobError{}, StartedAt: timestamp()}
	finished := []*Job{}
	for _, old := range jobState.jobs {
		if old.Status != "running" {
			finished = append(finished, old)
		}
	}
	if len(finished) > 20 {
		copy(finished, finished[len(finished)-20:])
		clear(finished[20:])
		finished = finished[:20]
	}
	jobState.jobs = append(finished, j)
	initial := cloneJob(j)
	jobState.Unlock()
	failAdmission := func() {
		jobState.Lock()
		j.Status, j.Message, j.FinishedAt = "failed", msgFailed, new(timestamp())
		release()
		jobState.Unlock()
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		failAdmission()
		return nil, nil, err
	}
	work := func(worker context.Context) (results map[string]Probe, err error) {
		workCtx := repo.WithScope(repo.WithRepository(worker, r))
		defer repo.ResetConfigContext(workCtx)
		var out outcome
		returned := false
		defer func() {
			_ = recover()
			if !returned {
				err = lifecycle.ErrPanic
				logger.Error("Domain discovery job panicked")
			}
			message := ""
			if err != nil {
				message = msgFailed
				var pe *ProbeError
				if errors.As(err, &pe) {
					message = pe.Message
				} else if !wait && !errors.Is(err, lifecycle.ErrPanic) {
					logger.Exception("Domain discovery job failed", err)
				}
				err = &jobFailure{user: &ProbeError{message}, cause: err}
			}
			jobState.Lock()
			defer jobState.Unlock()
			j.Status, j.Errors, j.Message = "done", out.Errors, message
			if j.Errors == nil {
				j.Errors = []JobError{}
			}
			if err != nil {
				j.Status, j.Errors = "failed", []JobError{}
			}
			j.FinishedAt = new(timestamp())
			release()
		}()
		out, err = run(workCtx, func(done, total int) { jobState.Lock(); j.Done = done; j.Total = total; jobState.Unlock() })
		returned = true
		return out.Results, err
	}
	if wait {
		results, err := work(held)
		return GetJob(j.ID, profile), results, err
	}
	if err := spawner.Spawn(repo.WithRepository(held, r), "domain-"+kind, func(worker context.Context) { _, _ = work(worker) }); err != nil {
		failAdmission()
		return nil, nil, err
	}
	return initial, nil, nil
}

func cleanValues(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 100 {
		return nil, invalid(msgDomains)
	}
	out := []string{}
	for _, v := range values {
		v = CleanDomain(v)
		if v == "" {
			return nil, invalid(msgDomains)
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out, nil
}
