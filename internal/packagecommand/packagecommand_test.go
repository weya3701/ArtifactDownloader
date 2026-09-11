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
		"gradle":       {"gradle", "build", "gradle", []string{"build", "--no-daemon"}, "GRADLE_USER_HOME"},
		"maven":        {"mvn", "build", "mvn", []string{"package", "--batch-mode", "-Dmaven.repo.local=/cache"}, ""},
		"npm":          {"npm", "install", "npm", []string{"ci", "--ignore-scripts"}, "npm_config_cache"},
		"npm unlocked": {"npm", "install-unlocked", "npm", []string{"install", "--ignore-scripts", "--no-package-lock"}, "npm_config_cache"},
		"yarn":         {"yarn", "install", "yarn", []string{"install", "--immutable", "--ignore-scripts"}, "YARN_CACHE_FOLDER"},
		"pip":          {"pip", "download", "python3", []string{"-m", "pip", "download", "-r", "requirements.txt", "--dest", "/output"}, "PIP_CACHE_DIR"},
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

func TestResolveExternalConfigFiles(t *testing.T) {
	tests := map[string]struct {
		manager     string
		action      string
		configFiles []ConfigFile
		wantArgs    []string
		wantEnv     map[string]string
	}{
		"gradle repeatable init scripts": {
			manager: "gradle", action: "build",
			configFiles: []ConfigFile{{Type: "init-script", Path: "/config/one.gradle"}, {Type: "init-script", Path: "/config/two.gradle.kts"}},
			wantArgs:    []string{"build", "--no-daemon", "--init-script", "/config/one.gradle", "--init-script", "/config/two.gradle.kts"},
		},
		"maven settings": {
			manager: "mvn", action: "build",
			configFiles: []ConfigFile{{Type: "settings", Path: "/config/settings.xml"}, {Type: "toolchains", Path: "/config/toolchains.xml"}},
			wantArgs:    []string{"package", "--batch-mode", "-Dmaven.repo.local=/cache", "--settings", "/config/settings.xml", "--toolchains", "/config/toolchains.xml"},
		},
		"npm user config": {
			manager: "npm", action: "install",
			configFiles: []ConfigFile{{Type: "userconfig", Path: "/config/npmrc"}},
			wantArgs:    []string{"ci", "--ignore-scripts", "--userconfig", "/config/npmrc"},
		},
		"pip config": {
			manager: "pip", action: "download",
			configFiles: []ConfigFile{{Type: "config-file", Path: "/config/pip.conf"}},
			wantArgs:    []string{"-m", "pip", "download", "-r", "requirements.txt", "--dest", "/output"},
			wantEnv:     map[string]string{"PIP_CONFIG_FILE": "/config/pip.conf"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			spec, err := Resolve(test.manager, test.action, Variables{
				Cache: "/cache", Output: "/output", Home: "/home", ConfigFiles: test.configFiles,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(spec.Args, test.wantArgs) {
				t.Fatalf("Args = %#v, want %#v", spec.Args, test.wantArgs)
			}
			for name, want := range test.wantEnv {
				if got := spec.Environment[name]; got != want {
					t.Fatalf("Environment[%q] = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestValidateConfigFilesRejectsUnsupportedCombination(t *testing.T) {
	err := ValidateConfigFiles("yarn", []ConfigFile{{Type: "init-script", Path: "/config/init.gradle"}})
	if err == nil {
		t.Fatal("ValidateConfigFiles() accepted a Gradle config type for Yarn")
	}
}

func TestValidateConfigFilesRejectsDuplicateSingleton(t *testing.T) {
	err := ValidateConfigFiles("mvn", []ConfigFile{
		{Type: "settings", Path: "/config/one.xml"},
		{Type: "settings", Path: "/config/two.xml"},
	})
	if err == nil {
		t.Fatal("ValidateConfigFiles() accepted duplicate Maven settings files")
	}
}
