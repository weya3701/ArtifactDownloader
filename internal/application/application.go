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
	"artifactdownloader/internal/environmentconfig"
	"artifactdownloader/internal/executor"
	"artifactdownloader/internal/packagecommand"
	"artifactdownloader/internal/report"
	"artifactdownloader/internal/repository"
)

// Runner 保存 job 編排時使用的輸出、workspace、callback 與環境安全策略。
// 輸入由呼叫端設定各欄位；輸出透過 Run 回傳 Result，不直接保存執行結果。
type Runner struct {
	Stdout             io.Writer
	Stderr             io.Writer
	KeepWorkspace      bool
	AllowCallback      bool
	EnvironmentConfig  *environmentconfig.Config
	InheritEnvironment bool
}

// Run 依設定順序執行全部或指定名稱的 job。
// 輸入為取消/逾時用 ctx、已驗證 cfg 與可留空的 selectedJob；輸出為已執行 job 的結果清單，
// 若工作不存在或 callback 未授權等原因使流程無法開始，則另回傳流程錯誤。
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
	if !r.AllowCallback {
		for _, job := range jobs {
			if job.Callback.Executable != "" {
				return nil, fmt.Errorf("job %q: callback is disabled; use --allow-callback only for trusted configuration", job.Name)
			}
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

// runJob 在 job 自身 timeout 內分派 URLs 或 package 流程，並於成功後執行 callback。
// 輸入為父 context、全域設定與單一 job；輸出為包含耗時、檔案數及錯誤的 Result。
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

// runCallback 在設定檔目錄執行已獲 CLI 授權的外部 callback。
// 輸入為 job context、路徑基準 cfg 與 callback 設定；成功輸出 nil，失敗輸出 callback 錯誤。
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
		Directory:          cfg.BaseDir,
		InheritEnvironment: true,
		Stdout:             r.output(),
		Stderr:             r.errorOutput(),
	}); err != nil {
		return fmt.Errorf("callback: %w", err)
	}
	return nil
}

// resolveOptional 將非空路徑依設定檔目錄解析為絕對/清理後路徑。
// 輸入為設定與可能為空的路徑；輸出為解析結果，空輸入仍輸出空字串。
func resolveOptional(cfg config.Config, path string) string {
	if path == "" {
		return ""
	}
	return cfg.Resolve(path)
}

// runURLs 讀取 URL 清單並以 job.Concurrency 大小的 worker pool 並行下載。
// 輸入為 job context、路徑基準 cfg 與 URLs job；輸出為完成檔案數及第一個下載/取消錯誤。
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

// runPackage 建立暫存 workspace、clone repository，解析固定 package 命令後在受控環境執行。
// 輸入為 job context、路徑基準 cfg 與 package job；成功輸出 nil，任一準備或執行階段失敗則輸出錯誤。
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

	spec, err := packagecommand.Resolve(job.PackageManager, job.Command.Action, packagecommand.Variables{
		Cache: cache, Output: output, Home: workspace,
	})
	if err != nil {
		return fmt.Errorf("resolve package command: %w", err)
	}

	environment := spec.Environment
	if r.InheritEnvironment {
		delete(environment, "HOME")
	} else {
		policy := environmentconfig.Default()
		if r.EnvironmentConfig != nil {
			policy = *r.EnvironmentConfig
		}
		environment, err = policy.Build(job.PackageManager)
		if err != nil {
			return fmt.Errorf("build package environment: %w", err)
		}
		for name, value := range spec.Environment {
			environment[name] = value
		}
	}

	runner := executor.Command{}
	if err := runner.Run(ctx, spec.Executable, spec.Args, executor.Options{
		Directory: workingDir, Environment: environment, InheritEnvironment: r.InheritEnvironment,
		Stdout: r.output(), Stderr: r.errorOutput(),
	}); err != nil {
		return err
	}
	return nil
}

// expandVariables 只替換呼叫端明確提供的 ${NAME} 變數，不進行 shell 展開。
// 輸入為原始字串與名稱到值的映射；輸出為完成受控替換的字串。
func expandVariables(value string, variables map[string]string) string {
	replacements := make([]string, 0, len(variables)*2)
	for name, replacement := range variables {
		replacements = append(replacements, "${"+name+"}", replacement)
	}
	return strings.NewReplacer(replacements...).Replace(value)
}

// readURLList 讀取文字 URL 清單，忽略空行、註解及完全重複的 URL。
// 輸入為清單檔案路徑；輸出為保持原順序的唯一 URL 切片或檔案讀取錯誤。
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

// safeWorkingDirectory 確認要求的工作目錄在 clone repository 範圍內。
// 輸入為 repository 根目錄與相對/絕對 requested 路徑；輸出為可用路徑，越界時輸出錯誤。
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

// output 取得一般外部命令輸出的 writer。
// 輸入來自 Runner.Stdout；輸出為該 writer，未設定時輸出 io.Discard。
func (r Runner) output() io.Writer {
	if r.Stdout == nil {
		return io.Discard
	}
	return r.Stdout
}

// errorOutput 取得外部命令錯誤輸出的 writer。
// 輸入來自 Runner.Stderr；輸出為該 writer，未設定時輸出 io.Discard。
func (r Runner) errorOutput() io.Writer {
	if r.Stderr == nil {
		return io.Discard
	}
	return r.Stderr
}
