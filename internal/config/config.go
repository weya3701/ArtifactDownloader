package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"artifactdownloader/internal/environmentconfig"
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
	Name             string            `yaml:"name"`
	Type             string            `yaml:"type"`
	Output           string            `yaml:"output"`
	Cache            string            `yaml:"cache"`
	URLList          string            `yaml:"urlList"`
	Concurrency      int               `yaml:"concurrency"`
	Timeout          Duration          `yaml:"timeout"`
	Overwrite        bool              `yaml:"overwrite"`
	Repository       Repository        `yaml:"repository"`
	Workspace        string            `yaml:"workspace"`
	WorkingDirectory string            `yaml:"workingDirectory"`
	PackageManager   string            `yaml:"packageManager"`
	Command          PackageCommand    `yaml:"command"`
	Environment      map[string]string `yaml:"environment"`
	Callback         CallbackCommands  `yaml:"callback"`
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

// CallbackCommands 描述依 YAML 設定順序執行的 callback 命令。
// YAML 可使用命令清單；為相容舊設定，也接受單一命令物件。
type CallbackCommands []ExternalCommand

var jobEnvironmentReference = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

var packageRuntimeReferences = map[string]struct{}{
	"ARTIFACT_CACHE":  {},
	"ARTIFACT_OUTPUT": {},
	"REPOSITORY_DIR":  {},
	"WORKSPACE":       {},
}

var callbackRuntimeReferences = map[string]struct{}{
	"ARTIFACT_CACHE":  {},
	"ARTIFACT_OUTPUT": {},
}

// UnmarshalYAML 將 callback 的單一物件或有序清單解析為統一的命令清單。
// 輸入為 callback YAML node；成功時寫入接收者，型別或欄位不合法時輸出解析錯誤。
func (c *CallbackCommands) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.MappingNode:
		command, err := decodeExternalCommand(node)
		if err != nil {
			return err
		}
		if strings.TrimSpace(command.Executable) == "" && len(command.Args) == 0 {
			*c = nil
			return nil
		}
		*c = CallbackCommands{command}
		return nil
	case yaml.SequenceNode:
		commands := make(CallbackCommands, len(node.Content))
		for i, item := range node.Content {
			command, err := decodeExternalCommand(item)
			if err != nil {
				return fmt.Errorf("callback[%d]: %w", i, err)
			}
			commands[i] = command
		}
		*c = commands
		return nil
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			*c = nil
			return nil
		}
	}
	return errors.New("callback must be an object or list")
}

// decodeExternalCommand 嚴格解析單一 callback 命令，避免自訂 YAML 解析略過未知欄位。
// 輸入為預期的 mapping node；輸出為命令設定，型別、欄位或值不合法時輸出錯誤。
func decodeExternalCommand(node *yaml.Node) (ExternalCommand, error) {
	if node.Kind != yaml.MappingNode {
		return ExternalCommand{}, errors.New("must be an object")
	}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i]
		switch field.Value {
		case "executable", "args":
		default:
			return ExternalCommand{}, fmt.Errorf("field %q is not supported", field.Value)
		}
	}

	var command ExternalCommand
	if err := node.Decode(&command); err != nil {
		return ExternalCommand{}, err
	}
	return command, nil
}

// ExpandJobEnvironment 將 job 路徑、repository 與命令設定中的主機環境參照展開。
// 輸入為已解析 job 與是否允許主機環境；輸出為不修改原 job 的展開副本，未授權或缺少變數時輸出定位錯誤。
func ExpandJobEnvironment(job Job, allowHostEnvironment bool) (Job, error) {
	fields := []struct {
		name  string
		value *string
	}{
		{"output", &job.Output},
		{"cache", &job.Cache},
		{"urlList", &job.URLList},
		{"repository.url", &job.Repository.URL},
		{"repository.ref", &job.Repository.Ref},
		{"workspace", &job.Workspace},
		{"workingDirectory", &job.WorkingDirectory},
		{"packageManager", &job.PackageManager},
		{"command.action", &job.Command.Action},
	}
	for _, field := range fields {
		expanded, err := expandJobValue(field.name, *field.value, allowHostEnvironment, nil)
		if err != nil {
			return Job{}, err
		}
		*field.value = expanded
	}

	if job.Environment != nil {
		environment := make(map[string]string, len(job.Environment))
		names := make([]string, 0, len(job.Environment))
		for name := range job.Environment {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			expanded, err := expandJobValue("environment."+name, job.Environment[name], allowHostEnvironment, packageRuntimeReferences)
			if err != nil {
				return Job{}, err
			}
			environment[name] = expanded
		}
		job.Environment = environment
	}

	callbacks := make(CallbackCommands, len(job.Callback))
	for i, callback := range job.Callback {
		executable, err := expandJobValue(fmt.Sprintf("callback[%d].executable", i), callback.Executable, allowHostEnvironment, callbackRuntimeReferences)
		if err != nil {
			return Job{}, err
		}
		args := make([]string, len(callback.Args))
		for j, arg := range callback.Args {
			args[j], err = expandJobValue(fmt.Sprintf("callback[%d].args[%d]", i, j), arg, allowHostEnvironment, callbackRuntimeReferences)
			if err != nil {
				return Job{}, err
			}
		}
		callbacks[i] = ExternalCommand{Executable: executable, Args: args}
	}
	job.Callback = callbacks
	return job, nil
}

// expandJobValue 展開單一設定值中的主機環境參照，並保留呼叫端指定的執行期變數。
// 輸入為欄位路徑、原值、主機環境授權及保留名稱；輸出為展開字串，未授權或來源不存在時輸出錯誤。
func expandJobValue(field, value string, allowHostEnvironment bool, preserved map[string]struct{}) (string, error) {
	var unavailable string
	var missing string
	expanded := jobEnvironmentReference.ReplaceAllStringFunc(value, func(reference string) string {
		name := reference[2 : len(reference)-1]
		if _, keep := preserved[name]; keep {
			return reference
		}
		if !allowHostEnvironment {
			if unavailable == "" {
				unavailable = name
			}
			return reference
		}
		replacement, exists := os.LookupEnv(name)
		if !exists {
			if missing == "" {
				missing = name
			}
			return reference
		}
		return replacement
	})
	if unavailable != "" {
		return "", fmt.Errorf("%s references environment variable %s; use --inherit-environment", field, unavailable)
	}
	if missing != "" {
		return "", fmt.Errorf("%s references environment variable %s, but it is not set", field, missing)
	}
	return expanded, nil
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
		for callbackIndex, callback := range job.Callback {
			if strings.TrimSpace(callback.Executable) == "" {
				return fmt.Errorf("job %q: callback[%d].executable is required", job.Name, callbackIndex)
			}
		}

		switch job.Type {
		case JobTypeURLs:
			if len(job.Environment) > 0 {
				return fmt.Errorf("job %q: environment is only supported for package jobs", job.Name)
			}
			if strings.TrimSpace(job.Workspace) != "" {
				return fmt.Errorf("job %q: workspace is only supported for package jobs", job.Name)
			}
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
			if err := environmentconfig.ValidateJobEnvironment(job.Environment); err != nil {
				return fmt.Errorf("job %q: %w", job.Name, err)
			}
			if strings.TrimSpace(job.Cache) == "" {
				return fmt.Errorf("job %q: cache is required", job.Name)
			}
			if strings.TrimSpace(job.Repository.URL) == "" {
				return fmt.Errorf("job %q: repository.url is required", job.Name)
			}
			if job.Repository.Depth < 0 {
				return fmt.Errorf("job %q: repository.depth cannot be negative", job.Name)
			}
			managerReference := jobEnvironmentReference.MatchString(job.PackageManager)
			actionReference := jobEnvironmentReference.MatchString(job.Command.Action)
			if !managerReference && !supportedPackageManager(job.PackageManager) {
				return fmt.Errorf("job %q: unsupported packageManager %q", job.Name, job.PackageManager)
			}
			if !managerReference && !actionReference {
				if err := packagecommand.Validate(job.PackageManager, job.Command.Action); err != nil {
					return fmt.Errorf("job %q: %w", job.Name, err)
				}
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
