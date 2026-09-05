package application

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"artifactdownloader/internal/config"
	"artifactdownloader/internal/environmentconfig"
)

func TestRunnerURLJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artifact.bin" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") != "ArtifactDownloader-Test/1.0" || r.Header.Get("Accept") != "*/*" {
			http.Error(w, "required headers are missing", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	dir := t.TempDir()
	t.Setenv("ARTIFACT_DOWNLOADER_URL_OUTPUT_TEST", "out")
	if err := os.WriteFile(filepath.Join(dir, "urls.txt"), []byte(server.URL+"/artifact.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "files", Type: config.JobTypeURLs, Output: "${ARTIFACT_DOWNLOADER_URL_OUTPUT_TEST}", URLList: "urls.txt",
		Concurrency: 2, Headers: map[string]string{
			"User-Agent": "ArtifactDownloader-Test/1.0",
			"Accept":     "*/*",
		}, Timeout: config.Duration(time.Minute),
	}}}

	results, err := (Runner{InheritEnvironment: true}).Run(context.Background(), cfg, "")
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

func TestRunnerURLJobSpacesRequestStartsWhileKeepingConcurrency(t *testing.T) {
	var mu sync.Mutex
	requestTimes := make([]time.Time, 0, 3)
	inFlight := 0
	maxInFlight := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestTimes = append(requestTimes, time.Now())
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()

		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("downloaded"))

		mu.Lock()
		inFlight--
		mu.Unlock()
	}))
	defer server.Close()

	dir := t.TempDir()
	list := []byte(server.URL + "/one.bin\n" + server.URL + "/two.bin\n" + server.URL + "/three.bin\n")
	if err := os.WriteFile(filepath.Join(dir, "urls.txt"), list, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "files", Type: config.JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 3,
		RequestDelay: config.RequestDelay{
			Min: config.Duration(25 * time.Millisecond),
			Max: config.Duration(25 * time.Millisecond),
		},
		Timeout: config.Duration(time.Minute),
	}}}

	results, err := (Runner{}).Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil || results[0].Files != 3 {
		t.Fatalf("unexpected results: %#v", results)
	}

	mu.Lock()
	times := append([]time.Time(nil), requestTimes...)
	peak := maxInFlight
	mu.Unlock()
	if len(times) != 3 {
		t.Fatalf("request count = %d, want 3", len(times))
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 20*time.Millisecond {
			t.Fatalf("request gap[%d] = %s, want at least 20ms", i, gap)
		}
	}
	if peak < 2 {
		t.Fatalf("maximum in-flight requests = %d, want concurrent requests", peak)
	}
}

func TestRunnerExecutesCallbacksInConfiguredOrderAfterURLDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "urls.txt"), []byte(server.URL+"/artifact.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	callback := filepath.Join(dir, "callback.sh")
	script := []byte("#!/bin/sh\nset -eu\ntest -f \"$1/artifact.bin\"\nprintf '%s\\n' \"$2\" >> \"$1/callback.txt\"\n")
	if err := os.WriteFile(callback, script, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "files", Type: config.JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 1, Timeout: config.Duration(time.Minute),
		Callback: config.CallbackCommands{
			{Executable: callback, Args: []string{"${ARTIFACT_OUTPUT}", "first"}},
			{Executable: callback, Args: []string{"${ARTIFACT_OUTPUT}", "second"}},
		},
	}}}

	results, err := (Runner{AllowCallback: true}).Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("unexpected results: %#v", results)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", "callback.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\n" {
		t.Fatalf("callback content = %q", data)
	}
}

func TestRunCallbacksPreservesOrder(t *testing.T) {
	var output bytes.Buffer
	job := config.Job{Callback: config.CallbackCommands{
		{Executable: "/bin/sh", Args: []string{"-c", "printf first"}},
		{Executable: "/bin/sh", Args: []string{"-c", "printf second"}},
	}}

	err := (Runner{Stdout: &output, Stderr: &output}).runCallbacks(
		context.Background(),
		config.Config{BaseDir: t.TempDir()},
		job,
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != "firstsecond" {
		t.Fatalf("callback output = %q", output.String())
	}
}

func TestRunCallbacksStopsAfterFailure(t *testing.T) {
	var output bytes.Buffer
	job := config.Job{Callback: config.CallbackCommands{
		{Executable: "/bin/sh", Args: []string{"-c", "printf failed; exit 7"}},
		{Executable: "/bin/sh", Args: []string{"-c", "printf should-not-run"}},
	}}

	err := (Runner{Stdout: &output, Stderr: &output}).runCallbacks(
		context.Background(),
		config.Config{BaseDir: t.TempDir()},
		job,
	)
	if err == nil {
		t.Fatal("runCallbacks() succeeded after a callback failure")
	}
	if output.String() != "failed" {
		t.Fatalf("callback output = %q", output.String())
	}
}

func TestSafeWorkingDirectoryRejectsEscape(t *testing.T) {
	if _, err := safeWorkingDirectory("/tmp/repo", "../../etc"); err == nil {
		t.Fatal("safeWorkingDirectory accepted escaping path")
	}
}

func TestRunnerPackageJob(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("POLICY_SOURCE_TEST", "from-policy")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gradle := []byte("#!/bin/sh\nset -eu\ntest \"$1\" = build\ntest \"$2\" = --no-daemon\ntest \"$POLICY_TARGET_TEST\" = from-policy\ntest \"$PACKAGE_CACHE_TEST\" = \"$ARTIFACT_CACHE\"\ntest \"$PACKAGE_OUTPUT_TEST\" = \"$ARTIFACT_OUTPUT\"\ntest \"$GRADLE_USER_HOME\" = \"$ARTIFACT_CACHE\"\ntest -f \"$PACKAGE_REPOSITORY_TEST/project/build.gradle\"\ntest -f \"$PACKAGE_WORKSPACE_TEST/repository/project/build.gradle\"\nprintf artifact > \"$ARTIFACT_OUTPUT/result.txt\"\nprintf cache > \"$ARTIFACT_CACHE/cache-used.txt\"\npackage_dir=\"$GRADLE_USER_HOME/caches/modules-2/files-2.1/com.example/demo/1.2.3/test-hash\"\nmkdir -p \"$package_dir\"\nprintf jar > \"$package_dir/demo-1.2.3.jar\"\nprintf pom > \"$package_dir/demo-1.2.3.pom\"\n")
	if err := os.WriteFile(filepath.Join(binDir, "gradle"), gradle, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	repositoryDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(repositoryDir, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repositoryDir, "project", "build.gradle"), []byte(""), 0o600); err != nil {
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
	git("add", "project/build.gradle")
	git("commit", "-q", "-m", "fixture")

	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "package", Type: config.JobTypePackage, Output: "artifacts", Cache: "cache", Workspace: "workspace",
		Repository: config.Repository{
			URL: repositoryDir, GitArgs: []string{"-c", "advice.detachedHead=false"},
			CloneArgs: []string{"--no-tags"},
		}, WorkingDirectory: "project",
		PackageManager: "gradle", Command: config.PackageCommand{Action: "build"},
		Environment: map[string]string{
			"PACKAGE_CACHE_TEST":      "${ARTIFACT_CACHE}",
			"PACKAGE_OUTPUT_TEST":     "${ARTIFACT_OUTPUT}",
			"PACKAGE_REPOSITORY_TEST": "${REPOSITORY_DIR}",
			"PACKAGE_WORKSPACE_TEST":  "${WORKSPACE}",
		},
		Timeout: config.Duration(time.Minute),
	}}}
	policy := environmentconfig.Config{
		Version: 1,
		Minimal: environmentconfig.Policy{Inherit: []string{"PATH"}},
		PackageManagers: map[string]environmentconfig.PackagePolicy{
			"gradle": {
				EnvironmentFrom: map[string]environmentconfig.EnvSource{
					"POLICY_TARGET_TEST": {Source: "POLICY_SOURCE_TEST", Required: true},
				},
			},
		},
	}
	var output bytes.Buffer
	results, err := (Runner{Stdout: &output, Stderr: &output, EnvironmentConfig: &policy}).Run(context.Background(), cfg, "")
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
	for filename, want := range map[string]string{
		"demo-1.2.3.jar": "jar",
		"demo-1.2.3.pom": "pom",
	} {
		data, err := os.ReadFile(filepath.Join(dir, "artifacts", "com", "example", "demo", "1.2.3", filename))
		if err != nil {
			t.Fatalf("read Maven repository artifact %s: %v", filename, err)
		}
		if string(data) != want {
			t.Fatalf("Maven repository artifact %s = %q", filename, data)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "workspace", "repository", "project", "build.gradle")); err != nil {
		t.Fatalf("configured workspace was not retained: %v", err)
	}
}

func TestExpandVariables(t *testing.T) {
	got := expandVariables("--dest=${ARTIFACT_CACHE}", map[string]string{"ARTIFACT_CACHE": "/cache"})
	if got != "--dest=/cache" {
		t.Fatalf("expandVariables() = %q", got)
	}
}

func TestRetainNPMInstall(t *testing.T) {
	dir := t.TempDir()
	workingDir := filepath.Join(dir, "repository")
	sourcePackage := filepath.Join(workingDir, "node_modules", "example-package")
	output := filepath.Join(dir, "output")
	if err := os.MkdirAll(sourcePackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(output, "node_modules", "stale-package"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourcePackage, "index.js"), []byte("module.exports = true"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := retainNPMInstall(workingDir, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "node_modules", "example-package", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "module.exports = true" {
		t.Fatalf("retained package content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(output, "node_modules", "stale-package")); !os.IsNotExist(err) {
		t.Fatalf("stale package was not removed: %v", err)
	}
}

func TestRunnerRejectsCallbackByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "urls.txt"), []byte(server.URL+"/artifact.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Version: 1, BaseDir: dir, Jobs: []config.Job{{
		Name: "files", Type: config.JobTypeURLs, Output: "out", URLList: "urls.txt",
		Concurrency: 1, Timeout: config.Duration(time.Minute),
		Callback: config.CallbackCommands{{Executable: "unused"}},
	}}}
	results, err := (Runner{}).Run(context.Background(), cfg, "")
	if err == nil {
		t.Fatalf("Runner.Run() accepted a callback by default: %#v", results)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out", "artifact.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("job ran before callback authorization failed: %v", statErr)
	}
}
