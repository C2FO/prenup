package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/git"
)

// newMigrateCmd converts a v1 config into a v2 config.
func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Convert a v1 .prenup.yml to a v2 .prenup.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(cmd)
		},
	}
	cmd.Flags().String("in", "", "input v1 config path (defaults to .prenup.yml in repo root)")
	cmd.Flags().String("out", "", "output v2 config path (defaults to .prenup.yaml in repo root)")
	cmd.Flags().Bool("force", false, "overwrite an existing output file")
	return cmd
}

func runMigrate(cmd *cobra.Command) error {
	in, _ := cmd.Flags().GetString("in")
	out, _ := cmd.Flags().GetString("out")
	force, _ := cmd.Flags().GetBool("force")

	repoRoot, err := git.RepoRoot("")
	if err != nil {
		return err
	}

	if in == "" {
		in = filepath.Join(repoRoot, ".prenup.yml")
	}
	if out == "" {
		out = filepath.Join(repoRoot, ".prenup.yaml")
	}

	in = filepath.Clean(in)
	data, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("reading %s: %w", in, err)
	}

	_, v2bytes, err := config.MigrateV1(data)
	if err != nil {
		return err
	}

	out = filepath.Clean(out)
	if _, statErr := os.Stat(out); statErr == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", out)
	}
	// out is a user-supplied destination path, cleaned above.
	if err := os.WriteFile(out, v2bytes, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", out, err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "Migrated %s -> %s\n", in, out); err != nil {
		return err
	}
	return nil
}
