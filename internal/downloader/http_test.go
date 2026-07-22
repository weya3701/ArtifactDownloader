package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("artifact"))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "nested", "file.txt")
	if err := (HTTP{}).Download(context.Background(), server.URL, destination, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "artifact" {
		t.Fatalf("content = %q", data)
	}
}

func TestFilename(t *testing.T) {
	name, err := Filename("https://example.com/releases/app%20one.tar.gz?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if name != "app one.tar.gz" {
		t.Fatalf("Filename() = %q", name)
	}
}
