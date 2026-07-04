# Contributing to Prenup

Thanks for your interest in improving Prenup! Issues and pull requests are
welcome. This document covers how to get set up and the conventions the
project follows.

By participating, you agree to abide by our
[Code of Conduct](CODE_OF_CONDUCT.md).

## Getting started

Prenup is a single Go module (`github.com/c2fo/prenup`).

```bash
git clone https://github.com/c2fo/prenup.git
cd prenup
go build ./...
go test ./...
```

You need Go 1.26 or newer, plus `git` and `bash` on your `PATH` (Prenup runs
tasks via `bash -c` and shells out to `git`).

## Development workflow

1. Fork the repo and create a topic branch off `main`.
2. Make your change, with tests.
3. Run the full local check suite (below).
4. Update `CHANGELOG.md` (see [Changelog](#changelog)).
5. Open a pull request describing the change and its motivation.

Prenup dogfoods itself: the repo ships a `.prenup.yaml`, so once you run
`prenup install` in your clone the hook will run tests, `golangci-lint`,
`go vet`, and a `gofmt` check on commit.

## Local checks

Before opening a PR, make sure these pass:

```bash
go build ./...
go test ./...
golangci-lint run
gofmt -l .        # should print nothing
```

## Testing

- **All new code must have tests.** Aim for coverage as close to 100% as is
  practical.
- Use [`testify`](https://github.com/stretchr/testify) — prefer `suite` for
  related tests, and `s.Require()` / `s.Assert()` within suites.
- Prefer **table-driven tests** with a slice of named cases.
- Generate mocks with `mockery` v3 (config in `.mockery.yaml`); do not
  hand-write mocks. Regenerate with `mockery` after changing a mocked
  interface.

Coverage thresholds are enforced in CI via `.testcoverage.yml`.

## Code style

- Run `gofmt` / `goimports`; imports are grouped with the
  `github.com/c2fo/` prefix local (see `.golangci.yml`).
- All exported types, functions, and methods need doc comments.
- Handle every error explicitly; wrap with context using
  `fmt.Errorf("...: %w", err)`.
- Keep functions small and prefer early returns.
- Do not add comments that merely restate what the code does.

## Changelog

Every PR must add an entry under the `## [Unreleased]` section of
[CHANGELOG.md](CHANGELOG.md). The changelog follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Use the appropriate heading for your change:

- `### Added` — new features
- `### Fixed` — bug fixes
- `### Changed` / `### Removed` — **breaking changes only**; include the exact
  phrase `BREAKING CHANGE` in the description
- `### Deprecated` — features slated for removal
- `### Security` — security-related updates

**Do not add a version number** — versions are assigned automatically at
release time from the changelog contents.

## Pull request expectations

- Focused, single-purpose changes are easier to review.
- CI (tests, lint, coverage, CodeQL, changelog validation) must be green.
- Update relevant docs (`README.md`, `docs/`) when behavior changes.

## Reporting bugs and requesting features

Use the GitHub issue templates. For security issues, please follow
[SECURITY.md](SECURITY.md) instead of opening a public issue.
