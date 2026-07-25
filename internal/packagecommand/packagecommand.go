package packagecommand

import (
	"fmt"
	"strings"
)

type Variables struct {
	Cache  string
	Output string
	Home   string
}

type Spec struct {
	Executable  string
	Args        []string
	Environment map[string]string
}

func Validate(manager, action string) error {
	manager = strings.ToLower(strings.TrimSpace(manager))
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return fmt.Errorf("command.action is required")
	}
	switch manager + ":" + action {
	case "gradle:build", "mvn:build", "npm:install", "yarn:install", "pip:download":
		return nil
	default:
		return fmt.Errorf("action %q is not allowed for packageManager %q", action, manager)
	}
}

func Resolve(manager, action string, variables Variables) (Spec, error) {
	manager = strings.ToLower(strings.TrimSpace(manager))
	action = strings.ToLower(strings.TrimSpace(action))
	if err := Validate(manager, action); err != nil {
		return Spec{}, err
	}

	spec := Spec{
		Environment: map[string]string{
			"ARTIFACT_CACHE":  variables.Cache,
			"ARTIFACT_OUTPUT": variables.Output,
			"HOME":            variables.Home,
		},
	}

	switch manager + ":" + action {
	case "gradle:build":
		spec.Executable = "gradle"
		spec.Args = []string{"build", "--no-daemon"}
		spec.Environment["GRADLE_USER_HOME"] = variables.Cache
	case "mvn:build":
		spec.Executable = "mvn"
		spec.Args = []string{"package", "--batch-mode", "-Dmaven.repo.local=" + variables.Cache}
	case "npm:install":
		spec.Executable = "npm"
		spec.Args = []string{"ci", "--ignore-scripts"}
		spec.Environment["npm_config_cache"] = variables.Cache
	case "yarn:install":
		spec.Executable = "yarn"
		spec.Args = []string{"install", "--immutable", "--ignore-scripts"}
		spec.Environment["YARN_CACHE_FOLDER"] = variables.Cache
	case "pip:download":
		if variables.Output == "" {
			return Spec{}, errorsForOutput(manager, action)
		}
		fmt.Println("variables output: ", variables.Output)
		spec.Executable = "python3"
		spec.Args = []string{"-m", "pip", "download", "-r", "requirements.txt", "--dest", variables.Output}
		spec.Environment["PIP_CACHE_DIR"] = variables.Cache
	default:
		return Spec{}, fmt.Errorf("package command %q has no execution specification", manager+":"+action)
	}
	return spec, nil
}

func errorsForOutput(manager, action string) error {
	return fmt.Errorf("output is required for %s action %q", manager, action)
}
