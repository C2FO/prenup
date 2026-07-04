package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/git"
)

// newInitCmd scaffolds a starter .prenup.yaml from light repo inspection.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a starter .prenup.yaml in the repository root",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd)
		},
	}
	cmd.Flags().Bool("force", false, "overwrite an existing config file")
	return cmd
}

func runInit(cmd *cobra.Command) error {
	force, _ := cmd.Flags().GetBool("force")

	repoRoot, err := git.RepoRoot("")
	if err != nil {
		return err
	}

	target := filepath.Join(repoRoot, ".prenup.yaml")
	if _, err := os.Stat(target); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", target)
	}

	cfg := config.DefaultConfig()
	cfg.CleanWorktree = nil // let it inherit the default on load

	hasGoMod := scanForGoMod(repoRoot)
	hasLint := scanForFile(repoRoot, ".golangci.yml", ".golangci.yaml")
	hasMakefile := scanForFile(repoRoot, "Makefile")

	cfg.Exclude = []string{".github/**", "**/*.yaml", "**/*.yml"}

	if hasGoMod {
		cfg.Tasks = append(cfg.Tasks, config.Task{
			Name:            "Run tests",
			DefaultSelected: true,
			Command:         "go test ./...",
			PerModule:       true,
			Paths:           []string{"**/*.go"},
		})
		if hasLint {
			cfg.Tasks = append(cfg.Tasks, config.Task{
				Name:            "Run golangci-lint",
				DefaultSelected: true,
				Command:         "golangci-lint run --max-same-issues 0 ./...",
				PerModule:       true,
				Paths:           []string{"**/*.go"},
			})
		}
	}
	if hasMakefile {
		cfg.Tasks = append(cfg.Tasks, config.Task{
			Name:            "Check Makefile targets",
			DefaultSelected: false,
			Command:         "make --dry-run help",
			PerModule:       false,
		})
	}
	if len(cfg.Tasks) == 0 {
		cfg.Tasks = append(cfg.Tasks, config.Task{
			Name:            "Example task",
			DefaultSelected: false,
			Command:         "echo 'replace me with a real command'",
		})
	}

	out, err := config.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, out, 0o600); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Wrote %s\n", target); err != nil {
		return err
	}
	return nil
}

func scanForGoMod(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if d.IsDir() {
			base := d.Name()
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			if base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func scanForFile(root string, names ...string) bool {
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}
