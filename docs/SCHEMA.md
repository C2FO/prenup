# Prenup schemas

This document describes the two stable contracts prenup exposes: the config
file schema (currently at `version: 1`) and the JSON event stream schema.

## Config schema

The authoritative machine-readable schema is
[assets/prenup.schema.json](../assets/prenup.schema.json). Editors with JSON
Schema support (VS Code YAML, IntelliJ, Zed) will offer autocomplete and
validation against it.

### Top-level fields

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | integer | — (required) | Config schema version; must be the integer `1`. Quoted strings (`"1"`) are rejected with a hint. |
| `module_markers` | string[] | `[go.mod]` | Filenames whose presence marks a module directory. |
| `exclude` | string[] | `[]` | Doublestar globs filtering change detection. |
| `clean_worktree` | bool | `true` | Stash unstaged changes around task execution. |
| `max_parallelism` | int | `0` | Cap on per-module fan-out. `0` means `NumCPU`. |
| `output` | string | `auto` | Default output mode. One of `auto`, `human`, `markdown`, `json`. |
| `tasks` | task[] | — (required) | Ordered task list. |

### Task fields

| Field | Type | Description |
|---|---|---|
| `name` | string (required) | Display name and dedup key. |
| `command` | string (required) | Shell command, run via `bash -c`. |
| `default_selected` | bool | Pre-checked in the selection UI. |
| `per_module` | bool | Run once per changed module (default working dir is the module root). |
| `working_dir` | string | Override for the command working directory. Template variables supported. |
| `paths` | string[] | Doublestar globs; restrict the task's changed-file set. Empty = all. |
| `paths_ignore` | string[] | Doublestar globs; files to exclude from the task's set. |
| `output_patterns` | string[] | Globs used by `stage_output` to identify generated files. |
| `stage_output` | bool | After success, `git add` files matching `output_patterns` that are newly present. |
| `parallel` | bool | Whether per-module iterations may run concurrently. Defaults to `true` for `per_module` tasks. |
| `clean_worktree` | bool | Override the repo-level default. |
| `env` | map[string]string | Environment variables injected for the task. |

### Template variables

Available inside `command` and `working_dir`:

- `{{.repo_root}}` — absolute path to the repo root.
- `{{.module_root}}` — absolute path to the current module.
- `{{.module_path}}` — module path relative to the repo root.
- `{{.module_name}}` — basename of the module.

## JSON event stream

`prenup run --output json` emits NDJSON: one JSON object per line on stdout.
The stream is stable: additive changes are permitted; fields may be added,
but existing fields keep their meaning and type across minor versions.

`prenup plan --output json` is **not** NDJSON; it emits a single
pretty-printed JSON document. See "Plan output" below.

### Common fields

| Field | Type | Notes |
|---|---|---|
| `type` | string | Event kind (see below). |
| `time` | RFC3339 timestamp | Present on every runner event. Omitted from the bootstrap `agent_hint` line, which is a static self-describing header rather than a timestamped event. |

### Event kinds

#### `agent_hint`
```json
{"type":"agent_hint","schema":"1","tool":"prenup","description":"Prenup is a Git pre-commit hook runner ...","homepage":"https://github.com/c2fo/prenup","hook_context":"This output is produced by the prenup pre-commit hook ...","bypass_hint":"Re-run `git commit` after fixing the issue, or use `git commit --no-verify` ...","stream_format":"ndjson","event_types_note":"Subsequent lines are runner events: ..."}
```
- Always the **first** line of the stream, emitted before any runner event.
- Self-describing bootstrap so a consumer that has no prior knowledge of
  prenup can identify the tool, find its docs, learn the stream format, and
  know how to recover on failure -- all from the first line of output.
- `schema` versions the agent_hint payload itself; bumped when the shape
  of the strings or surrounding fields changes.
- Consumers that don't care can skip any line whose `type` they don't
  recognize and continue parsing as normal.
- **Not** emitted from `prenup plan --output json` (that command produces
  a single JSON document, not an event stream).

#### `run_started`
```json
{"type":"run_started","time":"...","version":"v0.1.0","repo_root":"/path/to/repo","modules":["pkg/foo"],"tasks":["Run tests"],"message":"Update available v0.2.0 ..."}
```
- `repo_root` — absolute path of the git repository prenup was invoked
  from. Anchors every subsequent task's `working_dir` so consumers do
  not have to infer the root from substring matches.
- `modules` — final module list after exclude/path filters.
- `tasks` — tasks that will attempt to run.
- `message` — optional update-notice string; never blocks the run.

#### `task_started`
```json
{"type":"task_started","time":"...","task":"Run tests","module":"pkg/foo","command":"go test ./...","working_dir":"/repo/pkg/foo"}
```
- Emitted once per module per task invocation.
- `command` is the resolved shell command after template expansion
  (`{{.repo_root}}`, `{{.module_root}}`, etc.). A consumer can copy/paste
  it to reproduce the run without parsing `.prenup.yaml`.
- `working_dir` is the absolute path the command was run in. For
  per-module tasks it is the module root; otherwise it is the value of
  the task's `working_dir` (template-expanded) or the repo root.
- Both fields are omitted when empty (e.g. malformed task config).

#### `line`
```json
{"type":"line","time":"...","task":"Run tests","module":"pkg/foo","stream":"stdout","text":"ok  pkg/foo 0.12s"}
```
- `stream` is `stdout` or `stderr`.
- `text` is one unredirected line; no trailing newline.
- Lines exceeding ~1 MB are split and a synthetic
  `[prenup] output truncated: ... (line exceeded 1048576 bytes)` line is
  emitted on the same stream, so consumers can tell that the source produced
  an oversized line rather than silently losing data.

#### `task_completed`
```json
{"type":"task_completed","time":"...","task":"Run tests","status":"done","duration_ms":120}
```
- `status` is `done`, `failed`, or `skipped`.
- Emitted once per task after all its module iterations complete or are
  skipped.
- For `skipped`, `message` may describe why.

#### `notice`
```json
{"type":"notice","time":"...","message":"failed to restore stash: ..."}
{"type":"notice","time":"...","task":"Run tests","module":"pkg/b","message":"fail-fast: sibling module failed"}
```
- Non-fatal diagnostic messages. Does not affect exit code.
- May carry `task` and `module` fields. In particular, when one module of a
  `per_module` task fails, the remaining modules for that task are reported
  via per-module `notice` events so consumers can see exactly which modules
  were aborted by fail-fast.

#### `run_completed`
```json
{"type":"run_completed","time":"...","succeeded":1,"failed":1,"exit_code":1,"failed_tasks":["Run tests"]}
```
- Always the last event. `exit_code` matches the process exit code.
- `failed_tasks` — names of tasks that ended in `failed` status, in
  selection order. Omitted when no task failed. Lets a consumer index
  the failures in O(1) without rescanning every `task_completed` event.

### Plan output (`prenup plan --output json`)

Not NDJSON: a single pretty-printed JSON document describing the plan.

```json
{
  "repo_root": "/abs/path",
  "modules": ["pkg/foo"],
  "tasks": [
    {
      "name": "Run tests",
      "selected": true,
      "per_module": true,
      "clean_worktree": true,
      "parallel": true,
      "modules": ["pkg/foo"]
    }
  ]
}
```

## Stability

- The event `type` values are final.
- Existing fields may be extended with new enum values only in major
  versions.
- New fields may be added at any time; consumers must ignore unknown fields.
- The config `version` integer tracks schema major versions.
- The embedded schema (consulted by `prenup config validate` and exported by
  `prenup config schema`) and the public asset at
  [`assets/prenup.schema.json`](../assets/prenup.schema.json) are guaranteed
  to be byte-identical: a CI test fails the build if they ever diverge.
  External consumers can safely fetch the asset URL without worrying about
  drift from the binary's view of the schema.
