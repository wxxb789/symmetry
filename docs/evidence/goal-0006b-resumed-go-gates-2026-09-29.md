# Goal 0006b resumed Go verification

Date: 2026-09-29. Branch: `codex/goal-0006b-crash-safe-process-containment`. HEAD: `d14debbf6ae3ded09e5013ef90952d347c01474d`.

## Evidence boundary

The source tree was dirty. No commit was created, and no result certifies installed native containment or Goal completion. Both full-suite App binaries started before the later guard-only capacity test edit; the starting fixture SHA-256 was `2f1dd2b21f3ce3c0db0bea4dd0412009856c8b06acf5934dcc4583cf721ac774`. The final fixture hash is `4fbb4d5b0dfb7f28e1e515fae80aa7d03d38a9deefed3ae9b416396b93122a77`; that delta has separate focused normal/race, fault-injection, formatting, static-analysis and independent-review evidence below. Do not label these runs a frozen exact-commit matrix.

## Full daemon suites

Each command ran through `wsl.exe -d symmetry-race --exec /bin/sh -lc`, with working directory `/mnt/q/repos/symmetry/daemon` and `PATH=/opt/go/bin:/usr/bin:/bin`.

```sh
/opt/go/bin/go test -p 4 -count=1 -timeout 1200s -json ./...
/opt/go/bin/go test -p 4 -race -count=1 -timeout 1200s -json ./...
```

Both exited 0 with 3,984 test pass events and 95 test skip events. Counts include parent and table-subtest events, not 3,984 distinct test functions. All 23 package records settled: 21 passed and two had no test files. There were no fail events, malformed JSON lines or stderr. Full captured JSON stream SHA-256: normal `4385ccd2363e3ce2bf48c1891848dbfb5fd06b3383c29e04c2faf1ddffc68127`; race `8f4223f760e3c38d43e0306a27cd6d8147d73affe4376369c8b3549b1a6d0b52`. Raw streams stayed in the session; this report retains only bounded test metadata.

| Package | Normal | Race |
|---|---|---|
| `cmd/symmetry-daemon` | pass (0.201s) | pass (1.328s) |
| `cmd/symmetry-linux-clone-bootstrap` | skip (0s) | skip (0s) |
| `cmd/symmetry-linux-launcher` | pass (0.469s) | pass (1.115s) |
| `e2e` | pass (0.267s) | pass (1.296s) |
| `e2e/preauthority-sentinel` | skip (0s) | skip (0s) |
| `internal/authority` | pass (0.018s) | pass (1.08s) |
| `internal/config` | pass (0.448s) | pass (1.552s) |
| `internal/contracts` | pass (0.703s) | pass (2.723s) |
| `internal/execution` | pass (2.111s) | pass (22.391s) |
| `internal/harness` | pass (2.768s) | pass (7.896s) |
| `internal/harness/claude` | pass (0.077s) | pass (1.173s) |
| `internal/control` | pass (10.477s) | pass (9.934s) |
| `internal/harness/opencode` | pass (2.883s) | pass (3.379s) |
| `internal/harness/pi` | pass (2.755s) | pass (6.489s) |
| `internal/notification` | pass (0.812s) | pass (1.661s) |
| `cmd/symmetry-fake-agent` | pass (25.244s) | pass (15.119s) |
| `internal/harness/codex` | pass (14.177s) | pass (21.017s) |
| `internal/protocol` | pass (4.05s) | pass (7.343s) |
| `internal/platform` | pass (26.944s) | pass (50.343s) |
| `native/linux-gate/protocol` | pass (0.052s) | pass (1.124s) |
| `internal/workspace` | pass (50.183s) | pass (47.608s) |
| `internal/state` | pass (88.552s) | pass (144.676s) |
| `internal/app` | pass (240.274s) | pass (576.334s) |

## Final capacity-test delta

The resumed fixture first passed the selected 14 callback/admission/cleanup/capacity scenarios normally (9.598 s) and with race (42.340 s). Its fake session now suppresses TaskResult while WaitTurn stays live, preserving the distinction between RELEASE and task completion. Both cleanup replay matrices remain.

Independent review then found raw event waits that could hang until the package timeout. The final patch adds local 10-second failure guards and bounded cancellation/join. These guards are failure deadlines, never success-by-delay conditions.

Fault injection used a temporary Go overlay of only this test file, replacing the first session's `turnEntered := firstTurnEntered` with a separate buffered channel. Before the guard patch, the healthy capacity subtest with `-timeout 15s` exited 1 with `panic: test timed out after 15s` at the raw first-turn receive. After the guard patch, the same mutation with `-timeout 45s` exited 1 in 11.192 s with `first capacity-two Run did not enter its live turn after RELEASE`, without package panic or worker-cleanup failure. The temporary overlay and copied source were removed after exact ownership checks. This is an expected red result proving detection and teardown, not a green native witness.

Final unmutated commands, in the same WSL directory/environment:

```sh
/opt/go/bin/go test -p 2 -count=1 -timeout 180s -run '^TestLinuxV2CapacityTwoWaitsForReleaseThenRechecksOwnerState$' ./internal/app
/opt/go/bin/go test -p 2 -race -count=1 -timeout 180s -run '^TestLinuxV2CapacityTwoWaitsForReleaseThenRechecksOwnerState$' ./internal/app
/opt/go/bin/go vet -p 2 ./internal/app
```

All exited 0. The three capacity scenarios passed in 3.122 s normally and 8.088 s with race; vet emitted no output. Windows `gofmt -l Q:/repos/symmetry/daemon/internal/app/linux_owner_v2_app_test.go` exited 0 with no paths. `git diff --check` exited 0. gopls was unavailable, so no language-server pass is claimed. The [independent resumed review](goal-0006b-resumed-test-review-2026-09-29.md) matches the final fixture hash and resolves its blocking finding. Its earlier, separate timer-identity evidence limitation remains nonblocking.

## Explicit skipped test events

These 95 race-run skips are not passes. The normal run also had 95 skips. Reasons include opt-in services/native credentials, installed distinct-UID boundaries, platform-specific tests, root-host limitations and internal subprocess entrypoints. No missing prerequisite was weakened or automatically provisioned.

| Package | Test | Observed reason |
|---|---|---|
| `cmd/symmetry-linux-launcher` | `TestCloneCleanupRejectsCrossMountEntry` | main_test.go:224: cross-mount cleanup proof requires explicit opt-in on a disposable root test host |
| `cmd/symmetry-linux-launcher` | `TestInstalledLinuxOwnerCleanupRejectsBCallerScratch` | main_test.go:264: requires an operator-installed root:A cleanup wrapper |
| `cmd/symmetry-linux-launcher` | `TestInstalledLinuxOwnerCleanupLockSerializesWorkersScratch` | main_test.go:294: cleanup lock proof requires the explicit installed-owner scratch environment as root |
| `e2e` | `TestCoreDaemonWorkflows` | core_test.go:38: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestJSONValuesRoundTripThroughControl` | core_test.go:95: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestPollingFallbackDispatchesWithoutNotifications` | core_test.go:113: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestTaskInputDistinguishesOmissionFromEmptyObject` | core_test.go:130: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestGitWorktreeCleanupRemovesOwnedArtifacts` | core_test.go:163: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestTerminalTransitionRetriesAfterLeaseExpiry` | core_test.go:197: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestStaleRuntimeEpochCannotOverwriteNewGeneration` | core_test.go:267: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestDaemonReregistersAfterRestart` | core_test.go:351: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestDaemonReconnectReclaimsExpiredRunWithNewGeneration` | core_test.go:389: set SYMMETRY_E2E=1 to run live control-plane tests |
| `e2e` | `TestDaemonSurvivesControlRestart` | core_test.go:488: set SYMMETRY_E2E_RESTART_MARKER to run the external control restart scenario |
| `e2e` | `TestRealCodingAgentSmoke` | core_test.go:527: set SYMMETRY_E2E_REAL_AGENT=1 to run the configured Codex smoke test |
| `e2e` | `TestProductionDaemonCrashResponseLossAndReconnect` | production_daemon_crash_witness_test.go:40: set SYMMETRY_E2E_DAEMON_CRASH_WITNESS=1 to run the production daemon crash witness |
| `e2e` | `TestProductionLinuxContainmentNegativeWitness` | production_linux_containment_negative_witness_test.go:143: set SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_NEGATIVE_WITNESS=1 to run the Linux negative containment witness |
| `e2e` | `TestProductionLinuxContainmentReplayWitness` | production_linux_containment_replay_witness_test.go:44: set SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_REPLAY_WITNESS=1 to run the Linux production containment replay witness |
| `e2e` | `TestProductionLinuxContainmentWitnessSpine` | production_linux_containment_witness_test.go:59: set SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS=1 to run the Linux production containment witness |
| `e2e` | `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` | production_linux_v2_cleanup_witness_test.go:173: set SYMMETRY_PRODUCTION_LINUX_V2_SYNTHETIC_GOAL_CLEANUP_ROOT to run the installed synthetic-goal cleanup witness |
| `e2e` | `TestProductionLinuxV2HelperExitAfterReleaseBeforeNativeRegister` | production_linux_v2_helper_exit_witness_test.go:98: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the installed helper release-window crash witness |
| `e2e` | `TestProductionLinuxV2PiNativeProviderAction` | production_linux_v2_native_witness_test.go:74: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 owner witness |
| `e2e` | `TestProductionLinuxV2OpenCodeNativeHealth` | production_linux_v2_native_witness_test.go:86: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 owner witness |
| `e2e` | `TestProductionLinuxV2HelperSIGKILLAnchorReapsDetachedChild` | production_linux_v2_native_witness_test.go:99: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 owner witness |
| `e2e` | `TestProductionLinuxV2DaemonSIGKILLHelperSurvivesDetachedChild` | production_linux_v2_native_witness_test.go:111: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 owner witness |
| `e2e` | `TestProductionLinuxV2WSL2DenialWitness` | production_linux_v2_native_witness_test.go:125: set SYMMETRY_PRODUCTION_LINUX_V2_WSL2_DENIAL_WITNESS=1 to run the production WSL2 denial witness |
| `e2e` | `TestProductionLinuxV2StageCrashReplayMatrix` | production_linux_v2_native_witness_test.go:139: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 stage-replay matrix |
| `e2e` | `TestProductionLinuxV2AckLossReplayMatrix` | production_linux_v2_native_witness_test.go:161: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 ACK-loss replay matrix |
| `e2e` | `TestProductionLinuxV2HelperResponseLossReplayMatrix` | production_linux_v2_native_witness_test.go:181: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production helper response-loss replay witnesses |
| `e2e` | `TestProductionLinuxV2StaleOwnerRequestDenials` | production_linux_v2_native_witness_test.go:205: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 stale-request denials |
| `e2e` | `TestProductionLinuxV2UnresolvedJournalBlocksFreshRun` | production_linux_v2_native_witness_test.go:297: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 unresolved-journal guard witness |
| `e2e` | `TestProductionLinuxV2MigrationDenialRetainsJournal` | production_linux_v2_native_witness_test.go:564: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production Linux v2 migration denial witness |
| `e2e` | `TestProductionLinuxV2StaleLeafIdentityRetainsJournal` | production_linux_v2_native_witness_test.go:578: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the stale-leaf denial witness |
| `e2e` | `TestProductionLinuxV2LiteralPIDReuseAfterStopReceipt` | production_linux_v2_native_witness_test.go:592: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the production literal PID-reuse witness |
| `e2e` | `TestProductionLinuxV2StopReceiptPersistenceFailureRetainsJournal` | production_linux_v2_native_witness_test.go:606: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run the stop-receipt persistence failure witness |
| `e2e` | `TestProductionLinuxV2AlternateCgroupMountRefusal` | production_linux_v2_native_witness_test.go:620: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to run alternate cgroup mount refusal |
| `e2e` | `TestProductionLinuxV2UnprovisionedHostRefusal` | production_linux_v2_native_witness_test.go:634: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_UNPROVISIONED_WITNESS=1 in an uninstalled guest to run the refusal witness |
| `e2e` | `TestProductionLinuxV2HookMarkerReaderRejectsHostileEntries` | production_linux_v2_native_witness_test.go:771: set SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 to test B-writable hook marker safety |
| `e2e` | `TestProductionLinuxV2LegacyV1ReplayNoV2Receipt` | production_linux_v2_native_witness_test.go:884: set SYMMETRY_PRODUCTION_LINUX_V2_LEGACY_V1_REPLAY_WITNESS=1 to run the production legacy-v1 replay witness |
| `e2e` | `TestProductionLinuxV2RootlessComposeNativeWorkItemRefusal` | production_linux_v2_rootless_compose_witness_test.go:137: set SYMMETRY_PRODUCTION_LINUX_V2_ROOTLESS_COMPOSE_WITNESS=1 to run the rootless Compose native WorkItem witnesses |
| `internal/harness` | `TestNativeClaudeCandidateStructuredOutput` | claude_candidate_native_test.go:32: set SYMMETRY_CLAUDE_NATIVE_SMOKE=1 to run the native Claude structured-output smoke |
| `internal/control` | `TestGoalSessionReceiptHTTP` | goal_receipt_e2e_test.go:113: set SYMMETRY_GOAL_RECEIPT_E2E=1 to run the private receipt protocol fixture |
| `internal/harness/codex` | `TestNativeTransportOpenAndClose` | adapter_native_test.go:35: set SYMMETRY_CODEX_NATIVE_SMOKE=1 to run the native Codex transport smoke |
| `internal/harness/opencode` | `TestNativeOpenCodeTerminalCandidateWitness` | adapter_native_terminal_candidate_test.go:86: set SYMMETRY_OPENCODE_NATIVE_TERMINAL_CANDIDATE=1 to run the pinned OpenCode terminal candidate witness |
| `internal/harness/opencode` | `TestNativeTransportOpenAndClose` | adapter_native_test.go:32: set SYMMETRY_OPENCODE_NATIVE_SMOKE=1 to run the native OpenCode transport smoke |
| `internal/harness/opencode` | `TestNativeSyntheticGatewayToolIntegration` | adapter_native_work_test.go:70: set SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1 to run the synthetic OpenCode tool integration |
| `internal/harness/opencode` | `TestNativePromptAdmissionReplaysAfterPost` | adapter_native_work_test.go:251: set SYMMETRY_OPENCODE_NATIVE_ADMISSION_REPLAY=1 to run the native OpenCode admission replay |
| `internal/harness/opencode` | `TestNativeResumeEventCapture` | adapter_native_work_test.go:400: set SYMMETRY_OPENCODE_NATIVE_RESUME_CAPTURE=1 to run the native OpenCode Resume:true event capture scaffold |
| `internal/harness/opencode` | `TestNativeRepositoryTask` | adapter_native_work_test.go:1474: set SYMMETRY_OPENCODE_NATIVE_REPOSITORY_TASK=1 to run the real native OpenCode repository task |
| `internal/harness/opencode` | `TestOpenCodeQualifiedStartNamespacePeerDenials` | peer_linux_test.go:333: requires the explicitly qualified Linux v2 peer inspection host gate |
| `internal/harness/opencode` | `TestOpenCodeQualifiedNonDumpablePeerDeniedBeforeHTTP` | peer_linux_test.go:383: requires the explicitly qualified Linux v2 peer inspection host gate |
| `internal/harness/opencode` | `TestLegacyWindowsPeerVerifierAllowsOwnedHTTPConnection` | peer_test.go:19: Linux exact peer ownership is exercised through the launch-retained v2 owner test |
| `internal/harness/opencode` | `TestLegacyWindowsPeerVerifierRejectsWrongIdentityBeforeWriting` | peer_test.go:48: Linux exact peer rejection is exercised through launch-retained v2 owner tests |
| `internal/harness/pi` | `TestNativeRepositoryTaskRetainedResume` | adapter_native_resume_test.go:36: set SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1 to run the native Pi repository task test |
| `internal/harness/pi` | `TestNativeRPCOpenAndClose` | adapter_native_test.go:32: set SYMMETRY_PI_NATIVE_SMOKE=1 to run the native Pi RPC smoke |
| `internal/harness/pi` | `TestNativeRepositoryTask` | adapter_native_work_test.go:66: set SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1 to run the native Pi repository task test |
| `internal/harness/pi` | `TestNativeCancellationDrainsInFlightLoopbackRequest` | adapter_native_work_test.go:275: set SYMMETRY_PI_NATIVE_CANCELLATION=1 to run native Pi cancellation evidence |
| `internal/harness/pi` | `TestNativeRepositoryTaskWindowsEnvironmentPreservesExecutableResolution` | adapter_native_work_test.go:1033: Windows executable environment only |
| `internal/harness/pi` | `TestNativeRepositoryTaskWindowsEnvironmentDerivesMiseDataDirectory` | adapter_native_work_test.go:1066: Windows executable environment only |
| `internal/harness/pi` | `TestProviderBridgeQualifiedNonDumpablePeerFailsClosedBeforeProviderAction` | provider_bridge_peer_linux_test.go:164: requires the explicitly qualified Linux v2 peer inspection host gate |
| `internal/harness/pi` | `TestWindowsLegacyProviderBridgeRequiresExactPeerIdentity` | provider_bridge_peer_test.go:16: legacy v1 peer binding is retained for Windows only; Linux uses the v2 owner test |
| `internal/harness/pi` | `TestWindowsLegacyProviderBridgeRejectsDifferentProcessIdentity` | provider_bridge_peer_test.go:62: legacy v1 peer binding is retained for Windows only; Linux uses the v2 owner test |
| `internal/harness/pi` | `TestNativePiProviderBridgeAction` | provider_extension_native_test.go:32: set SYMMETRY_PI_PROVIDER_ACTION_SMOKE=1 to run the native provider action smoke |
| `internal/harness/pi` | `TestMaterializeProviderBridgeExtensionForShareRequiresProvisionedSafePath` | provider_extension_test.go:113: host cannot provision a setgid provider extension share |
| `internal/harness/pi` | `TestMaterializeProviderBridgeExtensionForShareProducesAOwnedBReadableImmutableFile` | provider_extension_test.go:165: host cannot provision a setgid provider extension share |
| `internal/harness/pi` | `TestMaterializeProviderBridgeExtensionForShareRejectsSymlinkAndDigestConflict/symlink` | provider_extension_test.go:213: host cannot provision a setgid provider extension share |
| `internal/harness/pi` | `TestMaterializeProviderBridgeExtensionForShareRejectsSymlinkAndDigestConflict/directory_symlink` | provider_extension_test.go:228: host cannot provision a setgid provider extension share |
| `internal/harness/pi` | `TestMaterializeProviderBridgeExtensionForShareRejectsSymlinkAndDigestConflict/conflicting_content` | provider_extension_test.go:240: host cannot provision a setgid provider extension share |
| `internal/harness/pi` | `TestLegacyProviderBridgeExtensionMaterializationRemainsAvailableOnWindows` | provider_extension_test.go:289: legacy A-only path compatibility is retained for Windows |
| `internal/harness/pi` | `TestAdapterBindsAndRevokesDaemonOwnedProviderBridge` | provider_extension_test.go:331: fake process binding covers only the legacy boundary; Linux binding requires a launch-retained v2 owner |
| `internal/harness/pi` | `TestAdapterProviderBridgeBindFailureReturnsOwnedSessionForCleanup` | provider_extension_test.go:389: fake process binding covers only the legacy boundary; Linux binding requires a launch-retained v2 owner |
| `internal/harness/pi` | `TestGeneratedProviderBridgeExtensionLoadsInNativePi` | provider_extension_test.go:506: set SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1 to load the generated extension in native Pi |
| `internal/harness/pi` | `TestNativePiProviderBridgeLifecycle` | provider_extension_test.go:542: set SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1 to run the native provider bridge smoke |
| `internal/workspace` | `TestLinuxCloneRejectsStaleIntentAndInaccessibleSeed` | linux_clone_linux_test.go:215: root can bypass directory permission denial |
| `internal/platform` | `TestLinuxOwnerAnchorPeerCredentialsAndManagerIdentityAreExact` | linux_owner_anchor_linux_test.go:389: exact unprivileged A UID/GID checks require an unprivileged test process |
| `internal/platform` | `TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt` | linux_owner_helper_v2_linux_test.go:89: requires the installed unprivileged owner config: open fixed Linux owner config directory "/etc/symmetry": no such file or directory |
| `internal/workspace` | `TestLinuxCloneAOnlyImportIgnoresBConfigAndPromotionRefReplays` | linux_clone_linux_test.go:720: Git fsmonitor command test is not meaningful as root |
| `native/linux-gate/protocol` | `TestInstalledLinuxOwnerCloneCleanupScratch` | clone_cleanup_linux_test.go:60: requires a root-preprovisioned B-owned cleanup scratch clone and installed setuid wrapper |
| `native/linux-gate/protocol` | `TestInstalledLinuxOwnerCloneCleanupParentDeathScratch` | clone_cleanup_linux_test.go:183: requires a root-preprovisioned parent-death cleanup target on a disposable Linux host |
| `native/linux-gate/protocol` | `TestLinuxOwnerCleanupParentDeathWorker` | clone_cleanup_linux_test.go:274: launched only by the installed parent-death cleanup scratch test |
| `native/linux-gate/protocol` | `TestInstalledLinuxOwnerCloneCheckoutScratch` | clone_integration_linux_test.go:41: requires an operator-created, explicitly named clone scratch leaf and capability-qualified installed A/B launcher |
| `native/linux-gate/protocol` | `TestLinuxOwnerCloneNativeGitHelper` | clone_integration_linux_test.go:314: internal post-checkout B native clone witness |
| `native/linux-gate/protocol` | `TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD` | launcher_integration_linux_test.go:106: requires an operator-created scratch cgroup leaf and installed A/B gate |
| `native/linux-gate/protocol` | `TestInstalledLinuxOwnerBoundaryScratch` | launcher_integration_linux_test.go:289: requires an operator-created, explicitly named scratch cgroup leaf and installed test launcher |
| `native/linux-gate/protocol` | `TestLinuxOwnerGateOwnerDeathHelper` | launcher_integration_linux_test.go:730: internal subprocess witness |
| `native/linux-gate/protocol` | `TestInstalledLinuxOwnerUnsupportedPeerHostRefusesNative` | launcher_integration_linux_test.go:785: requires an operator-created scratch cgroup leaf and installed A/B launcher |
| `native/linux-gate/protocol` | `TestLinuxOwnerNativeOwnerDeathScratch` | launcher_integration_linux_test.go:994: requires an operator-created scratch cgroup leaf and installed owner launcher |
| `native/linux-gate/protocol` | `TestLinuxOwnerNativeOwnerDeathHelper` | launcher_integration_linux_test.go:1129: internal post-release owner-death subprocess |
| `internal/app` | `TestPiResumeAdmissionReusesRetainedNativeSessionAndWorkspace` | harness_test.go:1047: Linux v2 retained-session resume requires a separately fenced B-clone reuse grant |
| `internal/app` | `TestPiResumeRejectedBeforeNativeStartReleasesExactNewBinding` | harness_test.go:1503: Linux v2 retained-session resume requires a separately fenced B-clone reuse grant |
| `internal/app` | `TestPiResumeUnknownStartRetainsReadyAttachmentBarrier` | harness_test.go:1549: Linux v2 retained-session resume requires a separately fenced B-clone reuse grant |
| `internal/app` | `TestPiResumeAttachTimeoutAfterKnownCloseRepairsThroughReadbackBarrier` | harness_test.go:1592: Linux v2 retained-session resume requires a separately fenced B-clone reuse grant |
| `internal/app` | `TestPiResumeDefinitiveAttachRejectionAfterKnownCloseQuarantinesSession` | harness_test.go:1648: Linux v2 retained-session resume requires a separately fenced B-clone reuse grant |
| `internal/app` | `TestLegacyOwnerDrainUsesExclusiveStoreAndDoesNotWriteJournals` | linux_owner_preflight_linux_test.go:148: the daemon owner preflight intentionally rejects root |
| `internal/app` | `TestLegacyOwnerDrainFailsWhenStoreLockIsHeld` | linux_owner_preflight_linux_test.go:229: the daemon owner preflight intentionally rejects root |
| `internal/app` | `TestPreparePiProviderBridgeRunsBoundPeerAndPersistsAction` | pi_provider_bridge_test.go:591: provider extension test needs a non-root fixed-B primary group |

