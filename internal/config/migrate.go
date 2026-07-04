package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// V1Config matches the v1 YAML shape so we can read existing .prenup.yml files
// and rewrite them as v2.
type V1Config struct {
	Exclude []string `yaml:"exclude"`
	Tasks   []V1Task `yaml:"tasks"`
}

// V1Task is the v1 task entry.
type V1Task struct {
	Name            string   `yaml:"name"`
	DefaultSelected bool     `yaml:"default_selected"`
	PerModule       bool     `yaml:"per_module"`
	Command         string   `yaml:"command,omitempty"`
	WorkingDir      string   `yaml:"working_dir,omitempty"`
	StageOutput     bool     `yaml:"stage_output,omitempty"`
	OutputPatterns  []string `yaml:"output_patterns,omitempty"`
}

// MigrateV1 converts v1 YAML bytes into a v2 Config and returns freshly
// marshaled YAML bytes. The returned Config is also populated for callers that
// want to inspect it directly.
func MigrateV1(data []byte) (Config, []byte, error) {
	var v1 V1Config
	if err := yaml.Unmarshal(data, &v1); err != nil {
		return Config{}, nil, fmt.Errorf("parsing v1 config: %w", err)
	}

	cfg := DefaultConfig()
	cfg.Exclude = v1.Exclude
	cfg.Tasks = make([]Task, 0, len(v1.Tasks))
	for _, t := range v1.Tasks {
		cfg.Tasks = append(cfg.Tasks, Task{
			Name:            t.Name,
			DefaultSelected: t.DefaultSelected,
			Command:         t.Command,
			PerModule:       t.PerModule,
			WorkingDir:      t.WorkingDir,
			OutputPatterns:  t.OutputPatterns,
			StageOutput:     t.StageOutput,
		})
	}

	// Omit the default CleanWorktree pointer from the marshaled output unless
	// the user has opinions; defaults are applied at load time.
	cfg.CleanWorktree = nil

	out, err := Marshal(cfg)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, out, nil
}

// Marshal writes a v2 config back to YAML with a stable, commented layout.
func Marshal(cfg Config) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# Prenup configuration. See https://github.com/c2fo/prenup for docs.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encoding config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("closing encoder: %w", err)
	}
	return buf.Bytes(), nil
}
