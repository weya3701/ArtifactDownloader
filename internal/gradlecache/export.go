package gradlecache

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const cachedFilesPath = "caches/modules-2/files-2.1"

type artifact struct {
	source  string
	modTime time.Time
}

// ExportMavenRepository 將 Gradle files-2.1 cache 內的實體檔複製成 Maven repository layout。
// 輸入為 GRADLE_USER_HOME 與 output 目錄；輸出為寫入的唯一套件檔數量。
func ExportMavenRepository(cacheDir, outputDir string) (int, error) {
	sourceRoot := filepath.Join(cacheDir, filepath.FromSlash(cachedFilesPath))
	info, err := os.Lstat(sourceRoot)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect Gradle artifact cache: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("Gradle artifact cache is not a physical directory: %s", sourceRoot)
	}
	if err := ensureWithinCache(cacheDir, sourceRoot); err != nil {
		return 0, err
	}

	artifacts := make(map[string]artifact)
	err = filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRoot || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			return nil
		}

		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(relative, string(filepath.Separator))
		if len(parts) != 5 {
			return nil
		}
		group, module, version, filename := parts[0], parts[1], parts[2], parts[4]
		destination, err := artifactDestination(group, module, version, filename)
		if err != nil {
			return fmt.Errorf("map Gradle cache entry %q: %w", relative, err)
		}

		current, exists := artifacts[destination]
		if !exists || entryInfo.ModTime().After(current.modTime) ||
			(entryInfo.ModTime().Equal(current.modTime) && path > current.source) {
			artifacts[destination] = artifact{source: path, modTime: entryInfo.ModTime()}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("scan Gradle artifact cache: %w", err)
	}

	destinations := make([]string, 0, len(artifacts))
	for destination := range artifacts {
		destinations = append(destinations, destination)
	}
	sort.Strings(destinations)
	for _, relative := range destinations {
		destination := filepath.Join(outputDir, relative)
		if err := copyFile(artifacts[relative].source, destination); err != nil {
			return 0, fmt.Errorf("publish %q: %w", relative, err)
		}
	}
	return len(destinations), nil
}

func artifactDestination(group, module, version, filename string) (string, error) {
	groupParts := strings.Split(group, ".")
	parts := make([]string, 0, len(groupParts)+3)
	parts = append(parts, groupParts...)
	parts = append(parts, module, version, filename)
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || filepath.Base(part) != part {
			return "", fmt.Errorf("unsafe Maven path component %q", part)
		}
	}
	return filepath.Join(parts...), nil
}

func ensureWithinCache(cacheDir, sourceRoot string) error {
	realCache, err := filepath.EvalSymlinks(cacheDir)
	if err != nil {
		return fmt.Errorf("resolve Gradle cache directory: %w", err)
	}
	realSource, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return fmt.Errorf("resolve Gradle artifact cache: %w", err)
	}
	relative, err := filepath.Rel(realCache, realSource)
	if err != nil {
		return fmt.Errorf("compare Gradle cache paths: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Gradle artifact cache resolves outside cache directory: %s", sourceRoot)
	}
	return nil
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create Maven directory: %w", err)
	}

	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open cached file: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("inspect cached file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cached file is not a regular file: %s", source)
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".gradle-artifact-*")
	if err != nil {
		return fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return fmt.Errorf("copy cached file: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set artifact permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary artifact: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("replace artifact: %w", err)
	}
	return nil
}
