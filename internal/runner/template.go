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
// Supported: {{.repo_root}}, {{.module_root}}, {{.module_path}}, {{.module_name}}.
// Unknown variables are left untouched.
func (v TemplateVars) Expand(s string) string {
	pairs := map[string]string{
		"{{.repo_root}}":   v.RepoRoot,
		"{{.module_root}}": v.ModuleRoot,
		"{{.module_path}}": v.ModulePath,
		"{{.module_name}}": v.ModuleName,
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
