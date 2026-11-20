package runner

import (
	"path/filepath"
	"strings"
)

// TemplateVars holds the values substituted into command and working_dir.
type TemplateVars struct {
	RepoRoot   string
	ModuleRoot string
	ModulePath string
	ModuleName string
	// GitDir is the absolute per-checkout git directory: <repo>/.git for a
	// normal checkout, <common>/worktrees/<name> for a linked worktree.
	GitDir string
	// GitCommonDir is the absolute git directory shared by all worktrees of
	// the repository (the main checkout's .git).
	GitCommonDir string
}

const (
	varGitDir       = "{{.git_dir}}"
	varGitCommonDir = "{{.git_common_dir}}"
)

// usesGitDirs reports whether any of the strings reference {{.git_dir}} or
// {{.git_common_dir}}. Resolving them costs a git invocation, so callers only
// do it when a task actually asks.
func usesGitDirs(ss ...string) bool {
	for _, s := range ss {
		if strings.Contains(s, varGitDir) || strings.Contains(s, varGitCommonDir) {
			return true
		}
	}
	return false
}

// NewTemplateVars computes the template variables for module (a path relative
// to repoRoot). A module of "." yields the repo itself; for global tasks, call
// with module = "".
func NewTemplateVars(repoRoot, module string) TemplateVars {
	if module == "" {
		return TemplateVars{RepoRoot: repoRoot}
	}
	return TemplateVars{
		RepoRoot:   repoRoot,
		ModuleRoot: filepath.Join(repoRoot, module),
		ModulePath: module,
		ModuleName: filepath.Base(module),
	}
}

// Expand replaces the supported template variables in s.
// Supported: {{.repo_root}}, {{.module_root}}, {{.module_path}}, {{.module_name}},
// {{.git_dir}}, {{.git_common_dir}}.
// Unknown variables are left untouched.
func (v TemplateVars) Expand(s string) string {
	pairs := map[string]string{
		"{{.repo_root}}":   v.RepoRoot,
		"{{.module_root}}": v.ModuleRoot,
		"{{.module_path}}": v.ModulePath,
		"{{.module_name}}": v.ModuleName,
		varGitDir:          v.GitDir,
		varGitCommonDir:    v.GitCommonDir,
		// Backward-compatible alias that v1 users may have in configs.
		"{{.module}}": v.ModuleName,
	}
	for k, val := range pairs {
		if val == "" {
			continue
		}
		s = strings.ReplaceAll(s, k, val)
	}
	return s
}
