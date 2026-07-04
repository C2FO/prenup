# Open-Sourcing `prenup` as `github.com/c2fo/prenup`

**Status:** Approved
**Date:** 2026-07-03
**Owner:** @jjudd

## Goal

Extract `prenup` from the internal `ep-tools` monorepo into a standalone open-source
repository at `github.com/c2fo/prenup`, using `github.com/c2fo/releasegen` as the
scaffolding template. Ship as `v0.1.0`.

## Source, Target, Reference

- **Source (internal):** `/Users/john.judd/gitprojects/ep-tools/prenup`
  (module `github.com/c2fo/ep-tools/prenup/v2`)
- **Target (fresh repo, remote pre-configured):** `/Users/john.judd/gitprojects/prenup`
  (module will be `github.com/c2fo/prenup`)
- **Reference (scaffolding template):** `/Users/john.judd/gitprojects/releasegen`

## Design

### 1. Repo bootstrap — fresh history

Copy the working tree from `ep-tools/prenup` into `gitprojects/prenup`. Do not
carry any internal git history; the first commit on `main` is the open-source
baseline. Overwrite the stub `README.md`. Exclude `.git`, IDE files, and any
build artifacts.

### 2. Module + import-path rename

Rename `github.com/c2fo/ep-tools/prenup/v2` → `github.com/c2fo/prenup` (drop the
`/v2` major-version suffix since we're resetting to `v0.1.0`). Rewrite every
import in `.go` sources, update `go.mod`, update `.mockery.yaml`, regenerate
mocks, run `go mod tidy`, and confirm `go build ./...` and `go test ./...` still
pass.

### 3. Scrub internal references

Based on the audit report, apply these changes:

**Code:**
- `internal/versioncheck/versioncheck.go`: `repoOwner="c2fo"`, `repoName="prenup"`,
  `tagPrefix="v"` (was `C2FO`/`ep-tools`/`prenup/`).
- `internal/cli/version.go`: remove `strings.TrimPrefix(..., "prenup/")` calls
  (module version strings will now be plain `vX.Y.Z`).
- `internal/ui/agent.go`: `HomepageURL = "https://github.com/c2fo/prenup"`.
- `internal/config/migrate.go`: update the docs-URL header comment.
- `internal/cli/run.go`: update the install-command hint string
  (`go install github.com/c2fo/prenup/cmd/prenup@latest`).
- `internal/versioncheck/versioncheck_test.go`: update fixture URLs to
  `c2fo/prenup`; remove or repoint the `rowtater` / `releasegen` sibling-tag
  fixtures.

**Config/schemas/assets:**
- `assets/prenup.schema.json` + `internal/config/schema.json`: update `$id` to
  `https://raw.githubusercontent.com/c2fo/prenup/main/assets/prenup.schema.json`.
- `.mockery.yaml`: update package path.
- `prenup.example.yaml`: replace the `.github/workflows/scripts/changelog.sh` and
  `make docs` tasks with generic standalone examples (e.g. `go test`, `go vet`,
  a `releasegen validate` hook — same pattern as sibling projects).

**Docs:**
- `README.md`: rewrite for a public audience — what prenup is (git-hook manager),
  install via `go install`, quickstart, config reference. Fix the
  `../LICENSE` reference to `LICENSE.md` in-repo.
- `docs/PRD.md`, `docs/V2_PLAN.md`, `docs/SCHEMA.md`, `docs/FUTURE.md`: scrub
  monorepo-relative paths (`prenup/README.md` → `README.md`), update module
  paths and URLs. Consider whether `V2_PLAN.md` is worth keeping in the public
  repo (historical rewrite plan); if not, drop it.
- `cmd/prenup/main.go` comment: fix monorepo-relative doc paths.
- `internal/config/schema_test.go`: fix the byte-identity error message paths.

### 4. Open-source scaffolding (from releasegen)

Add these files, adapted for prenup:

| File | Adaptation |
| --- | --- |
| `LICENSE.md` | Copy verbatim (MIT, C2FO Inc.) |
| `CODE_OF_CONDUCT.md` | Copy verbatim |
| `.golangci.yml` | Copy; change `prefix` / `local-prefixes` to `github.com/c2fo/prenup` |
| `.testcoverage.yml` | Copy; leave thresholds — tune after first CI run |
| `.github/ISSUE_TEMPLATE/{bug_report,feature_request,custom}.md` | Copy; retarget prenup |
| `.github/workflows/ci.yml` | Copy verbatim |
| `.github/workflows/golangci-lint.yml` | Copy verbatim |
| `.github/workflows/codeql.yml` | Copy verbatim |
| `.github/workflows/go-test-coverage.yml` | Copy verbatim |
| `.github/workflows/validate-changelog.yml` | Copy verbatim (already uses `releasegen validate`) |

Note: **not** copying releasegen's `deployments/Dockerfile` — prenup does not
ship a container.

### 5. Release workflow — vfs-style (tag-only)

Copy `.github/workflows/releasegen.yml` from **vfs**, not releasegen. Rationale:
releasegen's own workflow builds itself and pushes a Docker image (self-release
special case); vfs's workflow just runs the pre-built
`ghcr.io/c2fo/releasegen:v1` image to cut a tag and GitHub release from the
changelog — which is exactly what prenup needs.

Also add a `.releasegen.yaml` at the repo root (adapted from releasegen's), so
the release and validate workflows share one source of truth for change-type
config.

**Distribution:** users install via `go install github.com/c2fo/prenup/cmd/prenup@latest`.
No Docker image, no GoReleaser binaries.

### 6. CHANGELOG reset

Replace the entire `CHANGELOG.md` with:

```markdown
# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-07-04

### Added
- Initial open-source release of `prenup`, a git pre-commit hook manager.
- `run`, `plan`, `install`/`uninstall`, `init`, `migrate`, `config`, `version`
  commands.
- v2 YAML configuration schema with published JSON Schema for editor support.
- Change detection with module-marker discovery for monorepo-friendly parallel
  execution.
- Human (Bubble Tea TUI), NDJSON, and markdown output modes.
- Update check against GitHub Releases.
```

The internal `v2.x` history is dropped entirely.

### 7. Dogfooding config

Copy the releasegen `.prenup.yaml` template, adapted for a Go project of
prenup's shape (lint, test, changelog-validate). This makes prenup use itself.

## Validation

Before pushing to `main`:

1. `go build ./...` and `go test ./...` pass in the new repo.
2. `golangci-lint run` passes.
3. `grep -r "ep-tools\|C2FO/ep-tools" .` returns nothing (outside `docs/`
   historical mentions that we consciously keep, if any).
4. The scaffolding workflows are syntactically valid (YAML parse).
5. A manual local `prenup run` on the new repo succeeds (dogfood).

## Out of scope

- Docker image publishing.
- Cross-platform binary release (GoReleaser).
- CONTRIBUTING.md, SECURITY.md, Makefile (neither reference project has them).
- Rewriting the internal `docs/V2_PLAN.md` — will drop it unless there's a
  reason to keep it public.
- Any code refactoring beyond what's needed to remove monorepo assumptions.

## Addendum (2026-07-04): config versioning redesign

Decisions made after the initial extraction, before the first public commit:

1. **Drop the `migrate` command.** The v1→v2 config migration only ever
   applied to the internal ep-tools format; no public v1 config exists. Remove
   `internal/cli/migrate.go`, `internal/config/migrate.go` (+ tests), the CLI
   registration, and all migration docs. Relocate the still-needed `Marshal`
   helper from `migrate.go` into `config.go`.

2. **Reset the public config baseline to `version: 1`.** The first public
   config format is version 1 (was `version: 2` from the internal history).
   Update the schema (`const: 1`), validator, defaults, example config, and
   docs.

3. **Config schema version is a plain integer, decoupled from the prenup
   release version.** It increments *only* on a breaking config-format change.
   Additive, non-breaking changes require neither a version bump nor a
   migration — new optional fields default sensibly and old configs keep
   working.

4. **Version handling now (minimal):**
   - Accept `version: 1`.
   - Reject a *newer* version (e.g. `version: 2` seen by an older binary) with
     a clear "this config requires a newer version of prenup" error.
   - Reject unknown/missing/older-than-1 with the existing "unsupported
     version" style error.
   - Defer any in-memory adaptation layer and a `config upgrade` command until
     `version: 2` actually exists (nothing to convert from yet). The policy is
     documented in DESIGN.md so the contract is set.

5. **Discoverability header.** Scaffolded configs (`prenup init`) lead with a
   comment block identifying the tool and linking to the project, so anyone
   who encounters a `.prenup.yaml` can find out what it is. If prenup ever
   rewrites a config, it must re-emit this header.

## Non-negotiables recap

- Fresh git history.
- Module path: `github.com/c2fo/prenup` (no `/v2`).
- Changelog resets to `v0.1.0`.
- Scaffolding mirrors releasegen; release workflow mirrors vfs.
- Distribution is `go install`-only.
