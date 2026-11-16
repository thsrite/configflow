# Production snapshot differential

Scope: test-only tools/scripts, safe baseline and documentation; no business changes, no subagents.
Implementation uses Python under tools/proddiff because the reference environment already provides
SQLite, JWT, YAML and HTTP tooling; builds the real Go command with ephemeral overlays.

Initial implementation: loopback-only user/network/mount namespace; read-only bind mount of source;
two disposable copies; normal test-account login via existing env; named Python loop suppression and
Go lifecycle.Start overlay; registered Go route manifest; structural JSON/YAML/ZIP and text comparisons;
status mutation self-check; exact normalization list; schema-only baseline.

Initial remote command `rt -- server/scripts/proddiff.sh`: Go command built; isolation/source integrity checks passed; self-check PASS; 156 GET templates, 191 total selected templates, 50 implemented templates, 27 unique differences (expected exit 1 before baseline).

Next: harden coverage/comparison, record/recheck baseline, document findings,
rebase go and verify clean. No tests/builds run on Mac.
