package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	routeScriptBytes = 32 * 1024
	queryTerms       = 200
	removeBatch      = 500
	natSettleReads   = 8
	BatchSkipped     = "skipped"
)

var (
	consoleRouteFields   = stringSet("dst-address", "gateway", "routing-table", "distance", "scope", "target-scope", "comment", "disabled")
	consoleNATFieldOrder = []string{"chain", "action", "disabled", "src-address", "src-address-list", "dst-address",
		"dst-address-list", "in-interface", "in-interface-list", "out-interface", "out-interface-list", "protocol",
		"src-port", "dst-port", "connection-state", "connection-mark", "connection-nat-state", "routing-mark",
		"src-mac-address", "ipsec-policy", "log", "log-prefix", "place-before"}
	batchOutputLine = regexp.MustCompile(`^cf-(id|err):(\d+):(.*)$`)
)

type consoleAddResult struct {
	Rows        map[int]*Row
	Count       int
	Failure     *batchFailure
	Unexplained bool
}

type batchFailure struct {
	Index int
	Err   error
}

type natBatchResult struct {
	Rows []Row
	Used bool
}

// BatchKinds returns the RouterOS operations that can be grouped by ApplyMany.
func (*RouterOS) BatchKinds() []string { return []string{"route_add", "route_delete"} }

// ApplyMany applies an ordered run of route operations, recording each undo
// only after its write has been proven by RouterOS readback.
func (d *RouterOS) ApplyMany(ctx context.Context, ops []json.RawMessage, record func(json.RawMessage, json.RawMessage)) error {
	for index := 0; index < len(ops); {
		var first rosOperation
		if err := json.Unmarshal(ops[index], &first); err != nil {
			return routerError(messageOperationFormat)
		}
		batch := []int{index}
		if first.Kind == "route_add" || first.Kind == "route_delete" {
			batch = d.routeBatches(ops, index, first.Kind)
		}
		left := batch
		if len(batch) >= d.batchMin {
			var err error
			if first.Kind == "route_add" {
				left, err = d.addRoutes(ctx, ops, batch, record, rosRoutes)
			} else {
				left, err = d.deleteRoutes(ctx, ops, batch, record, rosRoutes)
			}
			if err != nil {
				return err
			}
		}
		for _, opIndex := range left {
			undo, err := d.Apply(ctx, ops[opIndex])
			if err != nil {
				return err
			}
			if record != nil {
				record(ops[opIndex], undo)
			}
		}
		index += len(batch)
	}
	return nil
}

func (d *RouterOS) routeBatches(ops []json.RawMessage, index int, kind string) []int {
	batch := []int{}
	seen := map[string]bool{}
	size := 0
	for i := index; i < len(ops); i++ {
		var op rosOperation
		if json.Unmarshal(ops[i], &op) != nil || op.Kind != kind || len(batch) == d.routeBatch || op.CIDR == "" {
			break
		}
		key := op.CIDR
		if kind == "route_delete" {
			key = op.ID
		}
		if seen[key] {
			break
		}
		if kind == "route_add" {
			line := routeAddLine(len(batch), routeAddValues(op), rosRoutes)
			size += len([]byte(line)) + 1
			if len(batch) > 0 && size > routeScriptBytes {
				break
			}
		}
		seen[key] = true
		batch = append(batch, i)
	}
	if len(batch) == 0 {
		return []int{index}
	}
	return batch
}

func (d *RouterOS) BatchRestorable(raw json.RawMessage) bool {
	var undo ROSUndo
	if json.Unmarshal(raw, &undo) != nil || undo.Kind != "ros_restore" || len(undo.Changes) != 1 {
		return false
	}
	change := undo.Changes[0]
	return isRoutePath(change.Path) && (change.Method == "DELETE" || change.Method == "RESTORE")
}

// RestoreMany restores route undo records in original order; errors are empty
// strings for success and BatchSkipped for untouched records after a failure.
func (d *RouterOS) RestoreMany(ctx context.Context, undos []json.RawMessage, stopOnFailure bool) []string {
	errors := []string{}
	for index := 0; index < len(undos); {
		if stopOnFailure && hasError(errors) {
			errors = append(errors, repeatString(BatchSkipped, len(undos)-index)...)
			break
		}
		method, path, canBatch := d.batchUndo(undos[index])
		end := index + 1
		if canBatch {
			for end < len(undos) {
				nextMethod, nextPath, nextCanBatch := d.batchUndo(undos[end])
				if !nextCanBatch || nextMethod != method || nextPath != path {
					break
				}
				end++
			}
		}
		group := undos[index:end]
		if canBatch && len(group) >= d.batchMin && !(stopOnFailure && method == "RESTORE") {
			changes := make([]ROSChange, len(group))
			for i, raw := range group {
				var undo ROSUndo
				_ = json.Unmarshal(raw, &undo)
				changes[i] = undo.Changes[0]
			}
			if method == "DELETE" {
				errors = append(errors, d.restoreDeleted(ctx, changes, stopOnFailure, path)...)
			} else {
				errors = append(errors, d.restoreRemoved(ctx, changes, path)...)
			}
		} else {
			for _, raw := range group {
				if stopOnFailure && hasError(errors) {
					errors = append(errors, BatchSkipped)
					continue
				}
				_, err := d.Apply(ctx, raw)
				if err != nil {
					errors = append(errors, err.Error())
				} else {
					errors = append(errors, "")
				}
			}
		}
		index = end
	}
	return errors
}

func (d *RouterOS) batchUndo(raw json.RawMessage) (method, path string, ok bool) {
	if !d.BatchRestorable(raw) {
		return "", "", false
	}
	var undo ROSUndo
	_ = json.Unmarshal(raw, &undo)
	return undo.Changes[0].Method, undo.Changes[0].Path, true
}

func repeatString(value string, count int) []string {
	values := make([]string, count)
	for i := range values {
		values[i] = value
	}
	return values
}

func hasError(values []string) bool {
	for _, value := range values {
		if value != "" && value != BatchSkipped {
			return true
		}
	}
	return false
}

func routeAddValues(op rosOperation) Row {
	return Row{"dst-address": op.CIDR, "gateway": op.Gateway, "comment": op.Comment, "disabled": "false"}
}

func (d *RouterOS) query(ctx context.Context, field string, values []string, path string) ([]Row, error) {
	unique := uniqueStrings(values)
	rows := []Row{}
	for start := 0; start < len(unique); start += queryTerms {
		end := min(start+queryTerms, len(unique))
		chunk := unique[start:end]
		terms := make([]string, 0, len(chunk)+1)
		for _, value := range chunk {
			terms = append(terms, field+"="+value)
		}
		if len(chunk) > 1 {
			terms = append(terms, "#"+strings.Repeat("|", len(chunk)-1))
		}
		value, err := d.request(ctx, "POST", path+"/print", Row{".query": terms}, rosTimeout)
		if err != nil {
			return nil, err
		}
		batch, ok := value.([]any)
		if !ok {
			return nil, routerError(fmt.Sprintf(messageROSListFormat, path))
		}
		for _, item := range batch {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, routerError(fmt.Sprintf(messageROSListFormat, path))
			}
			rows = append(rows, Row(row))
		}
	}
	return rows, nil
}

func (d *RouterOS) retryRead(ctx context.Context, read func() ([]Row, error)) ([]Row, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := read()
		if err == nil {
			return rows, nil
		}
		if !isRouterError(err) || attempt == 2 {
			return nil, err
		}
		if err := waitContext(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	return nil, routerError(messageROSReadbackFailure)
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func routeAddLine(index int, values Row, path string) string {
	return consoleAddLine(index, values, path, false)
}

func consoleAddLine(index int, values Row, path string, nat bool) string {
	keys := make([]string, 0, len(values))
	if nat {
		for _, key := range consoleNATFieldOrder {
			if _, ok := values[key]; ok {
				keys = append(keys, key)
			}
		}
		remaining := make([]string, 0)
		for key := range values {
			if !slices.Contains(keys, key) {
				remaining = append(remaining, key)
			}
		}
		slices.Sort(remaining)
		keys = append(keys, remaining...)
	} else {
		for _, key := range []string{"dst-address", "gateway", "routing-table", "distance", "scope", "target-scope", "comment", "disabled"} {
			if _, ok := values[key]; ok {
				keys = append(keys, key)
			}
		}
	}
	params := make([]string, 0, len(keys))
	for _, key := range keys {
		value := rosText(values[key])
		if nat && (key == "disabled" || key == "log") {
			switch comparable(key, value) {
			case "true":
				value = "yes"
			case "false":
				value = "no"
			}
		}
		encoded := ScriptValue(key, value)
		if nat && (key == "disabled" || key == "log") && (value == "yes" || value == "no") {
			encoded = value
		}
		params = append(params, key+"="+encoded)
	}
	command := path + "/add " + strings.Join(params, " ")
	return fmt.Sprintf(`:if ($f=0) do={:onerror e in={:put ("cf-id:%d:" . [%s])} do={:set f 1; :put ("cf-err:%d:" . $e)}}`, index, command, index)
}

func consoleSafe(values Row) bool {
	for key := range values {
		if !consoleRouteFields[key] {
			return false
		}
	}
	return rosText(values["dst-address"]) != "" &&
		(comparable("disabled", valueOr(values, "disabled", "false")) == "true" ||
			comparable("disabled", valueOr(values, "disabled", "false")) == "false")
}

func valueOr(row Row, key string, fallback any) any {
	if value, ok := row[key]; ok {
		return value
	}
	return fallback
}

func refusedConsole(error error) bool {
	return routerErrorKind(error) != KindUncertainWrite || permissionError.MatchString(error.Error())
}

func (d *RouterOS) consoleAdd(ctx context.Context, values []Row, current []Row, hasCurrent bool, path string) (*consoleAddResult, error) {
	destinations := make([]string, len(values))
	for i, value := range values {
		destinations[i] = rosText(value["dst-address"])
	}
	if !hasCurrent {
		var err error
		current, err = d.query(ctx, "dst-address", destinations, path)
		if err != nil {
			return nil, nil
		}
	}
	before := rowIDs(current)
	lines := make([]string, len(values))
	for i, item := range values {
		lines[i] = routeAddLine(i, item, path)
	}
	script := ":local f 0\n" + strings.Join(lines, "\n") + "\n:put \"cf-done\""
	result, err := d.request(ctx, "POST", "/execute", Row{"script": script, "as-string": ""}, rosTimeout)
	if err != nil {
		if refusedConsole(err) {
			return nil, nil
		}
		return nil, rollbackError(fmt.Sprintf(messageBatchUnknown, err.Error()))
	}
	output := strings.ReplaceAll(rosText(asRow(result)["ret"]), "\r", "")
	outputLines := strings.Split(output, "\n")
	ids := []string{}
	rejected := false
	for _, line := range outputLines {
		match := batchOutputLine.FindStringSubmatch(line)
		if len(match) != 4 {
			continue
		}
		index, _ := strconv.Atoi(match[2])
		if index != len(ids) || rejected {
			continue
		}
		if match[1] == "err" {
			rejected = true
		} else {
			ids = append(ids, strings.TrimSpace(match[3]))
		}
	}
	complete := slices.Contains(outputLines, "cf-done") && (rejected || len(ids) == len(values))
	after, err := d.retryRead(ctx, func() ([]Row, error) { return d.query(ctx, "dst-address", destinations, path) })
	if err != nil {
		return nil, rollbackError(fmt.Sprintf(messageBatchReadbackRoutes, err.Error()))
	}
	byID := map[string]Row{}
	for _, row := range after {
		byID[row.id()] = row
	}
	rows := map[int]*Row{}
	var failure *batchFailure
	for index, id := range ids {
		row, exists := byID[id]
		if !exists && rosIDPattern.MatchString(id) {
			found, readErr := d.find(ctx, path, id)
			if readErr != nil {
				if failure == nil {
					failure = &batchFailure{Index: index, Err: rollbackError(fmt.Sprintf(messageUncertainWrite, readErr.Error()))}
				}
				continue
			}
			row, exists = found, found != nil
		}
		validID := rosIDPattern.MatchString(id) && !before[id] && !duplicateID(ids, index, id)
		if !validID || exists && (rosTruthy(row["dynamic"]) || !inScope(path, row, values[index])) {
			if failure == nil {
				failure = &batchFailure{Index: index, Err: rollbackError(fmt.Sprintf(messageUncertainRouteWrite, path))}
			}
			continue
		}
		if exists {
			copy := row
			rows[index] = &copy
		} else {
			rows[index] = nil
		}
	}
	knownIDs := map[string]bool{}
	for id := range before {
		knownIDs[id] = true
	}
	for _, id := range ids {
		knownIDs[id] = true
	}
	unexplained := !complete
	if unexplained {
		unexplained = false
		for _, row := range after {
			if knownIDs[row.id()] {
				continue
			}
			for _, value := range values[len(ids):] {
				if inScope(path, row, value) {
					unexplained = true
					break
				}
			}
		}
	}
	return &consoleAddResult{Rows: rows, Count: len(ids), Failure: failure, Unexplained: unexplained}, nil
}

func asRow(value any) Row {
	if row, ok := value.(map[string]any); ok {
		return Row(row)
	}
	return nil
}

func duplicateID(ids []string, index int, id string) bool {
	for i := 0; i < index; i++ {
		if ids[i] == id {
			return true
		}
	}
	return false
}

func routeWriteCheck(d *RouterOS, row Row, values Row, rows []Row) error {
	return checkWritten(d, rosCheck{ID: row.id(), Values: values}, row, rows)
}

func checkWritten(d *RouterOS, check rosCheck, row Row, rows []Row) error {
	if check.Absent {
		if row != nil {
			return routerError(messageReadbackDeletePresent)
		}
		return nil
	}
	if row == nil {
		return routerError(messageReadbackMissingWrite)
	}
	keys := make([]string, 0, len(check.Values))
	for key := range check.Values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := check.Values[key]
		if key == "place-before" {
			target := rosText(value)
			if indexOfRow(rows, row.id()) < 0 || indexOfRow(rows, target) < 0 || indexOfRow(rows, row.id()) >= indexOfRow(rows, target) {
				return routerError(messageReadbackNATOrder)
			}
		} else if comparable(key, row[key]) != comparable(key, value) {
			return routerError(fmt.Sprintf(messageReadbackMismatch, check.ID, key))
		}
	}
	return nil
}

func (d *RouterOS) addRoutes(ctx context.Context, rawOps []json.RawMessage, batch []int,
	record func(json.RawMessage, json.RawMessage), path string) ([]int, error) {
	ops := make([]rosOperation, len(batch))
	values := make([]Row, len(batch))
	for i, index := range batch {
		if err := json.Unmarshal(rawOps[index], &ops[i]); err != nil {
			return nil, routerError(messageOperationFormat)
		}
		values[i] = routeAddValues(ops[i])
	}
	added, err := d.consoleAdd(ctx, values, nil, false, path)
	if err != nil {
		return nil, err
	}
	if added == nil {
		return batch, nil
	}
	owned := []provenRoute{}
	failure := added.Failure
	indices := make([]int, 0, len(added.Rows))
	for index := range added.Rows {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	for _, index := range indices {
		row := added.Rows[index]
		if row == nil {
			if failure == nil || index < failure.Index {
				failure = &batchFailure{Index: index}
			}
			continue
		}
		if failure == nil || index < failure.Index {
			if checkErr := routeWriteCheck(d, *row, values[index], nil); checkErr != nil {
				failure = &batchFailure{Index: index, Err: checkErr}
			}
		}
		expected := d.ownershipSnapshot(path, mergeRows(values[index], *row))
		change := ROSChange{Method: "DELETE", Path: path, ID: row.id(), Expected: marshalRow(expected)}
		undo := ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}}
		encoded, marshalErr := json.Marshal(undo)
		if marshalErr != nil {
			return nil, marshalErr
		}
		owned = append(owned, provenRoute{index: index, row: *row, undo: encoded, rollback: change})
	}
	first := added.Count
	if failure != nil {
		first = failure.Index
	}
	for index := 0; index < first; index++ {
		item := findProven(owned, index)
		if item != nil && record != nil {
			record(rawOps[batch[index]], item.undo)
		}
	}
	if failure != nil {
		cause := failure.Err
		if cause == nil {
			cause = routerError(messageReadbackMissingWrite)
		}
		cleanup := []string{}
		for _, item := range owned {
			if item.index < failure.Index {
				continue
			}
			_, cleanupErr := d.restore(context.WithoutCancel(ctx), []ROSChange{item.rollback})
			if cleanupErr != nil {
				cleanup = append(cleanup, cleanupErr.Error())
			}
		}
		if len(cleanup) > 0 {
			return nil, rollbackError(cause.Error() + "；" + cleanup[0])
		}
		return nil, cause
	}
	if added.Unexplained {
		return nil, rollbackError(fmt.Sprintf(messageBatchIncomplete, path))
	}
	left := make([]int, 0, len(batch)-added.Count)
	for _, index := range batch[added.Count:] {
		left = append(left, index)
	}
	return left, nil
}

type provenRoute struct {
	index    int
	row      Row
	undo     json.RawMessage
	rollback ROSChange
}

func findProven(items []provenRoute, index int) *provenRoute {
	for i := range items {
		if items[i].index == index {
			return &items[i]
		}
	}
	return nil
}

func (d *RouterOS) deleteRoutes(ctx context.Context, rawOps []json.RawMessage, batch []int,
	record func(json.RawMessage, json.RawMessage), path string) ([]int, error) {
	ops := make([]rosOperation, len(batch))
	cidrs := make([]string, len(batch))
	for i, index := range batch {
		if err := json.Unmarshal(rawOps[index], &ops[i]); err != nil {
			return nil, routerError(messageOperationFormat)
		}
		cidrs[i] = ops[i].CIDR
	}
	rows, err := d.query(ctx, "dst-address", cidrs, path)
	if err != nil {
		return batch, nil
	}
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.id()] = row
	}
	type preparedRoute struct {
		index int
		op    json.RawMessage
		undo  json.RawMessage
		id    string
	}
	prepared := []preparedRoute{}
	for i, op := range ops {
		row := byID[op.ID]
		if row == nil || rosTruthy(row["dynamic"]) || row.text("dst-address") != op.CIDR {
			break
		}
		known := []string{}
		for _, item := range rows {
			if item.text("dst-address") == row.text("dst-address") {
				known = append(known, item.id())
			}
		}
		change := ROSChange{Method: "RESTORE", Path: path, ID: op.ID, Values: payload(path, row),
			Expected: marshalRow(d.ownershipSnapshot(path, row)), KnownIDs: known}
		undo, marshalErr := json.Marshal(ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}})
		if marshalErr != nil {
			return nil, marshalErr
		}
		prepared = append(prepared, preparedRoute{index: i, op: rawOps[batch[i]], undo: undo, id: op.ID})
	}
	if len(prepared) < d.batchMin {
		return batch, nil
	}
	ids := make([]string, len(prepared))
	for i, item := range prepared {
		ids[i] = item.id
	}
	var failure error
	for start := 0; start < len(ids); start += removeBatch {
		end := min(start+removeBatch, len(ids))
		if _, err := d.request(ctx, "POST", path+"/remove", Row{".id": strings.Join(ids[start:end], ",")}, rosTimeout); err != nil {
			failure = err
			break
		}
	}
	remainingRows, readErr := d.retryRead(ctx, func() ([]Row, error) { return d.query(ctx, ".id", ids, path) })
	if readErr != nil {
		for _, item := range prepared {
			if record != nil {
				record(item.op, item.undo)
			}
		}
		return nil, readErr
	}
	remaining := rowIDs(remainingRows)
	left := []int{}
	for _, item := range prepared {
		if remaining[item.id] {
			left = append(left, batch[item.index])
		} else if record != nil {
			record(item.op, item.undo)
		}
	}
	if failure != nil {
		return nil, failure
	}
	left = append(left, batch[len(prepared):]...)
	return left, nil
}

func (d *RouterOS) restoreDeleted(ctx context.Context, changes []ROSChange, stop bool, path string) []string {
	errors := make([]string, len(changes))
	destinations := make([]string, len(changes))
	for i, change := range changes {
		var expected Row
		_, _ = json.Marshal(change.Expected)
		decodeRowInto(change.Expected, &expected)
		destinations[i] = rosText(expected["dst-address"])
	}
	rows, readErr := d.query(ctx, "dst-address", destinations, path)
	if readErr != nil {
		rows = nil
	}
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.id()] = row
	}
	pending := []int{}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		ids := make([]string, len(pending))
		for i, index := range pending {
			ids[i] = changes[index].ID
		}
		for start := 0; start < len(ids); start += removeBatch {
			end := min(start+removeBatch, len(ids))
			if _, err := d.request(ctx, "POST", path+"/remove", Row{".id": strings.Join(ids[start:end], ",")}, rosTimeout); err != nil {
				break
			}
		}
		remainingRows, err := d.retryRead(ctx, func() ([]Row, error) { return d.query(ctx, ".id", ids, path) })
		remaining := rowIDs(remainingRows)
		if err != nil {
			for _, id := range ids {
				remaining[id] = true
			}
		}
		failed := false
		for _, index := range pending {
			if stop && failed {
				errors[index] = BatchSkipped
				continue
			}
			if remaining[changes[index].ID] {
				errors[index] = d.restoreSingleChange(ctx, changes[index])
				if errors[index] != "" {
					failed = true
				}
			}
		}
		pending = nil
	}
	for index, change := range changes {
		if stop && hasError(errors[:index]) {
			for i := index; i < len(errors); i++ {
				errors[i] = BatchSkipped
			}
			break
		}
		row := byID[change.ID]
		expected, expectedSet := decodeRow(change.Expected)
		if rows != nil && row != nil && expectedSet && !rosTruthy(row["dynamic"]) &&
			maps.Equal(d.ownershipSnapshot(path, row), expected) {
			pending = append(pending, index)
			continue
		}
		flush()
		if rows == nil || row == nil || !expectedSet {
			errors[index] = d.restoreSingleChange(ctx, change)
		} else if rosTruthy(row["dynamic"]) {
			errors[index] = rollbackError(messageROSDynamicObject).Error()
		} else {
			errors[index] = rollbackError(messageRollbackChanged).Error()
		}
	}
	flush()
	return errors
}

func decodeRowInto(raw json.RawMessage, out *Row) {
	if len(raw) == 0 {
		return
	}
	_ = json.Unmarshal(raw, out)
}

func (d *RouterOS) restoreSingleChange(ctx context.Context, change ROSChange) string {
	_, err := d.restore(ctx, []ROSChange{change})
	if err != nil {
		return err.Error()
	}
	return ""
}

func (d *RouterOS) restoreRemoved(ctx context.Context, changes []ROSChange, path string) []string {
	errors := make([]string, len(changes))
	ids := make([]string, len(changes))
	for i, change := range changes {
		ids[i] = change.ID
	}
	presentRows, err := d.query(ctx, ".id", ids, path)
	if err != nil {
		for i, change := range changes {
			errors[i] = d.restoreSingleChange(ctx, change)
		}
		return errors
	}
	present := map[string]Row{}
	for _, row := range presentRows {
		present[row.id()] = row
	}
	pending := []int{}
	flush := func() {
		chunk := slices.Clone(pending)
		pending = nil
		var rows []Row
		if len(chunk) >= d.batchMin {
			destinations := make([]string, len(chunk))
			for i, index := range chunk {
				destinations[i] = rosText(changes[index].Values["dst-address"])
			}
			rows, err = d.query(ctx, "dst-address", destinations, path)
			if err != nil {
				rows = nil
			}
		}
		if rows == nil {
			for _, index := range chunk {
				errors[index] = d.restoreSingleChange(ctx, changes[index])
			}
			return
		}
		adding := []int{}
		for _, index := range chunk {
			change := changes[index]
			if hasUnexpectedRoute(rows, change.Values, change.KnownIDs) {
				errors[index] = rollbackError(messageRollbackRouteConflict).Error()
			} else {
				adding = append(adding, index)
			}
		}
		values := make([]Row, len(adding))
		for i, index := range adding {
			values[i] = changes[index].Values
		}
		var added *consoleAddResult
		if len(adding) >= d.batchMin {
			added, err = d.consoleAdd(ctx, values, rows, true, path)
			if err != nil {
				for _, index := range adding {
					errors[index] = rollbackError(err.Error()).Error()
				}
				return
			}
		}
		if added == nil {
			for _, index := range adding {
				errors[index] = d.restoreSingleChange(ctx, changes[index])
			}
			return
		}
		for position, index := range adding[:min(added.Count, len(adding))] {
			rowPtr, exists := added.Rows[position]
			if !exists {
				message := fmt.Sprintf(messageUncertainRouteWrite, path)
				if added.Failure != nil && added.Failure.Index == position && added.Failure.Err != nil {
					message = added.Failure.Err.Error()
				}
				errors[index] = rollbackError(message).Error()
				continue
			}
			var row Row
			if rowPtr != nil {
				row = *rowPtr
			}
			if checkErr := checkWritten(d, rosCheck{ID: rosText(row[".id"]), Values: changes[index].Values}, row, nil); checkErr != nil {
				errors[index] = rollbackError(checkErr.Error()).Error()
			}
		}
		for _, index := range adding[min(added.Count, len(adding)):] {
			if added.Unexplained {
				errors[index] = rollbackError(fmt.Sprintf(messageBatchIncomplete, path)).Error()
			} else {
				errors[index] = d.restoreSingleChange(ctx, changes[index])
			}
		}
	}

	for index, change := range changes {
		current := present[change.ID]
		if current != nil {
			expected, _ := decodeRow(change.Expected)
			if !maps.Equal(d.ownershipSnapshot(path, current), expected) {
				errors[index] = rollbackError(messageRollbackChanged).Error()
			}
			continue
		}
		for _, pendingIndex := range pending {
			if comparable("dst-address", changes[pendingIndex].Values["dst-address"]) == comparable("dst-address", change.Values["dst-address"]) {
				flush()
				break
			}
		}
		if !consoleSafe(change.Values) {
			errors[index] = d.restoreSingleChange(ctx, change)
			continue
		}
		pending = append(pending, index)
		if len(pending) == d.routeBatch {
			flush()
		}
	}
	flush()
	return errors
}

func (d *RouterOS) putNAT(ctx context.Context, changes []ROSChange, undo *[]ROSChange) (int, error) {
	rows, err := d.list(ctx, rosNAT)
	if err != nil {
		return 0, nil
	}
	handled, batch, size := 0, []ROSChange{}, 0
	flush := func() (bool, error) {
		if len(batch) == 0 {
			return true, nil
		}
		values := make([]Row, len(batch))
		for i, change := range batch {
			values[i] = change.Values
		}
		result, err := d.natBatch(ctx, values, rows, undo)
		if err != nil {
			return false, err
		}
		if !result.Used {
			return false, nil
		}
		rows = result.Rows
		handled += len(batch)
		batch, size = nil, 0
		return true, nil
	}
	for _, change := range changes {
		line := consoleAddLine(len(batch), change.Values, rosNAT, true)
		if len(batch) > 0 && (len(batch) == d.routeBatch || size+len([]byte(line)) > routeScriptBytes) {
			used, err := flush()
			if err != nil {
				return handled, err
			}
			if !used {
				return handled, nil
			}
			line = consoleAddLine(0, change.Values, rosNAT, true)
		}
		batch = append(batch, change)
		size += len([]byte(line)) + 1
	}
	if len(batch) > 0 {
		_, err := flush()
		if err != nil {
			return handled, err
		}
	}
	return handled, nil
}

func (d *RouterOS) natBatch(ctx context.Context, values []Row, rows []Row, undo *[]ROSChange) (natBatchResult, error) {
	before := rowIDs(rows)
	lines := make([]string, len(values))
	for i, item := range values {
		lines[i] = consoleAddLine(i, item, rosNAT, true)
	}
	script := ":local f 0\n" + strings.Join(lines, "\n") + "\n:put \"cf-done\""
	result, err := d.request(ctx, "POST", "/execute", Row{"script": script, "as-string": ""}, rosTimeout)
	if err != nil {
		if refusedConsole(err) {
			return natBatchResult{Used: false}, nil
		}
		return natBatchResult{}, rollbackError(fmt.Sprintf(messageBatchUnknownNAT, err.Error()))
	}
	output := strings.ReplaceAll(rosText(asRow(result)["ret"]), "\r", "")
	outputLines := strings.Split(output, "\n")
	ids := []string{}
	for _, line := range outputLines {
		match := batchOutputLine.FindStringSubmatch(line)
		if len(match) != 4 || match[1] != "id" {
			continue
		}
		index, _ := strconv.Atoi(match[2])
		if index == len(ids) {
			ids = append(ids, strings.TrimSpace(match[3]))
		}
	}
	rows, err = d.retryRead(ctx, func() ([]Row, error) { return d.list(ctx, rosNAT) })
	if err != nil {
		return natBatchResult{}, rollbackError(fmt.Sprintf(messageBatchReadbackNAT, err.Error()))
	}
	for attempt := 0; attempt < natSettleReads; attempt++ {
		pending := false
		for _, row := range rows {
			if rosTruthy(row["invalid"]) && slices.Contains(ids, row.id()) {
				pending = true
				break
			}
		}
		if !pending {
			break
		}
		if waitErr := waitContext(ctx, 250*time.Millisecond); waitErr != nil {
			return natBatchResult{}, waitErr
		}
		rows, err = d.retryRead(ctx, func() ([]Row, error) { return d.list(ctx, rosNAT) })
		if err != nil {
			return natBatchResult{}, rollbackError(fmt.Sprintf(messageBatchReadbackNAT, err.Error()))
		}
	}
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.id()] = row
	}
	owned := []struct {
		id       string
		expected Row
	}{}
	valid := slices.Contains(outputLines, "cf-done") && len(ids) == len(values)
	unproven := false
	if !slices.Contains(outputLines, "cf-done") {
		for _, row := range rows {
			if !before[row.id()] && !slices.Contains(ids, row.id()) {
				unproven = true
			}
		}
	}
	for index, id := range ids {
		if index >= len(values) {
			unproven, valid = true, false
			continue
		}
		row, exists := byID[id]
		if !rosIDPattern.MatchString(id) || before[id] || duplicateID(ids, index, id) {
			unproven, valid = true, false
			continue
		}
		if !exists {
			valid = false
			continue
		}
		expected := d.ownershipSnapshot(rosNAT, mergeRows(values[index], row))
		if valid {
			if checkErr := checkWritten(d, rosCheck{ID: id, Values: values[index]}, row, rows); checkErr != nil {
				valid = false
			}
		}
		owned = append(owned, struct {
			id       string
			expected Row
		}{id, expected})
	}
	if valid {
		for _, item := range owned {
			*undo = append(*undo, ROSChange{Method: "DELETE", Path: rosNAT, ID: item.id, Expected: marshalRow(item.expected)})
		}
		return natBatchResult{Rows: rows, Used: true}, nil
	}
	failures := []string{}
	for _, item := range owned {
		if item.expected == nil {
			unproven = true
			continue
		}
		if _, rollback := d.restore(context.WithoutCancel(ctx), []ROSChange{{Method: "DELETE", Path: rosNAT, ID: item.id,
			Expected: marshalRow(item.expected)}}); rollback != nil {
			failures = append(failures, rollback.Error())
		}
	}
	if unproven || len(failures) > 0 {
		message := strings.Join(failures, "；")
		if unproven {
			message = strings.Join(nonEmpty([]string{message,
				messageBatchNATUnknown}), "；")
		}
		return natBatchResult{}, rollbackError(message)
	}
	return natBatchResult{Used: false}, nil
}

func putNAT(ctx context.Context, d *RouterOS, changes []ROSChange, undo *[]ROSChange) (int, error) {
	return d.putNAT(ctx, changes, undo)
}
