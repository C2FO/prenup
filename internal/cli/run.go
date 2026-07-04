package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/discover"
	"github.com/c2fo/prenup/internal/git"
	"github.com/c2fo/prenup/internal/lock"
	"github.com/c2fo/prenup/internal/runner"
	"github.com/c2fo/prenup/internal/ui"
	"github.com/c2fo/prenup/internal/ui/human"
	"github.com/c2fo/prenup/internal/ui/jsonout"
	"github.com/c2fo/prenup/internal/ui/markdown"
	"github.com/c2fo/prenup/internal/versioncheck"
)

// runOptions holds the resolved flag values for `prenup run`.
type runOptions struct {
	configPath      string
	outputMode      string
	taskNames       []string
	all             bool
	noInteractive   bool
	noCleanWorktree bool
	parallelism     int
	dryRun          bool
}

func defaultRunOptions() runOptions { return runOptions{} }

// addRunFlags registers shared flags on cmd. Invoked for both the root command
// and the explicit `run` subcommand.
func addRunFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("config", "", "path to .prenup.yaml (defaults to repo root)")
	f.String("output", "", "output mode: auto, human, markdown, or json")
	f.StringSlice("task", nil, "run only the named task(s); repeatable")
	f.Bool("all", false, "ignore change detection and run against all configured module markers")
	f.Bool("no-interactive", false, "skip the selection UI; run default_selected tasks")
	f.Bool("no-clean-worktree", false, "disable stash-and-restore around task execution")
	f.Int("parallelism", 0, "max concurrent per-module task runs; 0 = NumCPU")
	f.Bool("dry-run", false, "print what would run without executing commands")
}

func readRunOptions(cmd *cobra.Command) runOptions {
	f := cmd.Flags()
	cfgPath, _ := f.GetString("config")
	out, _ := f.GetString("output")
	tasks, _ := f.GetStringSlice("task")
	all, _ := f.GetBool("all")
	noInt, _ := f.GetBool("no-interactive")
	noClean, _ := f.GetBool("no-clean-worktree")
	par, _ := f.GetInt("parallelism")
	dry, _ := f.GetBool("dry-run")
	return runOptions{
		configPath:      cfgPath,
		outputMode:      out,
		taskNames:       tasks,
		all:             all,
		noInteractive:   noInt,
		noCleanWorktree: noClean,
		parallelism:     par,
		dryRun:          dry,
	}
}

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the pre-commit checks without committing",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd, args, readRunOptions(cmd))
		},
	}
	addRunFlags(cmd)
	return cmd
}

// runRun is the entry point used by both `prenup` (default) and `prenup run`.
func runRun(cmd *cobra.Command, _ []string, opts runOptions) error {
	if cmd.Flags().NFlag() > 0 {
		opts = readRunOptions(cmd)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repoRoot, err := git.RepoRoot("")
	if err != nil {
		return fmt.Errorf("locating git repository: %w", err)
	}
	gitRunner := git.New(repoRoot)

	cfg, err := loadRunConfig(repoRoot, opts.configPath)
	if err != nil {
		return err
	}

	// Discover changes and modules. --all ignores change detection.
	disc, err := discoverChangesAndModules(gitRunner, repoRoot, cfg, opts.all)
	if err != nil {
		return err
	}
	if disc.done {
		return printlnSafe(disc.message)
	}

	// Resolve output mode.
	requested := config.OutputMode(strings.ToLower(opts.outputMode))
	if requested == "" {
		requested = cfg.Output
	}
	mode := ui.Resolve(requested)

	verInfo := maybeVersionCheck(ctx)

	sel, err := resolveSelection(opts, mode, disc.modules, cfg, verInfo)
	if err != nil {
		return err
	}
	if sel.done {
		if sel.exitCode != 0 {
			return &exitCodeError{code: sel.exitCode, err: errors.New(sel.message)}
		}
		return printlnSafe(sel.message)
	}

	plan := runner.BuildPlan(cfg, repoRoot, disc.changedFiles, disc.modules, sel.selected)

	cleanWorktree := cfg.CleanWorktreeEnabled()
	if opts.noCleanWorktree {
		cleanWorktree = false
	}

	runOpts := runner.Options{
		Git:            gitRunner,
		Version:        verInfo.version,
		UpdateNotice:   verInfo.notice,
		MaxParallelism: coalescePar(opts.parallelism, cfg.MaxParallelism),
		CleanWorktree:  cleanWorktree,
		DryRun:         opts.dryRun,
	}

	release, err := acquireRepoLock(repoRoot, opts.dryRun)
	if err != nil {
		return err
	}
	defer release()

	result, err := runWithMode(ctx, mode, plan, runOpts)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return &exitCodeError{code: result.ExitCode}
	}
	return nil
}

// printlnSafe writes msg to stdout when non-empty. Returns the underlying
// write error so callers can propagate it.
func printlnSafe(msg string) error {
	if msg == "" {
		return nil
	}
	_, err := fmt.Fprintln(os.Stdout, msg)
	return err
}

// acquireRepoLock takes the per-repo advisory lock that serializes
// concurrent `prenup run` invocations. Dry-run skips the lock since it
// performs no destructive work; this lets a planning probe run alongside a
// real commit-time invocation. The returned release func is always safe to
// call (it is a no-op when no lock was taken), so callers can defer it
// unconditionally.
func acquireRepoLock(repoRoot string, dryRun bool) (func(), error) {
	if dryRun {
		return func() {}, nil
	}
	held, err := lock.Acquire(repoRoot)
	if err != nil {
		if errors.Is(err, lock.ErrContended) {
			return nil, &exitCodeError{code: 1, err: fmt.Errorf(
				"%w (lock file: %s)", err, filepath.Join(repoRoot, ".git", lock.LockFileName))}
		}
		return nil, err
	}
	return func() { _ = held.Close() }, nil
}

func coalescePar(flag, cfg int) int {
	if flag > 0 {
		return flag
	}
	return cfg
}

// loadRunConfig finds and loads the prenup config, defaulting to repoRoot if no
// explicit path was provided.
func loadRunConfig(repoRoot, cfgPath string) (config.Config, error) {
	if cfgPath == "" {
		p, err := config.Find(repoRoot)
		if err != nil {
			return config.Config{}, err
		}
		if p == "" {
			return config.Config{}, fmt.Errorf("no .prenup.yaml found in %s; run `prenup init`", repoRoot)
		}
		cfgPath = p
	}
	return config.Load(cfgPath, repoRoot)
}

// discoverOutcome describes the result of change/module discovery.
//
//   - done == false: changedFiles + modules describe real work to do
//   - done == true:  the caller should print message (if any) and exit 0
type discoverOutcome struct {
	changedFiles []string
	modules      []string
	done         bool
	message      string
}

// discoverChangesAndModules returns the changed files and modules to run
// against, or signals that there's nothing to do.
func discoverChangesAndModules(gitRunner *git.Runner, repoRoot string, cfg config.Config, all bool) (discoverOutcome, error) {
	var out discoverOutcome
	if !all {
		files, err := discover.ChangedFiles(gitRunner, cfg.Exclude)
		if err != nil {
			return discoverOutcome{}, err
		}
		out.changedFiles = files
		if len(files) == 0 {
			return discoverOutcome{done: true, message: "No relevant files changed. Skipping Prenup."}, nil
		}
	}

	out.modules = discover.Modules(repoRoot, out.changedFiles, cfg.ModuleMarkers)
	if all || (len(out.modules) == 0 && len(out.changedFiles) > 0) {
		// --all or no modules detected: feed the plan a synthetic "." module
		// and let per-task path filters do the work.
		out.modules = []string{"."}
	}
	if len(out.modules) == 0 {
		return discoverOutcome{done: true, message: "No changed modules detected. Skipping Prenup."}, nil
	}
	return out, nil
}

// selectionOutcome describes the result of resolving which tasks to run.
//
//   - done == false: selected is the (possibly nil) selection map; nil means
//     "use the config's default_selected"
//   - done == true: print message (if any), exit with exitCode
type selectionOutcome struct {
	selected map[string]bool
	done     bool
	message  string
	exitCode int
}

// resolveSelection returns the selected task set, either from --task flags,
// the human selection UI, or nil (meaning the runner uses default_selected).
func resolveSelection(opts runOptions, mode config.OutputMode, modules []string, cfg config.Config,
	verInfo versionInfo) (selectionOutcome, error) {
	if len(opts.taskNames) > 0 {
		selected := make(map[string]bool, len(opts.taskNames))
		for _, n := range opts.taskNames {
			selected[n] = true
		}
		return selectionOutcome{selected: selected}, nil
	}
	if mode != config.OutputHuman || opts.noInteractive {
		return selectionOutcome{}, nil
	}
	res, err := human.SelectTasks(human.SelectionInput{
		Version: verInfo.version,
		Notice:  verInfo.notice,
		Modules: modules,
		Tasks:   cfg.Tasks,
	})
	if errors.Is(err, human.ErrCanceled) {
		return selectionOutcome{done: true, exitCode: 1, message: "canceled"}, nil
	}
	if err != nil {
		return selectionOutcome{}, err
	}
	if res.Skipped {
		return selectionOutcome{done: true, message: "Skipping tasks as requested."}, nil
	}
	if len(res.Selected) == 0 {
		return selectionOutcome{done: true, message: "No tasks selected. Committing without running checks."}, nil
	}
	return selectionOutcome{selected: res.Selected}, nil
}

// versionInfo carries the resolved version and optional update warning.
type versionInfo struct {
	version string
	notice  string
}

func maybeVersionCheck(ctx context.Context) versionInfo {
	ver := ResolvedVersion()
	info := versionInfo{version: ver}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res := versioncheck.Check(ctx, ver, githubTokenForVersionCheck())
	if res.Error == nil && res.IsOutdated {
		info.notice = fmt.Sprintf(
			"Update available %s - run `go install github.com/c2fo/prenup/cmd/prenup@latest`",
			res.LatestVersion)
	}
	return info
}

func githubTokenForVersionCheck() string {
	for _, key := range []string{"PRENUP_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// runWithMode wires the chosen output sink to the runner and blocks until the
// run completes.
func runWithMode(ctx context.Context, mode config.OutputMode, plan runner.Plan, opts runner.Options) (runner.Result, error) {
	switch mode {
	case config.OutputJSON:
		sink := jsonout.New(os.Stdout)
		opts.Sink = sink
		return runner.Run(ctx, plan, opts)
	case config.OutputMarkdown:
		sink := markdown.New(os.Stdout)
		opts.Sink = sink
		return runner.Run(ctx, plan, opts)
	case config.OutputHuman:
		return runHumanTUI(ctx, plan, opts)
	default:
		sink := markdown.New(os.Stdout)
		opts.Sink = sink
		return runner.Run(ctx, plan, opts)
	}
}

// runHumanTUI wires the Bubble Tea runner UI and the summary printer.
//
// Lifecycle:
//   - the runner runs in a goroutine that emits into channelSink
//   - the TUI runs on the main goroutine and consumes events
//   - when the runner returns we Close the sink so any late deferred-cleanup
//     events (stash pop notices, etc.) drop instead of pinning the goroutine
//   - if the TUI itself errors early (terminal init, etc.), we still Close
//     and cancel the context so the runner unblocks and tears down
func runHumanTUI(ctx context.Context, plan runner.Plan, opts runner.Options) (runner.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	channelSink := human.NewEventChannelSink()
	summarySink := human.NewSummarySink(os.Stdout)
	opts.Sink = runner.NewMultiSink(channelSink, summarySink)

	model := human.NewRunnerModel(channelSink)
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(os.Stdout))

	type runOutcome struct {
		result runner.Result
		err    error
	}
	runDone := make(chan runOutcome, 1)
	go func() {
		result, err := runner.Run(ctx, plan, opts)
		_ = channelSink.Close()
		runDone <- runOutcome{result: result, err: err}
	}()

	uiErr := func() error {
		_, err := prog.Run()
		return err
	}()

	// Either way (TUI exited cleanly or errored), tell the runner we're done
	// listening; Emit will start dropping immediately.
	cancel()
	_ = channelSink.Close()
	out := <-runDone
	_ = summarySink.Close()

	if uiErr != nil {
		return out.result, fmt.Errorf("runner UI: %w", uiErr)
	}
	if out.err != nil {
		return out.result, out.err
	}
	return out.result, nil
}
