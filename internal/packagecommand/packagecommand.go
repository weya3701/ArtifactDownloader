package packagecommand

import (
	"fmt"
	"strings"
)

// Variables 提供固定命令建構時使用的 cache、output 與隔離 home 路徑。
type Variables struct {
	Cache  string
	Output string
	Home   string
}

// Spec 是驗證後可交給 executor 的固定執行檔、參數與必要環境。
type Spec struct {
	Executable  string
	Args        []string
	Environment map[string]string
}

// Validate 檢查 manager/action 是否為內建 allowlist 的合法組合。
// 輸入為套件管理器與 action；合法時輸出 nil，空白或不允許的組合輸出錯誤。
func Validate(manager, action string) error {
	manager = strings.ToLower(strings.TrimSpace(manager))
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return fmt.Errorf("command.action is required")
	}
	switch manager + ":" + action {
	case "gradle:build", "mvn:build", "npm:install", "yarn:install", "pip:download":
		return nil
	default:
		return fmt.Errorf("action %q is not allowed for packageManager %q", action, manager)
	}
}

// Resolve 將合法 manager/action 與路徑變數解析為不可任意改寫的命令規格。
// 輸入為 manager、action 與 Variables；輸出為 Spec，組合不合法或缺少必要 output 時輸出錯誤。
func Resolve(manager, action string, variables Variables) (Spec, error) {
	manager = strings.ToLower(strings.TrimSpace(manager))
	action = strings.ToLower(strings.TrimSpace(action))
	if err := Validate(manager, action); err != nil {
		return Spec{}, err
	}

	spec := Spec{
		Environment: map[string]string{
			"ARTIFACT_CACHE":  variables.Cache,
			"ARTIFACT_OUTPUT": variables.Output,
			"HOME":            variables.Home,
		},
	}

	switch manager + ":" + action {
	case "gradle:build":
		spec.Executable = "gradle"
		spec.Args = []string{"build", "--no-daemon"}
		spec.Environment["GRADLE_USER_HOME"] = variables.Cache
	case "mvn:build":
		spec.Executable = "mvn"
		spec.Args = []string{"package", "--batch-mode", "-Dmaven.repo.local=" + variables.Cache}
	case "npm:install":
		spec.Executable = "npm"
		spec.Args = []string{"ci", "--ignore-scripts"}
		spec.Environment["npm_config_cache"] = variables.Cache
	case "yarn:install":
		spec.Executable = "yarn"
		spec.Args = []string{"install", "--immutable", "--ignore-scripts"}
		spec.Environment["YARN_CACHE_FOLDER"] = variables.Cache
	case "pip:download":
		if variables.Output == "" {
			return Spec{}, errorsForOutput(manager, action)
		}
		fmt.Println("variables output: ", variables.Output)
		spec.Executable = "python3"
		spec.Args = []string{"-m", "pip", "download", "-r", "requirements.txt", "--dest", variables.Output}
		spec.Environment["PIP_CACHE_DIR"] = variables.Cache
	default:
		return Spec{}, fmt.Errorf("package command %q has no execution specification", manager+":"+action)
	}
	return spec, nil
}

// errorsForOutput 建立缺少輸出目錄時的一致錯誤訊息。
// 輸入為 manager 與 action；輸出為描述 output 必填的 error。
func errorsForOutput(manager, action string) error {
	return fmt.Errorf("output is required for %s action %q", manager, action)
}
