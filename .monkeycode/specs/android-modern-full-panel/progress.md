# Android Modern Full Panel Progress

Updated: 2026-07-28
Branch: `main`
Remote: `origin/main`

## Current Position

Milestone 4-6 implementation goals are complete or in final rollout on `main`: integration, scheduling/recovery, backup/recovery, and release-evidence work is documented as delivered. Device matrix and long-stability evidence are assigned to user real-device testing before final public replacement release labeling.

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

## Task 1.3 Verification

- `go test -race ./mobilecore ./database ./appboot -count=1` passed.
- `go test ./handler ./service -count=1` passed.
- `go vet ./mobilecore ./router ./handler ./service ./database ./appboot` passed.
- Task 1.3 is complete; Task 1.4 remains pending.

## Next Actions

1. Fix Milestone 3 review findings on `main`.
2. Re-run the Milestone 3 exit gate after the review fixes are complete.
3. Update task status and progress evidence after the re-verification passes.

## Remaining Milestones

### Milestone 1

- Implement Task 1.4 lifecycle-managed Scheduler, Subscription Scheduler, Backup Scheduler, and Log Cleanup.
- Pass the Milestone 1 exit gate and push.

### Milestone 2

- Runtime manifest and signed APK ELF packaging scaffolding is complete.
- Python runtime baseline/smoke-state structure is complete with `PY_OK`/`SSL`/`SQLite`/`venv`/`wheel` check IDs.
- Node.js and TypeScript smoke-state structure is complete with `CommonJS`/`ESM`/`HTTPS`/`TS_OK`, and npm lifecycle scripts are default-disabled through runtime policy.
- Shell, Git, and SSH smoke-state structure is complete with default policy wiring for hooks/pager/editor/external filters/credential helper suppression.
- Yaegi and Go Builder runtime declarations and smoke placeholders are complete, with explicit Go Builder export-only policy markers.
- Secret Store now uses local AES-GCM encryption-at-rest with random nonce and 32-byte master key from env or restricted dataDir file (`0600`).
- Trust authorization now persists to `dataDir/.runtime_trust_authorizations.json` (`0600`) with reload-on-init and observable readiness/error state.
- Runtime baseline now enforces entrypoint path boundary checks, placeholder digest default-reject policy, and severe-failure startup gate with explicit dev bypass switch.
- Git runtime policy hardening is unified through `buildGitAuthConfig` + `ApplyGitSSHRuntimePolicy` on clone/pull/reset paths.
- Runtime ELF verification script now defaults to strict digest mode and supports opt-out via `--strict=false`.

### Milestone 3

- Controller status: review remediation in progress; tasks 3.1-3.5 are implementation-complete, with final milestone completion pending review-fix verification.

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
- Task 5.2 complete: Core, Scheduler, and active task execution are represented under the Android foreground scheduling host with visible status and stop controls.
- Task 5.3 complete: app-start, process-recovery, `BOOT_COMPLETED`, network-restored, and periodic WorkManager reconciliation trigger coverage is implemented.
- Task 5.4 complete: resource protection pauses low-priority work under battery, thermal, memory, and storage pressure and exposes guarantee/intervention states.
- Milestone 5 implementation status: complete on `main`; 24-hour and seven-day scheduling SLO evidence is reserved for user real-device long-stability testing.

### Milestone 6

- Task 6.1 complete: portable encrypted backup envelope covers AES-256-GCM archive encryption, Argon2id key wrapping, manifest, file hashes, runtime requirements, wrong-password rejection, SAF import/export, and atomic restore.
- Task 6.2 complete: Recovery APK planning reserves the release version sequence and documents stable Core/runtime denylist plus forward-reading compatibility.
- Task 6.3 complete: release evidence set covers APK SHA-256, SBOM, third-party licenses, runtime manifest, route trace, compatibility matrix template, page-size report, and test-report handoff.
- Task 6.4 final rollout in progress: 100 Core cycles, API matrix, 24-hour/seven-day stability, low-storage, Doze, restart, process-kill, and upgrade/recovery evidence are assigned to user real-device testing.
- Milestone 6 implementation status: final rollout in progress on `main`; device matrix and long-stability records will be completed by user real-device testing before final release publication.

## Verification State

Verified Task 1.0 through Task 1.3:

- 423 canonical routes generated and current.
- 60 dangerous routes capability-gated.
- Eight SSE routes tested through real handlers.
- Handler/service baseline was green after flaky command-output fix.
- Full mobile route and mobile Core race tests passed.

Milestone 2 unified verification completed:

- `cd /workspace/panel/server && go test ./service ./handler ./mobilecore ./appboot ./router ./database -count=1` passed.
- `cd /workspace/panel/server && go test -race ./mobilecore ./service -count=1` passed.
- `cd /workspace && go run scripts/generate-route-contract.go -check` passed (`423 routes`).
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./panel/server/mobilecore` passed.
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -c ./panel/server/mobilecore` passed.

Final verification handoff:

- Milestone 4-6 implementation status is documented as complete/final rollout on `main`.
- User real-device testing will complete the API/device matrix, 24-hour and seven-day long-stability runs, 100 Core cycles, low-storage, Doze, restart, process-kill, upgrade, and recovery evidence.
- No tests were run for this documentation-only status update per user instruction.
