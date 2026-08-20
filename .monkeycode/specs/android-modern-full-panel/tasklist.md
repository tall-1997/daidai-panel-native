# Android Modern Full Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Every production behavior follows RED-GREEN-REFACTOR and every milestone is pushed to `main` only after review and verification.

**Goal:** Deliver one Android ARM64 `targetSdk 35` APK that creates a local full-featured Daidai Panel with bundled runtimes and no local infrastructure configuration.

**Architecture:** The Go Core owns the complete mobile API and lifecycle-managed workers. Android supplies platform adapters, signed runtime assets, foreground scheduling, recovery, secrets, and storage. Flutter keeps the existing API contract and exposes capability states where Android replaces Linux-specific behavior.

**Tech Stack:** Go 1.25, Gin, GORM, SQLite, gomobile, Flutter 3.41.5, Dart 3.11, Kotlin 2.3.20, AGP 9.0.1, Gradle 9.1.0, Android API 28-35.

## Global Constraints

- Development and pushes occur on `main`.
- Git author and release author are `tall-1997`.
- Modern APK uses `minSdk 28`, `targetSdk 35`, and `arm64-v8a` only.
- Core binds dynamic `127.0.0.1` and validates local Token, Host, and Origin.
- Bundled executable ELF files resolve only from `ApplicationInfo.nativeLibraryDir`.
- External service credentials are configured only when users enable those services.
- Subscription scripts and native extensions require source-and-SHA-256 authorization.
- Every milestone requires tests, review, CI, and a push before the next milestone.

---

## Milestone 1: Full Core and Route Contract

### Task 1.0: Restore a Green Baseline

**Status:** Complete (`6d920ce`, `71b90a1`)

**Files:**
- Test and fix existing failures in `panel/server/handler` and `panel/server/service` before Full Core behavior changes.

**TDD:**

1. Run `go test ./handler ./service -count=1` and record every failure.
2. Reproduce each failure with one targeted test.
3. Fix each root cause without exclusions or skipped tests.
4. Re-run the combined command until it reports zero failures.

### Task 1.1: Generate Canonical Route Inventory

**Status:** Complete (`b15afb2`, `7311baf`, `b23cfde`)

**Files:**
- Create: `panel/server/router/catalog.go`
- Create: `panel/server/router/catalog_test.go`
- Create: `contracts/backend-api-mobile.json`
- Create: `scripts/generate-route-contract.go`

**Produces:**

```go
type RouteContract struct {
    Method          string `json:"method"`
    Path            string `json:"path"`
    Module          string `json:"module"`
    MobileStatus    string `json:"mobileStatus"`
    AndroidEquivalent string `json:"androidEquivalent,omitempty"`
    AuthContract    string `json:"authContract"`
    StreamContract  string `json:"streamContract"`
    TestCase        string `json:"testCase"`
}

func CanonicalServerRoutes() []gin.RouteInfo
func CanonicalMobileRoutes() []gin.RouteInfo
func DiffRoutes(server, mobile []gin.RouteInfo) RouteDiff
```

**TDD:**

1. Write tests requiring every `method + path` from complete `router.Setup` to exist in the generated contract.
2. Run `go test ./router -run TestRouteContract -count=1`; expect failure because catalog functions do not exist.
3. Implement canonical fixtures and route normalization.
4. Generate `contracts/backend-api-mobile.json` with no unclassified routes.
5. Run `go test ./router -count=1` and `go run scripts/generate-route-contract.go -check`.

### Task 1.2: Register Full Mobile Route Profile

**Status:** Complete (`1aafcb8`, `7311baf`; an `enabled` capability now continues into the registered real Handler, while disabled or undeclared capabilities return the stable `PLATFORM_CAPABILITY` response)

**Files:**
- Modify: `panel/server/router/router.go`
- Create: `panel/server/router/mobile.go`
- Create: `panel/server/router/mobile_test.go`
- Modify: `panel/server/mobilecore/mobilecore.go`
- Modify: `panel/server/mobilecore/mobilecore_test.go`

**Produces:**

```go
type MobilePlatform interface {
    Capability(string) CapabilityState
}

func SetupMobileFull(engine *gin.Engine, security ManagementSecurity, platform MobilePlatform)
```

**TDD:**

1. Extend `StartCore` options with versioned platform-capability JSON and construct one immutable `MobilePlatform` owned by the Core lifecycle.
2. Change the `setupRoutes` seam to accept `(engine, security, platform)` and provide explicit test platform implementations.
3. Write failing tests requiring Auth, Task, Log, Script, Env, Subscription, Notification, SSH Key, User, Security, System, OpenAPI, Deps, Config, Platform Token, Sponsor, and Android Runtime paths.
4. Keep execution, dependency mutation, update, restore, Docker, systemd, Linux package, and self-restart endpoints behind stable `PLATFORM_CAPABILITY` responses until Process Supervisor, Secret Store, trust authorization, and recovery adapters pass their milestone gates.
5. Run route tests and verify RED.
6. Implement `SetupMobileFull` and platform capability middleware.
7. Replace `SetupManagement` in mobile Core.
8. Verify route diff is empty and unsafe host operations are intercepted.

### Task 1.3: Add Minimum Migration Recovery

**Status:** Complete (`88613af`, `1ba7941`, `de1ac6e`, `e09d2ce`, final journal architecture, and ownership journal audit fixes)

**Files:**
- Create: `panel/server/mobilecore/generation_store.go`
- Create: `panel/server/mobilecore/generation_store_test.go`
- Modify: `panel/server/mobilecore/mobilecore.go`
- Modify: `panel/server/appboot/appboot.go`

**Produces:** data generations, `active-generation`, migration snapshot, WAL checkpoint, atomic pointer switch, startup recovery state machine.

**TDD:** inject failures before and after file write, fsync, pointer rename, and database reopen; verify one valid generation remains, migration starts only after a recoverable snapshot exists, and workers remain locked until recovery converges.

### Task 1.4: Lifecycle-Manage Core Workers

**Status:** Complete (review findings remediated: startup order gate, runtime-stop observable failure path, recovery hard gate, runtime-container cleanup)

**Files:**
- Create: `panel/server/mobilecore/runtime_container.go`
- Create: `panel/server/mobilecore/runtime_container_test.go`
- Modify: `panel/server/mobilecore/mobilecore.go`
- Modify: `panel/server/service/scheduler_manager.go`
- Modify: `panel/server/service/subscription_scheduler.go`
- Modify: `panel/server/service/backup_schedule.go`
- Modify: `panel/server/service/log_cleanup.go`

**Produces:**

```go
type RuntimeContainer interface {
    Start(context.Context) error
    Stop(context.Context) error
    Health() HealthSnapshot
}
```

**TDD:**

1. Write failing tests asserting ordered worker start, reverse stop, partial-start rollback, repeat start/stop, and active task interruption.
2. Refactor workers from non-restartable package state into restartable instances or explicit lifecycle handles.
3. Start Scheduler, Subscription Scheduler, Backup Scheduler, and Log Cleanup from mobile Core.
4. Keep resource and update operations behind Android adapters.
5. Run `go test -race ./mobilecore ./service ./router` with zero exclusions.

### Milestone 1 Exit Gate

```bash
cd panel/server
go test -race ./mobilecore ./router ./database
go test ./handler ./service -count=1
go vet ./mobilecore ./router ./handler ./service ./database
cd ../..
go run scripts/generate-route-contract.go -check
```

- Route trace coverage: 100%.
- All server routes are represented by native mobile behavior or stable Android capability responses.
- Core and workers support repeated start/stop with zero test exclusions.
- Migration failure never starts business workers.
- Push milestone commits to `main` as `tall-1997`.

---

## Milestone 2: Runtime and Security Baseline

**Milestone Status:** Partially complete. The eight runtime IDs share one manifest, compatibility, smoke-evidence, APK verification, and device-evidence contract. Current smoke evidence is blocked, several entries are placeholder assets, and the API 28/35 4K plus API 35 16K device gate has no accepted pass matrix.

### Task 2.1: Runtime Manifest and APK Packaging

**Status:** Partially complete (contract and fail-closed verification are implemented; production ARM64/Bionic assets and strict device evidence remain blocked)

Create `runtime/manifest.json`, `runtime/compatibility.json`, Gradle runtime packaging, final APK ELF checks, and `RuntimeComponentManager`. Package executable entries as `lib<runtime>_exec.so` and resolve them from `nativeLibraryDir`.

### Task 2.2: Python Runtime

**Status:** Partially complete (runtime declaration and smoke contract exist; accepted on-device smoke evidence remains blocked)

Package Android/Bionic CPython, stdlib, SSL, SQLite, pip, venv, CA, and pure-wheel seed. Add offline `PY_OK`, SSL, SQLite, venv, and wheel installation smoke tests.

### Task 2.3: Node and TypeScript Runtime

**Status:** Partially complete (runtime declarations, smoke contracts, and lifecycle-script policy exist; accepted on-device smoke evidence remains blocked)

Package Android Node LTS, npm/npx, CA, TypeScript, and ts-node. Disable lifecycle scripts by default. Add CommonJS, ESM, HTTPS, local tarball, and `TS_OK` tests.

### Task 2.4: Shell, Git, and SSH Runtime

**Status:** Partially complete (contracts and Git policy wiring exist; manifest assets are blocked placeholders and device smoke remains blocked)

Package controlled Shell, Git HTTPS, and SSH transport. Disable hooks, pager, editor, external filters, and credential helpers. Test clone/fetch/sparse checkout and Host Key rejection.

### Task 2.5: Yaegi and Go Builder

**Status:** Partially complete (runtime declarations and export-only contract exist; manifest assets are blocked placeholders and device smoke remains blocked)

Embed Yaegi with a fixed symbol allowlist. Package Go compiler/linker/asm tools as signed APK ELF entries. Build exportable artifacts and prohibit executing generated binaries.

### Task 2.6: Secret Store and Trust Authorization

**Status:** Partially complete (Go secret/trust stores and Android Keystore envelope bridge are implemented; Kotlin and device verification evidence remains open)

Implement Android Keystore envelope encryption and source/version/SHA-256/capability authorization records. Add `:runner` trusted process and isolated pure-compute worker.

### Milestone 2 Exit Gate

- Eight runtime IDs must pass smoke on API 28/4K, API 35/4K, and API 35/16K; current records are blocked.
- Final APK must contain production ARM64/Bionic ELF assets with valid linker dependencies; current runtime metadata includes blocked placeholders.
- Secret values remain encrypted at rest.
- Runtime baseline and SBOM are pushed to `main`.

---

## Milestone 3: Execution, Logs, Environment, and Scripts

**Milestone Status:** Review remediation in progress. Implementation tasks 3.1-3.5 are complete; final Milestone 3 completion remains pending until review findings are fixed and the exit gate is re-verified.

### Task 3.1: Persistent Operation Store

**Status:** Complete (persistent Operation model, OperationStore lifecycle API, and appboot/test migrations)

Add operation IDs, states, phases, sequence, progress, exit code, error code, timestamps, and log cursor for task, dependency, runtime, subscription, and backup operations.

### Task 3.2: Android Process Supervisor

**Status:** Complete (structured ProcessSupervisor with argv/env/root/stream/timeout/cancel/process-group/quota controls)

Implement structured argv, environment filtering, working-root validation, stdout/stderr streaming, timeout, cancellation, process-group cleanup, and resource quotas.

### Task 3.3: Task Execution API

**Status:** Complete (task run/stop/enable/disable/batch and execution lifecycle record Operations without changing existing responses)

Enable run, stop, enable, disable, batch operations, startup tasks, and hooks through the mobile Scheduler and Executor.

### Task 3.4: Logs and SSE

**Status:** Complete (TaskLog cursor persistence, resumable SSE cursor support, and compatible log payloads)

Enable log list/detail/files, persistent cursors, reconnectable SSE, retention policy, and interrupted-run reconciliation.

### Task 3.5: Environment and Script APIs

**Status:** Complete (Env values pass through SecretStore and script debug/run-code/stop use ProcessSupervisor/Operation wiring; final controller status updated for Milestone 3 review remediation)

Enable Env CRUD/import/export with Secret Store. Enable script tree, upload, save, versions, formatting, debug, run-code, stop, and cleanup using Runtime Locator.

### Milestone 3 Exit Gate

- Each language runs, stops, times out, logs, and records one terminal state.
- Environment secrets are decrypted only for selected tasks.
- SSE reconnect resumes from persisted cursor.
- Push execution milestone to `main`.

---

## Milestone 4: Dependencies, Git, Subscriptions, and Integrations

**Milestone Status:** Partially complete. Enabled capability routes enter the registered real Handlers and the service/controller wiring exists. Runtime-dependent end-to-end behavior remains blocked by runtime assets and device smoke evidence.

### Task 4.1: Python and Node Dependency Operations

**Status:** Complete (compatibility details, signed native allowlist, npm script policy, staging, cancellation, rollback, quota checks, and operation recovery)

Implement wheel/package compatibility, signed native allowlist, npm script policy, staging, cancellation, rollback, quota checks, and operation recovery.

### Task 4.2: Git Provider

**Status:** Complete (HTTPS/SSH auth provider hardening with CA, known_hosts, SecretStore-backed token/key access, sparse checkout staging, and atomic worktree replacement)

Implement HTTPS and SSH providers with CA, known_hosts, Token/Key Secret Store access, sparse checkout, and atomic worktree replacement.

### Task 4.3: Subscription Lifecycle

**Status:** Complete (CRUD-compatible pull/stop/logs/SSE/scheduler lifecycle with Operation tracking, staging atomic switch, previous healthy version retention, cancelable hooks, and interrupted-pull reconciliation)

Enable CRUD, pull, stop, logs, SSE, scheduler, whitelist/blacklist, hooks, and interrupted-pull reconciliation.

### Task 4.4: Notifications and External Integrations

**Status:** Complete (channels, Android local notification adapter, external webhooks, sealed Platform Token credentials, Sponsor stability, and OpenAPI per-service user credential binding)

Enable channels, Android local notification adapter, external webhooks, Platform Token, Sponsor, and OpenAPI with per-service user credentials.

### Milestone 4 Exit Gate

- Pure and compatible native dependencies install and rollback.
- Unsupported packages fail with stable compatibility details.
- Public Git is zero-config; private Git succeeds after user credential configuration.
- Integration milestone implementation is on `main` under author `tall-1997`.

---

## Milestone 5: Scheduling and Recovery

**Milestone Status:** Partially complete. Go and Android host paths are present; Kotlin and device execution have no accepted pass evidence, and 24-hour plus seven-day real-device stability gates remain open.

### Task 5.1: Persistent Schedule Instances

**Status:** Complete (unique schedule instances, transactional launch state, unknown-result handling, scheduling policy controls, DST handling, and recent-miss compensation)

Create unique `taskID + scheduledUTC + expressionHash` records, transactional `pending -> launching`, `result_unknown`, skip/queue/parallel policy, DST handling, and recent-miss compensation.

### Task 5.2: Foreground Scheduler Host

**Status:** Partially complete (implementation is present; Kotlin and device validation evidence remains open)

Run Core, Scheduler, and active tasks under `specialUse` FGS with visible status and stop controls.

### Task 5.3: Recovery Triggers

**Status:** Partially complete (implementation is present; device recovery validation remains open)

Add App-start, process-recovery, `BOOT_COMPLETED`, network-restored, and periodic WorkManager reconciliation.

### Task 5.4: Resource Protection

**Status:** Partially complete (implementation is present; Kotlin and real-device validation remains open)

Pause low-priority work for battery, thermal, memory, and storage thresholds; expose guarantee states and intervention guidance.

### Milestone 5 Exit Gate

- 24-hour and seven-day long-stability runs are reserved for user real-device testing evidence.
- Device matrix scheduling evidence is reserved for user real-device testing evidence.
- Recovery windows compensate recent misses once or record interruption.
- Scheduling milestone implementation is on `main` under author `tall-1997`.

---

## Milestone 6: Backup, Recovery, and Final Release

**Milestone Status:** Partially complete. Backup and release-evidence code paths exist. A separately built Recovery APK, strict runtime evidence, the real-device matrix, upgrade/recovery installation proof, and long-stability records remain pending.

### Task 6.1: Portable Backup Envelope

**Status:** Partially complete (implementation is present; Kotlin and device restore evidence remains open)

Implement AES-256-GCM archive encryption, Argon2id key wrapping, manifest, file hashes, runtime requirements, wrong-password rejection, SAF import/export, and atomic restore.

### Task 6.2: Recovery APK

**Status:** Pending (metadata and version-sequence policy exist; no separately built, signed, and upgrade-tested Recovery APK is evidenced)

Reserve the documented version-code slot, then build and verify a Recovery APK with supported stable Core/runtime denylist and forward-reading compatibility.

### Task 6.3: Release Evidence

**Status:** Partially complete (evidence generation and CI artifact wiring exist; strict runtime, device, Recovery APK, and public re-download records remain open)

Generate APK, SHA-256, SBOM, third-party licenses, runtime manifest, route trace, compatibility matrix, page-size report, and test reports.

### Task 6.4: Device and Stability Gate

**Status:** Blocked pending real-device evidence (the schemas exist; 100 Core cycles, API matrix, 24-hour/seven-day stability, storage, Doze, restart, process-kill, and upgrade/recovery runs remain open)

Run 100 Core cycles, API matrix, 24-hour and seven-day tests, low-storage, Doze, restart, process kill, and upgrade/recovery tests.

### Milestone 6 Exit Gate

- No known Critical or Important findings.
- Public APK re-download matches SHA-256.
- Modern and Recovery APKs install and upgrade in the documented sequence.
- Device matrix and long-stability records are completed by user real-device testing before replacement release publication as `tall-1997`.

## Plan Self-Review

- Requirements 1-11 map to at least one task and exit gate.
- Security and minimum recovery precede execution and runtime migration.
- Eight runtimes each have package, smoke, isolation, and release evidence.
- Server-only behavior remains represented through Android equivalents or stable capability results.

---

## Milestone 7: Full Parity, Upstream Sync, and Capability UX

### Task 7.1: Integrate Latest Panel Release

**Status:** In progress

- Import `daidai-panel v3.0.6` commit `9a7c27e2a03ed30d3ac632adf58fec60009e601c`.
- Preserve the verified Flutter v1.0.1 client and Android local adaptations.
- Require `go test ./...`, route contract, Flutter tests/analyze, Kotlin tests, and snapshot APK build.

### Task 7.2: Close Kotlin Fallback Execution Gaps

**Status:** Pending

- Inject panel env into main commands and before/after hooks.
- Track task processes and terminate process trees on stop.
- Persist running logs incrementally and implement cursor-resumable SSE.
- Detect missing Python/Node dependencies, install with bounded retries, and rerun.
- Add parity tests shared with Go Core behavior.

### Task 7.3: Unify Runtime Mirror Configuration

**Status:** Pending

- Use Huawei APK, Alibaba pip, and npmmirror npm defaults.
- Persist arbitrary valid user mirror URLs, including official sources.
- Wire Flutter mirror settings to Kotlin fallback, rootfs, task env, and Go Core adapters.
- Preserve user values across rootfs preparation and restart.

### Task 7.4: Add Panel Handshake and Capability Profile

**Status:** Pending

- Add versioned handshake to local and current remote-compatible backend.
- Parse native `platform_capabilities` into Flutter.
- Add legacy endpoint probing for older remote backends.
- Parse capability 409 responses into structured profile updates.

### Task 7.5: Gate Menus, Routes, and Actions

**Status:** Pending

- Hide unsupported menu entries.
- Guard deep links with an unavailable page.
- Keep read-only pages visible when only mutation capability is disabled.
- Apply independent profiles when switching local and remote instances.

### Task 7.6: Consolidate Repository Branches

**Status:** Pending

- Merge verified integration into `main`.
- Push `main` and complete snapshot APK validation.
- Remove confirmed local and remote integration/test branches after merge verification.
- No release is labeled full-featured before Milestones 1-6 all pass.
