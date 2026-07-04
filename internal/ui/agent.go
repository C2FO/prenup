package ui

// This file holds the canonical strings prenup emits to AI agents and other
// non-human consumers so the markdown digest and the NDJSON stream stay in
// lockstep. Humans get the Bubble Tea TUI and the chrome it provides, so
// these strings are intentionally absent from the human renderer.
//
// When you change one of these constants, please also:
//   - re-run `go test ./internal/ui/...` (golden assertions key off them)
//   - update prenup/docs/SCHEMA.md if you change AgentHintSchema or the
//     shape of the agent_hint event.

// Tool is the project name as it should appear to consumers. Stable.
const Tool = "prenup"

// HomepageURL points readers at human-facing docs. Stable.
const HomepageURL = "https://github.com/c2fo/prenup"

// Description is a one-sentence "what is this" so an agent that has never
// heard of prenup can orient itself from a single line of output.
const Description = "Prenup is a Git pre-commit hook runner that executes " +
	"configurable quality checks (tests, linters, generators) scoped to " +
	"the modules touched by the in-flight commit."

// HookContextNote tells consumers that this output came from a Git hook --
// not from `git` itself -- so they don't misattribute failures to git.
const HookContextNote = "This output is produced by the prenup pre-commit " +
	"hook, not by `git` itself. A non-zero exit means the hook blocked the " +
	"commit; the commit was not created."

// JSONHint advertises the structured output mode. Shown only in markdown.
const JSONHint = "For machine-readable output, run with " +
	"`--output json` or set `PRENUP_OUTPUT=json` in the environment."

// FailureBypassHint explains how to retry or bypass after a failed run.
// Used in the markdown digest's "Next steps" block and surfaced verbatim
// in the agent_hint event so JSON consumers see the same guidance.
const FailureBypassHint = "Re-run `git commit` after fixing the issue, or " +
	"use `git commit --no-verify` to bypass the hook for a single commit. " +
	"Configuration lives in `.prenup.yaml` at the repository root."

// AgentHintSchema is the version of the agent_hint event payload. Bump
// when the shape of the strings or the surrounding event fields changes
// in a way that would surprise an existing consumer.
const AgentHintSchema = "1"

// DevVersion is the literal Version string an unbuilt or `go install`-from-
// source binary reports. Anything else is taken to be a real semver-like
// release identifier.
const DevVersion = "dev"

// VersionLabel turns a raw version token into a human- and agent-friendly
// label that makes the field's role obvious. A pattern-matching consumer
// scanning for `Prenup dev` would otherwise have to guess whether `dev`
// is a build identifier, a status, or part of a task name. Examples:
//
//	"v2.0.1" -> "v2.0.1"
//	"dev"    -> "dev (development build, not a tagged release)"
//	""       -> "unknown"
//
// Wire/JSON consumers receive the raw token in Event.Version unchanged;
// this helper only formats it for human-readable contexts.
func VersionLabel(version string) string {
	switch version {
	case "":
		return "unknown"
	case DevVersion:
		return DevVersion + " (development build, not a tagged release)"
	default:
		return version
	}
}
