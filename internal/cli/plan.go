package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/discover"
	"github.com/c2fo/prenup/internal/git"
	"github.com/c2fo/prenup/internal/runner"
	"github.com/c2fo/prenup/internal/ui"
)

// newPlanCmd prints the plan for the current change set without executing it.
// Respects --output so agents can consume the plan as JSON.
func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Print what prenup would run for the current change set",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlan(cmd)
		},
	}
	cmd.Flags().String("config", "", "path to .prenup.yaml")
	cmd.Flags().String("output", "", "output mode: text (default) or json")
	cmd.Flags().Bool("all", false, "ignore change detection")
	return cmd
}

func runPlan(cmd *cobra.Command) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	outStr, _ := cmd.Flags().GetString("output")
	all, _ := cmd.Flags().GetBool("all")

	repoRoot, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	gitRunner := git.New(repoRoot)

	if cfgPath == "" {
		p, err := config.Find(repoRoot)
		if err != nil {
			return err
		}
		if p == "" {
			return fmt.Errorf("no .prenup.yaml found in %s", repoRoot)
		}
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath, repoRoot)
	if err != nil {
		return err
	}

	var changedFiles []string
	if !all {
		changedFiles, err = discover.ChangedFiles(gitRunner, cfg.Exclude)
		if err != nil {
			return err
		}
	}
	modules := discover.Modules(repoRoot, changedFiles, cfg.ModuleMarkers)
	if all || (len(modules) == 0 && len(changedFiles) > 0) {
		modules = []string{"."}
	}

	plan := runner.BuildPlan(cfg, repoRoot, changedFiles, modules, nil)

	mode := ui.Resolve(config.OutputMode(strings.ToLower(outStr)))
	switch mode {
	case config.OutputJSON:
		return renderPlanJSON(plan)
	default:
		return renderPlanText(plan, os.Stdout)
	}
}

func renderPlanJSON(plan runner.Plan) error {
	type taskView struct {
		Name          string   `json:"name"`
		Selected      bool     `json:"selected"`
		SkipReason    string   `json:"skip_reason,omitempty"`
		PerModule     bool     `json:"per_module"`
		CleanWorktree bool     `json:"clean_worktree"`
		Parallel      bool     `json:"parallel"`
		Modules       []string `json:"modules"`
	}
	type view struct {
		RepoRoot string     `json:"repo_root"`
		Modules  []string   `json:"modules"`
		Tasks    []taskView `json:"tasks"`
	}

	v := view{RepoRoot: plan.RepoRoot, Modules: plan.Modules}
	for i := range plan.Tasks {
		pt := &plan.Tasks[i]
		mods := pt.Modules
		if len(mods) == 1 && mods[0] == "" {
			mods = nil
		}
		v.Tasks = append(v.Tasks, taskView{
			Name:          pt.Task.Name,
			Selected:      pt.Selected,
			SkipReason:    pt.SkipReason,
			PerModule:     pt.Task.PerModule,
			CleanWorktree: pt.CleanWorktree,
			Parallel:      pt.Parallel,
			Modules:       mods,
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderPlanText(plan runner.Plan, w *os.File) error {
	if _, err := fmt.Fprintf(w, "Repository: %s\n", plan.RepoRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Modules:    %s\n\n", strings.Join(plan.Modules, ", ")); err != nil {
		return err
	}
	for i := range plan.Tasks {
		pt := &plan.Tasks[i]
		marker := "v"
		if !pt.Selected {
			marker = "-"
		}
		if _, err := fmt.Fprintf(w, "[%s] %s\n", marker, pt.Task.Name); err != nil {
			return err
		}
		if pt.SkipReason != "" {
			if _, err := fmt.Fprintf(w, "    skipped: %s\n", pt.SkipReason); err != nil {
				return err
			}
		}
		if pt.Task.PerModule && pt.Selected {
			if _, err := fmt.Fprintf(w, "    per_module, modules: %s\n",
				strings.Join(pt.Modules, ", ")); err != nil {
				return err
			}
		}
		if pt.Task.Command != "" {
			if _, err := fmt.Fprintf(w, "    command: %s\n", pt.Task.Command); err != nil {
				return err
			}
		}
	}
	return nil
}
