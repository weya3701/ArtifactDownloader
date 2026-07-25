package packagecommand

import (
	"reflect"
	"testing"
)

func TestResolveAllowedCommands(t *testing.T) {
	tests := map[string]struct {
		manager    string
		action     string
		executable string
		args       []string
		cacheKey   string
	}{
		"gradle": {"gradle", "build", "gradle", []string{"build", "--no-daemon"}, "GRADLE_USER_HOME"},
		"maven":  {"mvn", "build", "mvn", []string{"package", "--batch-mode", "-Dmaven.repo.local=/cache"}, ""},
		"npm":    {"npm", "install", "npm", []string{"ci", "--ignore-scripts"}, "npm_config_cache"},
		"yarn":   {"yarn", "install", "yarn", []string{"install", "--immutable", "--ignore-scripts"}, "YARN_CACHE_FOLDER"},
		"pip":    {"pip", "download", "python3", []string{"-m", "pip", "download", "-r", "requirements.txt", "--dest", "/output"}, "PIP_CACHE_DIR"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			spec, err := Resolve(test.manager, test.action, Variables{Cache: "/cache", Output: "/output", Home: "/home"})
			if err != nil {
				t.Fatal(err)
			}
			if spec.Executable != test.executable || !reflect.DeepEqual(spec.Args, test.args) {
				t.Fatalf("Resolve() = %#v", spec)
			}
			if test.cacheKey != "" && spec.Environment[test.cacheKey] != "/cache" {
				t.Fatalf("%s = %q", test.cacheKey, spec.Environment[test.cacheKey])
			}
			if spec.Environment["HOME"] != "/home" {
				t.Fatalf("HOME = %q", spec.Environment["HOME"])
			}
		})
	}
}

func TestResolveRejectsUnknownAction(t *testing.T) {
	if _, err := Resolve("gradle", "custom", Variables{}); err == nil {
		t.Fatal("Resolve() accepted an unknown action")
	}
}

func TestResolvePipRequiresOutput(t *testing.T) {
	if _, err := Resolve("pip", "download", Variables{Cache: "/cache"}); err == nil {
		t.Fatal("Resolve() accepted pip download without output")
	}
}
