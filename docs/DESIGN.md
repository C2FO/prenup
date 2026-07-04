# Prenup — Design & Behavior Specification

This document describes what `prenup` does and how it behaves. It is the
reference for the tool's design decisions, configuration surface, and runtime
semantics. For a field-by-field config and event reference, see
[SCHEMA.md](SCHEMA.md).

## 1. Overview

**Prenup** is an interactive, configuration-driven Git pre-commit hook utility.
It gives developers a fast, visible, and selective way to run quality checks
(tests, linters, doc generators, custom scripts) immediately before a commit
lands — scoped intelligently to only the parts of the repository that actually
changed.

Prenup targets repositories where running every check on every commit is too
slow or too noisy, and where a silent pre-commit hook either gets in the way
or gets bypassed. Prenup splits the difference: it shows the developer exactly
what is about to run, lets them choose, streams live results, and only blocks
the commit when something genuinely fails.

While usable in any repository, Prenup is purpose-built for **Go monorepos and
multi-module repositories**, where "what changed" naturally maps to one or
more Go modules. The module concept is generalized via configurable
`module_markers`, opening the door to polyglot repos without breaking the
core experience.

Prenup is designed to be used both by humans and by agentic tooling. Output is
TUI-first when run interactively, but auto-degrades to a structured markdown
digest when piped and can emit a stable NDJSON event stream on request.

## 2. Goals & Non-Goals

### Goals
- Give developers a single, declarative configuration (`.prenup.yaml`) that
  describes pre-commit checks for the whole team.
- Run only what is relevant to the current commit, scoped per changed module
  when appropriate and further narrowed by per-task path filters.
- Make the developer an active participant: visible task list, opt-in per
  task, live output, fast escape hatch.
- Block commits that fail real checks; never block on infrastructural issues
  (network, version check, stash failure, etc.).
- Automatically include generated artifacts (docs, mocks, generated code) in
  the commit so the developer doesn't have to remember `git add`.
- Make the pre-commit experience safe: tasks see exactly the content that
  will be committed, not a dirty worktree.
- Be usable by both humans and agentic tooling: the same binary, same
  behavior, different output shape.
- Stay lightweight: a single binary, no daemon, no shared state, no server.

### Non-Goals
- Not a general-purpose task runner or build system.
- Not a CI replacement.
- Does not generate or enforce repository structure beyond reading
  `.prenup.yaml` and the configured `module_markers`.
- Does not author commits or modify staged content beyond optionally
  `git add`-ing declared output files.
- Does not support Windows.

## 3. Target Users & Use Cases

### Primary users
- A developer in a Go repository (often a monorepo) who commits frequently
  and wants pre-commit feedback that is fast, selective, and obvious.
- An AI coding agent driving `git commit` from an automated loop, consuming
  prenup's markdown or JSON output to decide what to do next.

### Primary use cases
1. **Run the right tests, only.** Only the modules with changes get tested or
   linted, not the entire repo.
2. **Generated-file hygiene.** Automatically regenerate docs, mocks, or
   protobuf for changed modules and stage the regenerated files.
3. **Repo-wide guardrails.** Run a single check (e.g. CHANGELOG enforcement)
   once per commit.
4. **Interactive override.** Quickly skip a specific task for a one-off
   commit without disabling the hook.
5. **Safe checks.** Ensure tests run against the exact content being
   committed (not a dirty worktree).
6. **Agentic commits.** An agent runs `prenup run --output json --task "Run
   tests"` and parses the event stream to react to failures programmatically.

## 4. User Experience

### 4.1 Installation
- `go install github.com/c2fo/prenup/cmd/prenup@latest`
- `prenup init` scaffolds a starter `.prenup.yaml` based on simple repo
  inspection (Go modules, `.golangci.yml`, `Makefile`).
- `prenup install` writes `.git/hooks/pre-commit`. If another hook exists,
  the developer is offered `replace`, `chain`, or `force`. Non-interactive
  flags are available for automation; in non-interactive contexts (no TTY,
  or `--no-interactive`) `install` fails fast on conflict instead of
  hanging on a prompt. `--use-path` embeds `prenup` (resolved via `PATH`
  at commit time) instead of the absolute path of the binary that ran
  `install`, which is useful when the binary may move between installs.

After this, Prenup is invisible until the developer runs `git commit`.

### 4.2 Commit-time flow

1. **Version banner** — Prenup prints its version and a non-blocking notice
   when a newer release exists.
2. **Change discovery** — staged, unstaged, and untracked files minus
   `exclude` patterns.
3. **Module discovery** — the nearest ancestor with a configured marker
   (`module_markers`, default `[go.mod]`).
4. **Early exits (non-blocking)** — no relevant files or no modules →
   commit proceeds.
5. **Interactive task selection (human mode)** — version, update warning,
   module list, task checklist. Controls:
    - arrow keys to move
    - `space` to toggle
    - `enter` to confirm
    - `s` to skip all (commit proceeds)
    - `q` / `ctrl+c` to cancel (commit blocked)
6. **Non-interactive modes** — markdown and JSON modes skip the selection UI
   and run `default_selected` tasks (or the explicit `--task` set).
7. **Concurrency lock** — Prenup takes an OS-level advisory lock on
   `.git/prenup.lock`. A second concurrent `prenup run` against the same
   repo (e.g. a Git GUI that double-fires `git commit`) exits non-zero
   with a clear "another prenup run is already in progress" message
   instead of racing on the worktree. The lock auto-releases on process
   exit, including crashes. `--dry-run` skips the lock.
8. **Stash-and-restore** — when `clean_worktree: true` (default), unstaged
   changes are stashed before tasks run and restored afterward.
9. **Task execution** — tasks run sequentially; per-module tasks fan out
   across modules concurrently up to `max_parallelism`. Live output streams
   to the user. On first failure within a task, remaining modules for that
   task are skipped and execution continues with the next task.
10. **Output staging** — `stage_output: true` tasks get their
    `output_patterns` matches `git add`-ed post-success. The "before"
    snapshot is taken immediately before each such task, so files
    generated by an earlier task are not mis-attributed; for `per_module`
    tasks, staging is further restricted to files under the task's
    modules.
11. **Post-run summary** — version, update warning, full output, per-task
    status with durations.
12. **Commit decision** — zero if everything passed or was intentionally
    skipped; one otherwise.

### 4.3 Skip / bypass
- `s` during selection → commit proceeds with no tasks run.
- `q` / `ctrl+c` during selection → commit blocked.
- `q` / `ctrl+c` during execution → the running task receives `SIGTERM`
  with a short grace period to flush output and clean up before being
  forcefully killed; commit blocked if anything failed or did not
  complete.
- `git commit --no-verify` → Prenup not invoked at all.

### 4.4 Visibility
- Everything important prints to stdout; stderr is reserved for diagnostics.
- Version prints at startup and again in the post-run summary so it survives
  alt-screen teardown in wrapper clients.
- Task output streams live; the developer never waits for a long-running
  command to finish to see what it's doing.
- The TUI resizes dynamically and starts small to behave well in integrated
  terminals (VS Code, Windsurf) whose reported sizes can disagree.

## 5. Configuration

Single file, `.prenup.yaml` (or `.prenup.yml`), at the repository root.

Glob patterns in `exclude`, `paths`, `paths_ignore`, and `output_patterns`
are validated at load time; an invalid pattern fails the run with a
pointer to the offending field rather than silently never matching.
`prenup config validate` exposes the same check as a standalone command.

### 5.1 Top-level keys
- `version: 1` — required *integer* (not the string `"1"`). The config schema
  version; see [Config schema versioning](#511-config-schema-versioning).
- `module_markers` — filenames that define a module. Default `[go.mod]`.
- `exclude` — doublestar globs filtering change detection.
- `clean_worktree` — stash unstaged changes around task runs. Default `true`.
- `max_parallelism` — bound on per-module fan-out within a task. Default
  `0`, meaning `NumCPU`.
- `output` — default output mode: `auto`, `human`, `markdown`, `json`.
- `tasks` — the ordered task list.

#### 5.1.1 Config schema versioning

The `version` field is the config *schema* version: a plain integer,
deliberately **decoupled from the prenup release version**. It changes only
on a backward-incompatible change to the file format. Additive, non-breaking
changes (e.g. a new optional field) do **not** bump it and require no
migration — old configs keep working because new fields default sensibly.

Version handling on load:

- Matching version → parsed normally.
- Newer than the binary understands → rejected with a message directing the
  user to upgrade prenup.
- Missing or otherwise unsupported → rejected with an "unsupported version"
  error.

When a future breaking change introduces version 2, prenup will adapt older
configs in memory (so they keep running) and offer an **opt-in** command to
rewrite the file — it will never rewrite a user's config without consent.
That machinery is intentionally deferred until there is a second version to
convert from.

### 5.2 Per-task fields
- `name` *(required)* — display name.
- `default_selected` — pre-checked in the selection UI.
- `command` *(required)* — `bash -c`-executed command with template
  expansion.
- `per_module` — run once per changed module (default working dir is the
  module root) or once globally.
- `working_dir` — override for the command's working directory; template
  variables supported.
- `paths` / `paths_ignore` — doublestar filters on the changed-file set the
  task applies to. When no files match, the task is skipped.
- `output_patterns` — files the task may create; used by `stage_output`.
- `stage_output` — stage newly-created matches of `output_patterns`. The
  before-snapshot is taken per task, and for `per_module` tasks staging
  is further restricted to files under the task's modules so a generator
  in module `a/` will not auto-stage files produced under module `b/`.
- `parallel` — whether per-module iterations may run concurrently. Defaults
  to `true` for per-module tasks.
- `clean_worktree` — override the repo-level default.
- `env` — map of environment variables to inject.

### 5.3 Template variables

| Variable | Description |
|---|---|
| `{{.repo_root}}` | Absolute path to the repo root. |
| `{{.module_root}}` | Absolute path to the current module. |
| `{{.module_path}}` | Relative path to the module from the repo root. |
| `{{.module_name}}` | Basename of the module directory. |

## 6. Behavioral Requirements

### 6.1 Change detection
- Staged, unstaged, and untracked files are all considered "changed" for
  triggering and module discovery.
- `exclude` uses doublestar globs.
- Per-task `paths` / `paths_ignore` further restrict which files a task
  sees; this also restricts the module set `per_module` tasks fan out
  across.
- A file is "relevant" if it is changed and not excluded.

### 6.2 Module detection
- A module is the nearest ancestor containing any `module_markers` file.
- Modules are deduplicated and sorted.
- If no modules are detected, Prenup exits zero with an explanatory message.
- `--all` bypasses change detection and feeds a synthetic `.` module.

### 6.3 Task scheduling
- Tasks run sequentially in configuration order.
- Per-module iterations within a task fan out concurrently when
  `parallel: true` and `max_parallelism > 1`, bounded by the configured cap.
- The first module-level failure in a task skips remaining modules for that
  task and cancels their in-flight work; execution continues with the next
  task.
- Cancellation (Ctrl-C, parent timeout, fail-fast within a task) sends
  `SIGTERM` to the running command and allows a short grace period for
  it to flush output and exit cleanly before forcing termination.

### 6.4 Stash-and-restore
- When `clean_worktree: true`, Prenup runs `git stash push --include-untracked
  --keep-index` before tasks begin and pops the stash on exit.
- Failure to stash emits a notice but does not block the run.
- Per-task `clean_worktree` overrides the repo-level default.

### 6.5 Output staging
- Before *each* `stage_output` task runs, Prenup snapshots the set of
  tracked files. The per-task snapshot prevents files generated by an
  earlier task in the same run from being attributed to this one.
- After success, files that are newly present in `git status` and match any
  `output_patterns` are `git add`-ed.
- Pre-existing unstaged changes are not promoted.
- For `per_module` tasks, staging is further restricted to files under the
  task's modules.

### 6.6 Exit codes
- `0` — commit proceeds: nothing to do, user skipped via `s`, all tasks
  succeeded or were intentionally skipped.
- `1` — commit blocked: config error (including invalid glob patterns),
  change detection error, user cancel, any task failure, or another
  `prenup run` already holding the per-repo lock.

### 6.7 Version check
- Best-effort, short-timeout GitHub Releases query.
- Tokens picked up in order: `PRENUP_GITHUB_TOKEN`, `GITHUB_TOKEN`,
  `GH_TOKEN`. Required only for private repos.
- Network, auth, or format failures are swallowed silently.
- When a newer version exists, a single-line warning is displayed.

### 6.8 Concurrency safety
- Before any tasks run, Prenup acquires a non-blocking OS-level advisory
  lock on `.git/prenup.lock`.
- A second concurrent `prenup run` against the same repository exits
  non-zero with a clear "another prenup run is already in progress"
  message including the lock-file path; it does not wait.
- The lock auto-releases on process exit, including crashes and
  `kill -9`, so there is no stale-PID file to clean up.
- `--dry-run` skips the lock so a planning probe never blocks a real
  commit-time run.

## 7. Output modes

The same run produces identical events; the rendering differs:

- **human** — Bubble Tea TUI with a live task checklist and scrolling output
  viewport. Post-run summary printed after alt-screen exit.
- **markdown** — plain-text streaming output during execution followed by a
  structured markdown digest:
  - `[prenup] …` self-describing preamble identifying the tool, linking
    to docs, and pointing at the structured (`--output json`) mode
  - `## Prenup vX.Y.Z` header
  - summary counts
  - `### Task: ...` sections with status, duration, modules, and tail of
    output on failure
  - "Next steps" block on failure that disambiguates prenup-vs-git
    attribution and documents the `--no-verify` bypass
- **json** — NDJSON event stream on stdout; one JSON object per line. Stable
  schema documented in [docs/SCHEMA.md](docs/SCHEMA.md). The very first
  line is an `agent_hint` event carrying the same orienting context the
  markdown preamble provides, so a cold-start consumer can identify the
  stream without prior knowledge of prenup.

Auto-detection: TTY → `human`; piped or `NO_COLOR` or `TERM=dumb` →
`markdown`; `json` is always explicit.

The agent-orienting strings and the `agent_hint` payload are intentionally
absent from the human TUI: an interactive operator already knows what
prenup is and would only see them as noise.

## 8. Constraints & Assumptions

- The host has `git` and `bash` on `PATH`.
- The current directory is inside a git repository.
- `.prenup.yaml` lives at the repository root.
- Commands run via `bash -c`; shell features are available.
- Linux and macOS only.

## 9. Success Criteria

1. Developers leave the hook installed rather than disabling it.
2. Trivial commits complete in seconds.
3. CI failures that a pre-commit task could have caught drop noticeably after
   adoption.
4. Teams converge on a single shared `.prenup.yaml`.
5. Agent-driven workflows successfully parse `--output json` and react to
   failures without needing a human in the loop.

## 10. Out of scope / future considerations

See [docs/FUTURE.md](docs/FUTURE.md) for a full catalog. Highlights:

- Task-level DAG parallelism with `depends_on`.
- Aggregated failure summary across modules within a task.
- Caching / incremental skip for deterministic tasks.
- Plugin ecosystem beyond `module_markers`.
- `prenup stats` history and timing.
- Per-developer overrides layered on the team config.
- Windows support.
