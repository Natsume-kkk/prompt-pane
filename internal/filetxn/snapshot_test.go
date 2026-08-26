package filetxn

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRestoresChangedAndNewFiles(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	created := filepath.Join(root, "created")
	if err := os.WriteFile(existing, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(existing, created)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("unchanged file was replaced")
	}

	if err := os.WriteFile(existing, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "before" {
		t.Fatalf("restored existing file = %q, err = %v", data, err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("new file survived rollback: %v", err)
	}
}

func TestCaptureRejectsDirectories(t *testing.T) {
	if _, err := Capture(t.TempDir()); err == nil {
		t.Fatal("captured a directory as a regular file")
	}
}

func TestNilSnapshotRestoreIsSafe(t *testing.T) {
	var snapshot *Snapshot
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
}
