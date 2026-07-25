package environmentconfig

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version         int                      `yaml:"version"`
	Minimal         Policy                   `yaml:"minimal"`
	PackageManagers map[string]PackagePolicy `yaml:"packageManagers"`
}

type Policy struct {
	Inherit []string          `yaml:"inherit"`
	Values  map[string]string `yaml:"values"`
}

type PackagePolicy struct {
	Inherit         []string             `yaml:"inherit"`
	Values          map[string]string    `yaml:"values"`
	EnvironmentFrom map[string]EnvSource `yaml:"environmentFrom"`
}

type EnvSource struct {
	Source   string `yaml:"source"`
	Required bool   `yaml:"required"`
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var reserved = map[string]struct{}{
	"ARTIFACT_CACHE":    {},
	"ARTIFACT_OUTPUT":   {},
	"GRADLE_USER_HOME":  {},
	"PIP_CACHE_DIR":     {},
	"npm_config_cache":  {},
	"YARN_CACHE_FOLDER": {},
	"HOME":              {},
}

func Default() Config {
	return Config{
		Version: 1,
		Minimal: Policy{Inherit: []string{
			"PATH", "LANG", "LC_ALL", "TMPDIR",
			"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
			"http_proxy", "https_proxy", "no_proxy",
		}},
	}
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("read environment config: %w", err)
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported environment config version %d (expected 1)", c.Version)
	}
	if err := validatePolicy("minimal", c.Minimal.Inherit, c.Minimal.Values); err != nil {
		return err
	}
	for manager, policy := range c.PackageManagers {
		normalized := strings.ToLower(strings.TrimSpace(manager))
		if manager != normalized {
			return fmt.Errorf("package manager key %q must use its canonical lowercase name", manager)
		}
		manager = normalized
		switch manager {
		case "gradle", "mvn", "npm", "yarn", "pip":
		default:
			return fmt.Errorf("packageManagers contains unsupported package manager %q", manager)
		}
		prefix := "packageManagers." + manager
		if err := validatePolicy(prefix, policy.Inherit, policy.Values); err != nil {
			return err
		}
		for target, source := range policy.EnvironmentFrom {
			if err := validateWritableName(prefix+".environmentFrom", target); err != nil {
				return err
			}
			if !environmentName.MatchString(source.Source) {
				return fmt.Errorf("%s.environmentFrom.%s.source is not a valid environment variable name", prefix, target)
			}
		}
	}
	return nil
}

func validatePolicy(prefix string, inherit []string, values map[string]string) error {
	for _, name := range inherit {
		if !environmentName.MatchString(name) {
			return fmt.Errorf("%s.inherit contains invalid environment variable name %q", prefix, name)
		}
		if _, blocked := reserved[name]; blocked {
			return fmt.Errorf("%s cannot inherit reserved variable %s", prefix, name)
		}
	}
	for name := range values {
		if err := validateWritableName(prefix+".values", name); err != nil {
			return err
		}
	}
	return nil
}

func validateWritableName(prefix, name string) error {
	if !environmentName.MatchString(name) {
		return fmt.Errorf("%s contains invalid environment variable name %q", prefix, name)
	}
	if _, blocked := reserved[name]; blocked {
		return fmt.Errorf("%s cannot set reserved variable %s", prefix, name)
	}
	return nil
}

func (c Config) Build(manager string) (map[string]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	result := make(map[string]string)
	inherit(result, c.Minimal.Inherit)
	for name, value := range c.Minimal.Values {
		result[name] = value
	}

	policy := c.PackageManagers[strings.ToLower(strings.TrimSpace(manager))]
	inherit(result, policy.Inherit)
	for name, value := range policy.Values {
		result[name] = value
	}
	for target, source := range policy.EnvironmentFrom {
		value, exists := os.LookupEnv(source.Source)
		if !exists {
			if source.Required {
				return nil, fmt.Errorf("required environment source %s is not set", source.Source)
			}
			continue
		}
		result[target] = value
	}
	return result, nil
}

func inherit(destination map[string]string, names []string) {
	for _, name := range names {
		if value, exists := os.LookupEnv(name); exists {
			destination[name] = value
		}
	}
}
