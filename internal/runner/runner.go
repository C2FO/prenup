package runner

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/discover"
	"github.com/c2fo/prenup/internal/git"
)

// Plan describes what a run will do. It is computed up-front so it can be
// shown by `prenup plan` without executing anything.
type Plan struct {
	RepoRoot        string
	Modules         []string
	ExcludedModules []string // modules filtered out by per-task paths; informational
	Tasks           []PlannedTask
}

// PlannedTask is a task combined with its resolved module set.
type PlannedTask struct {
	Task          config.Task
	Modules       []string // empty slice means "run once with no module scope"
	Selected      bool
	SkipReason    string // populated when Selected is false or Modules is empty
	CleanWorktree bool
	Parallel      bool
}

// BuildPlan computes the plan without side effects. changedFiles and modules
// come from discover.
func BuildPlan(cfg config.Config, repoRoot string, changedFiles, modules []string, selected map[string]bool) Plan {
	plan := Plan{
		RepoRoot: repoRoot,
		Modules:  modules,
	}

	cleanDefault := cfg.CleanWorktreeEnabled()

	for i := range cfg.Tasks {
		// Range by index + pointer to avoid copying the (potentially large)
		// Task struct on every iteration; PlannedTask.Task takes its own
		// copy below so callers can mutate the plan without affecting cfg.
		t := &cfg.Tasks[i]
		planned := PlannedTask{
			Task:          *t,
			Selected:      true,
			CleanWorktree: t.CleanWorktreeEnabled(cleanDefault),
			Parallel:      t.ParallelEnabled(),
		}

		// Apply selection filter (nil means "take default_selected").
		if selected != nil {
			if _, ok := selected[t.Name]; !ok {
				planned.Selected = false
				planned.SkipReason = "not selected"
			}
		} else if !t.DefaultSelected {
			planned.Selected = false
			planned.SkipReason = "not default_selected"
		}

		// Apply per-task path filter to decide which files (and thus
		// which modules) this task cares about.
		taskFiles := discover.FilterByPaths(changedFiles, t.Paths, t.PathsIgnore)
		if (len(t.Paths) > 0 || len(t.PathsIgnore) > 0) && len(taskFiles) == 0 {
			planned.Selected = false
			if planned.SkipReason == "" {
				planned.SkipReason = "no files match task paths"
			}
		}

		if t.PerModule {
			// Intersect the global module list with modules that own at
			// least one taskFiles entry. When no path filter is set,
			// taskFiles is the full file list, so the intersection equals
			// the global module set. When path filters are set but there
			// is no marker-derived module (e.g. tests using a synthetic
			// module list), we still want to respect the task's filtered
			// file set by keeping the global modules that "own" those
			// files via prefix match.
			mods := discover.Modules(repoRoot, taskFiles, cfg.ModuleMarkers)
			if len(mods) == 0 && planned.Selected {
				// Fall back: keep only global modules that have at least
				// one taskFile under them.
				mods = intersectModulesWithFiles(modules, taskFiles)
			}
			planned.Modules = mods
			if planned.Selected && len(mods) == 0 {
				planned.Selected = false
				if planned.SkipReason == "" {
					planned.SkipReason = "no modules matched"
				}
			}
		} else {
			// Non-per-module tasks run once without a module scope.
			planned.Modules = []string{""}
		}

		plan.Tasks = append(plan.Tasks, planned)
	}
	return plan
}

// Options configures a Run invocation.
type Options struct {
	Executor       Executor
	Git            *git.Runner
	Sink           Sink
	Version        string
	UpdateNotice   string
	MaxParallelism int  // 0 == NumCPU
	CleanWorktree  bool // whether to stash unstaged changes around the run
	DryRun         bool // if true, emit events but do not execute commands
}

// Result aggregates the outcome of a Run.
type Result struct {
	Succeeded int
	Failed    int
	ExitCode  int
	// FailedTasks is the names of tasks that ended in TaskStatusFailed,
	// in selection order. Empty when Failed == 0.
	FailedTasks []string
}

// Run executes the plan's selected tasks and emits events to opts.Sink.
//
// Run never returns an error today; the (Result, error) shape is preserved so
// future failure modes (e.g. unrecoverable stash errors) can be surfaced
// without changing every caller.
func Run(ctx context.Context, plan Plan, opts Options) (Result, error) {
	if opts.Sink == nil {
		opts.Sink = DiscardSink{}
	}
	sink := opts.Sink
	exec := opts.Executor
	if exec == nil {
		exec = BashExecutor{}
	}
	maxPar := opts.MaxParallelism
	if maxPar <= 0 {
		maxPar = runtime.NumCPU()
	}

	emitRunStarted(sink, plan, opts)

	stash := beginStash(opts, sink)
	defer endStash(stash, sink, opts.Git)

	var result Result
	for i := range plan.Tasks {
		pt := &plan.Tasks[i]
		if !pt.Selected {
			sink.Emit(Event{
				Kind:    EventTaskCompleted,
				Time:    time.Now(),
				Task:    pt.Task.Name,
				Status:  TaskStatusSkipped,
				Message: pt.SkipReason,
			})
			continue
		}
		runOneTask(ctx, pt, plan.RepoRoot, opts, exec, sink, maxPar, &result)
	}

	if result.Failed > 0 {
		result.ExitCode = 1
	}
	sink.Emit(Event{
		Kind:        EventRunCompleted,
		Time:        time.Now(),
		Succeeded:   result.Succeeded,
		Failed:      result.Failed,
		ExitCode:    result.ExitCode,
		FailedTasks: result.FailedTasks,
	})
	return result, nil
}

// emitRunStarted publishes the EventRunStarted event with selected task names.
func emitRunStarted(sink Sink, plan Plan, opts Options) {
	taskNames := make([]string, 0, len(plan.Tasks))
	for i := range plan.Tasks {
		if plan.Tasks[i].Selected {
			taskNames = append(taskNames, plan.Tasks[i].Task.Name)
		}
	}
	sink.Emit(Event{
		Kind:     EventRunStarted,
		Time:     time.Now(),
		Version:  opts.Version,
		Modules:  plan.Modules,
		Tasks:    taskNames,
		RepoRoot: plan.RepoRoot,
		Message:  opts.UpdateNotice,
	})
}

// beginStash optionally stashes the worktree before the run; emits a notice on
// failure and returns nil so the run continues without stash protection.
func beginStash(opts Options, sink Sink) *git.Stash {
	if !opts.CleanWorktree || opts.Git == nil || opts.DryRun {
		return nil
	}
	s, err := opts.Git.Push()
	if err != nil {
		sink.Emit(Event{
			Kind:    EventNotice,
			Time:    time.Now(),
			Message: "stash failed; continuing without clean_worktree: " + err.Error(),
		})
		return nil
	}
	return s
}

// endStash pops a previously created stash, emitting a notice if restore
// fails. A failed pop typically means `stage_output` added files that
// conflicted with the user's own unstaged hunks; the original work is still
// recoverable from `git stash list`.
func endStash(stash *git.Stash, sink Sink, _ *git.Runner) {
	if stash == nil {
		return
	}
	if err := stash.Pop(); err != nil {
		sink.Emit(Event{
			Kind: EventNotice,
			Time: time.Now(),
			Message: "failed to restore stash: " + err.Error() +
				" (your unstaged changes are preserved; run `git stash list`, resolve any conflicts, then `git stash pop`)",
		})
	}
}

// snapshotTracked records the set of tracked files for a fresh "before"
// baseline. Used per-task so that stage_output only stages files that this
// task created, not files generated by an earlier task in the same run.
func snapshotTracked(opts Options) map[string]struct{} {
	if opts.Git == nil || opts.DryRun {
		return nil
	}
	snap, err := opts.Git.TrackedFiles()
	if err != nil {
		return nil
	}
	return snap
}

// runOneTask executes a single planned task (possibly across modules) and
// updates result with success/failure counts.
func runOneTask(ctx context.Context, pt *PlannedTask, repoRoot string, opts Options,
	exec Executor, sink Sink, maxPar int, result *Result) {
	taskStart := time.Now()

	// Snapshot the tracked file set immediately before each task with
	// stage_output so generated files from a previous task aren't
	// mis-attributed here.
	var beforeTracked map[string]struct{}
	if pt.Task.StageOutput && opts.Git != nil && !opts.DryRun {
		beforeTracked = snapshotTracked(opts)
	}

	var taskFailed bool
	if pt.Parallel && len(pt.Modules) > 1 && !opts.DryRun {
		taskFailed = runParallel(ctx, pt, repoRoot, opts, exec, sink, maxPar)
	} else {
		taskFailed = runSequential(ctx, pt, repoRoot, opts, exec, sink)
	}

	status := TaskStatusDone
	if taskFailed {
		status = TaskStatusFailed
		result.Failed++
		result.FailedTasks = append(result.FailedTasks, pt.Task.Name)
	} else {
		result.Succeeded++
	}
	sink.Emit(Event{
		Kind:       EventTaskCompleted,
		Time:       time.Now(),
		Task:       pt.Task.Name,
		Status:     status,
		DurationMs: time.Since(taskStart).Milliseconds(),
	})

	if !taskFailed && pt.Task.StageOutput && opts.Git != nil && !opts.DryRun {
		if err := stageGenerated(opts.Git, pt.Task.OutputPatterns, beforeTracked, pt.Modules); err != nil {
			sink.Emit(Event{
				Kind:    EventNotice,
				Time:    time.Now(),
				Task:    pt.Task.Name,
				Message: "staging output failed: " + err.Error(),
			})
		}
	}
}

// runSequential executes pt's modules one by one, fail-fast on the first
// error. When fail-fast triggers, remaining modules are reported as skipped
// so consumers see a terminating event for every module that was queued.
// Returns true if any module failed.
func runSequential(ctx context.Context, pt *PlannedTask, repoRoot string, opts Options, exec Executor, sink Sink) bool {
	for i, module := range pt.Modules {
		if err := runOne(ctx, pt, module, repoRoot, opts, exec, sink); err != nil {
			emitModuleSkips(sink, pt.Task.Name, pt.Modules[i+1:], "fail-fast: sibling module failed")
			return true
		}
	}
	return false
}

// runParallel fans per-module runs out with bounded concurrency. The first
// failure short-circuits subsequent work (fail-fast is preserved); modules
// that never started or aborted before runOne report a skip notice so the
// event stream stays in balance with EventTaskStarted/EventTaskCompleted
// expectations downstream. Returns true if any module failed.
func runParallel(ctx context.Context, pt *PlannedTask, repoRoot string, opts Options, exec Executor, sink Sink, maxPar int) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed bool
	skipped := make(map[string]bool, len(pt.Modules))

	modules := make([]string, len(pt.Modules))
	copy(modules, pt.Modules)
	sort.Strings(modules) // stable event ordering for tests

	for _, module := range modules {
		mu.Lock()
		if failed {
			skipped[module] = true
			mu.Unlock()
			continue
		}
		mu.Unlock()

		wg.Add(1)
		sem <- struct{}{}
		go func(module string) {
			defer wg.Done()
			defer func() { <-sem }()

			// Re-check failure state after acquiring the semaphore so
			// queued goroutines drop out as soon as a sibling fails.
			mu.Lock()
			abort := failed
			if abort {
				skipped[module] = true
			}
			mu.Unlock()
			if abort {
				return
			}

			if err := runOne(ctx, pt, module, repoRoot, opts, exec, sink); err != nil {
				mu.Lock()
				failed = true
				mu.Unlock()
				cancel()
			}
		}(module)
	}
	wg.Wait()
	if failed {
		// Emit deterministic skip notices in module order.
		var skippedList []string
		for _, m := range modules {
			if skipped[m] {
				skippedList = append(skippedList, m)
			}
		}
		emitModuleSkips(sink, pt.Task.Name, skippedList, "fail-fast: sibling module failed")
	}
	return failed
}

// emitModuleSkips emits a skip-flavored notice per module so consumers know
// these per-module runs were aborted by fail-fast rather than silently
// dropped. Notices are used (rather than synthetic task_completed events)
// because EventTaskCompleted is reserved for the per-task aggregate.
func emitModuleSkips(sink Sink, task string, modules []string, reason string) {
	for _, m := range modules {
		sink.Emit(Event{
			Kind:    EventNotice,
			Time:    time.Now(),
			Task:    task,
			Module:  m,
			Message: reason,
		})
	}
}

// runOne executes pt on a single module, emitting task_started, line events,
// and either nothing (caller emits task_completed) or an error message.
func runOne(ctx context.Context, pt *PlannedTask, module, repoRoot string, opts Options, exec Executor, sink Sink) error {
	vars := NewTemplateVars(repoRoot, module)
	command := vars.Expand(pt.Task.Command)

	workingDir := pt.Task.WorkingDir
	if workingDir != "" {
		workingDir = vars.Expand(workingDir)
	} else if pt.Task.PerModule && module != "" {
		workingDir = vars.ModuleRoot
	} else {
		workingDir = vars.RepoRoot
	}

	// Emit task_started after template expansion so the event carries the
	// resolved command and working directory. Consumers reading a failure
	// transcript can then reproduce the exact invocation without also
	// reading .prenup.yaml or knowing how prenup expands templates.
	sink.Emit(Event{
		Kind:       EventTaskStarted,
		Time:       time.Now(),
		Task:       pt.Task.Name,
		Module:     module,
		Command:    command,
		WorkingDir: workingDir,
	})

	if opts.DryRun {
		sink.Emit(Event{
			Kind:   EventLine,
			Time:   time.Now(),
			Task:   pt.Task.Name,
			Module: module,
			Stream: StreamStdout,
			Text:   fmt.Sprintf("[dry-run] would run: %s (in %s)", command, workingDir),
		})
		return nil
	}

	err := exec.Run(ctx, command, workingDir, pt.Task.Env, func(stream Stream, text string) {
		sink.Emit(Event{
			Kind:   EventLine,
			Time:   time.Now(),
			Task:   pt.Task.Name,
			Module: module,
			Stream: stream,
			Text:   text,
		})
	})
	if err != nil {
		// Prefix with [prenup] so consumers can distinguish prenup's own
		// synthetic failure attribution from a stderr line the user's
		// script happened to print (e.g. a test harness reporting on a
		// child process). The Message field carries the unprefixed error
		// for programmatic consumers.
		sink.Emit(Event{
			Kind:    EventLine,
			Time:    time.Now(),
			Task:    pt.Task.Name,
			Module:  module,
			Stream:  StreamStderr,
			Text:    "[prenup] command failed: " + err.Error(),
			Message: err.Error(),
		})
	}
	return err
}

// intersectModulesWithFiles returns the subset of modules that own at least
// one of files via a path-prefix match. Used as a fallback when the
// configured module_markers aren't present on disk (e.g. test scaffolding)
// but the caller still provides a plausible module list.
//
// "." is treated as the repo root, which owns every file.
func intersectModulesWithFiles(modules, files []string) []string {
	out := make([]string, 0, len(modules))
	for _, m := range modules {
		if m == "." {
			out = append(out, m)
			continue
		}
		for _, f := range files {
			if hasPathPrefix(f, m) {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// hasPathPrefix reports whether s is exactly prefix or sits below prefix in
// the path hierarchy. Operates on slash-separated paths (the form git uses).
func hasPathPrefix(s, prefix string) bool {
	if prefix == "" || prefix == "." {
		return true
	}
	return s == prefix || strings.HasPrefix(s, prefix+"/")
}
