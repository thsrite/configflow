package discovery

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
func BusyPayload(b *Busy, profile string) map[string]any {
	j := GetJob(b.JobID, profile)
	msg := msgBusy
	if j == nil {
		msg = msgOtherBusy
	}
	return map[string]any{"success": false, "message": msg, "job": j}
}
func startJob(ctx context.Context, profile, kind string, wait bool, run func(context.Context, func(int, int)) (outcome, error)) (*Job, map[string]Probe, error) {
	jobState.Lock()
	for _, j := range jobState.jobs {
		if j.Status == "running" {
			jobState.Unlock()
			return nil, nil, &Busy{j.ID}
		}
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
	// Start from the repository only, never from the request Scope or its values.
	r, err := repo.GetRepository(ctx)
	if err != nil {
		jobState.Lock()
		j.Status = "failed"
		j.Message = msgFailed
		j.FinishedAt = ptr(timestamp())
		jobState.Unlock()
		return nil, nil, err
	}
	work := func() (results map[string]Probe, err error) {
		workCtx := repo.WithScope(repo.WithRepository(context.Background(), r))
		defer repo.ResetConfigContext(workCtx)
		var out outcome
		defer func() {
			if p := recover(); p != nil {
				logger.Errorf("Domain discovery %s job failed: %v", kind, p)
				err = fmt.Errorf("discovery job failed: %v", p)
			}
			jobState.Lock()
			defer jobState.Unlock()
			j.Status = "done"
			j.Errors = out.Errors
			if j.Errors == nil {
				j.Errors = []JobError{}
			}
			if err != nil {
				j.Status = "failed"
				var pe *ProbeError
				if errors.As(err, &pe) {
					j.Message = pe.Message
				} else {
					logger.Exception("Domain discovery job failed", err)
					j.Message = msgFailed
				}
				err = &ProbeError{j.Message}
			}
			j.FinishedAt = ptr(timestamp())
		}()
		out, err = run(workCtx, func(done, total int) { jobState.Lock(); j.Done = done; j.Total = total; jobState.Unlock() })
		return out.Results, err
	}
	if wait {
		results, err := work()
		return GetJob(j.ID, profile), results, err
	}
	go func() { _, _ = work() }()
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
		if !contains(out, v) {
			out = append(out, v)
		}
	}
	return out, nil
}
