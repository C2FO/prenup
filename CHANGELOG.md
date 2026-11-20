# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Fixed
- `prenup install` and `prenup uninstall` now work from linked git worktrees
  and honor `core.hooksPath`. The hooks directory is resolved with
  `git rev-parse --git-path hooks` instead of being assumed to be
  `<repo>/.git/hooks`, and the printed install path is the real one
  ([#5](https://github.com/C2FO/prenup/issues/5)).
- Tasks no longer inherit git's repository-local environment variables
  (`GIT_DIR`, `GIT_INDEX_FILE`, `GIT_WORK_TREE`, and the rest of
  `git rev-parse --local-env-vars`) that git exports to hooks. Previously, git
  commands run by a task, including from `per_module` directories or in
  freshly initialized repositories, could operate on the committing worktree's
  metadata. As a result, a task that inspects the index (for example
  `git diff --cached`) during a partial commit now sees the real index rather
  than git's temporary one. Per-task `env:` entries are still applied and can
  set these variables explicitly
  ([#6](https://github.com/C2FO/prenup/issues/6)). `GIT_CONFIG_COUNT` and
  `GIT_CONFIG_PARAMETERS` are left intact so user-supplied git configuration
  (for example credential or `insteadOf` rewrites) still reaches tasks, and
  names are matched case-insensitively on Windows.
- `prenup install` and `prenup uninstall` print a notice on stderr when
  `core.hooksPath` points outside the repository, since the hook is then
  shared by every repository using that directory
  ([#5](https://github.com/C2FO/prenup/issues/5)).

## [[v0.1.2](https://github.com/C2FO/prenup/releases/tag/v0.1.2)] - 2026-11-20
### Fixed
- CLI subcommand and configuration errors are now written to `stderr` instead
  of exiting silently when `SilenceErrors` is enabled on the root command
  ([#4](https://github.com/C2FO/prenup/issues/4)).

## [[v0.1.1](https://github.com/C2FO/prenup/releases/tag/v0.1.1)] - 2026-08-14
### Fixed
- `prenup run` now cancels its context on SIGINT/SIGTERM instead of letting
  the OS terminate the process outright. Previously, an external kill (a
  CI/editor timeout, a git GUI's "cancel" button, etc.) skipped all deferred
  cleanup, so the `clean_worktree` stash was never popped, leaving an
  orphaned stash that surfaced as a confusing conflict later. A second
  SIGINT/SIGTERM still forces immediate termination.
- Once a run is canceled this way, any task that hadn't started yet is now
  reported as a single, clear "run interrupted" skip instead of a wall of
  misleading per-task "context canceled" failures.

## [[v0.1.0](https://github.com/C2FO/prenup/releases/tag/v0.1.0)] - 2026-07-04
### Fixed
- `prenup plan --output` help text no longer advertises unsupported
  `auto`/`human`/`markdown` modes; only `text` (default) and `json` are
  implemented.
- `docs/SCHEMA.md` no longer claims the `time` field is present on every
  NDJSON line — the bootstrap `agent_hint` line is a static header and
  intentionally omits it.
- Version checker requests the GitHub Releases API with `?per_page=100`
  so repos accumulating many releases (or non-semver tags) don't cause
  the latest valid semver release to fall off the first page.
- Rate-limit exhaustion errors now render the `X-RateLimit-Reset` header
  as an RFC3339 UTC time plus a "resets in" duration, instead of leaking
  the raw Unix epoch integer to the operator.
- Internal comments in `internal/ui/agent.go` and
  `internal/ui/jsonout/jsonout.go` (including the agent-facing
  `event_types_note` hint) now point at `docs/SCHEMA.md`, matching the
  actual in-repo path.
- Repo lock now resolves a *relative* `gitdir:` target in a `.git` file
  (git worktrees/submodules) against the repository root instead of the
  current working directory, so the lock lands in the real git metadata
  directory.
- Human-mode post-run summary bounds its retained output to the most
  recent lines (keeping a tail and noting how many were dropped) so a
  noisy task can't grow memory without limit during a hook run.
- Version-check requests now send a `User-Agent` header, which the GitHub
  REST API expects (some environments 403 without it).
- `lock` package now compiles on non-unix platforms via a build-tagged
  no-op `flock` stub; advisory locking remains unix-only by design, as
  documented.
- Corrected the `lock.Acquire` godoc, which incorrectly stated that
  `Close` removes the lock file (it intentionally does not).

### Added
- Initial open-source release.
- Interactive, configuration-driven Git pre-commit hook runner.
- Subcommands: `run`, `plan`, `install`, `uninstall`, `init`,
  `config validate`, `config schema`, `version`.
- Per-task path filtering with doublestar globs (`paths`, `paths_ignore`,
  `exclude`).
- Per-module change discovery via configurable `module_markers`, with
  bounded per-module concurrency for `per_module` tasks.
- Stash-and-restore of unstaged changes (`clean_worktree`) so tasks see
  exactly what will be committed.
- OS-level advisory lock on `.git/prenup.lock` to prevent concurrent
  `prenup run` invocations from racing on the worktree.
- Output modes: interactive Bubble Tea UI (`human`), streaming markdown
  digest (`markdown`), and NDJSON event stream (`json`) with a leading
  `agent_hint` bootstrap line for LLM/agent consumers.
- Automatic staging of newly-generated files matching `output_patterns`
  for tasks that declare `stage_output: true`.
- Graceful cancellation: SIGTERM with grace period on Ctrl-C or parent
  timeout, then SIGKILL if the task does not exit.
- Template variables in `command` and `working_dir`: `{{.repo_root}}`,
  `{{.module_root}}`, `{{.module_path}}`, `{{.module_name}}`.
- Embedded JSON Schema (config `version: 1`) for editor `` integration,
  published at `assets/prenup.schema.json`.
- GitHub Releases API version check with a one-line update notice; honors
  `PRENUP_GITHUB_TOKEN`, `GITHUB_TOKEN`, or `GH_TOKEN` for authenticated
  requests.
