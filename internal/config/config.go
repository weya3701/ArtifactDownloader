package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"artifactdownloader/internal/packagecommand"

	"gopkg.in/yaml.v3"
)

const (
	// JobTypeURLs 表示從文字清單並行下載 HTTP/HTTPS 檔案的 job 類型。
	JobTypeURLs = "urls"
	// JobTypePackage 表示 clone repository 後執行固定套件管理命令的 job 類型。
	JobTypePackage = "package"
)

// Duration 包裝 time.Duration，讓 YAML 可使用 10m、30s 等文字格式。
type Duration time.Duration

// UnmarshalYAML 將 YAML scalar 解析為正值檢查前的 Duration。
// 輸入為 YAML node；成功時寫入接收者並輸出 nil，格式無效時輸出解析錯誤。
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	value, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(value)
	return nil
}

// Value 將設定用 Duration 轉回標準 time.Duration。
// 輸入為接收者 d；輸出為等值的 time.Duration。
func (d Duration) Value() time.Duration { return time.Duration(d) }

// Config 表示完整任務設定及不寫回 YAML 的設定檔基準目錄。
type Config struct {
	Version int   `yaml:"version"`
	Jobs    []Job `yaml:"jobs"`

	BaseDir string `yaml:"-"`
}

// Job 描述一個 URLs 或 package 工作所需的共同與類型專用輸入。
type Job struct {
	Name             string          `yaml:"name"`
	Type             string          `yaml:"type"`
	Output           string          `yaml:"output"`
	Cache            string          `yaml:"cache"`
	URLList          string          `yaml:"urlList"`
	Concurrency      int             `yaml:"concurrency"`
	Timeout          Duration        `yaml:"timeout"`
	Overwrite        bool            `yaml:"overwrite"`
	Repository       Repository      `yaml:"repository"`
	WorkingDirectory string          `yaml:"workingDirectory"`
	PackageManager   string          `yaml:"packageManager"`
	Command          PackageCommand  `yaml:"command"`
	Callback         ExternalCommand `yaml:"callback"`
}

// Repository 描述 Git 來源、ref、clone 深度及受控的額外 Git 參數。
type Repository struct {
	URL       string   `yaml:"url"`
	Ref       string   `yaml:"ref"`
	Depth     int      `yaml:"depth"`
	GitArgs   []string `yaml:"gitArgs"`
	CloneArgs []string `yaml:"cloneArgs"`
}

// PackageCommand 只允許宣告 package action，不允許任意 executable 或 args。
type PackageCommand struct {
	Action string `yaml:"action"`
}

// ExternalCommand 描述需額外授權的 callback executable 與逐項參數。
type ExternalCommand struct {
	Executable string   `yaml:"executable"`
	Args       []string `yaml:"args"`
}

// Load 讀取、嚴格解析、套用預設值並驗證任務 YAML。
// 輸入為設定檔 path；輸出為含 BaseDir 的 Config，讀取、未知欄位或驗證失敗時輸出錯誤。
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.BaseDir = filepath.Dir(absPath)
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyDefaults 為未指定的 concurrency 與 timeout 寫入安全預設值。
// 輸入及輸出皆透過 Config 指標 c；本函式沒有回傳值。
func (c *Config) applyDefaults() {
	for i := range c.Jobs {
		if c.Jobs[i].Concurrency == 0 {
			c.Jobs[i].Concurrency = 4
		}
		if c.Jobs[i].Timeout == 0 {
			c.Jobs[i].Timeout = Duration(10 * time.Minute)
		}
	}
}

// Validate 檢查版本、job 唯一性、共同欄位與各 job 類型的必要條件。
// 輸入為 Config；合法時輸出 nil，第一個不合法欄位輸出帶 job 脈絡的錯誤。
func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d (expected 1)", c.Version)
	}
	if len(c.Jobs) == 0 {
		return errors.New("config must contain at least one job")
	}

	names := make(map[string]struct{}, len(c.Jobs))
	for i, job := range c.Jobs {
		prefix := fmt.Sprintf("jobs[%d]", i)
		if strings.TrimSpace(job.Name) == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if _, exists := names[job.Name]; exists {
			return fmt.Errorf("duplicate job name %q", job.Name)
		}
		names[job.Name] = struct{}{}
		if job.Timeout.Value() <= 0 {
			return fmt.Errorf("job %q: timeout must be positive", job.Name)
		}
		if len(job.Callback.Args) > 0 && strings.TrimSpace(job.Callback.Executable) == "" {
			return fmt.Errorf("job %q: callback.executable is required when callback.args is set", job.Name)
		}

		switch job.Type {
		case JobTypeURLs:
			if strings.TrimSpace(job.Output) == "" {
				return fmt.Errorf("job %q: output is required", job.Name)
			}
			if strings.TrimSpace(job.URLList) == "" {
				return fmt.Errorf("job %q: urlList is required", job.Name)
			}
			if job.Concurrency < 1 {
				return fmt.Errorf("job %q: concurrency must be positive", job.Name)
			}
		case JobTypePackage:
			if strings.TrimSpace(job.Cache) == "" {
				return fmt.Errorf("job %q: cache is required", job.Name)
			}
			if strings.TrimSpace(job.Repository.URL) == "" {
				return fmt.Errorf("job %q: repository.url is required", job.Name)
			}
			if job.Repository.Depth < 0 {
				return fmt.Errorf("job %q: repository.depth cannot be negative", job.Name)
			}
			if !supportedPackageManager(job.PackageManager) {
				return fmt.Errorf("job %q: unsupported packageManager %q", job.Name, job.PackageManager)
			}
			if err := packagecommand.Validate(job.PackageManager, job.Command.Action); err != nil {
				return fmt.Errorf("job %q: %w", job.Name, err)
			}
			if strings.EqualFold(job.PackageManager, "pip") && strings.EqualFold(job.Command.Action, "download") && strings.TrimSpace(job.Output) == "" {
				return fmt.Errorf("job %q: output is required for pip action %q", job.Name, job.Command.Action)
			}
		case "":
			return fmt.Errorf("job %q: type is required", job.Name)
		default:
			return fmt.Errorf("job %q: unsupported type %q", job.Name, job.Type)
		}
	}
	return nil
}

// supportedPackageManager 判斷名稱是否為內建支援的套件管理器。
// 輸入為可能含空白或大小寫差異的名稱；輸出為是否支援的布林值。
func supportedPackageManager(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "gradle", "mvn", "npm", "pip", "yarn":
		return true
	default:
		return false
	}
}

// Resolve 以 Config.BaseDir 為基準解析相對路徑，並清理絕對路徑。
// 輸入為相對或絕對 path；輸出為供檔案操作使用的標準化路徑。
func (c Config) Resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.BaseDir, path)
}
