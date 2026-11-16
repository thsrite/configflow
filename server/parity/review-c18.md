# C18 adversarial review

Scope: Mihomo config generation and artifact download lists in
`backend/converters/mihomo.py`; Mihomo base-form validation, takeover mode and
Agent context in `backend/common/mihomo_base_settings.py`. Go implementations:
`mihomo_generate.go`, `mihomo_downloads.go`, `base_settings.go`, shared
`mihomo_generate_py.go` / `mihomo_generate_env.go` and existing collaborator APIs.
Public APIs stay unchanged. Existing dynamic document/exception emulation is
explicitly assigned to S-convert after the N8 design, per the coordinator.

## Findings in progress

- **High:** YAML parser diagnostics include source excerpts. Both Python and Go
  copied the whole diagnostic into ERROR logs when custom base YAML was invalid,
  exposing secrets or credential-bearing URLs on the offending line. This is
  enabled even at the default INFO log threshold. Go now returns the same
  `CustomConfigYAMLError` without logging it twice. The original failure status,
  diagnostic to the caller, abort and absence of a generated output are kept.
  `TestMihomoReviewYAMLFailureLogOmitsConfiguration` failed on the original code
  for both a synthetic controller secret and a synthetic DNS URL with a token.
  Python was not changed; production logs were not accessed.
- **Medium:** an empty legacy library URL represented by false, zero, an empty
  list or an empty object was stringified into a nonempty source URL. That
  incorrectly forced text instead of inferring YAML from the rule's own fallback
  URL. Go now treats empty values as absent before formatting. Generation and
  ruleset downloads retain the same accepted inputs and produce the same format
  and extension as Python. `TestMihomoReviewEmptyLibraryURLUsesRuleFormat` failed
  for those four values before fixing; null/empty-string were already correct.
  Python independently confirmed all six variants yield `yaml` and
  `./ruleset/Rules.yaml`.

## Acceptance in progress

Baseline `.32` `rt -- server/scripts/proddiff.sh`: exit 0, four self-checks PASS,
`coverage_regressions=0`, `new=0`, `stale=0`, `baseline: new=0 stale=0`; five
existing unrelated global differences. Successful Mihomo outputs: 2 main configs,
1 Agent config, 4 subscriptions, 2 provider download lists and 2 rule downloads.
Production branch trace and independent synthetic boundary matrix are next.
