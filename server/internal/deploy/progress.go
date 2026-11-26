package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/lifecycle"
	"context"
	"fmt"
	"sync"
	"time"
)

func publishProgress(ctx context.Context, m *agents.Manager, id, did, token string) Progress {
	// Held across exactly one RecordDeployment; callbacks hold no other locks.
	var mu sync.Mutex
	var lastDetail, lastRules time.Time
	started := false
	clock := lifecycle.ClockFrom(ctx)
	write := func(message string) {
		if ctx.Err() == nil {
			message = diag.FailureText(message, []any{token}, 0)
			_, _ = recordUpdate(ctx, m, id, did, Update{Status: new("preparing"), Message: &message}, agents.RecordOptions{})
		}
	}
	detail := func(message string, force bool) {
		mu.Lock()
		defer mu.Unlock()
		now := clock.Now()
		if !force && !lastDetail.IsZero() && now.Sub(lastDetail) < time.Second {
			return
		}
		write(message)
		if !force {
			lastDetail = now
		}
	}
	return Progress{Phase: func(message string) { detail(message, true) }, Message: func(message string) { detail(message, false) }, Detail: func(done *int, total int, name, phase string) {
		if phase == "preparing" {
			mu.Lock()
			first := !started
			started = true
			mu.Unlock()
			detail(fmt.Sprintf(msgRulePreparing, name, total), first)
		} else if done != nil {
			detail(fmt.Sprintf(msgRulePrepared, name, *done, total), *done == total)
		}
	}, Rules: func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		now := clock.Now()
		if done != 0 && done != total && !lastRules.IsZero() && now.Sub(lastRules) < time.Second {
			return
		}
		message := msgRulesPacked
		if done < total {
			message = fmt.Sprintf(msgRuleProgress, done, total)
		}
		write(message)
		lastRules = now
	}}
}
