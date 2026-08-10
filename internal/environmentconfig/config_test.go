package environmentconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCombinesPoliciesAndEnvironmentSource(t *testing.T) {
	t.Setenv("PATH", "/trusted/bin")
	t.Setenv("COMPANY_PIP_INDEX", "https://packages.example.test/simple")
	cfg := Config{
		Version: 1,
		Minimal: Policy{
			Inherit: []string{"PATH"},
			Values:  map[string]string{"LANG": "C.UTF-8"},
		},
		PackageManagers: map[string]PackagePolicy{
			"pip": {
				Values: map[string]string{"PIP_DISABLE_PIP_VERSION_CHECK": "1"},
				EnvironmentFrom: map[string]EnvSource{
					"PIP_INDEX_URL": {Source: "COMPANY_PIP_INDEX", Required: true},
				},
			},
		},
	}

	environment, err := cfg.Build("pip")
	if err != nil {
		t.Fatal(err)
	}
	if environment["PATH"] != "/trusted/bin" ||
		environment["LANG"] != "C.UTF-8" ||
		environment["PIP_INDEX_URL"] != "https://packages.example.test/simple" {
		t.Fatalf("Build() = %#v", environment)
	}
}

func TestBuildRejectsMissingRequiredSource(t *testing.T) {
	const source = "ARTIFACT_DOWNLOADER_MISSING_POLICY_SOURCE"
	_ = os.Unsetenv(source)
	cfg := Config{
		Version: 1,
		PackageManagers: map[string]PackagePolicy{
			"pip": {
				EnvironmentFrom: map[string]EnvSource{
					"PIP_INDEX_URL": {Source: source, Required: true},
				},
			},
		},
	}
	if _, err := cfg.Build("pip"); err == nil {
		t.Fatal("Build() accepted a missing required source")
	}
}

func TestValidateRejectsReservedVariable(t *testing.T) {
	cfg := Config{
		Version: 1,
		PackageManagers: map[string]PackagePolicy{
			"pip": {Values: map[string]string{"PIP_CACHE_DIR": "/unsafe"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted a reserved variable")
	}
}

func TestValidateJobEnvironment(t *testing.T) {
	if err := ValidateJobEnvironment(map[string]string{"CI": "true", "NODE_OPTIONS": "--max-old-space-size=4096"}); err != nil {
		t.Fatalf("ValidateJobEnvironment() rejected valid names: %v", err)
	}
	if err := ValidateJobEnvironment(map[string]string{"INVALID-NAME": "value"}); err == nil {
		t.Fatal("ValidateJobEnvironment() accepted an invalid name")
	}
	if err := ValidateJobEnvironment(map[string]string{"ARTIFACT_CACHE": "/override"}); err == nil {
		t.Fatal("ValidateJobEnvironment() accepted a reserved name")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environment.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted an unknown field")
	}
}
