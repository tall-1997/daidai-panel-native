# Android Modern Full Panel Progress

Updated: 2026-07-27
Branch: `main`
Remote: `origin/main`

## Current Position

Milestone 1 is in progress. Tasks 1.0 through 1.3 are implemented and committed locally. Task 1.4 has not started.

The local branch is ahead of `origin/main`. Milestone 1 has not reached its exit gate and its commits have not been pushed.

## Completed Work

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

- Runtime manifest and signed APK ELF packaging.
- Python, Node.js, TypeScript, Shell, Git, SSH, Yaegi, and Go Builder.
- Secret Store, trusted runner, isolated pure-compute worker, source/SHA-256 authorization.

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

Not yet verified:

- Milestone 1 complete exit gate.
- Any Milestone 2-6 implementation.
