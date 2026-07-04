# Prenup — future work

This file tracks deliberate non-goals that remain good ideas for follow-up
releases. Nothing here is committed; think of it as a backlog with rationale.

## Correctness & reliability

### Continue-on-error within a task
Today, the first failing module in a `per_module` task cancels its
remaining modules. Offer an opt-in `fail_fast: false` per task so developers
see every failing module in one pass with an aggregated summary.

**Acceptance:** per-task config flag; runner collects all module errors;
final `task_completed` event carries the aggregate.

### Concurrency safety — shipped
`prenup run` takes an OS-level advisory lock on `.git/prenup.lock` before
executing any tasks. A second concurrent invocation exits non-zero with a
clear "another prenup run is already in progress for this repository"
message, including the lock-file path. The lock is released automatically
on process exit (including crashes / kill -9), so there is no stale-PID
file to clean up. `--dry-run` skips the lock so planning probes don't
contend with a real commit-time run.

### Staging guardrails
- Warn when a `stage_output: true` task modifies files outside its
  `output_patterns` (likely a config bug).
- Warn when `output_patterns` matched zero files for several runs (stale
  pattern).
- Refuse to stage files covered by `.gitignore`.

## Developer experience

### Per-task skip in the runner UI
Today `s` skips everything. Add an in-flight "press `n` to skip this task"
affordance in the runner model, emitting a `task_completed` with
`status: skipped`.

### `PRENUP_SKIP` env var
One-off bypass without editing config. Comma-separated task names; merged
with `--task` exclusions.

### Caching / incremental skip
For deterministic tasks (doc generation), skip when the hash of their
inputs (files matching `paths`) is unchanged since the last successful run.
Persist hash in `.git/prenup-cache/`.

### Result history & `prenup stats`
Persist per-task duration per run. Surface "slower than average" indicators
in the post-run summary and a `prenup stats` subcommand for trend analysis.

### Better failure presentation
- Highlight failing lines (regex on `FAIL`, `error:`, etc.) at the top of the
  summary.
- Embed `file:line` jumps where compilers produce them.

### UI affordances
- "Module N of M" progress indicator per running task.
- Wall-clock elapsed per running task.
- Colorblind-friendly icons (don't rely on color alone).

## Configuration surface

### `extends` / `include` / profiles
- `extends:` a shared base config (org-wide defaults).
- `include:` merge multiple files (e.g. `.prenup.d/*.yaml`).
- `profiles: { fast: [...], full: [...] }` with `prenup run --profile`.

### Task-level DAG parallelism
Add `depends_on` to enable independent tasks to run concurrently while
serializing generators before linters. Today's runner is
per-module-within-task only.

### Pluggable module detectors
Beyond `module_markers`, introduce named detector plugins (Go, Node,
Python, Rust) that encode per-language intelligence (workspace detection,
monorepo conventions) rather than just a filename check.

### Env pass-through allowlist
Today, tasks inherit prenup's environment, which under a Git hook is often
minimal. Add `pass_env: [HOME, PATH, GOPATH]` plus a globally-documented
allowlist for predictable behavior.

### Per-developer overrides
`.prenup.local.yaml` layered on top of `.prenup.yaml` for individual
preferences (e.g. disable a slow task locally) without affecting the team
config.

## Observability

### Structured logging beyond event stream
In addition to the NDJSON event mode, offer an opt-in structured log file
(`--log-file PATH`) that captures the full run for later inspection — useful
for bots that need to store the trace but don't want to consume it live.

### Opt-in telemetry
Aggregate anonymized stats on which tasks fail most often, average durations,
skip rates. Local-only unless explicitly opted in.

## Tooling & safety

### TUI snapshot tests
The Bubble Tea models have intricate state (selection cancel vs skip, runner
fail-fast, window resize). Add scripted input + golden-frame tests to prevent
regressions such as a cancel action inadvertently allowing the commit.

### Hook chaining edge cases
Today `install --chain` runs `pre-commit.local` before prenup. Handle the
inverse (run prenup first, then chain) behind a flag. Also add detection for
common managed pre-commit frameworks (husky, pre-commit.com) and offer a
clean compose story.

### Version check refinements
Distinguish "no network" (silent) from "token invalid" (one-line warning) so
misconfigured tokens get noticed without adding noise to offline setups.

### Inter-task parallelism
Today the runner serializes tasks (`Run tests` → `Run golangci-lint` → ...) and
only fans out *within* a task across modules. For most Go repos these tasks
are independent (different binaries, different file sets, no shared mutable
state) and a developer laptop has plenty of idle headroom while a single test
run is going. A typical hook of `15s test + 8s lint + 1s changelog ≈ 24s`
collapses to `max(15, 8, 1) = 15s` if those tasks run concurrently — a 30–50%
wall-clock cut on every commit.

Why it isn't on by default yet:
- **Resource contention can defeat the math.** Both `go test` and
  `golangci-lint` already spawn `GOMAXPROCS` workers each. Running them
  concurrently doubles the compile-worker count fighting for cores and the
  Go build cache, so the wall-clock saving sometimes flattens or inverts.
  Same hazard `make -j` users hit.
- **Output presentation needs work.** The Bubble Tea TUI assumes one
  "currently running" task; parallel tasks need either per-task panes,
  line prefixing, or per-task output buffering (which loses the live-stream
  property we deliberately preserved). Markdown and JSON modes already
  carry per-task identifiers and would handle interleaving cleanly.
- **Stash and `stage_output` semantics get subtler.** Two tasks writing
  files into the worktree concurrently can race on which task gets
  attribution for a generated file; the per-task `beforeTracked` snapshot
  needs review under concurrent execution.
- **Failure semantics need a knob.** Today a failing task lets siblings
  finish. Under parallelism we'd want `cancel_siblings_on_failure: true|false`
  so a long failing test doesn't kill a near-finished lint that would have
  produced useful output.

Proposed shape (when we do this):
- New top-level `task_parallelism: 1` (default — preserves today's behavior).
  Setting it to e.g. `3` allows up to N tasks to run concurrently, capped
  further by the existing `max_parallelism` so the *total* concurrent
  commands across all tasks is bounded.
- New per-task `serial: true` escape hatch for tasks that must run after
  prior tasks complete (e.g. a final "rebuild generated code" step that
  depends on tests passing).
- Default to off; recommend enabling per repo after measuring the actual
  wall-clock impact, since the contention curve is workload-dependent.
- When `task_parallelism > 1` is set with `--output human`, either degrade
  the TUI to a per-task running-list (no live stream) or emit a notice
  recommending `--output markdown` / `--output json` for cleaner interleaving.

## Explicitly **not** planned

- **Windows support.** Out of scope. Many prenup primitives assume
  bash, POSIX paths, and the platform's signal semantics; a Windows port
  would require a non-trivial rewrite of exec and IO and is not on the
  roadmap.
