// Package cli defines the cobra command tree for the prenup binary.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Execute parses os.Args, dispatches to the chosen subcommand, and returns
// the desired process exit code. A zero code means success.
func Execute() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		// cobra already prints its own errors; ensure we surface a non-zero
		// exit. Individual commands that want to pick a specific code use
		// exitCodeError below.
		ec := &exitCodeError{}
		if errors.As(err, &ec) {
			return ec.code
		}
		return 1
	}
	return 0
}

// exitCodeError lets a subcommand propagate a specific exit code without
// printing an extra usage summary.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit status %d", e.code)
	}
	return e.err.Error()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "prenup",
		Short: "Interactive pre-commit hook utility",
		Long: `Prenup runs user-defined tasks (tests, linters, doc generators, custom scripts)
as a Git pre-commit hook, scoped to the modules that actually changed.

When invoked with no subcommand, prenup behaves as the git hook entry point
and is equivalent to "prenup run". See the README for configuration.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd, args, defaultRunOptions())
		},
	}

	// Run's flags live on root so they also apply to the default (no-subcommand) invocation.
	addRunFlags(root)

	root.AddCommand(
		newRunCmd(),
		newPlanCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newInitCmd(),
		newMigrateCmd(),
		newConfigCmd(),
		newVersionCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print prenup version and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(os.Stdout, "prenup %s\n", ResolvedVersion())
			return err
		},
	}
}
