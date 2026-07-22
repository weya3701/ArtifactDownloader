package repository

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"

	"artifactdownloader/internal/config"
)

type Git struct {
	Stdout io.Writer
	Stderr io.Writer
}

var environmentReference = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

func (g Git) Clone(ctx context.Context, repository config.Repository, destination string) error {
	gitArgs, err := expandEnvironmentArgs(repository.GitArgs)
	if err != nil {
		return fmt.Errorf("expand repository.gitArgs: %w", err)
	}
	cloneArgs, err := expandEnvironmentArgs(repository.CloneArgs)
	if err != nil {
		return fmt.Errorf("expand repository.cloneArgs: %w", err)
	}

	args := append([]string{}, gitArgs...)
	args = append(args, "clone")
	args = append(args, cloneArgs...)
	if repository.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(repository.Depth))
	}
	args = append(args, "--", repository.URL, destination)
	if err := g.run(ctx, "", args...); err != nil {
		return fmt.Errorf("clone repository: %w", err)
	}
	if repository.Ref != "" {
		checkoutArgs := append([]string{}, gitArgs...)
		checkoutArgs = append(checkoutArgs, "checkout", "--detach", repository.Ref)
		if err := g.run(ctx, destination, checkoutArgs...); err != nil {
			return fmt.Errorf("checkout ref %q: %w", repository.Ref, err)
		}
	}
	return nil
}

func (g Git) run(ctx context.Context, directory string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = directory
	cmd.Stdout = g.Stdout
	cmd.Stderr = g.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git command failed: %w", err)
	}
	return nil
}

func expandEnvironmentArgs(args []string) ([]string, error) {
	expanded := make([]string, len(args))
	for i, arg := range args {
		var missing string
		expanded[i] = environmentReference.ReplaceAllStringFunc(arg, func(reference string) string {
			name := reference[2 : len(reference)-1]
			value, exists := os.LookupEnv(name)
			if !exists && missing == "" {
				missing = name
			}
			return value
		})
		if missing != "" {
			return nil, fmt.Errorf("environment variable %s is not set", missing)
		}
	}
	return expanded, nil
}
