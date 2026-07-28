# Android Modern Full Panel Progress

Updated: 2026-07-28
Branch: `main`
Remote: `origin/main`

## Current Position

Milestone 2 review remediation is complete. All Critical/Important findings from the Milestone 2 review are fixed and validated with the unified verification suite.

The local branch now contains Milestone 2 runtime/security hardening updates ready to push to `origin/main`.

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

1. Implement Task 1.4 worker lifecycle.
2. Run the Milestone 1 exit gate, push all Milestone 1 commits to `main` as `tall-1997`, and wait for CI before starting Milestone 2.

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

- Persistent operation store.
- Android process supervisor.
- Task execution, stopping, hooks, logs/SSE, environment, and script APIs.

### Milestone 4

- pip/npm dependency operations and compatibility allowlist.
- Git/SSH provider, subscriptions, notifications, external integrations, OpenAPI.

### Milestone 5

- Persistent schedule instances.
- Foreground Scheduler, recovery triggers, boot restoration, resource protection.
- 24-hour and seven-day scheduling evidence.

### Milestone 6

- Portable encrypted backup.
- Recovery APK and version sequence.
- SBOM, licenses, runtime manifest, route trace, compatibility matrix.
- Device matrix, stability gates, final replacement Release.

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

Remaining verification pending:

- Milestone 2 device-level runtime smoke matrix on API 28/35 and 4K/16K page-size environments.
