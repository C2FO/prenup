# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
