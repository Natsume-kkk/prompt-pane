package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeWritableDirectorySupportsUnicodeAndLeavesNoArtifacts(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "安装 path", "nested")
	if err := ProbeWritableDirectory(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("probe-created target was not cleaned: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("probe left artifacts: %v", entries)
	}
}

func TestProbeWritableDirectoryKeepsExistingDirectory(t *testing.T) {
	target := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ProbeWritableDirectory(target); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("existing target was removed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("probe left artifacts: %v", entries)
	}
}

func TestProbeWritableDirectoryRejectsInvalidTargets(t *testing.T) {
	if err := ProbeWritableDirectory("relative"); err == nil {
		t.Fatal("relative write target was accepted")
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ProbeWritableDirectory(file); err == nil {
		t.Fatal("regular file was accepted as a writable directory")
	}
}
