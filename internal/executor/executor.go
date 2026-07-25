package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Options 描述外部程式的工作目錄、環境繼承策略及標準輸出目的地。
// 輸入由 Command.Run 使用；此型別本身不產生輸出。
type Options struct {
	Directory          string
	Environment        map[string]string
	InheritEnvironment bool
	Stdout             io.Writer
	Stderr             io.Writer
}

// Command 是不透過 shell 執行單一外部程式的無狀態執行器。
type Command struct{}

// Run 使用 context 啟動指定 executable，並逐項傳入 args 與 options。
// 輸入為 context、執行檔、參數及執行選項；成功輸出 nil，啟動或非零結束時輸出包裝錯誤。
func (Command) Run(ctx context.Context, executable string, args []string, options Options) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = options.Directory
	cmd.Env = make([]string, 0, len(os.Environ())+len(options.Environment))
	if options.InheritEnvironment {
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if _, overridden := options.Environment[key]; !overridden {
				cmd.Env = append(cmd.Env, entry)
			}
		}
	}
	for key, value := range options.Environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdout = options.Stdout
	cmd.Stderr = options.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execute %q: %w", executable, err)
	}
	return nil
}
