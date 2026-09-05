package gradlecache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportMavenRepository(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "output")
	cacheRoot := filepath.Join(cache, filepath.FromSlash(cachedFilesPath))

	writeCachedFile(t, filepath.Join(cacheRoot, "com.example", "demo", "1.2.3", "jar-hash", "demo-1.2.3.jar"), "jar")
	writeCachedFile(t, filepath.Join(cacheRoot, "com.example", "demo", "1.2.3", "pom-hash", "demo-1.2.3.pom"), "pom")
	writeCachedFile(t, filepath.Join(cacheRoot, "org.sample", "library", "2.0", "module-hash", "library-2.0.module"), "module")
	writeCachedFile(t, filepath.Join(output, "keep.txt"), "keep")

	count, err := ExportMavenRepository(cache, output)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("ExportMavenRepository() count = %d, want 3", count)
	}

	assertFileContent(t, filepath.Join(output, "com", "example", "demo", "1.2.3", "demo-1.2.3.jar"), "jar")
	assertFileContent(t, filepath.Join(output, "com", "example", "demo", "1.2.3", "demo-1.2.3.pom"), "pom")
	assertFileContent(t, filepath.Join(output, "org", "sample", "library", "2.0", "library-2.0.module"), "module")
	assertFileContent(t, filepath.Join(output, "keep.txt"), "keep")
}

func TestExportMavenRepositoryUsesNewestDuplicate(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	cacheRoot := filepath.Join(cache, filepath.FromSlash(cachedFilesPath), "com.example", "demo", "1.0")
	oldPath := filepath.Join(cacheRoot, "old-hash", "demo-1.0.jar")
	newPath := filepath.Join(cacheRoot, "new-hash", "demo-1.0.jar")
	writeCachedFile(t, oldPath, "old")
	writeCachedFile(t, newPath, "new")
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	count, err := ExportMavenRepository(cache, filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ExportMavenRepository() count = %d, want 1", count)
	}
	assertFileContent(t, filepath.Join(dir, "output", "com", "example", "demo", "1.0", "demo-1.0.jar"), "new")
}

func TestExportMavenRepositoryAllowsEmptyCache(t *testing.T) {
	count, err := ExportMavenRepository(filepath.Join(t.TempDir(), "cache"), filepath.Join(t.TempDir(), "output"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ExportMavenRepository() count = %d, want 0", count)
	}
}

func writeCachedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", path, content, want)
	}
}
