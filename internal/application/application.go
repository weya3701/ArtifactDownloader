package application

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"artifactdownloader/internal/config"
	"artifactdownloader/internal/downloader"
	"artifactdownloader/internal/executor"
	"artifactdownloader/internal/report"
	"artifactdownloader/internal/repository"
)

type Runner struct {
	Stdout        io.Writer
	Stderr        io.Writer
	KeepWorkspace bool
}

func (r Runner) Run(ctx context.Context, cfg config.Config, selectedJob string) ([]report.Result, error) {
	jobs := cfg.Jobs
	if selectedJob != "" {
		jobs = nil
		for _, job := range cfg.Jobs {
			if job.Name == selectedJob {
				jobs = append(jobs, job)
				break
			}
		}
		if len(jobs) == 0 {
			return nil, fmt.Errorf("job %q not found", selectedJob)
		}
	}

	results := make([]report.Result, 0, len(jobs))
	for _, job := range jobs {
		result := r.runJob(ctx, cfg, job)
		results = append(results, result)
		if ctx.Err() != nil {
			break
		}
	}
	return results, nil
}

func (r Runner) runJob(parent context.Context, cfg config.Config, job config.Job) report.Result {
	result := report.Result{Name: job.Name, Type: job.Type, StartedAt: time.Now()}
	ctx, cancel := context.WithTimeout(parent, job.Timeout.Value())
	defer cancel()

	switch job.Type {
	case config.JobTypeURLs:
		result.Files, result.Err = r.runURLs(ctx, cfg, job)
	case config.JobTypePackage:
		result.Err = r.runPackage(ctx, cfg, job)
	default:
		result.Err = fmt.Errorf("unsupported job type %q", job.Type)
	}
	if result.Err == nil && job.Callback.Executable != "" {
		result.Err = r.runCallback(ctx, cfg, job)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Err = fmt.Errorf("job timed out after %s: %w", job.Timeout.Value(), result.Err)
	}
	result.Duration = time.Since(result.StartedAt)
	return result
}

func (r Runner) runCallback(ctx context.Context, cfg config.Config, job config.Job) error {
	variables := map[string]string{
		"ARTIFACT_CACHE":  resolveOptional(cfg, job.Cache),
		"ARTIFACT_OUTPUT": resolveOptional(cfg, job.Output),
	}
	executable := expandVariables(job.Callback.Executable, variables)
	args := make([]string, len(job.Callback.Args))
	for i, arg := range job.Callback.Args {
		args[i] = expandVariables(arg, variables)
	}

	runner := executor.Command{}
	if err := runner.Run(ctx, executable, args, executor.Options{
		Directory: cfg.BaseDir,
		Stdout:    r.output(),
		Stderr:    r.errorOutput(),
	}); err != nil {
		return fmt.Errorf("callback: %w", err)
	}
	return nil
}

func resolveOptional(cfg config.Config, path string) string {
	if path == "" {
		return ""
	}
	return cfg.Resolve(path)
}

func (r Runner) runURLs(ctx context.Context, cfg config.Config, job config.Job) (int, error) {
	urls, err := readURLList(cfg.Resolve(job.URLList))
	if err != nil {
		return 0, err
	}
	if len(urls) == 0 {
		return 0, errors.New("URL list contains no downloads")
	}

	type item struct {
		url  string
		name string
	}
	items := make([]item, 0, len(urls))
	names := make(map[string]string, len(urls))
	for _, rawURL := range urls {
		name, err := downloader.Filename(rawURL)
		if err != nil {
			return 0, fmt.Errorf("invalid URL %q: %w", rawURL, err)
		}
		if previous, exists := names[name]; exists {
			return 0, fmt.Errorf("URLs %q and %q produce the same filename %q", previous, rawURL, name)
		}
		names[name] = rawURL
		items = append(items, item{url: rawURL, name: name})
	}

	output := cfg.Resolve(job.Output)
	if err := os.MkdirAll(output, 0o755); err != nil {
		return 0, fmt.Errorf("create output directory: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan item)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	completed := 0
	download := downloader.HTTP{}

	worker := func() {
		defer wg.Done()
		for current := range work {
			if ctx.Err() != nil {
				continue
			}
			destination := filepath.Join(output, current.name)
			if err := download.Download(ctx, current.url, destination, job.Overwrite); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("download %q: %w", current.url, err)
					cancel()
				}
				mu.Unlock()
				continue
			}
			mu.Lock()
			completed++
			mu.Unlock()
		}
	}

	workers := min(job.Concurrency, len(items))
	for range workers {
		wg.Add(1)
		go worker()
	}
	for _, current := range items {
		if ctx.Err() != nil {
			break
		}
		work <- current
	}
	close(work)
	wg.Wait()
	if firstErr != nil {
		return completed, firstErr
	}
	if err := ctx.Err(); err != nil {
		return completed, err
	}
	return completed, nil
}

func (r Runner) runPackage(ctx context.Context, cfg config.Config, job config.Job) error {
	workspace, err := os.MkdirTemp("", "artifact-downloader-*")
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if r.KeepWorkspace {
		fmt.Fprintf(r.output(), "workspace for %s: %s\n", job.Name, workspace)
	} else {
		defer os.RemoveAll(workspace)
	}

	repositoryDir := filepath.Join(workspace, "repository")
	git := repository.Git{Stdout: r.output(), Stderr: r.errorOutput()}
	if err := git.Clone(ctx, job.Repository, repositoryDir); err != nil {
		return err
	}

	workingDir, err := safeWorkingDirectory(repositoryDir, job.WorkingDirectory)
	if err != nil {
		return err
	}
	if stat, err := os.Stat(workingDir); err != nil {
		return fmt.Errorf("inspect working directory: %w", err)
	} else if !stat.IsDir() {
		return fmt.Errorf("working directory is not a directory: %s", workingDir)
	}
	realRepositoryDir, err := filepath.EvalSymlinks(repositoryDir)
	if err != nil {
		return fmt.Errorf("resolve repository directory: %w", err)
	}
	realWorkingDir, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	if _, err := safeWorkingDirectory(realRepositoryDir, realWorkingDir); err != nil {
		return errors.New("workingDirectory resolves outside the cloned repository")
	}

	cache := cfg.Resolve(job.Cache)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	output := ""
	if job.Output != "" {
		output = cfg.Resolve(job.Output)
		if err := os.MkdirAll(output, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	variables := map[string]string{
		"ARTIFACT_CACHE":  cache,
		"ARTIFACT_OUTPUT": output,
		"WORKSPACE":       workspace,
		"REPOSITORY_DIR":  repositoryDir,
	}
	environment := make(map[string]string, len(job.Environment)+6)
	for key, value := range job.Environment {
		environment[key] = expandVariables(value, variables)
	}
	environment["ARTIFACT_CACHE"] = cache
	environment["ARTIFACT_OUTPUT"] = output
	applyPackageCache(job.PackageManager, cache, environment)

	executable := expandVariables(job.Command.Executable, variables)
	args := make([]string, len(job.Command.Args))
	for i, arg := range job.Command.Args {
		args[i] = expandVariables(arg, variables)
	}
	if strings.EqualFold(job.PackageManager, "mvn") {
		args = append(args, "-Dmaven.repo.local="+cache)
	}

	runner := executor.Command{}
	if err := runner.Run(ctx, executable, args, executor.Options{
		Directory: workingDir, Environment: environment,
		Stdout: r.output(), Stderr: r.errorOutput(),
	}); err != nil {
		return err
	}
	return nil
}

func applyPackageCache(manager, cache string, environment map[string]string) {
	switch strings.ToLower(manager) {
	case "gradle":
		environment["GRADLE_USER_HOME"] = cache
	case "npm":
		environment["npm_config_cache"] = cache
	case "yarn":
		environment["YARN_CACHE_FOLDER"] = cache
	case "pip":
		environment["PIP_CACHE_DIR"] = cache
	}
}

func expandVariables(value string, variables map[string]string) string {
	replacements := make([]string, 0, len(variables)*2)
	for name, replacement := range variables {
		replacements = append(replacements, "${"+name+"}", replacement)
	}
	return strings.NewReplacer(replacements...).Replace(value)
}

func readURLList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open URL list: %w", err)
	}
	defer file.Close()

	seen := make(map[string]struct{})
	var urls []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, exists := seen[line]; exists {
			continue
		}
		seen[line] = struct{}{}
		urls = append(urls, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read URL list: %w", err)
	}
	return urls, nil
}

func safeWorkingDirectory(repositoryDir, requested string) (string, error) {
	workingDir := requested
	if !filepath.IsAbs(workingDir) {
		workingDir = filepath.Join(repositoryDir, filepath.Clean(requested))
	}
	relative, err := filepath.Rel(repositoryDir, workingDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("workingDirectory must stay inside the cloned repository")
	}
	return workingDir, nil
}

func (r Runner) output() io.Writer {
	if r.Stdout == nil {
		return io.Discard
	}
	return r.Stdout
}

func (r Runner) errorOutput() io.Writer {
	if r.Stderr == nil {
		return io.Discard
	}
	return r.Stderr
}
