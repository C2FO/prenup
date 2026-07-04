# Security Policy

## Supported versions

Prenup is distributed as a single Go binary. Security fixes are applied to the
latest released version. Please make sure you are on the most recent release
before reporting an issue.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
pull requests, or discussions.**

Instead, report them privately using GitHub's private vulnerability reporting:

1. Go to the [Security tab](https://github.com/c2fo/prenup/security) of this
   repository.
2. Click **Report a vulnerability**.
3. Provide as much detail as you can, including:
   - a description of the issue and its impact,
   - steps to reproduce (a proof of concept if possible),
   - affected version(s), and
   - any suggested remediation.

We will acknowledge your report, investigate, and keep you informed of the
resolution. Once a fix is available we will publish a release and, where
appropriate, a security advisory crediting the reporter (unless you prefer to
remain anonymous).

## Scope

Prenup executes user-defined commands from a repository's `.prenup.yaml` via
`bash`. Configuration files are treated as trusted input controlled by the
repository owner; running Prenup against a repository is equivalent to
trusting that repository's configured commands. Reports that rely on a
maliciously crafted `.prenup.yaml` in a repository you already control are
generally out of scope.

Vulnerabilities of interest include (but are not limited to):

- unintended command execution outside of configured tasks,
- path traversal or writing outside the repository during output staging,
- leaking credentials (e.g. `PRENUP_GITHUB_TOKEN`) to task output or logs,
- privilege or worktree-integrity issues in the stash/restore or hook-install
  logic.

Thank you for helping keep Prenup and its users safe.
