# gate 环境变量覆盖审查（2026-11-20，基线 go@1ccf31fb5）

范围：server module 的默认 gate、全部测试中 CONFIGFLOW_DIFF / CONFIGFLOW_DIFF_PYTHON /
CONFIGFLOW_PY / CONFIGFLOW_REPO / CONFIGFLOW_YAML_PYTHON 引用、PYREF_* 和真实 Python 路径，
以及此前本文件登记的其他 opt-in。这里只改测试工具，不修产品代码，不 push。
“旧阶段”指本分支修改前的默认 gate；不能据此推断人工验收历史。

## 结论与当前接线

- 旧 core 的 `go test -race ./...` 不开启 DIFF/PY/REPO。旧 difftest 只选 `/difftest`，
  旧 synthetic-live 白名单只有 6 包、21 个顶层测试及 2 个 Python 子例。
- 当前发现 **24 个 difftest 包、197 个必跑顶层测试**，以及 **15 个普通包、42 个必跑顶层测试**
  （另加原有 2 个 Python 子例）。普通包新增 21 个：14 个 DIFF 缺口、3 个 repo Python 缺口、
  3 个原本 core 已执行的 YAML Python 测试、1 个原本 core 已执行的 testgolden 父自检。
- `go run ./tools/gatediscover` 用 `go list -json ./...` 的当前启用文件及 AST 自动发现，
  对 helper 的跨文件调用者做传递闭包；包级 env 引用保守选全包测试。新增包/测试无须编辑白名单。
  包含相关 env 引用的测试包若未进入 diff/live 集合，Go 发现器和 Python runner 均直接失败。
- diff 与 live 通过同一个 runner 预检并设置绝对 PY/REPO/DIFF_PYTHON/YAML_PYTHON、DIFF=1，
  禁止 bytecode；unset SNAPSHOT、PYREF_MODE、PYREF_ACCEPT、PYREF_INPUT_FINGERPRINT。
  diff 仍跑整包（非 race）；普通包自动发现的测试在 compare 的 synthetic live 中带 race。
- 两组分别保存 `*-discovery.json`（源码与排除理由）、`*-manifest.json`（必跑项）、
  `*.json`（Go 原始事件）、`*-results.json`（逐测试终态）。每个必跑项必须有 run+pass，
  普通测试及子例出现 skip/fail 都失败；仅 difftest 的无效 fuzz 输入允许 Skip。
- 保留 repo/store Python 专有子例和 WebDAV 实际 oracle 的 strace 证据，避免静默 return 冒充执行。
  compare 即使前一组失败也继续收集另外两组结果，最终仍返回失败。

## 普通包逐测试盘点

下表包名均相对 `internal/`，路径可直接在该目录定位。新阶段全部为 compare/synthetic-live。
“Python 分支未执行”包括显式 skip 和 Go 部分通过后 return，不等价于整个测试原来未运行。

| 包 | 测试 | 旧阶段实际覆盖 | 本次结果 |
|---|---|---|---|
| `agents` | `TestLocalWallZoneTransitionsAgainstPython` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `agents` | `TestProxyDifferentialAgainstRequests` | compare/synthetic-live 已覆盖 | PASS |
| `agents/dnsha` | `TestManualPairRulesForcesUnchangedLibraryMaterialization` | core 仅 Go 部分；DIFF Python 分支未执行 | PASS |
| `agents/dnsha` | `TestValidationAgainstPython` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `agents/ipv6` | `TestAgentTransportAgainstPython` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `agents/ipv6` | `TestConfigurationAndTargetsPython` | core 仅 Go 部分；DIFF Python 分支未执行 | PASS |
| `agents/ipv6` | `TestTupleEvidenceAgainstPython` | core 仅 Go 部分；DIFF Python 分支未执行 | PASS |
| `agents/rulesync` | `TestIsolationAgainstPython` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `agents/rulesync` | `TestProjectionAgainstPython` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | FAIL |
| `agents/rulesync` | `TestSnapshotAgainstPython` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `api/agents` | `TestA24FlaskBoundaries` | compare/synthetic-live 已覆盖 | PASS |
| `api/agents` | `TestPublicAgentPython` | compare/synthetic-live 已覆盖 | PASS |
| `api/agents` | `TestReviewA24TargetsIgnoreUnrelatedLegacySettings` | compare/synthetic-live 已覆盖 | PASS |
| `api/ipv6` | `TestRoutesAgainstFlask` | core 仅 Go 部分；DIFF Python 分支未执行 | PASS |
| `gateway/installation` | `TestPythonPublicationStateAndGeneratedArtifacts` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `gateway/onboarding` | `TestPythonPreparation` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | FAIL |
| `gateway/onboarding` | `TestRealDNSLoopPreparation` | core 仅 Go 部分；DIFF Python 分支未执行 | PASS |
| `model` | `TestHRPythonWriters` | compare/synthetic-live 已覆盖 | PASS |
| `model` | `TestTypesPythonRoundTrip` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestConfigTemplateMatchesBackend` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestCrossCheckC04Operations` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestCrossCheckFreshInit` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestCrossCheckInterchange` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestCrossCheckLockExcludesPythonAndGo` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestCrossCheckOperations` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestCrossCheckPythonWriterKilledBeforeCommit` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestReviewN85PythonFullStorageDifferential` | core 因缺 DIFF/PY/REPO skip；旧 compare 未纳入 | PASS |
| `repo` | `TestSectionWritesStoredRoundTrip` | core 仅 Go 部分；Python 分支静默 return | PASS |
| `repo` | `TestStoredRoundTrip` | compare/synthetic-live 已覆盖 | PASS |
| `repo` | `TestTypedUpdateStoredRoundTrip` | core 仅 Go 部分；Python 分支静默 return | PASS |
| `repo` | `TestWriteFromAnotherProcessIsSeen` | compare/synthetic-live 已覆盖 | PASS |
| `server` | `TestSetupServerOwnerRegistrationRemovesUnavailable` | core 仅 Go 部分；DIFF Python 分支未执行 | PASS |
| `store` | `TestCrossCheckPython` | compare/synthetic-live 已覆盖 | PASS |
| `store` | `TestCrossCheckPythonConcurrentCommits` | compare/synthetic-live 已覆盖 | PASS |
| `store` | `TestCrossCheckPythonErrors` | compare/synthetic-live 已覆盖 | PASS |
| `store` | `TestCrossCheckPythonOddDocuments` | compare/synthetic-live 已覆盖 | PASS |
| `store` | `TestStoredRoundTrip` | compare/synthetic-live 已覆盖 | PASS |
| `testgolden` | `TestRecordingAndReplayNegativeControls` | core 已覆盖（真实解释器回退 / 父测试自设 PYREF_*） | PASS |
| `webdav` | `TestTLSVerificationMatchesPythonDefault` | compare/synthetic-live 已覆盖 | PASS |
| `yamlx` | `TestNativeAmbiguousStringsReadByPyYAML` | core 已覆盖（真实解释器回退 / 父测试自设 PYREF_*） | PASS |
| `yamlx` | `TestNativeDocumentedPyYAMLDifferences` | core 已覆盖（真实解释器回退 / 父测试自设 PYREF_*） | PASS |
| `yamlx` | `TestNativeSyntheticPyYAML` | core 已覆盖（真实解释器回退 / 父测试自设 PYREF_*） | PASS |

原有 Python 子例：`repo.TestStoredRoundTrip/python-written-rows`、
`store.TestStoredRoundTrip/committed-python-fixture`，旧/新 compare 均要求 run+pass。
`webdav.TestTLSVerificationMatchesPythonDefault` 的顶层 pass 不足以证明 Python 分支，
旧/新 compare 均要求成功执行含 `webdav3.client` / `TLS_REJECTED` 的 oracle。

`api/agents.TestReviewA24TargetsIgnoreUnrelatedLegacySettings` 通过另一个文件中的 helper
间接依赖 DIFF，现由 AST 调用闭包发现。`testgolden.TestGoldenHelperProcess` 的
PYREF_SELFTEST_DIR 缺失时 skip 是正常子进程入口：父测试
`TestRecordingAndReplayNegativeControls` 自行设置 SELFTEST_*、PYREF_MODE、PYREF_ACCEPT、
假解释器 metadata，core 和新 compare 均执行父测试，不能在全局设置 child 开关。
PYREF_MODE 为空表示已验证 metadata 的精确 golden replay，并非 skip；check/record 是单独的
参考刷新/校验机制，compare 不全局强开 record，也不擅自接受 golden 变化。

## difftest 全部必跑清单

旧阶段均为 compare/difftest；新阶段仍为 compare/difftest（与普通包使用相同 Python 环境）。
下面列的是全包顶层必跑清单，包含不需 env 的 Go-only 负控；这些负控 core 也会执行。
DIFF 保护的 live 路径在 core 通常 skip 或不执行，不能用 core 的包级绿色代替此阶段。
发现器只读真实 AST，因此测试源码中的字符串 fixture 不会被误认为真实 Test 函数。

### `agents/dnsfollow/difftest`（2）

- `TestNormalizationRetainsSemanticChanges`
- `TestPythonRoutesAndArtifacts`

### `agents/dnsha/difftest`（1）

- `TestHAHTTPAgainstFlask`

### `api/agents/difftest`（29）

- `TestContentChangesAgainstFlask`
- `TestContentReadsAgainstFlask`
- `TestDifferentialAgainstFlask`
- `TestDockerHADifferentialAgainstFlask`
- `TestDownloadDifferentialAgainstFlask`
- `TestInstallerDifferentialAgainstFlask`
- `TestInstallerFuzzAgainstFlask`
- `TestInvalidUTF8InstallerQueryRemainsShellSafe`
- `TestL1CompactColonDifferentialAgainstFlask`
- `TestL1DownloadErrorFormatsAgainstFlask`
- `TestN73GuideComposeComparisonKeepsEveryOtherValue`
- `TestN810APIOriginalBinding`
- `TestN810ArtifactComparisonKeepsShellBytesAndJSONValues`
- `TestN810HTTPPolicyRowsCannotBeSkipped`
- `TestN810InstallerHTTPBindingRejectsChangedInput`
- `TestN810IntegerComparisonCannotHideUnrelatedFailures`
- `TestN810MetricsHTTPPolicyBinding`
- `TestN810UpgradesHTTPPolicyNegativeControl`
- `TestNativeComposeHTTPComparison`
- `TestNativeWriterHTTPProvenance`
- `TestNotificationStoreComparisonPreservesDisplayAndData`
- `TestOxidnsDifferentialAgainstFlask`
- `TestPanelDifferentialAgainstFlask`
- `TestPublishHTTPAgainstFlask`
- `TestPublishLegacyProxyAgainstFlask`
- `TestRankingCollectorDifferentialAgainstFlask`
- `TestRuleSyncHTTPAgainstFlask`
- `TestUpgradesDifferentialAgainstFlask`
- `TestUpgradesProxyDifferentialAgainstFlask`

### `api/aggregations/difftest`（13）

- `TestAutomaticHEADDoesNotMutateCatalog`
- `TestCachedCountsAndFiltering`
- `TestCatalogAndReorder`
- `TestCorruptNodeCacheCannotProducePartialProvider`
- `TestDryGeneratorCorpus`
- `TestGlobalRenderedBundle`
- `TestInvalidAggregateRegex`
- `TestNormalizationDoesNotHidePayloadChanges`
- `TestProviderAndPreview`
- `TestProviderScalarAndUnknownFieldRoundTrip`
- `TestRejectedSourceDoesNotPublish`
- `TestReviewN88dProviderByteSamples`
- `TestSharedCatalogSelectionAndProtection`

### `api/config/difftest`（19）

- `TestArtifactsApprovedRequestBoundaries`
- `TestArtifactsFlaskErrorClasses`
- `TestArtifactsFlaskExpiredAndForeignOwnership`
- `TestArtifactsFlaskGenerationFailureDoesNotSavePreview`
- `TestArtifactsFlaskSuccessfulGenerationAndReads`
- `TestC34BaseURLInputAlternatives`
- `TestC34ExpectedDifferenceCustomJSONShapes`
- `TestC34ExpectedDifferenceHEADReadOnly`
- `TestC34ExplicitBindingDifferences`
- `TestC34GenerationAcceptedJSONShapes`
- `TestC34GenerationAndLiveConfig`
- `TestC34GlobalExports`
- `TestC34ObjectFieldValues`
- `TestC34ProfilesAndCustomConfiguration`
- `TestReviewC34ClientImportDryRunShapes`
- `TestReviewC34CustomEmptyBodies`
- `TestReviewC34InvalidCloneSourceCannotCreateProfile`
- `TestReviewC34ProfileEmptyAlternatives`
- `TestSetupFlaskHTTPStateAndAuthentication`

### `api/gatewayonboarding/difftest`（3）

- `TestFlaskAdoptionRevisionCondition`
- `TestFlaskHEADExpectedReadOnlyDifference`
- `TestFlaskHTTP`

### `api/linuxgateway/difftest`（10）

- `TestA20CapturedProfileHeaderSemantics`
- `TestA20DraftPythonCredentialLeaksAreFixed`
- `TestA20FlaskHTTPAndStorage`
- `TestA20ForwardingStatesAndVersions`
- `TestA20HostFlask`
- `TestA20LegacyEditorRoundTrip`
- `TestA20NormalizationKeepsUserFields`
- `TestA20PythonHEADForwardingWriteIsFixed`
- `TestA20RealWizardFixtures`
- `TestA20SplitHTTPGuards`

### `api/oxidns/difftest`（8）

- `TestC29EditorWorkflowAndValidation`
- `TestC29ExpectedDifferenceHEADReadOnly`
- `TestC29ExpectedDifferenceNonFinite`
- `TestC29FlaskRoutes`
- `TestReviewC29AdminBoundary`
- `TestReviewC29ExactRequestKeys`
- `TestReviewC29NativeReferenceTargets`
- `TestReviewC29ReaderPolicyHTTP`

### `api/rulelibrary/difftest`（20）

- `TestBothSidesRejectConcurrentDraftSubmissions`
- `TestCacheSelectionCannotUseStringMembership`
- `TestDraftHEADCannotWrite`
- `TestDraftNativeRevisionContract`
- `TestGroupListenerRelinkingHTTPParity`
- `TestLibraryNativeParkingHTTPParity`
- `TestMalformedRouteInputHTTPParity`
- `TestManualCacheUsesGlobalForwardProxy`
- `TestProxyGroupFollowDNSHTTPParity`
- `TestProxyGroupHTTPParity`
- `TestReviewAuthenticationAllowlist`
- `TestReviewDraftRevisionShape`
- `TestReviewNonFiniteBindingUsesMalformedBoundary`
- `TestReviewPreviewSelectionShapes`
- `TestReviewPythonNonFiniteAcceptanceIsExplicit`
- `TestReviewReorderMalformedHTTPParity`
- `TestReviewStrictAndSilentBinding`
- `TestRuleLibraryEditAndChecksHTTPParity`
- `TestRuleLibraryReadCreateHTTPParity`
- `TestUnavailableRefreshKeepsRawCacheAndPreferences`

### `api/rules/difftest`（19）

- `TestAcceptedSourceLineBoundaries`
- `TestComposeRemoteLibraryAtomicValidation`
- `TestComposeSourcesDNSAndErrors`
- `TestCustomNativeRoutingPreservesCommittedMetadata`
- `TestDNSAgentReadModelLegacyNames`
- `TestEveryRouteSuccessAndFailureWithStorage`
- `TestLargePolicyGroupAndDNSLimit`
- `TestMarkedDeliveryDoesNotAlterSource`
- `TestMatchingWildcardQuerySpellings`
- `TestMosDNSGuardAndCleanup`
- `TestPinnedUpstreamChoicesAndLinkedRejection`
- `TestPythonHEADWriteIsFixed`
- `TestRemoteProviderAndLazyMatching`
- `TestReorderCompatibilityFailuresAndNoClobber`
- `TestReviewAuthenticationAndProviderTokens`
- `TestReviewBatchNullValues`
- `TestRouterOfflineMissingCacheMatchesPython`
- `TestSyntaxValidationAcrossWriteEntries`
- `TestTemplateAndNativeDNSWrites`

### `api/subscriptions/difftest`（24）

- `TestAdversarialProviderURLs`
- `TestAlternateFormatFilters`
- `TestApprovedShapePolicyRejectsOtherErrors`
- `TestCachePreviewNeverRefreshes`
- `TestCombinedCrossFeedAndStaging`
- `TestConfigFindingsProviderMetadataAndServedFile`
- `TestDeletionConfirmationSurvivesOnlyUnrelatedWrites`
- `TestDeletionDoesNotReviseUnaffectedOrderGroups`
- `TestDeletionWarnsOnEmptySources`
- `TestDeliverySnapshotSurvivesConcurrentProfileChange`
- `TestEveryGlobalRouteRequiresJWT`
- `TestFetchPreviewAndImport`
- `TestFetchWithNullCacheNodes`
- `TestMalformedManagementBodies`
- `TestManagementWrites`
- `TestMissingAggregationDependencyFailsClosed`
- `TestNodeMutationsProtectChains`
- `TestProfileSelectionIsolation`
- `TestProviderDeliveryGraphs`
- `TestProviderSecurity`
- `TestReviewedDeletion`
- `TestSubscriptionDeletionProtectsSelectedSource`
- `TestTransportFallbackAndFormats`
- `TestUTF8BOMRequestBodies`

### `devicenotes/difftest`（1）

- `TestDeviceNotesBoundary`

### `discovery/difftest`（4）

- `TestApplyNormalizationKeepsEmptyHistoryAndOrderedArrays`
- `TestCFG06DuplicateTargetDifference`
- `TestHTTPAndStorageParity`
- `TestNativeNonFiniteRequestDifferences`

### `gateway/installation/difftest`（1）

- `TestGatewayFlaskHTTPAndDurableState`

### `health/difftest`（2）

- `TestDifferentialLatencyRoutes`
- `TestWorkerJSONComparisonRejectsChangedValues`

### `httpx/difftest`（8）

- `TestBindingComparatorRejectsUnexpectedChanges`
- `TestC06SettingsFlaskStorageBoundary`
- `TestDifferentialAgainstFlask`
- `TestN87JWTBoundaryAgainstFlask`
- `TestPythonAuthenticationMatrixForEveryRegisteredRoute`
- `TestPythonSafeRequestErrorDetails`
- `TestPythonTextRedactionAndStoredSecretFinder`
- `TestReviewC06SettingsMemberTypes`

### `mcp/difftest`（1）

- `TestRealFlaskHTTP`

### `mosdns/difftest`（2）

- `TestDifferentialMosDNSRoutes`
- `TestMosDNSNativeRequestDifferences`

### `provider/difftest`（1）

- `TestSlowProviderParity`

### `refresh/difftest`（2）

- `TestD2FileKeysPreserveContentAndUnknownFiles`
- `TestDifferentialTaskRoutes`

### `subs/difftest`（2）

- `TestChainProviderCaptureGroups`
- `TestPublicRelinkTransactionalHTTPBoundary`

### `substore/difftest`（5）

- `TestDifferentialBundledSubStore`
- `TestDifferentialExplicitRemoteProxy`
- `TestDifferentialExternalSubStore`
- `TestDifferentialReviewLoopbackProxy`
- `TestDifferentialReviewMalformedState`

### `tasks/difftest`（11）

- `TestC07ExpectedHEADReadOnlyDifference`
- `TestC07ExpectedNonFiniteBindingDifference`
- `TestC07ExpectedReadinessDifferenceKeepsUnseenFailures`
- `TestC07FlaskRoutesAndStoredState`
- `TestC07MalformedHistoryRetainsPythonUnavailableStatus`
- `TestC07NotificationReadAndCombinedRedaction`
- `TestN87NativeNonfiniteRequests`
- `TestN87OverviewOverflowAgainstFlask`
- `TestReviewC07FixtureRestoresRunner`
- `TestReviewC07LegacyCountersAndSilentBinding`
- `TestReviewC07UnknownExactKeyAgainstFlask`

### `webdav/difftest`（9）

- `TestApprovedEditorSecurityCorrections`
- `TestAutomaticBackupFollowsCommittedConfig`
- `TestAutomaticChangesCoalesceAndPendingDisableCancels`
- `TestBackupRoutesAgainstFlask`
- `TestConnectionRefusedByDAVMatchesFlask`
- `TestImportedRetentionFallbackMatchesFlask`
- `TestManualRequestOverridesMalformedSavedConnection`
- `TestWebDAVDirectoryVariantsAgainstFlask`
- `TestWebDAVFailuresAndRetentionAgainstFlask`

## 其他 env / 前提与当前阶段

| 测试或开关 | 当前实际阶段 / 未覆盖原因 |
|---|---|
| repo.TestN82ReviewPrivateRepositoryInterchange；model.TestTypesPrivateSnapshot、TestHRSnapshotSchema | 默认 gate 未执行；需 CONFIGFLOW_SNAPSHOT。自动发现记录 3 个精确排除及理由；这不是 synthetic Python 前提，不能造空目录冒充执行。proddiff 不调用它们 |
| repo/store.TestStoredRoundTrip 的 private-snapshot 子例 | 默认 synthetic compare 明确排除；合成和 Python 互通部分必跑。普通 proddiff 不替代此私人数据审计 |
| api/agents.TestRealPreparationProof / CONFIGFLOW_A13_PROOF | 默认 skip；专用 `proddiff.sh --a13-proof` 准备 namespace 和 fixture，非普通 synthetic compare |
| installer.TestL1CompactMountsWithRealCompose、TestL1HAStartupWithRealSocketsAndSupervisor、TestL1RollbackWithRealInitManagers、TestL1ReviewFailureInjectionWithRealInitManagers、TestL1HAVerificationWithRealDNSClients / CONFIGFLOW_L1_CONTAINERS | 默认 skip；独立 Docker/Compose/init 作业，未全局强开 |
| deploy.TestWriteServerArchives / CONFIGFLOW_WRITE_ARCHIVES；go-agent/routes.TestServerArchive / CONFIGFLOW_SERVER_ARCHIVES | 导出入口默认 skip；消费端属于另一个 module。需独立合成归档 conformance 链 |
| installer.TestWriteShellcheckSample / INSTALLER_SAMPLE_DIR | 默认 skip；产物导出入口，需接 shellcheck 作业，不能把只导出当成校验成功 |
| routers 的 TestRouterOSBoundaryCorpus、TestIkuaiBoundaryCorpus 等真实解释器测试（见下） | core 已执行；解释器缺失会 skip。compare 预检固定回退解释器存在，普通 DIFF/PY env 测试则由 manifest 保证执行 |
| installer 的 Python stub helper | core 已执行；依次找 python3/python，缺失会 skip。测试服务器有 python3；不是 DIFF 门开关 |
| CONFIGFLOW_*_HELPER、CONFIGFLOW_REVIEW_N73D_*_CHILD、CONFIGFLOW_C37_RESTART_DIR/JOB、PYREF_SELFTEST_* | 父测试自设子进程环境，core 已执行；不应全局开启 child 分支 |
| CONFIGFLOW_ROOT（oxidns） | TestMain 填默认根路径，core 已执行，无缺 env 缺口 |
| CONFIGFLOW_RECORD_CLIENT_INDEX、CONFIGFLOW_RECORD_N88、N810_RECORD_API_PYTHON、N810_RECORD_INSTALLER_HTTP、N810_RECORD_UPGRADES_PYTHON | 默认继续执行已有 corpus/golden 对照与负控；这些变量用于写入/接受记录，缺失不使测试 skip，门禁不全局开启记录模式 |
| CONFIGFLOW_R07_MUTATE、INSTALLER_MATRIX_FULL | 正常路径 core 执行；前者故意破坏，后者增强诊断，不全局开启 |
| backend/node-probe、backend/agents/go-agent 的 child/CA/DNS env | 不属 server module；各模块另行验收，未宣称被本门禁覆盖 |

固定真实 Python 路径的 routers 测试由以下文件承载，正常 .32/.64 的 core 会跑；缺解释器才 skip：
`routeros_boundary_test.go`、`ikuai_boundary_test.go`、`routerfake_python_test.go`、
`addresslists_test.go`、`natlist_write_test.go`、`natlist_plan_test.go`、`r07_review_boundary_test.go`。
逐测试为：`TestRouterOSBoundaryCorpus`、`TestIkuaiBoundaryCorpus`、
`TestRouterFakePythonScriptHelpers`、`TestAddressListBoundary`、`TestNATListWriteBoundary`、
`TestNATListPlanBoundary`、`TestR07RangeKeysMatchPython`，以及调用 runR07Boundary 的
`TestNATListFailureBoundary`、`TestAddressWriteBoundary`、`TestNATRestoreFaultBoundary`、
`TestNATListRemovalBoundary`。YAML 的 3 个真实 PyYAML 测试已由新 compare 一并自动纳入，
上表列出精确名字。

## 本分支验证

主机 `.32`。`GATE_ONLY=compare ... gate.sh -p 4` 返回 1：

- proddiff：`new=0 stale=0`，self-check PASS。
- synthetic live：42 个顶层测试全部 run，40 PASS、2 FAIL、0 SKIP；另 2 个 Python 专有子例 PASS，
  WebDAV 实际 Python oracle exec 已验证。相对旧白名单新增 21 个顶层测试，19 PASS、2 FAIL。
- difftest：首次运行期间后续 rt 同步误删 JSON，导致 runner 无法读取证据；已恢复仍打开的主日志，
  synthetic live JSON 不受影响，单独重跑 `GATE_ONLY=difftest` 补齐结果（日志改放同步目录之外），
  **24 包、197 个必跑测试全部 run+pass，0 skip**，最后输出 `GATE STAGE PASS: difftest`。
- 工具：Go discovery race 测试/定向 vet、9 项 Python 执行证据检查、15 项门禁路由检查 PASS。
  末次发现器增加同名 helper 防覆盖处理，重新执行工具测试通过，并核对生成的 discovery 与本次
  实际执行清单完全相等。全模块 vet/最终格式检查通过，最后输出 `gofmt clean; go vet ./... PASS`。

失败项及根因初判（未改产品代码）：

| 失败子例 | 观察 | 根因初判 |
|---|---|---|
| rulesync.TestProjectionAgainstPython/oxidns/falsy-enabled/None | Go kind=structure，Python none | OxiDNS typed adapter 的 `enabled(*bool)` 把显式 null 与缺省都视为 true；生成文档纳入本应禁用的规则，RuleSignatures 误判结构变化。已知 W2 范围 |
| rulesync.TestProjectionAgainstPython/oxidns/falsy-enabled/0 | Go kind=structure，Python none | 错型数值 0 解码为 nil 后同样落入默认 true，丢失显式 falsy 状态。已知 W2 范围 |
| rulesync.TestProjectionAgainstPython/oxidns/falsy-enabled/'' | Go kind=structure，Python none | 空字符串解码为 nil 后同样落入默认 true。已知 W2 范围 |
| gateway/onboarding.TestPythonPreparation/null-policy | 两边 ready=false；Go blockers 中策略为 'PROXY'，Python 为 'None'，其余 report 相同 | `rules.ValidatePolicies` 对 ruleset 用 `valueOr(rule.Policy, defaultPolicy)`，将显式 null 当成缺省；Python `rule.get('policy', default)` 保留显式 None，中文错误消息不同。需产品分支裁定/修复 |

新增 PASS 清单：agents.TestLocalWallZoneTransitionsAgainstPython；dnsha 的 2 项；ipv6 的 3 项；
rulesync.TestIsolationAgainstPython、TestSnapshotAgainstPython；api/ipv6.TestRoutesAgainstFlask；
gateway/installation.TestPythonPublicationStateAndGeneratedArtifacts；
gateway/onboarding.TestRealDNSLoopPreparation；repo.TestReviewN85PythonFullStorageDifferential、
TestSectionWritesStoredRoundTrip、TestTypedUpdateStoredRoundTrip；
server.TestSetupServerOwnerRegistrationRemovesUnavailable；testgolden.TestRecordingAndReplayNegativeControls；
yamlx 的 3 项（精确名字见普通包表）。新增 FAIL 为上表两个顶层测试，共 4 个失败子例。

只跑 .32 的 compare、受日志丢失影响的 difftest 重跑和工具定向检查，未跑全量 gate。
最终原始证据目录：测试服务器 `~/rt/go-gate-diff-coverage-20261120/`。

```sh
# 首次执行主体（带 --back 取回 live 原始 JSON/manifest/results）
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'GATE_ONLY=compare GATE_LOGS="$PWD/server/.gate-diff-coverage" server/scripts/gate.sh -p 4'
# 补齐 difftest 的执行主体；保留旧 proddiff 与 live 的有效结果
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'GATE_ONLY=difftest GATE_LOGS="$HOME/rt/go-gate-diff-coverage-20261120/diff-retry" server/scripts/gate.sh -p 4'
```
