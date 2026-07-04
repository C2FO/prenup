package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/c2fo/prenup/internal/git"
	"github.com/c2fo/prenup/internal/hook"
)

// newInstallCmd writes .git/hooks/pre-commit.
func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install prenup as the pre-commit hook in this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd)
		},
	}
	cmd.Flags().Bool("force", false, "overwrite any existing pre-commit hook without backup")
	cmd.Flags().Bool("replace", false, "back up the existing hook and replace it")
	cmd.Flags().Bool("chain", false, "save the existing hook as pre-commit.local and chain it before prenup")
	cmd.Flags().String("binary", "", "path to the prenup binary (defaults to the currently running executable)")
	cmd.Flags().Bool("use-path", false,
		"invoke `prenup` from PATH inside the hook instead of recording an absolute path (good for shared dotfiles)")
	cmd.Flags().Bool("non-interactive", false, "refuse to prompt; fail if a non-prenup hook exists and no mode flag was given")
	return cmd
}

// installFlags is the parsed view of `prenup install`'s flag set.
type installFlags struct {
	force          bool
	replace        bool
	chain          bool
	binary         string
	usePath        bool
	nonInteractive bool
}

func readInstallFlags(cmd *cobra.Command) installFlags {
	f := installFlags{}
	f.force, _ = cmd.Flags().GetBool("force")
	f.replace, _ = cmd.Flags().GetBool("replace")
	f.chain, _ = cmd.Flags().GetBool("chain")
	f.binary, _ = cmd.Flags().GetString("binary")
	f.usePath, _ = cmd.Flags().GetBool("use-path")
	f.nonInteractive, _ = cmd.Flags().GetBool("non-interactive")
	return f
}

// resolveInstallBinary picks the binary path to embed in the hook script.
func resolveInstallBinary(f installFlags) (string, error) {
	switch {
	case f.usePath && f.binary != "":
		return "", errors.New("--use-path and --binary are mutually exclusive")
	case f.usePath:
		return "prenup", nil
	case f.binary != "":
		return f.binary, nil
	default:
		return resolveBinary()
	}
}

// installModeFromFlags returns the explicit mode requested by --force/
// --replace/--chain, defaulting to ModeAbort when none is supplied.
func installModeFromFlags(f installFlags) hook.Mode {
	switch {
	case f.force:
		return hook.ModeForce
	case f.replace:
		return hook.ModeReplace
	case f.chain:
		return hook.ModeChain
	}
	return hook.ModeAbort
}

func runInstall(cmd *cobra.Command) error {
	f := readInstallFlags(cmd)
	repoRoot, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	binary, err := resolveInstallBinary(f)
	if err != nil {
		return err
	}

	if err := installWithConflictResolution(repoRoot, binary, f); err != nil {
		return err
	}

	_, err = fmt.Fprintf(os.Stdout, "Installed pre-commit hook at %s\n",
		filepath.Join(repoRoot, ".git", "hooks", "pre-commit"))
	return err
}

// installWithConflictResolution performs the install and, on ExistsError,
// either fails fast (non-interactive contexts) or prompts the user for a
// resolution mode and retries.
func installWithConflictResolution(repoRoot, binary string, f installFlags) error {
	err := hook.Install(repoRoot, binary, installModeFromFlags(f))
	var existsErr *hook.ExistsError
	if !errors.As(err, &existsErr) {
		return err
	}
	if f.nonInteractive || !isatty.IsTerminal(os.Stdin.Fd()) {
		return err
	}
	choice, promptErr := promptInstallMode(existsErr.Path)
	if promptErr != nil {
		return promptErr
	}
	if choice == hook.ModeAbort {
		return errors.New("install aborted")
	}
	return hook.Install(repoRoot, binary, choice)
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the prenup pre-commit hook",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := git.RepoRoot("")
			if err != nil {
				return err
			}
			if err := hook.Uninstall(repoRoot); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout, "Pre-commit hook removed."); err != nil {
				return err
			}
			return nil
		},
	}
}

// resolveBinary returns the absolute path to the currently running prenup
// binary. Prefer os.Executable; fall back to looking it up on PATH.
func resolveBinary() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		if abs, aerr := filepath.EvalSymlinks(exe); aerr == nil {
			return abs, nil
		}
		return exe, nil
	}
	if p, lerr := exec.LookPath("prenup"); lerr == nil {
		return p, nil
	}
	return "", fmt.Errorf("could not determine prenup binary path: %w", err)
}

func promptInstallMode(existingPath string) (hook.Mode, error) {
	// Stderr writes for an interactive prompt are routinely ignorable: a
	// failing terminal would also fail the subsequent ReadString.
	_, _ = fmt.Fprintf(os.Stderr, "A pre-commit hook already exists at %s.\n", existingPath)
	_, _ = fmt.Fprintln(os.Stderr, "Choose how to proceed:")
	_, _ = fmt.Fprintln(os.Stderr, "  [r]eplace (back up existing hook and replace with prenup)")
	_, _ = fmt.Fprintln(os.Stderr, "  [c]hain   (keep existing hook as pre-commit.local, run it before prenup)")
	_, _ = fmt.Fprintln(os.Stderr, "  [f]orce   (overwrite without backup)")
	_, _ = fmt.Fprintln(os.Stderr, "  [a]bort")
	_, _ = fmt.Fprint(os.Stderr, "> ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return hook.ModeAbort, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "r", "replace":
		return hook.ModeReplace, nil
	case "c", "chain":
		return hook.ModeChain, nil
	case "f", "force":
		return hook.ModeForce, nil
	default:
		return hook.ModeAbort, nil
	}
}
