package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("version: 1\njobs:\n  - name: files\n    type: urls\n    output: ./out\n    urlList: ./urls.txt\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Jobs[0].Concurrency; got != 4 {
		t.Fatalf("concurrency = %d, want 4", got)
	}
	if got := cfg.Jobs[0].Timeout.Value(); got != 10*time.Minute {
		t.Fatalf("timeout = %s, want 10m", got)
	}
	if got := cfg.Resolve("out"); got != filepath.Join(dir, "out") {
		t.Fatalf("resolved output = %q", got)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nunknown: true\njobs: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() succeeded with unknown field")
	}
}

func TestPackageRequiresCache(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "build", Type: JobTypePackage, Repository: Repository{URL: "repo"},
		PackageManager: "gradle", Command: PackageCommand{Action: "build"},
		Timeout: Duration(time.Minute),
	}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted package job without cache")
	}
}

func TestLoadRepositoryArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: ado
    type: package
    cache: ./cache
    packageManager: gradle
    repository:
      url: https://dev.azure.com/org/project/_git/repo
      gitArgs:
        - -c
        - "http.extraHeader=AUTHORIZATION: basic ${ADO_AUTH_HEADER}"
      cloneArgs:
        - --no-tags
    command:
      action: build
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	repository := cfg.Jobs[0].Repository
	if len(repository.GitArgs) != 2 || len(repository.CloneArgs) != 1 {
		t.Fatalf("unexpected repository arguments: %#v", repository)
	}
}

func TestCallbackArgsRequireExecutable(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "files", Type: JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 1, Timeout: Duration(time.Minute),
		Callback: ExternalCommand{Args: []string{"done"}},
	}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted callback args without executable")
	}
}

func TestPackageRejectsUnknownAction(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "build", Type: JobTypePackage, Cache: "cache",
		Repository: Repository{URL: "repo"}, PackageManager: "gradle",
		Command: PackageCommand{Action: "custom"}, Timeout: Duration(time.Minute),
	}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted an unknown package action")
	}
}

func TestPackageAcceptsNPMInstallUnlocked(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "npm-unlocked", Type: JobTypePackage, Cache: "cache",
		Repository: Repository{URL: "repo"}, PackageManager: "npm",
		Command: PackageCommand{Action: "install-unlocked"}, Timeout: Duration(time.Minute),
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected npm install-unlocked: %v", err)
	}
}

func TestLoadRejectsLegacyPackageCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: build
    type: package
    cache: cache
    packageManager: gradle
    repository:
      url: repo
    command:
      executable: ./gradlew
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted legacy package executable")
	}
}
