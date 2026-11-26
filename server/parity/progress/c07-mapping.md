# C07 Python coverage mapping

All names below are source test function names, omitting `test_`. Go files are
under `server/internal/`. Fixtures are synthetic. Device actions remain external.

## backend/test_web_listener.py — 21 contracts

| Python contract | Go acceptance / disposition |
|---|---|
| startup_default_dual_stack_then_saved_port_survives_restart | weblistener.TestSavedPortSurvivesRestartAndUpdateMovesTheListener |
| environment_override_preserves_saved_preference_and_is_read_only | TestEnvironmentOverrideIsReadOnly |
| bad_api_port_does_not_change_deployment | TestBadAndReservedPorts |
| missing_port_is_rejected | TestActiveListenerHTTPAuthenticationMissingPortAndSavedSettings, actual HTTP |
| internal_services_keep_their_ports_even_when_asleep | TestBadAndReservedPorts |
| custom_sub_store_port_is_reserved_at_startup_and_in_web | TestCustomReservedPortsAtStartupAndUpdate, Sub-Store case |
| invalid_environment_is_rejected_before_overwriting_config | TestInvalidEnvironmentIsRejected |
| recovery_environment_works_with_corrupt_saved_file | TestCorruptSavedFileAndRecoveryEnvironment |
| no_ipv6_kernel_emits_only_ipv4_listener | TestIPv4OnlyKernelKeepsOneNativeSocketAcrossUpdate |
| reload_failure_restores_original_config_and_confirms_old_listener | native equivalent TestNativeHealthFailureLeavesOldListenerAndStateUsable: actual failing health response, old token/port still works |
| persistent_state_write_failure_does_not_block_live_rollback | TestPersistenceFailureStillRollsBackLiveSockets: real unwritable state directory |
| nginx_validation_failure_restores_config_without_reload | retired by design: no generated nginx config or nginx -t in native server |
| concurrent_change_is_rejected_without_touching_state | TestConcurrentChangeIsRejected, TestListenerAdmissionPreservesLegacyFilesAndValidationOrder |
| get_and_post_require_authentication | TestActiveListenerHTTPAuthenticationMissingPortAndSavedSettings, actual HTTP 401 for both methods |
| unsupported_deployment_is_read_only | same active HTTP test: nil-manager status and POST 503; TestPortInUseAndUnsupported |
| success_api_returns_active_settings | same active HTTP test: old-socket POST, new-socket GET, saved file agrees |
| custom_backend_port_updates_proxy_and_reserved_ports | native API has no proxy upstream; preserved reservation tested by TestCustomReservedPortsAtStartupAndUpdate, backend case |
| invalid_internal_port_preserves_active_config | TestInvalidInternalEnvironmentAndSharedPortPreserveActiveConfig, both names × seven invalid values, old listener/file unchanged |
| internal_services_cannot_share_a_port | same test, equal internal ports case |
| docker_rejects_backend_address_unreachable_by_nginx | retired by design: native in-process API has no nginx-to-backend hop |
| source_update_preserves_web_port_changes_with_old_image_template | retired by design: no nginx image template; native saved-port restart contract remains above |

18 retained portable contracts (the old seven plus eleven completed); exactly
three retired nginx-only contracts. Header/body HEAD and native Bind belong to
the independent C06 review, not a second implementation in this module.

## backend/test_notifications.py — 16 contracts

| Python contract | Go notifications acceptance |
|---|---|
| read_removes_body_persists_dedupe_and_a_new_failure_notifies_again | TestReadRemovesBodyPersistsDedupeAndANewFailureNotifiesAgain |
| recovery_and_recurrence_without_browser_and_stale_read_is_ignored | TestRecoveryAndRecurrenceWithoutBrowserAndStaleReadIsIgnored |
| removed_and_disabled_sources_clear_notifications_and_dedupe_markers | TestRemovedAndDisabledSourcesClearNotificationsAndDedupeMarkers |
| task_running_or_source_read_failure_never_clears_pending | TestTaskRunningOrSourceReadFailureNeverClearsPending |
| completed_receipt_is_persisted_before_worker_releases_running_flag | TestCompletedReceiptIsPersistedBeforeWorkerReleasesRunningFlag |
| manual_backup_failure_is_actionable_even_without_automatic_backup | TestManualBackupFailureIsActionableEvenWithoutAutomaticBackup |
| corrupt_store_is_not_replaced_by_empty_success | TestCorruptStoreIsNotReplacedByEmptySuccess |
| read_payload_validation | TestReadPayloadValidation; api/notifications.TestReadBindingRejectsMalformedAndWrongShapes; tasks/difftest real Flask malformed/non-object/non-finite cases |
| existing_history_and_read_bodies_are_removed_on_upgrade | TestExistingHistoryAndReadBodiesAreRemovedOnUpgrade |
| old_unread_problem_is_retained_and_stale_read_cannot_clear_a_new_receipt | TestOldUnreadProblemIsRetainedAndStaleReadCannotClearANewReceipt |
| latest_deployment_status_uses_exact_record_and_keeps_failures_while_retrying | TestLatestDeploymentStatusUsesExactRecordAndKeepsFailuresWhileRetrying |
| router_notices_keep_profile_section_freshness_and_unknown_is_not_recovery | TestRouterNoticesKeepProfileSectionFreshnessAndUnknownIsNotRecovery |
| same_router_id_in_different_profiles_does_not_merge | TestSameRouterIDInDifferentProfilesDoesNotMerge |
| confirmed_empty_router_release_clears_previous_failure | C07 notification projection: TestConfirmedRouterReleaseRemovesPreviousFailure; real POST /api/routers/r/apply confirmed-empty planning/device behavior remains the routers release owner, explicitly external |
| router_read_is_not_replayed_by_repeated_checks_and_marker_clears_on_removal | TestRouterReadIsNotReplayedByRepeatedChecksAndMarkerClearsOnRemoval |
| notifications_require_authentication | TestNotificationsRequireAuthentication; real Flask 401 matrix |

Additional C07: api/notifications 503 retains router/agent pending receipts;
HEAD no migration/create/rename; real Runtime transaction callback/waiter/MarkRead
shutdown; existing stale-observation/newer-notice/read races; combined shared
EventMessage sanitization and public JSON/storage equality against Python.

## backend/test_logs_tail.py — seven contracts

| Python contract | api/logs acceptance |
|---|---|
| tail_precedes_filters_and_nonpositive_limits_return_all | TestTailPrecedesFiltersAndNonpositiveLimitsReturnAll |
| tail_default_invalid_limit_and_maximum | TestTailDefaultInvalidLimitAndMaximum |
| tail_counts_blank_lines_ignores_invalid_utf8_and_normalizes_newlines | TestTailCountsBlankLinesIgnoresInvalidUTF8AndNormalizesNewlines |
| tail_reads_full_count_without_reading_file_into_a_list | TestTailReadsFullCountWithoutReadingFileIntoAList |
| tail_missing_or_empty_file | TestTailMissingOrEmptyFile |
| tail_read_error_and_invalid_level_keep_existing_error_response | TestTailReadErrorAndInvalidLevelKeepExistingErrorResponse |
| tail_response_still_redacts_repository_credentials | TestTailResponseStillRedactsRepositoryCredentials |

See exact exported test names in logs_test.go (mapping names retained from the
existing C07 port). Clear/info + missing/error cases also run through the real
Flask/state differential; Runtime clear tests add both disconnect and shutdown.

## Tasks and stats (no dedicated Python stats file)

- backend/routes/tasks.py: real Flask task rows, all three commands' validation,
  interval success/invalid/missing resource and post-write DB/files, nine-route
  authentication matrix; 17 direct _history cases including decimal strings and
  large integers. C15 test_scheduled_tasks.py fetch/cache/scheduler contracts stay
  in refresh; C07 adds dispatch/readiness and interval owner Runtime regressions.
- backend/routes/stats.py: real Flask counts/profile/history JSON + stored samples;
  native tests cover 30-minute sampling, 48-point cap, optional corruption/write
  failure, concurrent requests, HEAD inode/mtime/bytes, and real Runtime checks.
- C35 backup transport/retention differential is reused; C07 adds actual DAV
  stored-configuration run-now admission, complete upload/receipt and lifetime.
- TestC07ExpectedReadinessDifferenceKeepsUnseenFailures,
  TestC07ExpectedHEADReadOnlyDifference and
  TestC07ExpectedNonFiniteBindingDifference are explicit approved differences,
  not broad normalizations. Python's dynamic due-now clock is eliminated by
  fixed persisted receipt times rather than masking next_run_at.
