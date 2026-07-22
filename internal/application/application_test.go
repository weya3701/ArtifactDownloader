package application

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"artifactdownloader/internal/config"
)

func TestRunnerURLJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artifact.bin" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "urls.txt"), []byte(server.URL+"/artifact.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "files", Type: config.JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 2, Timeout: config.Duration(time.Minute),
	}}}

	results, err := (Runner{}).Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil || results[0].Files != 1 {
		t.Fatalf("unexpected results: %#v", results)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", "artifact.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "downloaded" {
		t.Fatalf("content = %q", data)
	}
}

func TestSafeWorkingDirectoryRejectsEscape(t *testing.T) {
	if _, err := safeWorkingDirectory("/tmp/repo", "../../etc"); err == nil {
		t.Fatal("safeWorkingDirectory accepted escaping path")
	}
}

func TestRunnerPackageJob(t *testing.T) {
	dir := t.TempDir()
	repositoryDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(repositoryDir, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/bin/sh\nset -eu\ntest \"$TEST_CACHE\" = \"$GRADLE_USER_HOME\"\nprintf artifact > \"$ARTIFACT_OUTPUT/result.txt\"\nprintf cache > \"$ARTIFACT_CACHE/cache-used.txt\"\n")
	if err := os.WriteFile(filepath.Join(repositoryDir, "project", "download.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = repositoryDir
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-q")
	git("add", "project/download.sh")
	git("commit", "-q", "-m", "fixture")

	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "package", Type: config.JobTypePackage, Output: "artifacts", Cache: "cache",
		Repository: config.Repository{
			URL: repositoryDir, GitArgs: []string{"-c", "advice.detachedHead=false"},
			CloneArgs: []string{"--no-tags"},
		}, WorkingDirectory: "project",
		PackageManager: "gradle", Command: config.Command{Executable: "./download.sh"},
		Environment: map[string]string{"TEST_CACHE": "${ARTIFACT_CACHE}"}, Timeout: config.Duration(time.Minute),
	}}}
	var output bytes.Buffer
	results, err := (Runner{Stdout: &output, Stderr: &output}).Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("unexpected results: %#v; output: %s", results, output.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "artifacts", "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "artifact" {
		t.Fatalf("artifact content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", "cache-used.txt")); err != nil {
		t.Fatalf("package cache was not retained: %v", err)
	}
}

func TestApplyPackageCache(t *testing.T) {
	tests := map[string]struct {
		key string
	}{
		"gradle": {key: "GRADLE_USER_HOME"},
		"npm":    {key: "npm_config_cache"},
		"yarn":   {key: "YARN_CACHE_FOLDER"},
		"pip":    {key: "PIP_CACHE_DIR"},
	}
	for manager, test := range tests {
		t.Run(manager, func(t *testing.T) {
			environment := map[string]string{}
			applyPackageCache(manager, "/cache", environment)
			if environment[test.key] != "/cache" {
				t.Fatalf("%s = %q", test.key, environment[test.key])
			}
		})
	}
}

func TestExpandVariables(t *testing.T) {
	got := expandVariables("--dest=${ARTIFACT_CACHE}", map[string]string{"ARTIFACT_CACHE": "/cache"})
	if got != "--dest=/cache" {
		t.Fatalf("expandVariables() = %q", got)
	}
}
