package config

import (
	"os"
	"path/filepath"
	"strings"
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
    workspace: .
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
	if cfg.Jobs[0].Workspace != "." {
		t.Fatalf("workspace = %q, want .", cfg.Jobs[0].Workspace)
	}
}

func TestLoadPackageEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: npm
    type: package
    cache: ./cache
    packageManager: npm
    repository:
      url: https://example.test/repository.git
    command:
      action: install
    environment:
      CI: "true"
      PACKAGE_CACHE: ${ARTIFACT_CACHE}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	environment := cfg.Jobs[0].Environment
	if environment["CI"] != "true" || environment["PACKAGE_CACHE"] != "${ARTIFACT_CACHE}" {
		t.Fatalf("environment = %#v", environment)
	}
}

func TestLoadAcceptsTemplatedPackageCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: package
    type: package
    cache: ./cache
    output: ./output
    packageManager: ${PKGMANAGER}
    repository:
      url: https://example.test/repository.git
    command:
      action: ${ACTION}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs[0].PackageManager != "${PKGMANAGER}" || cfg.Jobs[0].Command.Action != "${ACTION}" {
		t.Fatalf("templated package command = %#v", cfg.Jobs[0])
	}
}

func TestExpandJobEnvironmentFromHost(t *testing.T) {
	t.Setenv("PROJECT", "project-name")
	t.Setenv("REPOSITORY", "repository-name")
	t.Setenv("BRANCH", "main")
	t.Setenv("WORKDIR", "src")
	t.Setenv("PIPELINE_WORKSPACE", "./pipeline-workspace")
	t.Setenv("OUTPUT", "./artifacts")
	t.Setenv("ADO_COLLECTION", "collection-name")
	t.Setenv("PKGMANAGER", "npm")
	t.Setenv("ACTION", "install-unlocked")

	job := Job{
		Output: "${OUTPUT}", Cache: "./cache", Workspace: "${PIPELINE_WORKSPACE}", WorkingDirectory: "${WORKDIR}",
		PackageManager: "${PKGMANAGER}", Command: PackageCommand{Action: "${ACTION}"},
		Repository: Repository{
			URL:     "https://dev.azure.com/org/${PROJECT}/_git/${REPOSITORY}",
			Ref:     "${BRANCH}",
			GitArgs: []string{"header=${ADO_AUTH_HEADER}"},
		},
		Environment: map[string]string{
			"COLLECTION":    "${ADO_COLLECTION}",
			"PACKAGE_CACHE": "${ARTIFACT_CACHE}",
		},
		Callback: CallbackCommands{{
			Executable: "./publisher-${PROJECT}",
			Args:       []string{"${ADO_COLLECTION}", "${ARTIFACT_OUTPUT}"},
		}},
	}

	expanded, err := ExpandJobEnvironment(job, true)
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Repository.URL != "https://dev.azure.com/org/project-name/_git/repository-name" ||
		expanded.Repository.Ref != "main" || expanded.Workspace != "./pipeline-workspace" ||
		expanded.WorkingDirectory != "src" || expanded.Output != "./artifacts" ||
		expanded.PackageManager != "npm" || expanded.Command.Action != "install-unlocked" {
		t.Fatalf("expanded job fields = %#v", expanded)
	}
	if expanded.Environment["COLLECTION"] != "collection-name" || expanded.Environment["PACKAGE_CACHE"] != "${ARTIFACT_CACHE}" {
		t.Fatalf("expanded environment = %#v", expanded.Environment)
	}
	if expanded.Callback[0].Executable != "./publisher-project-name" ||
		expanded.Callback[0].Args[0] != "collection-name" || expanded.Callback[0].Args[1] != "${ARTIFACT_OUTPUT}" {
		t.Fatalf("expanded callback = %#v", expanded.Callback)
	}
	if expanded.Repository.GitArgs[0] != "header=${ADO_AUTH_HEADER}" {
		t.Fatalf("gitArgs should remain deferred: %#v", expanded.Repository.GitArgs)
	}
}

func TestExpandedPackageCommandStillUsesAllowlist(t *testing.T) {
	t.Setenv("PKGMANAGER", "custom")
	t.Setenv("ACTION", "execute-anything")
	job := Job{
		Name: "package", Type: JobTypePackage, Cache: "cache", Output: "output",
		Repository: Repository{URL: "repository"}, Timeout: Duration(time.Minute),
		PackageManager: "${PKGMANAGER}", Command: PackageCommand{Action: "${ACTION}"},
	}
	expanded, err := ExpandJobEnvironment(job, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Config{Version: 1, Jobs: []Job{expanded}}).Validate(); err == nil {
		t.Fatal("Validate() accepted an unsupported expanded package command")
	}
}

func TestExpandJobEnvironmentRequiresInheritance(t *testing.T) {
	job := Job{Repository: Repository{URL: "https://example.test/${PROJECT}"}}
	if _, err := ExpandJobEnvironment(job, false); err == nil || !strings.Contains(err.Error(), "--inherit-environment") {
		t.Fatalf("ExpandJobEnvironment() error = %v", err)
	}
}

func TestExpandJobEnvironmentRejectsMissingVariable(t *testing.T) {
	const name = "ARTIFACT_DOWNLOADER_MISSING_JOB_VARIABLE"
	_ = os.Unsetenv(name)
	job := Job{WorkingDirectory: "${" + name + "}"}
	if _, err := ExpandJobEnvironment(job, true); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("ExpandJobEnvironment() error = %v", err)
	}
}

func TestPackageRejectsReservedEnvironmentVariable(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "npm", Type: JobTypePackage, Cache: "cache",
		Repository: Repository{URL: "repo"}, PackageManager: "npm",
		Command: PackageCommand{Action: "install"}, Timeout: Duration(time.Minute),
		Environment: map[string]string{"npm_config_cache": "/override"},
	}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted a reserved package environment variable")
	}
}

func TestURLJobRejectsEnvironment(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "files", Type: JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 1, Timeout: Duration(time.Minute),
		Environment: map[string]string{"CI": "true"},
	}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted environment on a URL job")
	}
}

func TestCallbackArgsRequireExecutable(t *testing.T) {
	cfg := Config{Version: 1, Jobs: []Job{{
		Name: "files", Type: JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 1, Timeout: Duration(time.Minute),
		Callback: CallbackCommands{{Args: []string{"done"}}},
	}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted callback args without executable")
	}
}

func TestLoadCallbackListPreservesOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: files
    type: urls
    output: out
    urlList: urls.txt
    callback:
      - executable: first
        args: [one]
      - executable: second
        args: [two]
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := cfg.Jobs[0].Callback
	if len(callbacks) != 2 || callbacks[0].Executable != "first" || callbacks[1].Executable != "second" {
		t.Fatalf("callback order = %#v", callbacks)
	}
}

func TestLoadAcceptsLegacySingleCallbackObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: files
    type: urls
    output: out
    urlList: urls.txt
    callback:
      executable: legacy
      args: [done]
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := cfg.Jobs[0].Callback
	if len(callbacks) != 1 || callbacks[0].Executable != "legacy" {
		t.Fatalf("legacy callback = %#v", callbacks)
	}
}

func TestLoadRejectsUnknownCallbackField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
jobs:
  - name: files
    type: urls
    output: out
    urlList: urls.txt
    callback:
      - executable: first
        unknown: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted an unknown callback field")
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
