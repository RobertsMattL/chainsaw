package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Root struct {
	Configs []LogConfig `yaml:"configs"`
}

type LogConfig struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Sources     []Source    `yaml:"sources"`
	Highlights  []Highlight `yaml:"highlights,omitempty"`
	Filters     Filter      `yaml:"filters,omitempty"`
	JSONMode    bool        `yaml:"json_mode,omitempty"`
}

type Source struct {
	Type    string `yaml:"type"`              // "file" or "command"
	Path    string `yaml:"path,omitempty"`    // file type: path or glob
	Command string `yaml:"command,omitempty"` // command type: shell command
	Label   string `yaml:"label,omitempty"`   // display label for this source
}

type Highlight struct {
	Pattern string `yaml:"pattern"`
	Style   string `yaml:"style"`
	Mode    string `yaml:"mode,omitempty"` // "line" (default) or "match"
}

type Filter struct {
	Include string `yaml:"include,omitempty"` // regex: only show matching lines
	Exclude string `yaml:"exclude,omitempty"` // regex: hide matching lines
}

func DefaultHighlights() []Highlight {
	return []Highlight{
		{Pattern: `\b(FATAL|CRITICAL)\b`, Style: "magenta+bold", Mode: "line"},
		{Pattern: `\b(ERROR|ERR)\b`, Style: "red+bold", Mode: "line"},
		{Pattern: `\b(WARN|WARNING)\b`, Style: "yellow", Mode: "line"},
		{Pattern: `\b(INFO)\b`, Style: "cyan", Mode: "line"},
		{Pattern: `\b(DEBUG)\b`, Style: "gray", Mode: "line"},
		{Pattern: `\b(TRACE)\b`, Style: "gray+faint", Mode: "line"},
	}
}

func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "chainsaw", "configs.yaml")
}

func Load() (*Root, error) {
	path := Path()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Root{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var root Root
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &root, nil
}

func Save(root *Root) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (r *Root) Find(name string) *LogConfig {
	for i := range r.Configs {
		if r.Configs[i].Name == name {
			return &r.Configs[i]
		}
	}
	return nil
}
