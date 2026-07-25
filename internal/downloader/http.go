package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// HTTP 是可注入 http.Client 的下載器；Client 為 nil 時使用 http.DefaultClient。
type HTTP struct {
	Client *http.Client
}

// Download 以 GET 下載 HTTP/HTTPS 內容，先同步寫入暫存檔，再原子發布到目的路徑。
// 輸入為 context、來源 URL、目的檔及覆寫旗標；成功輸出 nil，HTTP、檔案或取消問題輸出錯誤。
func (d HTTP) Download(ctx context.Context, sourceURL, destination string, overwrite bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		return fmt.Errorf("unexpected HTTP status %s", response.Status)
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if !overwrite {
		if _, err := os.Stat(destination); err == nil {
			return fmt.Errorf("destination already exists: %s", destination)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect destination: %w", err)
		}
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".artifact-download-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if _, err := io.Copy(temporary, response.Body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write response: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	if overwrite {
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("replace destination: %w", err)
		}
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("publish download: %w", err)
	}
	return nil
}

// Filename 從 HTTP/HTTPS URL path 的最後一段解析安全目的檔名。
// 輸入為原始 URL；輸出為解碼後且不含路徑分隔符的檔名，無效 scheme/host/path 時輸出錯誤。
func Filename(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("URL host is required")
	}
	name := path.Base(parsed.EscapedPath())
	name, err = url.PathUnescape(name)
	if err != nil {
		return "", fmt.Errorf("decode filename: %w", err)
	}
	if name == "" || name == "." || name == "/" || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("URL does not contain a safe filename")
	}
	return name, nil
}
