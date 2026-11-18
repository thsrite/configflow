package routers

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"configflow/internal/lifecycle"
)

const fixedROSLeases = "/ip/dhcp-server/lease"

func (d *RouterOS) fixedLease(ctx context.Context, id string) (Row, error) {
	if err := fixedROSID(id); err != nil {
		return nil, err
	}
	value, err := d.get(ctx, fixedROSLeases+"/"+escapeROSPathID(id), rosTimeout)
	if routerErrorKind(err) == KindMissingResource {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row := asRow(value)
	if row == nil || row.id() != id {
		return nil, routerError(messageFixedROSReadID)
	}
	return row, nil
}
func (d *RouterOS) removeFixedLease(ctx context.Context, b *FixedIPBinding, expected Row) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	row, err := d.fixedLease(ctx, b.ID)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	if !fixedROSMatches(row, b) || expected != nil && !sameRow(fixedROSFields(row), expected) {
		return routerError(messageFixedROSExternal)
	}
	_, failure := d.request(ctx, "DELETE", fixedROSLeases+"/"+escapeROSPathID(b.ID), nil, rosTimeout)
	if failure != nil && routerErrorKind(failure) != KindUncertainWrite {
		return failure
	}
	remaining, readError := d.fixedLease(ctx, b.ID)
	if readError != nil {
		remaining, err = d.fixedLease(ctx, b.ID)
		if err != nil {
			return rollbackError(fmt.Sprintf(messageFixedROSDeleteUnknown, err))
		}
	}
	if remaining != nil {
		if failure != nil {
			return failure
		}
		if readError != nil {
			return readError
		}
		return routerError(messageFixedROSStillPresent)
	}
	return nil
}

// ApplyFixedIP detaches a client disconnect, rechecks the preview, and retains
// exact proof for compensation. Runtime shutdown still cancels every device I/O.
func (d *RouterOS) ApplyFixedIP(ctx context.Context, op *FixedIPOperation) (binding *FixedIPBinding, err error) {
	ctx = lifecycle.Detach(ctx)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, routerError(messageOperationFormat)
	}
	latest, err := d.PreviewFixedIP(ctx, op.IP, op.MAC, fixedText(op.Name))
	if err != nil {
		return nil, err
	}
	if !jsonEqual(latest, op) {
		return nil, routerError(messageFixedROSPreviewChanged)
	}
	expected := fixedOperationBinding(op, "routeros")
	nonce, err := fixedNonce()
	if err != nil {
		return nil, err
	}
	expected.Comment += ":" + nonce
	original := maps.Clone(op.Expected)
	var acquired *FixedIPBinding
	var acquiredFields Row
	conversionPending := false
	defer func() {
		if err == nil || ctx.Err() != nil {
			return
		}
		if acquired == nil && !conversionPending {
			return
		}
		target := acquired
		if target == nil {
			target = expected
		}
		current, e := d.fixedLease(ctx, target.ID)
		if e == nil && current != nil && !current.flag("dynamic") {
			fields := maps.Clone(acquiredFields)
			if fields == nil {
				fields = maps.Clone(original)
			}
			if fields == nil {
				e = routerError(messageFixedRollbackChanged)
			} else {
				comment := fields.text("comment")
				fields["comment"] = current.text("comment")
				if current.text("address") != expected.IP || fixedMAC(current.text("mac-address")) != expected.MAC || current.text("server") != fixedText(expected.Server) || (current.text("comment") != comment && current.text("comment") != expected.Comment) || !sameRow(fixedROSFields(current), fields) {
					e = routerError(messageFixedRollbackChanged)
				} else {
					var b *FixedIPBinding
					b, e = fixedROSBinding(current)
					if e == nil {
						e = d.removeFixedLease(ctx, b, fields)
					}
					if e == nil && op.Kind == "make_static" {
						err = routerError(err.Error() + "；" + messageFixedDynamicUndo)
					}
				}
			}
		}
		if e != nil {
			err = rollbackError(fmt.Sprintf(messageFixedROSRollback, err, e))
		}
	}()
	if op.Kind == "make_static" {
		_, err = d.request(ctx, "POST", fixedROSLeases+"/make-static", Row{".id": fixedText(op.ID)}, rosTimeout)
		if err != nil {
			if routerErrorKind(err) == KindUncertainWrite {
				conversionPending = true
				row, e := d.fixedLease(ctx, fixedText(op.ID))
				if e != nil {
					return nil, e
				}
				if row != nil && !row.flag("dynamic") {
					acquired, e = fixedROSBinding(row)
					if e != nil {
						return nil, e
					}
					acquiredFields = maps.Clone(original)
				}
			}
			return nil, err
		}
		conversionPending = true
		row, e := d.fixedLease(ctx, fixedText(op.ID))
		if e != nil {
			return nil, e
		}
		if row == nil || row.flag("dynamic") {
			return nil, routerError(messageFixedROSNotStatic)
		}
		acquired, e = fixedROSBinding(row)
		if e != nil {
			return nil, e
		}
		acquiredFields = maps.Clone(original)
		if !sameRow(fixedROSFields(row), original) {
			return nil, rollbackError(messageFixedROSConversionChanged)
		}
		if acquired.IP != expected.IP || acquired.MAC != expected.MAC || fixedText(acquired.Server) != fixedText(expected.Server) {
			acquired = nil
			return nil, rollbackError(messageFixedROSConversionTarget)
		}
		if _, err = d.request(ctx, "PATCH", fixedROSLeases+"/"+escapeROSPathID(expected.ID), Row{"comment": expected.Comment}, rosTimeout); err != nil {
			return nil, err
		}
	} else {
		rows, e := d.list(ctx, fixedROSLeases)
		if e != nil {
			return nil, e
		}
		known := rowIDs(rows)
		values := Row{"mac-address": op.MAC, "address": op.IP, "server": fixedText(op.Server), "comment": expected.Comment, "disabled": "false"}
		var response any
		response, err = d.request(ctx, "PUT", fixedROSLeases, values, rosTimeout)
		row := asRow(response)
		newID := ""
		if row != nil {
			newID = row.id()
		}
		if err == nil && (!rosIDPattern.MatchString(newID) || known[newID]) {
			err = rosError(KindUncertainWrite, messageFixedROSNewID)
		}
		if err != nil {
			if routerErrorKind(err) != KindUncertainWrite {
				return nil, err
			}
			cause := err
			rows, e = d.list(ctx, fixedROSLeases)
			if e != nil {
				return nil, rollbackError(fmt.Sprintf(messageFixedKeepScene, cause, e))
			}
			matches := []Row{}
			for _, r := range rows {
				candidate := *expected
				candidate.ID = r.id()
				if !known[r.id()] && fixedROSMatches(r, &candidate) {
					matches = append(matches, r)
				}
			}
			if len(matches) != 1 {
				return nil, rollbackError(fmt.Sprintf(messageFixedKeepScene, cause, messageFixedROSNotUnique))
			}
			recovered, e := d.fixedLease(ctx, matches[0].id())
			if e != nil {
				return nil, rollbackError(fmt.Sprintf(messageFixedKeepScene, cause, e))
			}
			if recovered == nil || !sameRow(fixedROSFields(recovered), fixedROSFields(matches[0])) {
				return nil, rollbackError(fmt.Sprintf(messageFixedKeepScene, cause, messageFixedROSRecoveryChanged))
			}
			acquired, e = fixedROSBinding(recovered)
			if e != nil {
				return nil, e
			}
			acquiredFields = fixedROSFields(recovered)
			expected.ID = acquired.ID
			return nil, cause
		}
		expected.ID = newID
		copy := *expected
		acquired = &copy
		fields := maps.Clone(values)
		maps.Copy(fields, row)
		acquiredFields = fixedROSFields(fields)
	}
	row, err := d.fixedLease(ctx, expected.ID)
	if err != nil {
		return nil, err
	}
	if err = fixedROSCheck(row, expected, false); err != nil {
		return nil, err
	}
	expected.Undo = &FixedIPUndo{Kind: op.Kind, Expected: fixedROSFields(row), Original: original}
	return expected, nil
}

// RollbackFixedIP removes only the proven reservation created by this job.
func (d *RouterOS) RollbackFixedIP(ctx context.Context, b *FixedIPBinding) error {
	ctx = lifecycle.Detach(ctx)
	if b == nil || !strings.HasPrefix(b.Comment, fixedIPPrefix) || b.Undo == nil || b.Undo.Expected == nil {
		return routerError(messageFixedROSProof)
	}
	return d.removeFixedLease(ctx, b, b.Undo.Expected)
}

// PreviewDeleteFixedIP captures the complete stable before-image for deletion.
func (d *RouterOS) PreviewDeleteFixedIP(ctx context.Context, b *FixedIPBinding) (*FixedIPOperation, error) {
	if b == nil || b.Vendor != "routeros" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, routerError(messageFixedROSDeleteOwn)
	}
	row, err := d.fixedLease(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	if !fixedROSMatches(row, b) {
		return nil, routerError(messageFixedROSDeleteChanged)
	}
	return fixedDeleteOperation(b, fixedROSFields(row), fmt.Sprintf(messageFixedROSDeleteSummary, b.IP, b.MAC)), nil
}

// DeleteFixedIP re-proves the approved lease and reconciles lost delete replies.
func (d *RouterOS) DeleteFixedIP(ctx context.Context, op *FixedIPOperation) (*FixedIPBinding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, routerError(messageOperationFormat)
	}
	b := fixedOperationBinding(op, "routeros")
	latest, err := d.PreviewDeleteFixedIP(ctx, b)
	if err != nil {
		return nil, err
	}
	if !jsonEqual(latest, op) {
		return nil, routerError(messageFixedROSDeletePreview)
	}
	if err = d.removeFixedLease(ctx, b, op.Expected); err != nil {
		return nil, err
	}
	b.Undo = &FixedIPUndo{Kind: "delete", Expected: maps.Clone(op.Expected)}
	return b, nil
}

// RestoreDeletedFixedIP refuses ID reuse and conflicting IP/MAC allocations.
func (d *RouterOS) RestoreDeletedFixedIP(ctx context.Context, b *FixedIPBinding) (*FixedIPBinding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.Undo == nil || b.Undo.Expected == nil || b.Undo.Kind != "delete" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, routerError(messageFixedROSRestoreProof)
	}
	current, err := d.fixedLease(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	if current != nil {
		if fixedROSMatches(current, b) && sameRow(fixedROSFields(current), b.Undo.Expected) {
			return fixedROSBinding(current)
		}
		return nil, routerError(messageFixedROSRestoreID)
	}
	rows, err := d.list(ctx, fixedROSLeases)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.text("address") == b.IP || fixedMAC(row.text("mac-address")) == b.MAC {
			return nil, routerError(messageFixedROSRestoreConflict)
		}
	}
	value, err := d.request(ctx, "PUT", fixedROSLeases, b.Undo.Expected, rosTimeout)
	if err != nil {
		return nil, err
	}
	row := asRow(value)
	if row == nil {
		return nil, routerError(messageFixedROSID)
	}
	if err = fixedROSID(row.id()); err != nil {
		return nil, err
	}
	restored := *b
	restored.ID = row.id()
	actual, err := d.fixedLease(ctx, restored.ID)
	if err != nil {
		return nil, err
	}
	if err = fixedROSCheck(actual, &restored, b.Undo.Expected.flag("disabled")); err != nil {
		return nil, err
	}
	if !sameRow(fixedROSFields(actual), b.Undo.Expected) {
		return nil, rollbackError(messageFixedROSRestoreCheck)
	}
	return fixedROSBinding(actual)
}
