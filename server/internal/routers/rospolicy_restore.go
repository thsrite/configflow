package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
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
	rows, err := d.Query(ctx, ".id", requested, ros.PathRoutes)
	if err != nil {
		for _, step := range steps {
			if err := d.restoreResource(ctx, step, ids, remapped, true); err != nil {
				*failures = append(*failures, err.Error())
			}
		}
		return
	}
	live := ROSPolicySnapshot{ros.PathRoutes: rows}
	present := ros.RowIDs(rows)
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
		var added *ros.ConsoleAddResult
		var err error
		if len(chunk) >= d.BatchMin() {
			added, err = d.ConsoleAdd(ctx, values, nil, false, ros.PathRoutes)
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
					errors[i] = fmt.Sprintf(device.MessageUncertainRouteWrite, ros.PathRoutes)
					if added.Failure != nil && added.Failure.Index == position && added.Failure.Err != nil {
						errors[i] = added.Failure.Err.Error()
					}
					continue
				}
				if pointer == nil {
					// Written under a proven fresh ID, then gone: the read-back
					// mismatch is reported like any other missing object.
					_, err := d.confirmResource(ctx, ros.PathRoutes, steps[i].Values, "", &NATStep{}, nil)
					errors[i] = err.Error()
					continue
				}
				row := *pointer
				id := row.ID()
				ids.Table(ros.PathRoutes)[id] = true
				undo := ros.StepResource("delete", ros.NewResource(ros.PathRoutes, row))
				if _, err := d.confirmResource(ctx, ros.PathRoutes, steps[i].Values, id, &undo, row); err != nil {
					errors[i] = err.Error()
				} else {
					remapped[[2]string{ros.PathRoutes, steps[i].ID}] = id
				}
			}
			if added.Unexplained {
				for _, i := range chunk[count:] {
					errors[i] = fmt.Sprintf(device.MessageUncertainRouteWrite, ros.PathRoutes)
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
			if err := guardPolicyIn(live, ROSResource{Path: step.Path, ID: step.ID, Config: step.Config}); err != nil {
				errors[i] = err.Error()
			}
			continue
		}
		if !ros.ConsoleSafe(step.Values) || step.Values.Set("place-before") {
			flush()
			if err := d.restoreResource(ctx, step, ids, remapped, true); err != nil {
				errors[i] = err.Error()
			}
			continue
		}
		size := len(ros.RouteAddLine(len(pending), step.Values, ros.PathRoutes)) + 1
		if len(pending) > 0 && (len(pending) >= d.RouteBatch() || bytes+size > ros.RouteScriptBytes-64) {
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
