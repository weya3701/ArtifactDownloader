package executor

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestCommandOverridesInheritedEnvironment(t *testing.T) {
	const key = "ARTIFACT_DOWNLOADER_EXECUTOR_TEST"
	t.Setenv(key, "inherited")
	var stdout bytes.Buffer
	err := (Command{}).Run(context.Background(), "sh", []string{"-c", "printf %s \"$" + key + "\""}, Options{
		Environment: map[string]string{key: "overridden"}, Stdout: &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "overridden" {
		t.Fatalf("environment value = %q", stdout.String())
	}
	_ = os.Unsetenv(key)
}
