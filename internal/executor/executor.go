package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Options struct {
	Directory          string
	Environment        map[string]string
	InheritEnvironment bool
	Stdout             io.Writer
	Stderr             io.Writer
}

type Command struct{}

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
