package environmentconfig

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 表示獨立於任務 YAML 的完整受信任環境政策。
type Config struct {
	Version         int                      `yaml:"version"`
	Minimal         Policy                   `yaml:"minimal"`
	PackageManagers map[string]PackagePolicy `yaml:"packageManagers"`
}

// Policy 描述所有 package job 共用的繼承名稱與固定非敏感值。
type Policy struct {
	Inherit []string          `yaml:"inherit"`
	Values  map[string]string `yaml:"values"`
}

// PackagePolicy 描述單一 package manager 額外允許的繼承、固定值與來源映射。
type PackagePolicy struct {
	Inherit         []string             `yaml:"inherit"`
	Values          map[string]string    `yaml:"values"`
	EnvironmentFrom map[string]EnvSource `yaml:"environmentFrom"`
}

// EnvSource 指定目標環境變數應從啟動程序的哪個名稱取得，以及是否必須存在。
type EnvSource struct {
	Source   string `yaml:"source"`
	Required bool   `yaml:"required"`
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var reserved = map[string]struct{}{
	"ARTIFACT_CACHE":    {},
	"ARTIFACT_OUTPUT":   {},
	"GRADLE_USER_HOME":  {},
	"PIP_CACHE_DIR":     {},
	"npm_config_cache":  {},
	"YARN_CACHE_FOLDER": {},
	"HOME":              {},
}

// Default 建立未提供政策檔時使用的內建最小環境政策。
// 函式沒有輸入；輸出為只繼承 PATH、locale、暫存目錄與 proxy 的 Config。
func Default() Config {
	return Config{
		Version: 1,
		Minimal: Policy{Inherit: []string{
			"PATH", "LANG", "LC_ALL", "TMPDIR",
			"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
			"http_proxy", "https_proxy", "no_proxy",
		}},
	}
}

// Load 從檔案嚴格解析並驗證受信任環境政策。
// 輸入為政策檔 path；輸出為 Config，讀取、未知欄位或安全規則失敗時輸出錯誤。
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("read environment config: %w", err)
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 檢查版本、manager 名稱、環境變數格式及保留變數衝突。
// 輸入為環境 Config；合法時輸出 nil，第一個政策問題輸出錯誤。
func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported environment config version %d (expected 1)", c.Version)
	}
	if err := validatePolicy("minimal", c.Minimal.Inherit, c.Minimal.Values); err != nil {
		return err
	}
	for manager, policy := range c.PackageManagers {
		normalized := strings.ToLower(strings.TrimSpace(manager))
		if manager != normalized {
			return fmt.Errorf("package manager key %q must use its canonical lowercase name", manager)
		}
		manager = normalized
		switch manager {
		case "gradle", "mvn", "npm", "yarn", "pip":
		default:
			return fmt.Errorf("packageManagers contains unsupported package manager %q", manager)
		}
		prefix := "packageManagers." + manager
		if err := validatePolicy(prefix, policy.Inherit, policy.Values); err != nil {
			return err
		}
		for target, source := range policy.EnvironmentFrom {
			if err := validateWritableName(prefix+".environmentFrom", target); err != nil {
				return err
			}
			if !environmentName.MatchString(source.Source) {
				return fmt.Errorf("%s.environmentFrom.%s.source is not a valid environment variable name", prefix, target)
			}
		}
	}
	return nil
}

// validatePolicy 驗證一段政策中的繼承名稱與固定值名稱。
// 輸入為錯誤路徑 prefix、inherit 清單及 values；合法時輸出 nil，否則輸出定位錯誤。
func validatePolicy(prefix string, inherit []string, values map[string]string) error {
	for _, name := range inherit {
		if !environmentName.MatchString(name) {
			return fmt.Errorf("%s.inherit contains invalid environment variable name %q", prefix, name)
		}
		if _, blocked := reserved[name]; blocked {
			return fmt.Errorf("%s cannot inherit reserved variable %s", prefix, name)
		}
	}
	for name := range values {
		if err := validateWritableName(prefix+".values", name); err != nil {
			return err
		}
	}
	return nil
}

// validateWritableName 確認政策目標名稱格式合法且不是工具保留變數。
// 輸入為錯誤路徑 prefix 與變數 name；可寫入時輸出 nil，否則輸出錯誤。
func validateWritableName(prefix, name string) error {
	if !environmentName.MatchString(name) {
		return fmt.Errorf("%s contains invalid environment variable name %q", prefix, name)
	}
	if _, blocked := reserved[name]; blocked {
		return fmt.Errorf("%s cannot set reserved variable %s", prefix, name)
	}
	return nil
}

// ValidateJobEnvironment 檢查 package job 自訂環境變數的名稱與保留變數衝突。
// 輸入為 job environment 名稱到固定值的映射；合法時輸出 nil，名稱無效或嘗試覆寫工具變數時輸出錯誤。
func ValidateJobEnvironment(environment map[string]string) error {
	for name := range environment {
		if err := validateWritableName("environment", name); err != nil {
			return err
		}
	}
	return nil
}

// Build 依共同政策及指定 manager 政策建立乾淨的子程序環境。
// 輸入為 package manager 名稱；輸出為環境名稱到值的映射，必要來源缺少時輸出錯誤。
func (c Config) Build(manager string) (map[string]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	result := make(map[string]string)
	inherit(result, c.Minimal.Inherit)
	for name, value := range c.Minimal.Values {
		result[name] = value
	}

	policy := c.PackageManagers[strings.ToLower(strings.TrimSpace(manager))]
	inherit(result, policy.Inherit)
	for name, value := range policy.Values {
		result[name] = value
	}
	for target, source := range policy.EnvironmentFrom {
		value, exists := os.LookupEnv(source.Source)
		if !exists {
			if source.Required {
				return nil, fmt.Errorf("required environment source %s is not set", source.Source)
			}
			continue
		}
		result[target] = value
	}
	return result, nil
}

// inherit 將啟動程序中存在且被明確列出的變數複製到目的映射。
// 輸入為可修改 destination 與名稱清單；輸出直接寫入 destination，沒有回傳值。
func inherit(destination map[string]string, names []string) {
	for _, name := range names {
		if value, exists := os.LookupEnv(name); exists {
			destination[name] = value
		}
	}
}
