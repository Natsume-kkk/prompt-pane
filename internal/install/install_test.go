package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

func TestStageStateTransitionsAndBoundedCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "Prompt Pane 数据"))
	source := filepath.Join(root, "source.exe")
	if err := os.WriteFile(source, []byte("version one"), 0o700); err != nil {
		t.Fatal(err)
	}
	first, created, err := Stage(source, "1.0.0")
	if err != nil || !created {
		t.Fatalf("first stage created = %v, err = %v", created, err)
	}
	launcherDigest, err := InstallLauncher(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := State{SchemaVersion: SchemaVersion, LauncherSHA256: launcherDigest, Current: releasePointer(first)}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(source, []byte("version two"), 0o700); err != nil {
		t.Fatal(err)
	}
	second, _, err := Stage(source, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	state, changed, err := SetPending(state, second)
	if err != nil || !changed {
		t.Fatalf("pending changed = %v, err = %v", changed, err)
	}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = ActivatePending(state)
	if err != nil {
		t.Fatal(err)
	}
	if state.Current.Generation != second.Generation || state.Previous == nil || state.Previous.Generation != first.Generation || state.Pending != nil {
		t.Fatalf("activated state = %#v", state)
	}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(source, []byte("version three"), 0o700); err != nil {
		t.Fatal(err)
	}
	third, _, err := Stage(source, "3.0.0")
	if err != nil {
		t.Fatal(err)
	}
	state, changed, err = SetPending(state, third)
	if err != nil || !changed || state.Previous != nil {
		t.Fatalf("third pending state = %#v, changed = %v, err = %v", state, changed, err)
	}
	if err := Save(state); err != nil {
		t.Fatal(err)
	}
	if errs := CleanupUnreferenced(state); len(errs) != 0 {
		t.Fatalf("cleanup errors = %v", errs)
	}
	firstPath, _ := RuntimePath(first)
	if _, err := os.Stat(firstPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old previous version remains: %v", err)
	}
	for _, release := range []Release{second, third} {
		if err := VerifyRelease(release); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStateRejectsTraversalAndDuplicateReferences(t *testing.T) {
	valid := Release{Generation: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Version: "1.0.0"}
	state := State{SchemaVersion: SchemaVersion, LauncherSHA256: valid.Generation, Current: releasePointer(valid)}
	if err := Validate(state); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Generation = `..\\outside`
	state.Pending = releasePointer(invalid)
	if err := Validate(state); err == nil {
		t.Fatal("traversal generation was accepted")
	}
	state.Pending = releasePointer(valid)
	if err := Validate(state); err == nil {
		t.Fatal("duplicate generation was accepted")
	}
}

func TestSnapshotRestoresLauncherAndState(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "state"))
	launcher, _ := LauncherPath()
	statePath, _ := StatePath()
	if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("old launcher"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("old state"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("restoring an unchanged launcher replaced the file")
	}
	if err := os.WriteFile(launcher, []byte("new launcher"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("new state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{launcher: "old launcher", statePath: "old state"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("restored %s = %q, err = %v", path, data, err)
		}
	}
}
