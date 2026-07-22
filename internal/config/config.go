package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	JobTypeURLs    = "urls"
	JobTypePackage = "package"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	value, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(value)
	return nil
}

func (d Duration) Value() time.Duration { return time.Duration(d) }

type Config struct {
	Version int   `yaml:"version"`
	Jobs    []Job `yaml:"jobs"`

	BaseDir string `yaml:"-"`
}

type Job struct {
	Name             string            `yaml:"name"`
	Type             string            `yaml:"type"`
	Output           string            `yaml:"output"`
	Cache            string            `yaml:"cache"`
	URLList          string            `yaml:"urlList"`
	Concurrency      int               `yaml:"concurrency"`
	Timeout          Duration          `yaml:"timeout"`
	Overwrite        bool              `yaml:"overwrite"`
	Repository       Repository        `yaml:"repository"`
	WorkingDirectory string            `yaml:"workingDirectory"`
	PackageManager   string            `yaml:"packageManager"`
	Command          Command           `yaml:"command"`
	Environment      map[string]string `yaml:"environment"`
}

type Repository struct {
	URL       string   `yaml:"url"`
	Ref       string   `yaml:"ref"`
	Depth     int      `yaml:"depth"`
	GitArgs   []string `yaml:"gitArgs"`
	CloneArgs []string `yaml:"cloneArgs"`
}

type Command struct {
	Executable string   `yaml:"executable"`
	Args       []string `yaml:"args"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.BaseDir = filepath.Dir(absPath)
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	for i := range c.Jobs {
		if c.Jobs[i].Concurrency == 0 {
			c.Jobs[i].Concurrency = 4
		}
		if c.Jobs[i].Timeout == 0 {
			c.Jobs[i].Timeout = Duration(10 * time.Minute)
		}
	}
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d (expected 1)", c.Version)
	}
	if len(c.Jobs) == 0 {
		return errors.New("config must contain at least one job")
	}

	names := make(map[string]struct{}, len(c.Jobs))
	for i, job := range c.Jobs {
		prefix := fmt.Sprintf("jobs[%d]", i)
		if strings.TrimSpace(job.Name) == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if _, exists := names[job.Name]; exists {
			return fmt.Errorf("duplicate job name %q", job.Name)
		}
		names[job.Name] = struct{}{}
		if job.Timeout.Value() <= 0 {
			return fmt.Errorf("job %q: timeout must be positive", job.Name)
		}

		switch job.Type {
		case JobTypeURLs:
			if strings.TrimSpace(job.Output) == "" {
				return fmt.Errorf("job %q: output is required", job.Name)
			}
			if strings.TrimSpace(job.URLList) == "" {
				return fmt.Errorf("job %q: urlList is required", job.Name)
			}
			if job.Concurrency < 1 {
				return fmt.Errorf("job %q: concurrency must be positive", job.Name)
			}
		case JobTypePackage:
			if strings.TrimSpace(job.Cache) == "" {
				return fmt.Errorf("job %q: cache is required", job.Name)
			}
			if strings.TrimSpace(job.Repository.URL) == "" {
				return fmt.Errorf("job %q: repository.url is required", job.Name)
			}
			if job.Repository.Depth < 0 {
				return fmt.Errorf("job %q: repository.depth cannot be negative", job.Name)
			}
			if strings.TrimSpace(job.Command.Executable) == "" {
				return fmt.Errorf("job %q: command.executable is required", job.Name)
			}
			if !supportedPackageManager(job.PackageManager) {
				return fmt.Errorf("job %q: unsupported packageManager %q", job.Name, job.PackageManager)
			}
		case "":
			return fmt.Errorf("job %q: type is required", job.Name)
		default:
			return fmt.Errorf("job %q: unsupported type %q", job.Name, job.Type)
		}
	}
	return nil
}

func supportedPackageManager(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "gradle", "mvn", "npm", "pip", "yarn":
		return true
	default:
		return false
	}
}

func (c Config) Resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.BaseDir, path)
}
