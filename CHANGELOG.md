# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- Embedded JSON Schema (config `version: 1`) for editor `$schema` integration,
  published at `assets/prenup.schema.json`.
- GitHub Releases API version check with a one-line update notice; honors
  `PRENUP_GITHUB_TOKEN`, `GITHUB_TOKEN`, or `GH_TOKEN` for authenticated
  requests.
