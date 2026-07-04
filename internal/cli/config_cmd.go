package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/git"
)

// newConfigCmd houses configuration-related subcommands.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect prenup configuration",
	}
	cmd.AddCommand(newConfigValidateCmd(), newConfigSchemaCmd())
	return cmd
}

func newConfigValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a .prenup.yaml file against the schema",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := git.RepoRoot("")
			if err != nil {
				return err
			}
			var path string
			if len(args) == 1 {
				path = args[0]
			} else {
				p, err := config.Find(repoRoot)
				if err != nil {
					return err
				}
				if p == "" {
					return fmt.Errorf("no config file found in %s", repoRoot)
				}
				path = p
			}
			cfg, err := config.Load(path, repoRoot)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(os.Stdout, "OK: %s parses cleanly (v%d, %d tasks)\n",
				cfg.Path, cfg.Version, len(cfg.Tasks)); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func newConfigSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the embedded JSON schema for .prenup.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := os.Stdout.Write(config.Schema())
			return err
		},
	}
}
