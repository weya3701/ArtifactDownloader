package repository

import (
	"reflect"
	"testing"
)

func TestExpandEnvironmentArgs(t *testing.T) {
	t.Setenv("ADO_AUTH_HEADER", "encoded-secret")
	got, err := expandEnvironmentArgs([]string{
		"-c",
		"http.extraHeader=AUTHORIZATION: basic ${ADO_AUTH_HEADER}",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-c", "http.extraHeader=AUTHORIZATION: basic encoded-secret"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expandEnvironmentArgs() = %#v, want %#v", got, want)
	}
}

func TestExpandEnvironmentArgsRejectsMissingVariable(t *testing.T) {
	t.Setenv("MISSING_GIT_VALUE", "temporary")
	t.Setenv("MISSING_GIT_VALUE", "")
	// An explicitly empty variable is valid.
	if _, err := expandEnvironmentArgs([]string{"${MISSING_GIT_VALUE}"}); err != nil {
		t.Fatal(err)
	}

	if _, err := expandEnvironmentArgs([]string{"${ARTIFACT_DOWNLOADER_UNDEFINED_TEST}"}); err == nil {
		t.Fatal("expandEnvironmentArgs() accepted an undefined variable")
	}
}
