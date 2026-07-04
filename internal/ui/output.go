// Package ui wires the runner's event stream into the user's chosen renderer
// (human TUI, markdown digest, or NDJSON stream).
package ui

import (
	"os"

	"github.com/mattn/go-isatty"

	"github.com/c2fo/prenup/internal/config"
)

// Resolve collapses "auto" to a concrete mode based on environment signals.
// Rules:
//   - explicit override wins
//   - NO_COLOR or TERM=dumb -> markdown
//   - stdout not a TTY -> markdown
//   - otherwise -> human
//
// "json" is only ever returned when explicitly requested.
func Resolve(requested config.OutputMode) config.OutputMode {
	switch requested {
	case config.OutputHuman, config.OutputMarkdown, config.OutputJSON:
		return requested
	}
	if os.Getenv("NO_COLOR") != "" {
		return config.OutputMarkdown
	}
	if term := os.Getenv("TERM"); term == "dumb" {
		return config.OutputMarkdown
	}
	fd := os.Stdout.Fd()
	if isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd) {
		return config.OutputHuman
	}
	return config.OutputMarkdown
}
