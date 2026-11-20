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
	return runRoot(newRootCmd())
}

// runRoot executes root and maps any returned error to an exit code, writing
// user-facing errors to root.ErrOrStderr() (which defaults to os.Stderr).
// SilenceErrors is set on root so bare exitCodeError values (used when a task
// runner sink has already rendered the failure summary) do not produce a
// redundant "Error: exit status N" line.
func runRoot(root *cobra.Command) int {
	err := root.Execute()
	if err == nil {
		return 0
	}
	ec := &exitCodeError{}
	if errors.As(err, &ec) {
		if errors.Unwrap(err) != nil {
			_, _ = fmt.Fprintf(root.ErrOrStderr(), "Error: %v\n", err)
		}
		return ec.code
	}
	_, _ = fmt.Fprintf(root.ErrOrStderr(), "Error: %v\n", err)
	return 1
}

// exitCodeError lets a subcommand propagate a specific exit code without
// printing an extra usage summary. When err is nil, runRoot suppresses stderr
// output because the caller has already rendered the failure.
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

func (e *exitCodeError) Unwrap() error {
	return e.err
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
