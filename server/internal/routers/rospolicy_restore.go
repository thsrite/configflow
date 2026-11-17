package routers

import (
	"context"
	"fmt"
	"slices"
)

// restorePolicyRoutes uses the shared bounded console writer, but retains
// original-to-new identities for updating the private policy manifest.
func (d *RouterOS) restorePolicyRoutes(ctx context.Context, steps []NATStep, ids ROSIDs, remapped map[[2]string]string, failures *[]string) {
	requested := []string{}
	for _, step := range steps {
		requested = append(requested, step.ID)
	}
	rows, err := d.query(ctx, ".id", requested, rosRoutes)
	if err != nil {
		for _, step := range steps {
			if err := d.restoreResource(ctx, step, ids, remapped, true); err != nil {
				*failures = append(*failures, err.Error())
			}
		}
		return
	}
	live := ROSPolicySnapshot{rosRoutes: rows}
	present := rowIDs(rows)
	errors := make([]string, len(steps))
	pending := []int{}
	bytes := 0
	flush := func() {
		chunk := slices.Clone(pending)
		pending = nil
		bytes = 0
		if len(chunk) == 0 {
			return
		}
		values := []Row{}
		for _, i := range chunk {
			values = append(values, steps[i].Values)
		}
		var added *consoleAddResult
		var err error
		if len(chunk) >= d.batchMin {
			added, err = d.consoleAdd(ctx, values, nil, false, rosRoutes)
		}
		if err != nil {
			for _, i := range chunk {
				errors[i] = err.Error()
			}
			return
		}
		count := 0
		if added != nil {
			count = min(added.Count, len(chunk))
			for position, i := range chunk[:count] {
				pointer, exists := added.Rows[position]
				if !exists {
					errors[i] = fmt.Sprintf(messageUncertainRouteWrite, rosRoutes)
					if added.Failure != nil && added.Failure.Index == position && added.Failure.Err != nil {
						errors[i] = added.Failure.Err.Error()
					}
					continue
				}
				if pointer == nil {
					// Written under a proven fresh ID, then gone: the read-back
					// mismatch is reported like any other missing object.
					_, err := d.confirmResource(ctx, rosRoutes, steps[i].Values, "", &NATStep{}, nil)
					errors[i] = err.Error()
					continue
				}
				row := *pointer
				id := row.id()
				ids.table(rosRoutes)[id] = true
				undo := stepResource("delete", resource(rosRoutes, row))
				if _, err := d.confirmResource(ctx, rosRoutes, steps[i].Values, id, &undo, row); err != nil {
					errors[i] = err.Error()
				} else {
					remapped[[2]string{rosRoutes, steps[i].ID}] = id
				}
			}
			if added.Unexplained {
				for _, i := range chunk[count:] {
					errors[i] = fmt.Sprintf(messageUncertainRouteWrite, rosRoutes)
				}
				return
			}
		}
		for _, i := range chunk[count:] {
			if err := d.restoreResource(ctx, steps[i], ids, remapped, true); err != nil {
				errors[i] = err.Error()
			}
		}
	}
	for i, step := range steps {
		if present[step.ID] {
			if err := guardPolicyIn(live, ROSResource{step.Path, step.ID, step.Config}); err != nil {
				errors[i] = err.Error()
			}
			continue
		}
		if !consoleSafe(step.Values) || step.Values.set("place-before") {
			flush()
			if err := d.restoreResource(ctx, step, ids, remapped, true); err != nil {
				errors[i] = err.Error()
			}
			continue
		}
		size := len(routeAddLine(len(pending), step.Values, rosRoutes)) + 1
		if len(pending) > 0 && (len(pending) >= d.routeBatch || bytes+size > routeScriptBytes-64) {
			flush()
		}
		pending = append(pending, i)
		bytes += size
	}
	flush()
	for _, failure := range errors {
		if failure != "" {
			*failures = append(*failures, failure)
		}
	}
}
