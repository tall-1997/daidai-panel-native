# Android Modern Full Panel Progress

Updated: 2026-07-27
Branch: `main`
Remote: `origin/main`

## Resume Command

```bash
git status --short --branch
git stash list
git stash apply stash^{/wip-modern-full-panel-task-1.3-hardening}
```

Apply the named stash only when resuming Task 1.3. Review the diff before running tests or committing.

## Current Position

Milestone 1 is in progress. Tasks 1.0, 1.1, and 1.2 are implemented and committed locally. Task 1.3 has an initial committed implementation plus unverified hardening changes saved as a named stash. Task 1.4 has not started.

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

### Task 1.3: Initial Recovery Implementation

- `88613af` `feat(core): add migration recovery generations`
  - Added generation directories, active pointer, recovery transaction phases, WAL checkpoint, integrity checks, and startup convergence gate.
  - Initial tests passed, followed by review findings that require hardening before Task 1.3 can be marked complete.

## Task 1.3 Review Findings Still Being Fixed

1. Generation creation must occur only for actual Schema/runtime migration, not every start.
2. Disk-space preflight, failed-candidate cleanup, orphan cleanup, and retention of only active plus previous healthy generation are required.
3. Failures after pointer commit but before full Core readiness must rollback to the previous healthy generation.
4. A Serve failure must set `RecoveryConverged` to false.
5. Generation validation must verify every required file size and SHA-256, including missing or tampered files.
6. Mutable active data needs a deterministic seal/update strategy before the next startup validation.
7. `database.EnsureColumns` must return errors; every failed `ALTER TABLE` must propagate through appboot and trigger migration rollback.

## Saved Unverified Changes

The named stash `wip-modern-full-panel-task-1.3-hardening` contains changes to:

- `panel/server/appboot/appboot.go`
- `panel/server/appboot/appboot_recovery_test.go`
- `panel/server/database/database.go`
- `panel/server/database/recovery_test.go`
- `panel/server/database/task_success_exit_codes_migration_test.go`
- `panel/server/mobilecore/generation_store.go`
- `panel/server/mobilecore/generation_store_test.go`
- `panel/server/mobilecore/mobilecore.go`
- `panel/server/mobilecore/mobilecore_test.go`
- `panel/server/service/task_lifecycle_test.go`

These changes were interrupted before verification. Treat them as WIP, inspect them, and continue TDD. Do not claim Task 1.3 complete until review findings and the exit commands pass.

## Next Actions

1. Apply the named Task 1.3 stash.
2. Inspect the WIP diff and run its focused RED/GREEN tests.
3. Complete all seven Task 1.3 review findings.
4. Run:

```bash
cd panel/server
go test -race ./mobilecore ./database ./appboot
go test ./handler ./service -count=1
go vet ./mobilecore ./router ./handler ./service ./database ./appboot
cd ../..
go run scripts/generate-route-contract.go -check
```

5. Request a fresh Task 1.3 code review.
6. Commit Task 1.3 hardening only after the review is clean.
7. Implement Task 1.4 worker lifecycle.
8. Run the Milestone 1 exit gate, push all Milestone 1 commits to `main` as `tall-1997`, and wait for CI before starting Milestone 2.

## Remaining Milestones

### Milestone 1

- Finish Task 1.3 migration recovery hardening.
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

Verified before the interrupted WIP:

- 423 canonical routes generated and current.
- 60 dangerous routes capability-gated.
- Eight SSE routes tested through real handlers.
- Handler/service baseline was green after flaky command-output fix.
- Full mobile route and mobile Core race tests passed.

Not yet verified:

- Task 1.3 hardening WIP.
- Milestone 1 complete exit gate.
- Any Milestone 2-6 implementation.
