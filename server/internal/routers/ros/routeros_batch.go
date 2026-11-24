package ros

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/routers/device"
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
	RouteScriptBytes = 32 * 1024
	queryTerms       = 200
	removeBatch      = 500
	natSettleReads   = 8
	BatchSkipped     = "skipped"
)

var (
	consoleRouteFields   = device.StringSet("dst-address", "gateway", "routing-table", "distance", "scope", "target-scope", "comment", "disabled")
	consoleNATFieldOrder = []string{"chain", "action", "disabled", "src-address", "src-address-list", "dst-address",
		"dst-address-list", "in-interface", "in-interface-list", "out-interface", "out-interface-list", "protocol",
		"src-port", "dst-port", "connection-state", "connection-mark", "connection-nat-state", "routing-mark",
		"src-mac-address", "ipsec-policy", "log", "log-prefix", "place-before"}
	batchOutputLine = regexp.MustCompile(`^cf-(id|err):(\d+):(.*)$`)
)

type ConsoleAddResult struct {
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

// BatchKinds returns the Client operations that can be grouped by ApplyMany.
func (*Client) BatchKinds() []string { return []string{"route_add", "route_delete"} }

// ApplyMany applies an ordered run of route operations, recording each undo
// only after its write has been proven by Client readback. A cancelled ctx
// stops it only between batches, when everything written is recorded; a
// batch once sent is proven (or reported uncertain) to the end unless the
// shutdown drain deadline cancels the detached work context.
func (d *Client) ApplyMany(ctx context.Context, ops []json.RawMessage, record func(json.RawMessage, json.RawMessage)) error {
	return d.ApplyManyWith(ctx, ops, record, d.ApplyBase)
}

// ApplyManyWith applies an ordered run of operations, using apply for any
// single operation not sent in a route batch.
func (d *Client) ApplyManyWith(ctx context.Context, ops []json.RawMessage, record func(json.RawMessage, json.RawMessage), apply func(context.Context, json.RawMessage) (json.RawMessage, error)) error {
	work := lifecycle.Detach(ctx)
	if err := work.Err(); err != nil {
		return err
	}
	for index := 0; index < len(ops); {
		if err := work.Err(); err != nil {
			return err
		}
		var first Operation
		if err := jsondecode.Unmarshal(ops[index], &first); err != nil {
			return device.Error(device.MessageOperationFormat)
		}
		batch := []int{index}
		if first.Kind == "route_add" || first.Kind == "route_delete" {
			batch = d.RouteBatches(ops, index, first.Kind)
		}
		left := batch
		if len(batch) >= d.batchMin {
			var err error
			if first.Kind == "route_add" {
				left, err = d.AddRoutes(work, ops, batch, record, PathRoutes)
			} else {
				left, err = d.DeleteRoutes(work, ops, batch, record, PathRoutes)
			}
			if err != nil {
				return err
			}
		}
		for _, opIndex := range left {
			undo, err := apply(work, ops[opIndex])
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

func (d *Client) RouteBatches(ops []json.RawMessage, index int, kind string) []int {
	batch := []int{}
	seen := map[string]bool{}
	size := 0
	for i := index; i < len(ops); i++ {
		var op Operation
		if jsondecode.Unmarshal(ops[i], &op) != nil || op.Kind != kind || len(batch) == d.routeBatch || op.CIDR == "" {
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
			line := RouteAddLine(len(batch), RouteAddValues(op), PathRoutes)
			size += len([]byte(line)) + 1
			if len(batch) > 0 && size > RouteScriptBytes {
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

func (d *Client) BatchRestorable(raw json.RawMessage) bool {
	var undo ROSUndo
	if jsondecode.Unmarshal(raw, &undo) != nil || undo.Kind != "ros_restore" || len(undo.Changes) != 1 {
		return false
	}
	change := undo.Changes[0]
	return isRoutePath(change.Path) && (change.Method == "DELETE" || change.Method == "RESTORE")
}

// RestoreMany restores route undo records in original order; errors are empty
// strings for success and BatchSkipped for untouched records after a failure.
// Rollback is the safety net, so request cancellation never stops it; only the
// shutdown drain deadline does (lifecycle.Detach).
func (d *Client) RestoreMany(ctx context.Context, undos []json.RawMessage, stopOnFailure bool) []string {
	return d.RestoreManyWith(ctx, undos, stopOnFailure, d.ApplyBase)
}

// RestoreManyWith restores undo records in original order, using apply for any
// single undo record not sent in a route batch.
func (d *Client) RestoreManyWith(ctx context.Context, undos []json.RawMessage, stopOnFailure bool, apply func(context.Context, json.RawMessage) (json.RawMessage, error)) []string {
	ctx = lifecycle.Detach(ctx)
	errors := []string{}
	for index := 0; index < len(undos); {
		if stopOnFailure && anySet(errors) {
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
				_ = jsondecode.Unmarshal(raw, &undo)
				changes[i] = undo.Changes[0]
			}
			if method == "DELETE" {
				errors = append(errors, d.RestoreDeleted(ctx, changes, stopOnFailure, path)...)
			} else {
				errors = append(errors, d.RestoreRemoved(ctx, changes, path)...)
			}
		} else {
			for _, raw := range group {
				if stopOnFailure && anySet(errors) {
					errors = append(errors, BatchSkipped)
					continue
				}
				_, err := apply(ctx, raw)
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

func (d *Client) batchUndo(raw json.RawMessage) (method, path string, ok bool) {
	if !d.BatchRestorable(raw) {
		return "", "", false
	}
	var undo ROSUndo
	_ = jsondecode.Unmarshal(raw, &undo)
	return undo.Changes[0].Method, undo.Changes[0].Path, true
}

func repeatString(value string, count int) []string {
	values := make([]string, count)
	for i := range values {
		values[i] = value
	}
	return values
}

// anySet: some earlier record has an error or was skipped.
func anySet(values []string) bool {
	return slices.ContainsFunc(values, func(value string) bool { return value != "" })
}

// rollbackFailed is the text of a batched rollback record that failed.
func rollbackFailed(message string) string { return fmt.Sprintf(messageRollbackIncomplete, message) }

func RouteAddValues(op Operation) Row {
	return Row{"dst-address": op.CIDR, "gateway": op.Gateway, "comment": op.Comment, "disabled": "false"}
}

func (d *Client) Query(ctx context.Context, field string, values []string, path string) ([]Row, error) {
	unique := device.Unique(values)
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
		value, err := d.Send(ctx, "POST", path+"/print", rosPrint{Query: terms}, Timeout)
		if err != nil {
			return nil, err
		}
		batch, ok := asRows(value)
		if !ok {
			return nil, device.Error(fmt.Sprintf(device.MessageROSListFormat, path))
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}

func (d *Client) retryRead(ctx context.Context, read func() ([]Row, error)) ([]Row, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := read()
		if err == nil {
			return rows, nil
		}
		if !device.IsRouterError(err) || attempt == 2 {
			return nil, err
		}
		if err := WaitContext(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	return nil, device.Error(messageROSReadbackFailure)
}

func WaitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func RouteAddLine(index int, values Row, path string) string {
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
		value := values[key]
		if nat && (key == "disabled" || key == "log") {
			switch Comparable(key, value) {
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

func ConsoleSafe(values Row) bool {
	for key := range values {
		if !consoleRouteFields[key] {
			return false
		}
	}
	return values["dst-address"] != "" &&
		(Comparable("disabled", values.TextOr("disabled", "false")) == "true" ||
			Comparable("disabled", values.TextOr("disabled", "false")) == "false")
}

func refusedConsole(error error) bool {
	return device.RouterErrorKind(error) != device.KindUncertainWrite || permissionError.MatchString(error.Error())
}

func (d *Client) ConsoleAdd(ctx context.Context, values []Row, current []Row, hasCurrent bool, path string, beforeWrite ...func([]Row) error) (*ConsoleAddResult, error) {
	destinations := make([]string, len(values))
	for i, value := range values {
		destinations[i] = value["dst-address"]
	}
	if !hasCurrent {
		var err error
		current, err = d.Query(ctx, "dst-address", destinations, path)
		if err != nil {
			return nil, nil
		}
	}
	for _, guard := range beforeWrite {
		if err := guard(current); err != nil {
			return nil, err
		}
	}
	before := RowIDs(current)
	lines := make([]string, len(values))
	for i, item := range values {
		lines[i] = RouteAddLine(i, item, path)
	}
	script := ":local f 0\n" + strings.Join(lines, "\n") + "\n:put \"cf-done\""
	result, err := d.Request(ctx, "POST", "/execute", Row{"script": script, "as-string": ""}, Timeout)
	if err != nil {
		if refusedConsole(err) {
			return nil, nil
		}
		return nil, device.RollbackError(fmt.Sprintf(messageBatchUnknown, err.Error()))
	}
	output := strings.ReplaceAll(AsRow(result)["ret"], "\r", "")
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
	// More reported adds than the script holds is a malformed reply: the
	// extra IDs are nobody's to claim, and what was proven before them must
	// still be recorded (Python indexed past its values here).
	var failure *batchFailure
	if len(ids) > len(values) {
		failure = &batchFailure{Index: len(values), Err: device.RollbackError(fmt.Sprintf(device.MessageUncertainRouteWrite, path))}
	}
	reported := ids[:min(len(ids), len(values))]
	after, err := d.retryRead(ctx, func() ([]Row, error) { return d.Query(ctx, "dst-address", destinations, path) })
	if err != nil {
		return nil, device.RollbackError(fmt.Sprintf(messageBatchReadbackRoutes, err.Error()))
	}
	byID := map[string]Row{}
	for _, row := range after {
		byID[row.ID()] = row
	}
	rows := map[int]*Row{}
	for index, id := range reported {
		row, exists := byID[id]
		if !exists && IDPattern.MatchString(id) {
			found, readErr := d.Find(ctx, path, id)
			if readErr != nil {
				if failure == nil || index < failure.Index {
					failure = &batchFailure{Index: index, Err: device.RollbackError(fmt.Sprintf(messageUncertainWrite, readErr.Error()))}
				}
				continue
			}
			row, exists = found, found != nil
		}
		validID := IDPattern.MatchString(id) && !before[id] && !duplicateID(ids, index, id)
		if !validID || exists && (Flag(row["dynamic"]) || !inScope(path, row, values[index])) {
			if failure == nil || index < failure.Index {
				failure = &batchFailure{Index: index, Err: device.RollbackError(fmt.Sprintf(device.MessageUncertainRouteWrite, path))}
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
			if knownIDs[row.ID()] {
				continue
			}
			for _, value := range values[len(reported):] {
				if inScope(path, row, value) {
					unexplained = true
					break
				}
			}
		}
	}
	return &ConsoleAddResult{Rows: rows, Count: len(reported), Failure: failure, Unexplained: unexplained}, nil
}

// rosPrint is a /print request body.
type rosPrint struct {
	Query []string `json:".query"`
}

func duplicateID(ids []string, index int, id string) bool {
	for i := 0; i < index; i++ {
		if ids[i] == id {
			return true
		}
	}
	return false
}

// checkWritten compares one read-back row (and, for NAT order, its table)
// with what was written.
func checkWritten(check Check, row Row, rows []Row) error {
	if check.Absent {
		if row != nil {
			return device.Error(messageReadbackDeletePresent)
		}
		return nil
	}
	if row == nil {
		return device.Error(messageReadbackMissingWrite)
	}
	keys := make([]string, 0, len(check.Values))
	for key := range check.Values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := check.Values[key]
		if key == "place-before" {
			target := value
			if IndexOfRow(rows, row.ID()) < 0 || IndexOfRow(rows, target) < 0 || IndexOfRow(rows, row.ID()) >= IndexOfRow(rows, target) {
				return device.Error(messageReadbackNATOrder)
			}
		} else if Comparable(key, row[key]) != Comparable(key, value) {
			return device.Error(fmt.Sprintf(messageReadbackMismatch, check.ID, key))
		}
	}
	return nil
}

// RouteCreateProof carries R10's complete creation proof without changing the
// serialized shared undo contract. Other route callers omit it.
type RouteCreateProof struct {
	Guard   func([]Row) error
	Created map[string]Row
}

func (d *Client) AddRoutes(ctx context.Context, rawOps []json.RawMessage, batch []int,
	record func(json.RawMessage, json.RawMessage), path string, options ...RouteCreateProof) ([]int, error) {
	ops := make([]Operation, len(batch))
	values := make([]Row, len(batch))
	for i, index := range batch {
		if err := jsondecode.Unmarshal(rawOps[index], &ops[i]); err != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		values[i] = RouteAddValues(ops[i])
	}
	guards := []func([]Row) error{}
	for _, option := range options {
		if option.Guard != nil {
			guards = append(guards, option.Guard)
		}
	}
	added, err := d.ConsoleAdd(ctx, values, nil, false, path, guards...)
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
			if checkErr := checkWritten(Check{ID: row.ID(), Values: values[index]}, *row, nil); checkErr != nil {
				failure = &batchFailure{Index: index, Err: checkErr}
			}
		}
		expected := d.OwnershipSnapshot(path, MergeRows(values[index], *row))
		change := ROSChange{Method: "DELETE", Path: path, ID: row.ID(), Expected: MarshalRow(expected)}
		undo := ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}}
		encoded, marshalErr := json.Marshal(undo)
		if marshalErr != nil {
			return nil, marshalErr
		}
		for _, option := range options {
			if option.Created != nil {
				option.Created[row.ID()] = StableIPv6Row(*row)
			}
		}
		owned = append(owned, provenRoute{index: index, row: *row, undo: encoded})
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
			cause = device.Error(messageReadbackMissingWrite)
		}
		// Routes after the failed one never existed in a one-by-one release:
		// remove them with the same batched rollback a release uses.
		extra := []json.RawMessage{}
		for _, item := range owned {
			if item.index >= failure.Index {
				extra = append(extra, item.undo)
			}
		}
		for _, problem := range d.RestoreMany(ctx, extra, false) {
			if problem != "" {
				return nil, device.RollbackError(cause.Error() + "；" + problem)
			}
		}
		return nil, cause
	}
	if added.Unexplained {
		return nil, device.RollbackError(fmt.Sprintf(messageBatchIncomplete, path))
	}
	left := append(make([]int, 0, len(batch)-added.Count), batch[added.Count:]...)
	return left, nil
}

type provenRoute struct {
	index int
	row   Row
	undo  json.RawMessage
}

func findProven(items []provenRoute, index int) *provenRoute {
	for i := range items {
		if items[i].index == index {
			return &items[i]
		}
	}
	return nil
}

func (d *Client) DeleteRoutes(ctx context.Context, rawOps []json.RawMessage, batch []int,
	record func(json.RawMessage, json.RawMessage), path string, proofs ...map[string]Row) ([]int, error) {
	ops := make([]Operation, len(batch))
	cidrs := make([]string, len(batch))
	for i, index := range batch {
		if err := jsondecode.Unmarshal(rawOps[index], &ops[i]); err != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		cidrs[i] = ops[i].CIDR
	}
	rows, err := d.Query(ctx, "dst-address", cidrs, path)
	if err != nil {
		return batch, nil
	}
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.ID()] = row
	}
	if len(proofs) > 0 {
		counts := map[string]int{}
		for _, row := range rows {
			counts[row.ID()]++
		}
		for _, op := range ops {
			row := byID[op.ID]
			want, ok := proofs[0][op.ID]
			if !ok || row == nil || counts[op.ID] != 1 || !SameRow(StableIPv6Row(row), want) || PolicyFlag(row["dynamic"]) || PolicyFlag(row["connect"]) {
				return nil, device.Error(MessageIPv6RoutesOwnership)
			}
		}
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
		if row == nil || Flag(row["dynamic"]) || row.Text("dst-address") != op.CIDR {
			break
		}
		known := []string{}
		for _, item := range rows {
			if item.Text("dst-address") == row.Text("dst-address") {
				known = append(known, item.ID())
			}
		}
		change := ROSChange{Method: "RESTORE", Path: path, ID: op.ID, Values: Payload(path, row),
			Expected: MarshalRow(d.OwnershipSnapshot(path, row)), KnownIDs: known}
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
		if len(proofs) > 0 {
			current, readErr := d.Query(ctx, ".id", ids[start:end], path)
			if readErr != nil {
				failure = readErr
				break
			}
			byID := map[string]Row{}
			counts := map[string]int{}
			for _, row := range current {
				byID[row.ID()] = row
				counts[row.ID()]++
			}
			for _, id := range ids[start:end] {
				if counts[id] != 1 || !SameRow(StableIPv6Row(byID[id]), proofs[0][id]) || PolicyFlag(byID[id]["dynamic"]) || PolicyFlag(byID[id]["connect"]) {
					failure = device.Error(MessageIPv6RoutesOwnership)
					break
				}
			}
			if failure != nil {
				break
			}
		}
		if _, err := d.Request(ctx, "POST", path+"/remove", Row{".id": strings.Join(ids[start:end], ",")}, Timeout); err != nil {
			failure = err
			break
		}
	}
	remainingRows, readErr := d.retryRead(ctx, func() ([]Row, error) { return d.Query(ctx, ".id", ids, path) })
	if readErr != nil {
		for _, item := range prepared {
			if record != nil {
				record(item.op, item.undo)
			}
		}
		return nil, readErr
	}
	remaining := RowIDs(remainingRows)
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

func (d *Client) RestoreDeleted(ctx context.Context, changes []ROSChange, stop bool, path string) []string {
	errors := make([]string, len(changes))
	destinations := make([]string, len(changes))
	for i, change := range changes {
		var expected Row
		decodeRowInto(change.Expected, &expected)
		destinations[i] = expected["dst-address"]
	}
	rows, readErr := d.Query(ctx, "dst-address", destinations, path)
	if readErr != nil {
		rows = nil
	}
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.ID()] = row
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
			if _, err := d.Request(ctx, "POST", path+"/remove", Row{".id": strings.Join(ids[start:end], ",")}, Timeout); err != nil {
				break
			}
		}
		remainingRows, err := d.retryRead(ctx, func() ([]Row, error) { return d.Query(ctx, ".id", ids, path) })
		remaining := RowIDs(remainingRows)
		if err != nil {
			for _, id := range ids {
				remaining[id] = true
			}
		}
		for _, index := range pending {
			// Removed routes are restored; only those still present after a
			// failure are left untouched (skipped).
			if !remaining[changes[index].ID] {
				continue
			}
			if stop && anySet(errors[:index]) {
				errors[index] = BatchSkipped
				continue
			}
			errors[index] = d.restoreSingleChange(ctx, changes[index])
		}
		pending = nil
	}
	for index, change := range changes {
		if stop && anySet(errors[:index]) {
			for i := index; i < len(errors); i++ {
				errors[i] = BatchSkipped
			}
			break
		}
		row := byID[change.ID]
		expected, expectedSet := decodeRow(change.Expected)
		if rows != nil && row != nil && expectedSet && !Flag(row["dynamic"]) &&
			maps.Equal(d.OwnershipSnapshot(path, row), expected) {
			pending = append(pending, index)
			continue
		}
		flush()
		if rows == nil || row == nil || !expectedSet {
			errors[index] = d.restoreSingleChange(ctx, change)
		} else if Flag(row["dynamic"]) {
			errors[index] = rollbackFailed(messageROSDynamicObject)
		} else {
			errors[index] = rollbackFailed(messageChangedDuringWrite)
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

func (d *Client) restoreSingleChange(ctx context.Context, change ROSChange) string {
	_, err := d.Restore(ctx, []ROSChange{change})
	if err != nil {
		return err.Error()
	}
	return ""
}

func (d *Client) RestoreRemoved(ctx context.Context, changes []ROSChange, path string, remaps ...map[string]string) []string {
	restored := func(change ROSChange) string {
		check, err := d.Perform(ctx, change, &[]ROSChange{}, true)
		if err == nil {
			err = d.VerifyChange(ctx, check)
		}
		if err != nil {
			return rollbackFailed(err.Error())
		}
		if len(remaps) > 0 {
			remaps[0][change.ID] = check.ID
		}
		return ""
	}
	errors := make([]string, len(changes))
	ids := make([]string, len(changes))
	for i, change := range changes {
		ids[i] = change.ID
	}
	presentRows, err := d.Query(ctx, ".id", ids, path)
	if err != nil {
		for i, change := range changes {
			errors[i] = restored(change)
		}
		return errors
	}
	present := map[string]Row{}
	for _, row := range presentRows {
		present[row.ID()] = row
	}
	pending := []int{}
	pendingBytes := 0
	flush := func() {
		chunk := slices.Clone(pending)
		pending, pendingBytes = nil, 0
		var rows []Row
		if len(chunk) >= d.batchMin {
			destinations := make([]string, len(chunk))
			for i, index := range chunk {
				destinations[i] = changes[index].Values["dst-address"]
			}
			rows, err = d.Query(ctx, "dst-address", destinations, path)
			if err != nil {
				rows = nil
			}
		}
		if rows == nil {
			for _, index := range chunk {
				errors[index] = restored(changes[index])
			}
			return
		}
		adding := []int{}
		for _, index := range chunk {
			change := changes[index]
			if hasUnexpectedRoute(rows, change.Values, change.KnownIDs) {
				errors[index] = rollbackFailed(messageRollbackRouteConflict)
			} else {
				adding = append(adding, index)
			}
		}
		values := make([]Row, len(adding))
		for i, index := range adding {
			values[i] = changes[index].Values
		}
		var added *ConsoleAddResult
		if len(adding) >= d.batchMin {
			added, err = d.ConsoleAdd(ctx, values, rows, true, path)
			if err != nil {
				for _, index := range adding {
					errors[index] = rollbackFailed(err.Error())
				}
				return
			}
		}
		if added == nil {
			for _, index := range adding {
				errors[index] = restored(changes[index])
			}
			return
		}
		for position, index := range adding[:min(added.Count, len(adding))] {
			rowPtr, exists := added.Rows[position]
			if !exists {
				message := fmt.Sprintf(device.MessageUncertainRouteWrite, path)
				if added.Failure != nil && added.Failure.Index == position && added.Failure.Err != nil {
					message = added.Failure.Err.Error()
				}
				errors[index] = rollbackFailed(message)
				continue
			}
			var row Row
			if rowPtr != nil {
				row = *rowPtr
			}
			if checkErr := checkWritten(Check{ID: row[".id"], Values: changes[index].Values}, row, nil); checkErr != nil {
				errors[index] = rollbackFailed(checkErr.Error())
			} else if len(remaps) > 0 {
				remaps[0][changes[index].ID] = row.ID()
			}
		}
		for _, index := range adding[min(added.Count, len(adding)):] {
			if added.Unexplained {
				errors[index] = rollbackFailed(fmt.Sprintf(messageBatchIncomplete, path))
			} else {
				errors[index] = restored(changes[index])
			}
		}
	}

	for index, change := range changes {
		current := present[change.ID]
		if current != nil {
			if Payload(path, current) == nil {
				errors[index] = rollbackFailed(PayloadError(path, current).Error())
			} else if !RowsEqual(d.OwnershipSnapshot(path, current), change.Expected) {
				errors[index] = rollbackFailed(messageRollbackChanged)
			}
			if errors[index] == "" && len(remaps) > 0 {
				remaps[0][change.ID] = change.ID
			}
			continue
		}
		for _, pendingIndex := range pending {
			if Comparable("dst-address", changes[pendingIndex].Values["dst-address"]) == Comparable("dst-address", change.Values["dst-address"]) {
				flush()
				break
			}
		}
		if !ConsoleSafe(change.Values) {
			errors[index] = restored(change)
			continue
		}
		// Keep each script within the limit the add path uses (Python did
		// not check it here and could send a script Client rejects).
		size := len(RouteAddLine(len(pending), change.Values, path)) + 1
		if len(pending) > 0 && pendingBytes+size > RouteScriptBytes {
			flush()
			size = len(RouteAddLine(0, change.Values, path)) + 1
		}
		pending = append(pending, index)
		pendingBytes += size
		if len(pending) == d.routeBatch {
			flush()
		}
	}
	flush()
	return errors
}

func (d *Client) PutNAT(ctx context.Context, changes []ROSChange, undo *[]ROSChange) (int, error) {
	rows, err := d.List(ctx, PathNAT)
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
		line := consoleAddLine(len(batch), change.Values, PathNAT, true)
		if len(batch) > 0 && (len(batch) == d.routeBatch || size+len([]byte(line)) > RouteScriptBytes) {
			used, err := flush()
			if err != nil {
				return handled, err
			}
			if !used {
				return handled, nil
			}
			line = consoleAddLine(0, change.Values, PathNAT, true)
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

func (d *Client) natBatch(ctx context.Context, values []Row, rows []Row, undo *[]ROSChange) (natBatchResult, error) {
	before := RowIDs(rows)
	lines := make([]string, len(values))
	for i, item := range values {
		lines[i] = consoleAddLine(i, item, PathNAT, true)
	}
	script := ":local f 0\n" + strings.Join(lines, "\n") + "\n:put \"cf-done\""
	result, err := d.Request(ctx, "POST", "/execute", Row{"script": script, "as-string": ""}, Timeout)
	if err != nil {
		if refusedConsole(err) {
			return natBatchResult{Used: false}, nil
		}
		return natBatchResult{}, device.RollbackError(fmt.Sprintf(messageBatchUnknownNAT, err.Error()))
	}
	output := strings.ReplaceAll(AsRow(result)["ret"], "\r", "")
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
	rows, err = d.retryRead(ctx, func() ([]Row, error) { return d.List(ctx, PathNAT) })
	if err != nil {
		return natBatchResult{}, device.RollbackError(fmt.Sprintf(messageBatchReadbackNAT, err.Error()))
	}
	for attempt := 0; attempt < natSettleReads; attempt++ {
		pending := false
		for _, row := range rows {
			if Flag(row["invalid"]) && slices.Contains(ids, row.ID()) {
				pending = true
				break
			}
		}
		if !pending {
			break
		}
		if waitErr := WaitContext(ctx, 250*time.Millisecond); waitErr != nil {
			return natBatchResult{}, device.RollbackError(fmt.Sprintf(messageBatchReadbackNAT, waitErr.Error()))
		}
		rows, err = d.retryRead(ctx, func() ([]Row, error) { return d.List(ctx, PathNAT) })
		if err != nil {
			return natBatchResult{}, device.RollbackError(fmt.Sprintf(messageBatchReadbackNAT, err.Error()))
		}
	}
	byID := map[string]Row{}
	for _, row := range rows {
		byID[row.ID()] = row
	}
	owned := []struct {
		id       string
		expected Row
	}{}
	valid := slices.Contains(outputLines, "cf-done") && len(ids) == len(values)
	unproven := false
	if !slices.Contains(outputLines, "cf-done") {
		for _, row := range rows {
			if !before[row.ID()] && !slices.Contains(ids, row.ID()) {
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
		if !IDPattern.MatchString(id) || before[id] || duplicateID(ids, index, id) {
			unproven, valid = true, false
			continue
		}
		if !exists {
			valid = false
			continue
		}
		expected := d.OwnershipSnapshot(PathNAT, MergeRows(values[index], row))
		if valid {
			if checkErr := checkWritten(Check{ID: id, Values: values[index]}, row, rows); checkErr != nil {
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
			*undo = append(*undo, ROSChange{Method: "DELETE", Path: PathNAT, ID: item.id, Expected: MarshalRow(item.expected)})
		}
		return natBatchResult{Rows: rows, Used: true}, nil
	}
	failures := []string{}
	for _, item := range owned {
		if item.expected == nil {
			unproven = true
			continue
		}
		if _, rollback := d.Restore(lifecycle.Detach(ctx), []ROSChange{{Method: "DELETE", Path: PathNAT, ID: item.id,
			Expected: MarshalRow(item.expected)}}); rollback != nil {
			failures = append(failures, rollback.Error())
		}
	}
	if unproven || len(failures) > 0 {
		message := strings.Join(failures, "；")
		if unproven {
			message = strings.Join(device.NonEmpty([]string{message,
				messageBatchNATUnknown}), "；")
		}
		return natBatchResult{}, device.RollbackError(message)
	}
	return natBatchResult{Used: false}, nil
}

func putNAT(ctx context.Context, d *Client, changes []ROSChange, undo *[]ROSChange) (int, error) {
	return d.PutNAT(ctx, changes, undo)
}
