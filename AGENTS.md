# Agent Guidelines for the Prenup Repository

This document provides guidelines for AI agents (and humans) working on the
`prenup` repository. It captures the conventions the codebase already follows
so contributions stay consistent.

## Table of Contents
- [Development Guidelines](#development-guidelines)
- [Testing](#testing)
- [Config Schema Versioning](#config-schema-versioning)
- [CHANGELOG and PR Process](#changelog-and-pr-process)
- [Go Version Policy](#go-version-policy)
- [Dependency Upgrades](#dependency-upgrades)
- [GitHub Actions Maintenance](#github-actions-maintenance)
- [Release Process](#release-process)
- [Repository Structure](#repository-structure)

---

## Development Guidelines

### Code Style

- Follow standard Go idioms; run `gofmt` and `goimports`.
- Imports are grouped with `github.com/c2fo/` local — see `.golangci.yml`.
- All exported types, functions, and methods have godoc comments.
- Keep functions small and focused; prefer early returns to reduce nesting.
- **Do not add narrating comments** that restate what the code does
  (e.g. `// increment counter`). Comments explain non-obvious intent,
  trade-offs, or constraints — not the mechanics.

### Error Handling

- Handle every error explicitly; no silent failures.
- Wrap with context: `fmt.Errorf("operation failed: %w", err)`.
- Never ignore return values.
- Config, git, and hook errors are user-facing — write messages that tell the
  user what to fix.

### Interface Design

- Prefer small, focused interfaces.
- Define interfaces in the *consuming* package, not the implementing one.
- Use dependency injection for testability. The `runner.Executor` and
  `runner.Sink` interfaces are the canonical examples.

### File Organization

- Group related functionality in packages. One package per major feature.
- Place mocks in `mocks/` subdirectories (see `internal/runner/mocks/`).
- CLI commands live in `internal/cli/`, one file per command.

### Linting

- All code must pass `golangci-lint run` with 0 issues.
- Config: `.golangci.yml`.
- CI runs the linter with `--only-new-issues` on PRs.

---

## Testing

### Coverage requirements

- **All new code must have tests.** Aim for coverage close to 100% where
  practical.
- Minimum thresholds are enforced by CI via `.testcoverage.yml` and the
  `go-test-coverage` GitHub Action.
- The thresholds start conservative and **only ratchet up** — never lower a
  threshold to make CI pass. If a change lowers coverage, add tests until it
  recovers.
- Some code is legitimately excluded from coverage: the interactive Bubble
  Tea TUI (`internal/ui/human/`), the trivial output-mode shim
  (`internal/ui/output.go`), and `cmd/prenup/main.go`. Update
  `.testcoverage.yml` if that list needs to change.

### Test organization

- **Use `testify/suite`** for related tests sharing setup/teardown. One suite
  per component; naming: `[Component]TestSuite` (e.g. `RunnerTestSuite`).
- Simple unit tests don't require a suite; a plain `TestXxx(t *testing.T)`
  is fine for small, standalone cases.
- Use `SetupTest()` / `TearDownTest()` (or `t.TempDir()`) for isolation.

### Test style

- **Prefer table-driven tests.** Slice of anonymous structs with `name`,
  inputs, and expected outputs. Every case gets a descriptive name.
- Use suite methods (`s.Require()` / `s.Assert()`) inside suites; use the
  standalone `require`/`assert` packages outside them.
- Cover both success and error paths in the same table.
- Example (matches the style used throughout the repo):

  ```go
  tests := []struct {
      name           string
      input          string
      expectedOutput string
      expectedError  string
  }{
      {name: "success", input: "valid", expectedOutput: "result"},
      {name: "invalid input", input: "", expectedError: "input is required"},
  }
  for _, tt := range tests {
      s.Run(tt.name, func() {
          got, err := fn(tt.input)
          if tt.expectedError != "" {
              s.Require().Error(err)
              s.Assert().Contains(err.Error(), tt.expectedError)
              return
          }
          s.Require().NoError(err)
          s.Assert().Equal(tt.expectedOutput, got)
      })
  }
  ```

### Mocking

- **Use `mockery` v3** with the EXPECT() pattern for readability and type
  safety. Do not hand-write mocks.
- Config lives in `.mockery.yaml`. Mocks land in `mocks/` subdirectories.
- Regenerate after changing a mocked interface: `mockery`.
- Example EXPECT() usage:

  ```go
  exec := mocks.NewExecutor(t)
  exec.EXPECT().
      Run(mock.Anything, mock.MatchedBy(func(spec runner.ExecSpec) bool {
          return spec.Command == "go test ./..."
      })).
      Return(nil)
  ```

### Test types

- **Unit tests** live alongside the code (`foo_test.go` next to `foo.go`).
- **End-to-end tests** live in `cmd/prenup/e2e_test.go`; they build the
  binary and drive real `git` repos in `t.TempDir()`. E2E does not
  contribute to package coverage (external process), so critical logic
  still needs unit tests.
- **Race detection:** `go test -race ./...` runs cleanly.

### Running the tests

```bash
go test ./...                          # full suite
go test -race ./...                    # with race detector
go test -coverprofile=coverage.out ./...  # generate coverage profile
go tool cover -func=coverage.out       # per-function coverage
go tool cover -html=coverage.out       # browse coverage
```

---

## Config Schema Versioning

The `.prenup.yaml` file declares `version:` — currently `1`. This is the
config *schema* version, a plain integer **decoupled from the prenup release
version**. Rules:

- It bumps **only** on a backward-incompatible change to the file format.
- Additive, non-breaking changes (new optional fields with sensible defaults)
  do **not** bump the version — old configs keep working.
- The current version lives in `internal/config/config.go` as
  `SchemaVersion`; the JSON Schema files (`internal/config/schema.json` and
  `assets/prenup.schema.json`) must stay byte-identical and be updated
  together.
- Loading a config with a *newer* version than the binary understands must
  fail with a clear "upgrade prenup" message.
- Configs that prenup writes (`prenup init`, future `config upgrade`) go
  through `config.Marshal`, which prepends the discoverability header that
  links back to the project. Preserve it.
- Do **not** silently rewrite a user's config. Any future format-migration
  command must be opt-in.

## CHANGELOG and PR Process

### Required for every PR

Every PR **must** add an entry under the `## [Unreleased]` section of
`CHANGELOG.md`. Do **not** add a version number — versions are assigned at
release time from the changelog contents.

### CHANGELOG format

Follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Standard
headings:

- `### Added` — new features
- `### Fixed` — bug fixes
- `### Changed` / `### Removed` — **breaking changes only**; the description
  must include the exact phrase `BREAKING CHANGE`
- `### Deprecated` — features slated for removal
- `### Security` — security-related updates

If a change is not breaking, use `### Added` (new features) or `### Fixed`
(bug fixes) — never `### Changed`.

### Version bump rules

The next release version is derived from the `[Unreleased]` sections:

- **Major** — any `### Changed` or `### Removed` with `BREAKING CHANGE`
- **Minor** — `### Added`, `### Deprecated`, or `### Security`
- **Patch** — only `### Fixed`

---

## Go Version Policy

Prenup supports the latest Go version and the previous minor version. The
`go.mod` `go` directive pins the older of the two so both keep working.

### Files to update on a Go version bump

- `go.mod` — `go` directive
- `.golangci.yml` — `run.go`
- `.github/workflows/ci.yml` — `go-version`
- `.github/workflows/codeql.yml` — `go-version`
- `.github/workflows/golangci-lint.yml` — `go-version`
- `.github/workflows/go-test-coverage.yml` — `go-version`
- `.github/workflows/validate-changelog.yml` — `go-version`
- `.github/workflows/releasegen.yml` — `go-version` if the workflow builds
  from source (this repo's release workflow runs a prebuilt image, so no
  change usually needed)

---

## Dependency Upgrades

- Update dependencies regularly for security and bug fixes.
- **Dependency bumps are their own PR** — do not fold them into unrelated
  feature or docs work.
- Test with race detection after every bump.
- Document breaking changes in `CHANGELOG.md`.

```bash
go get -u -t ./...
go mod tidy
go test -race ./...
```

---

## GitHub Actions Maintenance

### SHA pinning

All actions must be pinned to a full 40-character commit SHA, with the tag
name in a trailing comment:

```yaml
uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2
```

### Update policy

- Select the newest version that is **at least 10 days old**. If the latest
  is too fresh, pick a slightly older one.
- Verify release dates and check for security advisories before updating.
- Test the change in a PR before merging.

### Workflow files

- `ci.yml` — PR test workflow
- `golangci-lint.yml` — PR lint workflow
- `codeql.yml` — security scanning (PR + weekly cron)
- `go-test-coverage.yml` — enforces `.testcoverage.yml` thresholds
- `validate-changelog.yml` — runs `releasegen validate` on PRs
- `releasegen.yml` — cuts tags and GitHub releases from `CHANGELOG.md` on
  push to `main`; uses `ghcr.io/c2fo/releasegen:v1`

---

## Release Process

Releases are automated by the `releasegen.yml` workflow:

1. Every PR updates `## [Unreleased]` with the change.
2. On merge to `main`, `releasegen` computes the next SemVer from the
   `[Unreleased]` entries, promotes them into a versioned section, tags the
   commit, and publishes a GitHub Release with the notes.
3. Users install via `go install github.com/c2fo/prenup/cmd/prenup@<tag>` or
   `@latest`.

Prenup does not currently ship a Docker image or prebuilt binaries.

---

## Repository Structure

```
prenup/
├── AGENTS.md                    # this file
├── CHANGELOG.md
├── CODE_OF_CONDUCT.md
├── CONTRIBUTING.md
├── LICENSE.md
├── README.md
├── SECURITY.md
├── go.mod                       # single Go module
├── .prenup.yaml                 # prenup dogfoods itself
├── .releasegen.yaml
├── .golangci.yml
├── .testcoverage.yml
├── .mockery.yaml
├── assets/prenup.schema.json    # published JSON Schema (byte-identical to internal copy)
├── prenup.example.yaml          # user-facing example config
├── cmd/prenup/                  # binary entry point + end-to-end tests
│   ├── main.go
│   └── e2e_test.go
├── docs/                        # DESIGN.md, SCHEMA.md, FUTURE.md
├── internal/
│   ├── cli/                     # cobra command tree
│   ├── config/                  # .prenup.yaml schema + loading + validation
│   ├── discover/                # change + module detection
│   ├── git/                     # git subprocess wrapper
│   ├── hook/                    # pre-commit hook install/uninstall
│   ├── lock/                    # per-repo advisory lock
│   ├── runner/                  # task planning + execution
│   │   └── mocks/               # mockery-generated Executor/Sink mocks
│   ├── ui/                      # output-mode dispatch
│   │   ├── human/               # Bubble Tea TUI (excluded from coverage)
│   │   ├── jsonout/             # NDJSON event stream
│   │   └── markdown/            # markdown digest
│   └── versioncheck/            # GitHub Releases update checker
└── .github/
    ├── ISSUE_TEMPLATE/
    └── workflows/
```

---

## References

- [Go Release Policy](https://go.dev/doc/devel/release)
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
- [golangci-lint](https://golangci-lint.run/)
- [testify](https://github.com/stretchr/testify)
- [mockery](https://github.com/vektra/mockery)
- [releasegen](https://github.com/c2fo/releasegen) — computes the next SemVer
  from `[Unreleased]` and cuts the tag + GitHub Release
- [Prenup CHANGELOG](CHANGELOG.md)
