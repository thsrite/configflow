// Package rulesource reads rule-set contents from shared caches, inline
// declarations, and remote rule sources. It owns the shared content-reading
// logic used by api/rules, gateway, setup, and routers without forcing domain
// packages to import an API package.
package rulesource
