// Package main is the entry point for the prenup binary.
//
// Prenup is an interactive, configuration-driven Git pre-commit hook utility.
// See README.md and docs/PRD.md for the product description and docs/ for
// reference material (config schema, event schema, future work).
package main

import (
	"os"

	"github.com/c2fo/prenup/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
