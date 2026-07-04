![prenup.png](prenup.png)

# Prenup — Interactive Pre-Commit Hook Utility

Prenup runs user-defined tasks (tests, linters, doc generators, custom scripts)
as a Git pre-commit hook. It is designed to be fast, selective, and visible:
only the modules that actually changed get checked, task output streams live,
and the developer stays in the loop through a simple interactive UI.

Prenup provides dedicated subcommands (`install`, `init`, `run`, `plan`,
`migrate`), per-task path filtering, per-module parallelism, stash-and-restore
for safer checks, and agent-friendly output modes (markdown digest when piped,
NDJSON with `--output json`).

## Quick start

```bash
# Install the binary.
go install github.com/c2fo/prenup/cmd/prenup@latest

# Inside your repo:
prenup init      # scaffold .prenup.yaml
prenup install   # write .git/hooks/pre-commit
```

You commit. Prenup runs. Pass or fail, you see why.

## Subcommands

| Command | Purpose |
|---|---|
| `prenup` | Default: run as the Git pre-commit hook. Equivalent to `prenup run`. |
| `prenup run [flags]` | Run the pre-commit checks without committing. |
| `prenup plan` | Print the plan for the current change set without executing. |
| `prenup install` | Install `.git/hooks/pre-commit`. Prompts on conflicts. |
| `prenup uninstall` | Remove the managed hook, restoring backups when present. |
| `prenup init` | Scaffold a starter `.prenup.yaml` from repo inspection. |
| `prenup migrate` | Convert a v1 `.prenup.yml` into a v2 `.prenup.yaml`. |
| `prenup config validate [path]` | Validate a config against the v2 schema. |
| `prenup config schema` | Print the embedded JSON schema. |
| `prenup version` | Print the binary version. |

### `prenup run` flags

- `--config PATH` — explicit config path.
- `--output MODE` — `auto`, `human`, `markdown`, or `json`. Default `auto`.
- `--task NAME` — run only the named task(s); repeatable, non-interactive.
- `--all` — ignore change detection; run against the full repo scope.
- `--no-interactive` — skip the selection UI; run all `default_selected` tasks.
- `--no-clean-worktree` — disable stash-and-restore.
- `--parallelism N` — cap per-module fan-out (0 = `NumCPU`).
- `--dry-run` — report what would run without executing.

### `prenup install` conflict handling

If a `pre-commit` hook already exists, `install` prompts for one of:

- **replace** — back up the existing hook and write the prenup hook.
- **chain** — move the existing hook to `pre-commit.local`; the prenup hook
  runs it first on every commit.
- **force** — overwrite without a backup.
- **abort** — leave everything alone.

Non-interactive shells can pass `--force`, `--replace`, `--chain`, or
`--non-interactive` directly. When stdin is not a TTY, `install` refuses to
prompt and surfaces the conflict so CI scripts fail loudly instead of hanging.

Use `--use-path` to record `prenup` (resolved via `$PATH` at commit time)
inside the hook script instead of the absolute path of the binary that ran
`install`. This is the right choice for shared dotfiles or any setup where
the binary may live in different locations across machines.

## Output modes

- **human** — full-screen Bubble Tea UI with a live task checklist and a
  scrolling output viewport. Selected automatically when stdout is a TTY.
- **markdown** — plain streaming output during execution, followed by a
  structured markdown digest summarizing each task. Selected automatically
  when stdout is piped, when `NO_COLOR` is set, or when `TERM=dumb`. This is
  the default for CI logs and for LLM-readable post-run summaries.
- **json** — one JSON object per line (NDJSON). Stable schema, safe for
  agents and tooling. Always explicit: `--output json`.

Both `markdown` and `json` modes lead with a self-describing preamble so
an AI agent (or operator) that has never heard of prenup can identify the
tool, find docs, and recognize hook-attributed failures from the very
first line of output. In `markdown` it is a `[prenup] …` block; in `json`
it is a single `agent_hint` event before the runner stream begins. On
failure, the markdown digest's "Next steps" block also disambiguates
prenup-vs-git attribution and documents the `--no-verify` bypass.

See [docs/SCHEMA.md](docs/SCHEMA.md) for the JSON event schema (including
the `agent_hint` bootstrap line).

## Configuration

Configuration lives at the repository root as `.prenup.yaml` (or `.prenup.yml`).

```yaml
version: 2
module_markers:
  - go.mod
exclude:
  - ".github/**"
  - "**/*.yaml"
clean_worktree: true
max_parallelism: 0
output: auto

tasks:
  - name: "Run tests"
    default_selected: true
    command: "go test ./..."
    per_module: true
    paths:
      - "**/*.go"
    paths_ignore:
      - "**/*_mock.go"

  - name: "Generate CLI docs"
    default_selected: true
    per_module: false
    command: "make docs"
    output_patterns:
      - "doc/cmd/**/*.md"
    stage_output: true
```

See [prenup.example.yaml](prenup.example.yaml) for a larger example and
[docs/SCHEMA.md](docs/SCHEMA.md) for the full field reference.

`prenup config validate` (and `prenup run` itself) checks structural fields
and the syntax of every doublestar pattern in `exclude`, `paths`,
`paths_ignore`, and `output_patterns`. Invalid patterns are reported with
the offending value and field path so they can be fixed before they silently
fail to match anything at runtime.

### Template variables

Available inside `command` and `working_dir`:

| Variable | Description |
|---|---|
| `{{.repo_root}}` | Absolute path to the repo root. |
| `{{.module_root}}` | Absolute path to the current module. |
| `{{.module_path}}` | Module path relative to the repo root. |
| `{{.module_name}}` | Basename of the module directory. |

## Behavior

1. **Change discovery** — staged, unstaged, and untracked files (filtered by
   repo-level `exclude` patterns).
2. **Module detection** — the nearest ancestor directory of each changed file
   that contains any `module_markers` file.
3. **Task planning** — tasks filter their relevant files via `paths` /
   `paths_ignore` (doublestar globs). `per_module: true` tasks fan out across
   the resulting module set.
4. **Selection** — in human mode, an interactive checklist. In other modes,
   `default_selected` tasks run automatically; `--task` overrides.
5. **Concurrency lock** — before any tasks run, prenup takes an OS-level
   advisory lock on `.git/prenup.lock`. A second concurrent `prenup run`
   against the same repo (e.g. a Git GUI that double-fires `git commit`)
   exits non-zero with a clear "another prenup run is already in progress"
   message instead of racing on the worktree. The lock auto-releases on
   process exit, including crashes. `--dry-run` skips the lock.
6. **Stash-and-restore** — when `clean_worktree: true`, unstaged changes are
   stashed with `--keep-index --include-untracked` before tasks run, and
   restored afterward.
7. **Execution** — tasks run sequentially; a per-module task fans out across
   modules concurrently (bounded by `max_parallelism`). The first failure
   within a task skips remaining modules for that task and continues to the
   next task.
8. **Output staging** — tasks that declare `stage_output: true` have
   newly-created files matching `output_patterns` automatically `git add`-ed.
   The "before" snapshot is taken immediately before each such task runs, so
   one task cannot accidentally stage files generated by an earlier task in
   the same run. For `per_module` tasks, staging is further scoped to the
   task's modules: a task running in module `a/` will not auto-stage files
   produced under module `b/`.
9. **Cancellation** — when prenup is interrupted (Ctrl-C, parent timeout,
   etc.) the running task receives `SIGTERM` and is given a short grace
   period to flush output and clean up before being forcefully killed.
10. **Exit** — non-zero if any task failed; zero otherwise.

## Version check

Each invocation queries the GitHub Releases API for the latest `v*` tag
with a short timeout. A newer version triggers a one-line update notice.
Failures are silent and never block a commit.

For private repositories or to avoid unauthenticated rate limits, set one
of `PRENUP_GITHUB_TOKEN`, `GITHUB_TOKEN`, or `GH_TOKEN`.

## Migrating from v1

Run `prenup migrate` in the root of a repo with a v1 `.prenup.yml`:

```bash
prenup migrate                       # writes .prenup.yaml beside .prenup.yml
prenup migrate --in path --out path  # explicit locations
```

Field names are preserved. New v2 features (`paths`, `paths_ignore`,
`module_markers`, `parallel`, `env`, `clean_worktree`) are not invented for
you; add them manually where useful. `prenup config validate` confirms the
migrated file parses cleanly.

## Documentation

- [docs/PRD.md](docs/PRD.md) — product requirements and behavior spec.
- [docs/SCHEMA.md](docs/SCHEMA.md) — config schema and JSON event stream.
- [docs/FUTURE.md](docs/FUTURE.md) — deferred improvements.
- [CHANGELOG.md](CHANGELOG.md) — release history.

## Platform support

Prenup targets Linux and macOS. Windows support is deliberately out of scope;
see [docs/FUTURE.md](docs/FUTURE.md).

## Contributing

Issues and pull requests are welcome. See the [Contributor Covenant Code of
Conduct](CODE_OF_CONDUCT.md). All PRs must update the `[Unreleased]` section
of [CHANGELOG.md](CHANGELOG.md); version numbers are assigned automatically at
release time.

## License

MIT. See [LICENSE.md](LICENSE.md).
