// Package ipv6 implements RouterOS IPv6 Fake-IP routing, IPv6 ruleset routing,
// IPv6 DNS distribution, IPv6 PD guard, IPv6 readiness/policy evaluation, and
// forward-chain advisory hints.
package ipv6

import (
	"context"
	"encoding/json"

	"configflow/internal/routers/ros"
)

type rosDevice struct {
	*ros.Client
}

// Exported RouterOS path, PD guard, and message constants shared with tests.
const (
	PathIPv4Addresses     = rosIPv4Addresses
	PathIPv6Addresses     = rosIPv6Addresses
	PathPDScripts         = pdScripts
	PathPDSchedulers      = pdSchedulers
	PathPDClients         = pdClients
	PathPDPrefixes        = pdPrefixes
	PDLastList            = pdLast
	PDWatchScript         = pdWatch
	PDPollScheduler       = pdPoll
	PDPolicy              = pdPolicy
	PDEvent               = pdEvent
	PDHook                = pdHook
	PDRollback            = pdRollback
	MessagePreviewChanged = messageIPv6PreviewChanged
	MessageFakeSettings   = messageIPv6FakeSettings
	MessageRange          = messageIPv6Range
	MessageForwardIPv4    = messageForwardIPv4Drop
)

// ApplyIPv6DNS applies an IPv6 DNS update operation on c.
func ApplyIPv6DNS(ctx context.Context, c *ros.Client, op IPv6DNSUpdate) (ros.ROSUndo, error) {
	return (rosDevice{c}).ApplyIPv6DNS(ctx, op)
}

// ApplyIPv6PDGuard applies an IPv6 PD guard sync operation on c.
func ApplyIPv6PDGuard(ctx context.Context, c *ros.Client, op IPv6PDGuardSync) (*IPv6PDGuardUndo, error) {
	return (rosDevice{c}).ApplyIPv6PDGuard(ctx, op)
}

// RestoreIPv6PDGuard restores an IPv6 PD guard undo journal on c.
func RestoreIPv6PDGuard(ctx context.Context, c *ros.Client, op IPv6PDGuardUndo) error {
	return (rosDevice{c}).RestoreIPv6PDGuard(ctx, op)
}

// ApplyIPv6FakeIP applies an IPv6 Fake-IP route replace operation on c.
func ApplyIPv6FakeIP(ctx context.Context, c *ros.Client, op IPv6FakeIPReplace) (*IPv6FakeIPUndo, error) {
	return (rosDevice{c}).ApplyIPv6FakeIP(ctx, op)
}

// RestoreIPv6FakeIP restores an IPv6 Fake-IP undo journal on c.
func RestoreIPv6FakeIP(ctx context.Context, c *ros.Client, op IPv6FakeIPUndo) error {
	return (rosDevice{c}).RestoreIPv6FakeIP(ctx, op)
}

// ApplyIPv6Routes applies an IPv6 ruleset route replace operation on c.
func ApplyIPv6Routes(ctx context.Context, c *ros.Client, op IPv6RoutesReplace) (*IPv6RoutesUndo, error) {
	return (rosDevice{c}).ApplyIPv6Routes(ctx, op)
}

// RestoreIPv6Routes restores an IPv6 ruleset route undo journal on c.
func RestoreIPv6Routes(ctx context.Context, c *ros.Client, op IPv6RoutesUndo) error {
	return (rosDevice{c}).RestoreIPv6Routes(ctx, op)
}

// AddMissingIPv6Route adds one missing IPv6 route during readiness repair on c.
func AddMissingIPv6Route(ctx context.Context, c *ros.Client, plan ros.Row) (*IPv6ReadinessReceipt, error) {
	return (rosDevice{c}).AddMissingIPv6Route(ctx, plan)
}

// UndoIPv6ReadinessRoute removes a route created by AddMissingIPv6Route on c.
func UndoIPv6ReadinessRoute(ctx context.Context, c *ros.Client, receipt IPv6ReadinessReceipt) error {
	return (rosDevice{c}).UndoIPv6ReadinessRoute(ctx, receipt)
}

// RemapIPv6 refreshes stored IPv6 manifest resource IDs after rollback on c.
func RemapIPv6(ctx context.Context, c *ros.Client, previous json.RawMessage, remap map[string]string, fake bool) error {
	return (rosDevice{c}).remapIPv6(ctx, previous, remap, fake)
}
