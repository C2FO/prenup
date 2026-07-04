// Package config defines the v2 .prenup.yaml schema, loading, defaults, and validation.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// ConfigFilenames lists the filenames prenup looks for in the repository root,
// in order of preference. The first one found is used.
var ConfigFilenames = []string{".prenup.yaml", ".prenup.yml"}

// OutputMode controls how prenup renders progress and results.
type OutputMode string

const (
	// OutputAuto selects human for TTYs, markdown otherwise.
	OutputAuto OutputMode = "auto"
	// OutputHuman is the Bubble Tea interactive UI.
	OutputHuman OutputMode = "human"
	// OutputMarkdown is a plain-text streaming + final markdown digest mode.
	OutputMarkdown OutputMode = "markdown"
	// OutputJSON is an NDJSON event stream mode for agents.
	OutputJSON OutputMode = "json"
)

// Config is the parsed v2 configuration.
type Config struct {
	Version        int        `yaml:"version"`
	ModuleMarkers  []string   `yaml:"module_markers,omitempty"`
	Exclude        []string   `yaml:"exclude,omitempty"`
	CleanWorktree  *bool      `yaml:"clean_worktree,omitempty"`
	MaxParallelism int        `yaml:"max_parallelism,omitempty"`
	Output         OutputMode `yaml:"output,omitempty"`
	Tasks          []Task     `yaml:"tasks"`

	// Path is the absolute path the config was loaded from. Not serialized.
	Path string `yaml:"-"`
}

// Task is a single entry in the tasks list.
type Task struct {
	Name            string            `yaml:"name"`
	DefaultSelected bool              `yaml:"default_selected,omitempty"`
	Command         string            `yaml:"command"`
	PerModule       bool              `yaml:"per_module,omitempty"`
	WorkingDir      string            `yaml:"working_dir,omitempty"`
	Paths           []string          `yaml:"paths,omitempty"`
	PathsIgnore     []string          `yaml:"paths_ignore,omitempty"`
	OutputPatterns  []string          `yaml:"output_patterns,omitempty"`
	StageOutput     bool              `yaml:"stage_output,omitempty"`
	Parallel        *bool             `yaml:"parallel,omitempty"`
	CleanWorktree   *bool             `yaml:"clean_worktree,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
}

// CleanWorktreeEnabled returns the effective clean_worktree value for this task
// given the config-level default.
func (t Task) CleanWorktreeEnabled(cfgDefault bool) bool {
	if t.CleanWorktree != nil {
		return *t.CleanWorktree
	}
	return cfgDefault
}

// ParallelEnabled returns whether per-module runs of this task may fan out.
// Defaults to true for per_module tasks, false otherwise.
func (t Task) ParallelEnabled() bool {
	if t.Parallel != nil {
		return *t.Parallel
	}
	return t.PerModule
}

// DefaultConfig returns a Config populated with all defaults applied.
// Callers then overlay user-provided YAML on top.
func DefaultConfig() Config {
	trueVal := true
	return Config{
		Version:        2,
		ModuleMarkers:  []string{"go.mod"},
		CleanWorktree:  &trueVal,
		MaxParallelism: 0, // 0 == NumCPU at runtime
		Output:         OutputAuto,
	}
}

// CleanWorktreeEnabled returns the effective config-level default for stashing.
func (c Config) CleanWorktreeEnabled() bool {
	if c.CleanWorktree == nil {
		return true
	}
	return *c.CleanWorktree
}

// Find returns the absolute path to the first config file found under repoRoot.
// Returns an empty string and nil error if no config exists.
func Find(repoRoot string) (string, error) {
	for _, name := range ConfigFilenames {
		p := filepath.Join(repoRoot, name)
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			return p, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("checking %s: %w", p, err)
		}
	}
	return "", nil
}

// Load parses the YAML file at path and returns a fully-defaulted, validated Config.
// The path must be inside repoRoot.
func Load(path, repoRoot string) (Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolving config path: %w", err)
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolving repo root: %w", err)
	}
	// Resolve symlinks on both sides before comparing. A purely
	// lexical check would let a symlink that lives inside the repo
	// but targets a file outside the repo slip through and be read,
	// which would defeat the "config must be inside the repo" rule.
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return Config{}, fmt.Errorf("resolving config path: %w", err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(absRepo)
	if err != nil {
		return Config{}, fmt.Errorf("resolving repo root: %w", err)
	}
	rel, err := filepath.Rel(resolvedRepo, resolvedPath)
	if err != nil {
		return Config{}, fmt.Errorf("checking config path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Config{}, errors.New("config file path is outside the repository")
	}

	// resolvedPath is validated above to live under the repository root.
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return Config{}, fmt.Errorf("reading config: %w", err)
	}

	return Parse(data, resolvedPath)
}

// Parse decodes YAML bytes into a Config, applies defaults, and validates.
// pathForErrors is used only in error messages.
func Parse(data []byte, pathForErrors string) (Config, error) {
	// Start from a zero Config so we can detect a missing `version:` key; the
	// user-provided YAML is then overlaid and defaults applied afterwards.
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		// Common footgun: `version: "2"` (string instead of int).
		if hint := versionTypeHint(data); hint != "" {
			return Config{}, fmt.Errorf("parsing %s: %s (underlying error: %w)", pathForErrors, hint, err)
		}
		return Config{}, fmt.Errorf("parsing %s: %w", pathForErrors, err)
	}

	if cfg.Version == 0 {
		// Missing `version:` is treated as v1 and rejected to force users
		// through `prenup migrate`.
		return Config{}, errors.New(`config is missing "version: 2". If this is a v1 config, run "prenup migrate"`)
	}

	// Apply defaults for fields YAML unmarshal would leave at zero.
	defaults := DefaultConfig()
	if cfg.CleanWorktree == nil {
		cfg.CleanWorktree = defaults.CleanWorktree
	}
	if len(cfg.ModuleMarkers) == 0 {
		cfg.ModuleMarkers = []string{"go.mod"}
	}
	if cfg.Output == "" {
		cfg.Output = OutputAuto
	}
	cfg.Path = pathForErrors

	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate performs structural checks on cfg and returns a combined error.
func Validate(cfg Config) error {
	errs := validateTopLevel(cfg)
	seen := make(map[string]bool)
	for i := range cfg.Tasks {
		errs = append(errs, validateTask(&cfg.Tasks[i], i, seen)...)
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid config %s:\n  - %s", cfg.Path, strings.Join(errs, "\n  - "))
}

// validateTopLevel checks all non-task fields.
func validateTopLevel(cfg Config) []string {
	var errs []string
	if cfg.Version != 2 {
		errs = append(errs, fmt.Sprintf("unsupported config version %d (expected 2)", cfg.Version))
	}
	switch cfg.Output {
	case OutputAuto, OutputHuman, OutputMarkdown, OutputJSON, "":
	default:
		errs = append(errs, fmt.Sprintf("unknown output mode %q (want auto, human, markdown, or json)", cfg.Output))
	}
	if cfg.MaxParallelism < 0 {
		errs = append(errs, fmt.Sprintf("max_parallelism must be >= 0, got %d", cfg.MaxParallelism))
	}
	if len(cfg.Tasks) == 0 {
		errs = append(errs, "no tasks defined")
	}
	for _, p := range cfg.Exclude {
		if err := validatePattern(p); err != nil {
			errs = append(errs, "exclude: "+err.Error())
		}
	}
	return errs
}

// validateTask checks a single task entry. seen accumulates names across
// calls so duplicates can be flagged.
func validateTask(t *Task, idx int, seen map[string]bool) []string {
	var errs []string
	label := fmt.Sprintf("tasks[%d]", idx)
	switch {
	case t.Name == "":
		errs = append(errs, label+": name is required")
	case seen[t.Name]:
		errs = append(errs, fmt.Sprintf("%s: duplicate task name %q", label, t.Name))
	default:
		seen[t.Name] = true
	}
	if strings.TrimSpace(t.Command) == "" {
		errs = append(errs, fmt.Sprintf("%s (%s): command is required", label, t.Name))
	}
	if t.StageOutput && len(t.OutputPatterns) == 0 {
		errs = append(errs, fmt.Sprintf("%s (%s): stage_output: true requires output_patterns", label, t.Name))
	}
	errs = append(errs, validateTaskPatterns(label, t)...)
	return errs
}

// validateTaskPatterns runs validatePattern over every pattern field on t.
func validateTaskPatterns(label string, t *Task) []string {
	var errs []string
	for _, group := range []struct {
		field    string
		patterns []string
	}{
		{"paths", t.Paths},
		{"paths_ignore", t.PathsIgnore},
		{"output_patterns", t.OutputPatterns},
	} {
		for _, p := range group.patterns {
			if err := validatePattern(p); err != nil {
				errs = append(errs, fmt.Sprintf("%s (%s) %s: %s", label, t.Name, group.field, err))
			}
		}
	}
	return errs
}

// validatePattern returns an error if the doublestar pattern is malformed.
func validatePattern(p string) error {
	if _, err := doublestar.Match(p, ""); err != nil {
		return fmt.Errorf("invalid pattern %q: %w", p, err)
	}
	return nil
}

// versionTypeHint returns a friendly hint when YAML decoding fails because
// `version:` was a string instead of an int. Returns "" if the source does
// not contain that pattern.
func versionTypeHint(data []byte) string {
	var probe struct {
		Version yaml.Node `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return ""
	}
	if probe.Version.Kind == yaml.ScalarNode && probe.Version.Tag == "!!str" {
		return `"version" must be an integer (use "version: 2", not "version: \"2\"")`
	}
	return ""
}
