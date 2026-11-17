// Package lockx provides cancellable in-process locks, keyed operation claims
// and shared calls. It checks the server's L1 job → L2 device → L3 repository
// ordering using the contexts returned by acquisitions. L1 is try-only; L4 leaf
// stores and L5 memory guards use sync.Mutex in their owning packages.
//
// Locks create, open and remove no files. Only instance and repo own file locks;
// old per-resource lock files are left untouched. Callers own lock names and
// timeouts, map busy errors to their own responses, and pass held contexts to
// everything invoked before release. Release functions are safe to call twice.
// The package does not detach cancellation for lock waits: owners do that for
// writes, while read-only callers retain cancellable waits.
package lockx
