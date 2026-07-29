# Android Modern Full Panel Progress

Updated: 2026-07-29
Branch: `main`
Remote: `origin/main`

## Current Position

The management Core, route registration, capability dispatch, fallback diagnostic boundary, runtime contracts, and CI gates are implemented in the current working tree. Overall delivery is partially complete: production runtime assets, accepted device smoke, the real-device matrix, Recovery APK, upgrade/recovery proof, and long-stability evidence remain open.

The single release version source is `VERSION.json`: version `0.3.15`, Android version code `30150`. `scripts/version.py` checks derived version fields before release work proceeds.

## Completed Work

### Task 1.4: Lifecycle-Manage Core Workers

- Added `panel/server/mobilecore/runtime_container.go` and `panel/server/mobilecore/runtime_container_test.go`.
  - Introduced `RuntimeContainer` with ordered start, reverse stop, rollback on partial startup failure, idempotent start/stop, and health snapshot.
  - Wired container-managed startup sequence as Scheduler -> Subscription Scheduler -> Backup Scheduler -> Log Cleanup.
- Updated `panel/server/mobilecore/mobilecore.go`.
  - `StartCore` now starts runtime workers only after recovery convergence path reaches HTTP readiness.
  - Runtime startup failure keeps Core in stopped state and preserves Task 1.3 recovery semantics.
  - `StopCore` now stops runtime workers in reverse order before HTTP shutdown and continues cleanup while preserving observable shutdown diagnostics.
- Refactored worker lifecycles in service layer to restartable handles:
  - `panel/server/service/scheduler_manager.go`
  - `panel/server/service/subscription_scheduler.go`
  - `panel/server/service/backup_schedule.go`
  - `panel/server/service/log_cleanup.go`
  - Added context-aware start/stop APIs and retained existing compatibility wrappers.
- Added and expanded tests for ordered start/reverse stop/partial rollback/restart/idempotent/active interruption paths.
- Task 1.4 review fixes completed:
  - Runtime workers now start only after recovery finalize reaches `markReady` success; any finalize failure keeps workers stopped.
  - `StopCore` runtime stop failure now returns `SHUTDOWN_FAILED` and preserves diagnosable `failed` lifecycle state.
  - Added a hard runtime-start recovery gate seam enforcing `RecoveryConverged` on the worker startup path.
  - Removed redundant `LastError` no-op logic in `runtime_container.go` and added regression test coverage.
- Verification completed:
  - `go test -race ./mobilecore ./service ./router -count=1`
  - `go test ./handler ./service -count=1`
  - `go vet ./mobilecore ./router ./handler ./service ./database ./appboot`
  - `go run scripts/generate-route-contract.go -check`

### Specification

- `cd7d3be` `docs: define Modern Android full panel`
- Modern requirements and design reviewed with no remaining Critical or Important findings.
- Six-milestone implementation plan written in `tasklist.md`.

### Task 1.0: Green Baseline

- `6d920ce` `fix(core): restore full server test baseline`
  - Fixed platform-dependent binary update fixture.
  - Fixed scoped npm package parsing across slash styles.
- `71b90a1` `fix(core): stabilize managed dependency command test`
  - Removed the `StdoutPipe`/`Wait` race that dropped short process output.
  - Target test passed 20 repeated runs.
  - Combined `go test ./handler ./service -count=1` passed at that commit.

### Task 1.1: Canonical Route Contract

- `b15afb2` `feat(core): generate canonical route contract`
  - Generated canonical server and mobile route inventory.
  - Added route diff and `contracts/backend-api-mobile.json`.
- `7311baf` `test(core): enforce full mobile route contract`
  - Added 423 route descriptors and CI freshness checks.
  - Added all 60 dangerous capability routes to full gate tests.
- `b23cfde` `test(core): verify real SSE route contracts`
  - Bound all 423 entries to explicit route descriptors.
  - Added eight real SSE handler contract tests.
  - Route generator check passed with 423 routes.

### Task 1.2: Full Mobile Route Profile

- `1aafcb8` `feat(core): register full mobile route profile`
  - Registered all backend modules in the mobile route profile.
  - Added immutable versioned platform capability input.
  - Classified 60 dangerous routes as Android capability operations.
  - Dangerous handlers remain blocked until their adapters pass later milestones.
  - `go test -race ./router ./mobilecore` passed.

### Task 1.3: Minimum Migration Recovery

- `88613af` `feat(core): add migration recovery generations`
  - Added generation directories, active pointer, recovery transaction phases, WAL checkpoint, integrity checks, and startup convergence gate.
  - Initial tests passed, followed by recovery hardening completed in the subsequent Task 1.3 commit.
- `1ba7941` `fix(core): harden migration recovery`
  - Added migration baseline checks, strict file manifests, retention, startup rollback, and `EnsureColumns` error propagation.
- Follow-up recovery review fixes:
  - Reserved `verified` for complete Core readiness after HTTP health probing.
  - Made first-generation bootstrap crash-resumable without accepting mutable verified data.
  - Derived schema migration identity from the actual AutoMigrate model structure.
  - Cleaned interrupted candidates and orphans before migration space preflight.
  - Propagated rollback, close, reopen, PRAGMA, legacy PID, and legacy index errors.
  - Added clean-stop WAL checkpoint, symlink rejection, and atomic manifest replacement.
- Second recovery review fixes:
  - Replaced reflection tags with a stable GORM-resolved schema descriptor covering table naming, SQLite datatypes, indexes, and constraints.
  - Added an explicit manual schema revision for `EnsureColumns` and manual index migrations.
  - Propagated startup database close failures as stable recovery-required responses without overwriting live globals.
  - Rejected generation-root symlinks with `Lstat`.
  - Made verified-generation pruning idempotent on every startup, including crash recovery after verified transaction persistence.
- Final recovery review fixes verified:
  - Added trusted-root guards for `dataDir`, the generations container, and every generation path before create, validate, prune, or rollback operations.
  - Preserved committed candidate state when candidate database close fails.
  - Persisted recovery-required lifecycle state, blocked repeated starts, and exposed cleanup retry through `StopCore`.
  - Propagated startup default-config, Python migration, runtime-policy, and dependency-dedupe database errors through appboot.
- Final Task 1.3 metadata fixes verified:
  - Replaced predictable metadata `.tmp` paths with same-directory random exclusive temporary files and symlink-safe target validation.
  - Extended the schema fingerprint with resolved SQLite migration attributes for primary keys, autoincrement, defaults, constraints, and composite primary-key order.
- Final Task 1.3 atomic-publish review fixes verified:
  - Bound Linux/Android metadata publication to an open unnamed inode with `O_TMPFILE`, `linkat`, and `renameat2`, including post-publish rollback from an open recovery inode.
  - Added a strict internal metadata temporary-prefix protocol and startup cleanup that rejects symlinks and multi-link files.
  - Split free-space, no-follow, link-count, and atomic-publish primitives into Linux/Android, Windows, and fallback build-tag files.
- Final Task 1.3 recovery metadata architecture verified:
  - Moved all owned recovery artifacts into the marked `.recovery-meta/ops/<operation-id>` namespace while preserving legacy prefix-shaped business files.
  - Added checksummed PREPARED, COMMITTED, and ROLLED_BACK ownership journals with startup convergence and target allowlisting.
  - Added platform capability probing before legacy checkpoint, platform-owned directory durability, schema descriptor format v2, and durable rollback-before-candidate-delete ordering.
- Ownership journal audit fixes verified:
  - Made `journal.json.next` a recoverable protocol object with strict dual-candidate validation and monotonic state selection.
  - Registered a fixed `exchange-state` slot before publication, preserved deletion authority until payload cleanup was durable, and used handle-bound reads/restores.
  - Moved capability probing before data-directory creation, synchronized generation deletion parents, and propagated Windows handle-close failures.
- Ownership authority crash-window fixes verified:
  - Classified journal-less operation directories as pre-authority cleanup, terminal retirement, or unsafe unknown layouts.
  - Added canonical duplicate-free journal JSON, monotonic dual-candidate selection, fixed exchange-slot PREPARED convergence, and handle-bound old-absent rollback.
  - Made the platform probe name-free, kept cleanup authority until payload and journal deletion were durable, and preserved exact reserved-namespace import behavior.
- Final probe, exchange, and cleanup audit fixes verified:
  - Exercised the complete production probe sequence on the target filesystem with two unnamed inodes, linkat, NOREPLACE, EXCHANGE, content verification, cleanup, and parent durability.
  - Persisted the fixed exchange slot before publication, synchronized both directories before verification, and bound PREPARED rollback to validated inode identities.
  - Added restartable cleanup fault boundaries covering payload, journal, operation-directory removal, and every associated durability phase.
- Final production-primitive review fixes verified:
  - Made the capability probe execute two O_TMPFILE inodes, linkat, NOREPLACE, EXCHANGE, content/identity checks, cleanup, and parent fsync inside an exact recoverable probe namespace.
  - Synchronized the operation slot immediately after linking and both target/operation parents after publication before validation or COMMITTED.
  - Bound PREPARED exchange rollback to opened inode identities before and after rename, and covered every cleanup durability boundary with second-start convergence tests.
- Final probe ownership and rollback retention fixes verified:
  - Replaced the fixed probe directory with marker-bound random operation namespaces, flock-based concurrent isolation, and marker-last tombstone cleanup.
  - Preserved ordinary probe-named business directories during flat import and safely rejected marker-less reserved-looking operations.
  - Removed platform-side destructive rollback, retained exchange-state until journal-level rollback succeeded, and verified restart convergence after persistent rollback failures.
- Reserved-namespace probe architecture verified:
  - Creates and durably synchronizes `dataDir/.recovery-meta/ops` before probing platform primitives or touching legacy data.
  - Runs each probe as a random pre-authority operation inside `ops`, allowing normal journal authority convergence to retire every crash layout.
  - Accepts the PREPARED `target=old, exchange=old` intermediate state and persists ROLLED_BACK before owned cleanup.

## Task 1.3 Review Findings Resolved

1. Generation creation now occurs only when the schema or runtime baseline changes.
2. Migration performs disk-space preflight, removes failed candidates and orphans, and retains only active plus previous healthy generations.
3. Failures after pointer commit and before full Core readiness rollback and reopen the previous healthy generation.
4. Serve failure closes the recovery gate by setting `RecoveryConverged` to false.
5. Generation validation checks required files, regular-file type, size, SHA-256, missing files, and unexpected persistent files.
6. Clean shutdown checkpoints and deterministically seals mutable active data; crash recovery falls back to the previous healthy generation when the active seal no longer matches.
7. `database.EnsureColumns` returns every failed `ALTER TABLE` error through appboot so migration rollback can run.

## Historical Task 1.3 Verification

- `go test -race ./mobilecore ./database ./appboot -count=1` passed.
- `go test ./handler ./service -count=1` passed.
- `go vet ./mobilecore ./router ./handler ./service ./database ./appboot` passed.
- This historical checkpoint covered Task 1.3. Task 1.4 is implemented as recorded in the current Task 1.4 section above.

### Milestone 2

- Eight runtime IDs use the same manifest, compatibility, smoke-evidence, APK extraction, and device-evidence contracts.
- Current `runtime/smoke-evidence.json` marks all eight records blocked. Shell, Git, SSH, Yaegi, and Go Builder remain `blocked-placeholder` assets; current Python, Node, and TypeScript records also carry blocked smoke evidence.
- Core startup preserves the management HTTP Core when runtime components or smoke checks are blocked. Health reports `degraded-ready`, and runtime execution remains fail-closed through the runtime locator.
- API 28/4K, API 35/4K, and API 35/16K accepted device smoke evidence remains pending.
- Secret Store now uses local AES-GCM encryption-at-rest with random nonce and 32-byte master key from env or restricted dataDir file (`0600`).
- Trust authorization now persists to `dataDir/.runtime_trust_authorizations.json` (`0600`) with reload-on-init and observable readiness/error state.
- Runtime baseline now enforces entrypoint path boundary checks, placeholder digest default-reject policy, and severe-failure startup gate with explicit dev bypass switch.
- Git runtime policy hardening is unified through `buildGitAuthConfig` + `ApplyGitSSHRuntimePolicy` on clone/pull/reset paths.
- Runtime ELF verification script now defaults to strict digest mode and supports opt-out via `--strict=false`.

### Milestone 3

- Controller and service paths are implemented. Runtime-backed end-to-end execution remains subject to the blocked runtime baseline and device gates.

- Task 3.1 complete: added persistent `Operation` model/table, `OperationStore` service methods, and migration registration.
- Task 3.2 complete: added structured `ProcessSupervisor` with argv, env filtering, working-root validation, stdout/stderr streaming, timeout, cancellation, process-group cleanup, and quota fields.
- Task 3.3 complete: task run/stop/enable/disable/batch and execution lifecycle now create/update Operations while retaining existing API payloads.
- Task 3.4 complete: TaskLog now persists `log_cursor`; SSE accepts cursor/Last-Event-ID and resumes from persisted history while retaining existing log APIs.
- Task 3.5 complete: Env CRUD/import/export seals values through SecretStore when available and opens only selected runtime values; script debug/run-code/stop use ProcessSupervisor and Operations.
- Verification completed:
  - `cd /workspace/panel/server && go test ./service ./handler ./mobilecore ./appboot ./router ./database -count=1`
  - `cd /workspace/panel/server && go test -race ./mobilecore ./service -count=1`
  - `cd /workspace && go run scripts/generate-route-contract.go -check`

### Milestone 4

- `router.mobileCapabilityMiddleware` allows an `enabled` capability to continue into the registered real Handler. Disabled and undeclared capabilities return the stable `PLATFORM_CAPABILITY` response.
- Handler/service implementation is present for dependency, Git, subscription, notification, integration, Sponsor, and OpenAPI paths. Runtime-dependent completion remains partial until production assets and device evidence pass.

- Task 4.1 complete: pip/npm dependency operations now return stable compatibility details for unsupported packages, enforce signed native allowlist checks, keep npm lifecycle scripts disabled, stage install attempts, rollback failed or canceled installs, check dependency quota, persist dependency Operations, and recover interrupted operations after restart.
- Task 4.1 verification attempted: `cd /workspace/panel/server && go test ./service -run 'TestEvaluateDependencyCompatibility|TestEnforceNpmScriptPolicy|TestCheckDependencyQuota|TestPrepareDependencyStaging|TestReconcileDependenciesAfterRestartRecoversInterruptedOperation' -count=1` and `cd /workspace/panel/server && go test ./handler -run 'TestDependencyCreate(ReturnsCompatibilityDetails|PersistsOperation)' -count=1` are blocked by existing `service/notifier.go:278:6: err redeclared in this block` package compile failure outside Task4.1 files; `cd /workspace/panel/server && go test ./model -count=1` passed.
- Task 4.2 complete: Git Provider now supports HTTPS/SSH provider hardening with CA env wiring, persistent known_hosts, SecretStore-backed token/key opening, sparse checkout through staging clone, and atomic worktree replacement for force-overwrite subscription pulls.
- Task 4.2 compatibility: existing subscription paths, SaveDir/Alias derivation, legacy token storage, SSHKeyID-backed key material, sparse checkout filters, and preserve-local-modification mode remain supported.
- Task 4.2 safety: `GitClone`/`GitPull` keep SSH key parameters as filesystem paths, reject inline private key material, and convert Host Key verification failures into a stop-and-confirm error message.
- Task 4.2 verification passed: `cd /workspace/panel/server && go test ./service -run 'Test(AppendGitSSHEnvUsesPersistentKnownHosts|BuildGitAuthConfig|ResolveSubscriptionSSHKeyPath|NormalizeGitProviderError|GitAuthTypeForSSHKeyPath|PullGitRepoWithCallback|GitHasWorkingTreeChanges|ApplySubscriptionForceOverwriteSetting)' -count=1`.
- Task 4.3 complete: subscription pull now creates and updates subscription Operations, returns `operation_id` from pull/stop APIs, emits the operation ID on pull SSE, filters logs by operation ID, and reconciles interrupted pending/running subscription Operations to `unknown` on scheduler startup.
- Task 4.3 safety: force-overwrite Git pulls clone into staging, run hooks before publish, atomically switch the healthy staging worktree into place, retain the previous healthy worktree at `.<saveDir>.previous`, and leave the active version untouched on cancellation or staging failure; single-file subscriptions download to a staging file before rename.
- Task 4.3 compatibility: existing CRUD payload fields, schedules, whitelist/blacklist sparse checkout behavior, hook environment, preserve-local-modification mode, legacy token/SSHKeyID paths, and SubLog list payloads remain compatible while adding optional `operation_id` fields.
- Task 4.3 verification attempted: `cd /workspace/panel/server && go test ./service -run 'Test(PullGitRepoWithCallbackAtomicSwitchKeepsPreviousHealthyVersion|PullGitRepoWithCallbackCancelledStagingLeavesActiveVersion|ExecuteSubscriptionPullTracksOperationAndLog|ReconcileInterruptedSubscriptionPullsMarksRunningOperationsUnknown|PullGitRepoWithCallbackConvertsExistingNonGitDirectoryInPlace|PullGitRepoWithCallbackPreserveModeSkipsFalseConflictWhenRepoIsClean|BuildSubscriptionHookEnvUsesSubscriptionWorkDir|NormalizeSubscriptionHookScriptRewritesQingLongPaths)$' -count=1` is blocked by existing `service/notifier.go:278:6: err redeclared in this block` package compile failure outside Task4.3 files.
- Task 4.4 complete: notification channels now include Android local notification and external webhook adapters while retaining existing notification send/test interfaces.
- Task 4.4 security: new notification channel configs and Platform Token values are SecretStore-backed or explicitly redacted in responses; Platform Token entries now support service and service-user binding for per-service credentials.
- Task 4.4 OpenAPI: new/reset app secrets are stored as SHA-256 hashes, token issuance validates hashed secrets, and per-service token requests require matching enabled service credentials.
- Task 4.4 Sponsor coverage remains stable for unavailable feed fallback, remote URL normalization, and stripped portal payload fields.
- Task 4.4 verification completed:
  - `cd /workspace/panel/server && go test ./service -run 'Test(SendAndroidLocalNotificationAdapter|SendExternalWebhookUsesTemplatesAndBearerToken|SealedNotificationConfigOpensAtDispatchAndRedacts|SendToChannel|SendWecom|SendWxPusher)' -count=1`
  - `cd /workspace/panel/server && go test ./handler -run 'Test(OpenAPI|PlatformToken|Notification|Sponsor)' -count=1`
  - `cd /workspace/panel/server && go test ./middleware -run 'TestAppToken|TestOpenAPIAccess' -count=1`

### Milestone 5

- Task 5.1 complete: persistent schedule instances use unique `taskID + scheduledUTC + expressionHash` identity, transactional launch states, `result_unknown`, skip/queue/parallel policy controls, DST handling, and recent-miss compensation.
- Task 5.2 complete: Android `specialUse` foreground scheduler host reports foreground status, scheduler host state, active recovery trigger, resource snapshot, guarantee state, and intervention guidance through the local host status payload; the visible notification keeps stop control and shows resource-protection status.
- Task 5.2 Core bridge: Android passes scheduler host platform capabilities and scheduler guarantee snapshots into gomobile StartCore options; mobilecore exposes the current scheduler guarantee in Core status.
- Task 5.3 complete: app-start, process-recovery service recreation, `BOOT_COMPLETED`, network-restored callback, and 15-minute WorkManager periodic reconciliation triggers are implemented; recovery Work starts the FGS only when the user has enabled persistent scheduling.
- Task 5.4 complete: Android evaluates battery, thermal, memory, and storage pressure into guarantee states, while Go `service` pauses low-priority cron/startup/background work under `resource_limited` or `system_stopped` and preserves manual execution.
- Task 5.2-5.4 verification completed:
  - `cd /workspace/panel/server && go test ./service -run 'TestShouldPauseLowPriorityWorkOnlyPausesBackgroundTriggers|TestTaskExecutorFailsCronOperationWhenResourceLimited' -count=1`
  - `cd /workspace/panel/server && go test ./mobilecore -run 'TestStartCoreRejectsInvalidCapabilityVersionAndState|TestStartCorePublishesImmutableCapabilitySnapshot' -count=1`
- Android unit verification attempted with `cd /workspace/app/android && ./gradlew :app:testDebugUnitTest --tests 'com.daidai.daidai_app.AndroidResourceProtectionTest' --tests 'com.daidai.daidai_app.AndroidSchedulerHostStatusTest'`, but the repository currently has no Gradle wrapper; `gradle` and `kotlinc` are not installed in the execution image.
- Kotlin test commands and real-device runs have no accepted pass result recorded in this workspace. Milestone 5 remains partially complete until device recovery and long-stability evidence pass.

### Milestone 6

- Task 6.1 is partially complete: portable backup implementation covers the planned envelope and restore paths; Kotlin and device restore evidence remains open.
- Task 6.2 remains pending: Recovery metadata and version policy exist, while a separately built, signed, and upgrade-tested Recovery APK is absent from current evidence.
- Task 6.3 is partially complete: `scripts/generate-release-evidence.go` and CI artifact upload exist; strict runtime, device, Recovery APK, and public re-download records remain open.
- Task 6.4 is blocked pending real-device records for 100 Core cycles, the API/page-size matrix, 24-hour/seven-day stability, low storage, Doze, restart, process kill, and upgrade/recovery.

## Current Contract State

Verified Task 1.0 through Task 1.3:

- 423 canonical routes generated and current.
- 60 dangerous routes capability-gated.
- Eight SSE routes tested through real handlers.
- Handler/service baseline was green after flaky command-output fix.
- Full mobile route and mobile Core race tests passed.

- Enabled capability dispatch reaches real Handlers.
- Kotlin fallback serves a diagnostic-only degraded surface and applies the same installation token plus exact loopback Host and Origin boundary.
- Blocked runtime components produce `degraded-ready`; management APIs remain available and runtime execution remains blocked.
- The eight-runtime contract is structurally unified. Production assets and device pass evidence remain blocked.
- No current claim is made that Kotlin tests, Flutter tests, emulator smoke, physical-device smoke, or the real-device matrix passed.

## CI Order And Gate Difference

1. Resolve and validate `VERSION.json`.
2. On tags, validate signing secrets, prepare the keystore, and require a successful same-commit `android-device-smoke.yml` run containing all three matrix IDs with eight passing runtime records each.
3. Prepare Python and Node runtime inputs; run Go and contract-script tests; verify the generated route contract.
4. Build and inspect the mobile Core AAR; resolve Flutter packages; run Flutter and Kotlin checks.
5. Snapshot builds generate local smoke evidence. Both paths verify runtime contracts before APK build; tag verification adds `--strict`.
6. Build the APK, extract and compare embedded metadata, repeat runtime verification against the APK, then generate hashes, update metadata, and release evidence.
7. Tags additionally verify the signing certificate and publish the Release; snapshots only upload CI artifacts and may retain blocked runtime evidence.

## Remaining Gates

1. Replace blocked runtime assets and produce accepted eight-runtime smoke evidence.
2. Complete the real API/page-size device matrix and Kotlin/Flutter/device verification records.
3. Build, sign, and verify the Recovery APK and its upgrade/recovery sequence.
4. Complete 100 Core cycles, 24-hour and seven-day scheduling, low-storage, Doze, restart, process-kill, public re-download, and recovery evidence.
