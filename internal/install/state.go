package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Natsume-kkk/prompt-pane/internal/filetxn"
	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

const (
	SchemaVersion         = 1
	EnvPreparedGeneration = "PROMPT_PANE_PREPARED_GENERATION"
	stateName             = "install-state.json"
	executableName        = "prompt-pane.exe"
)

type Release struct {
	Generation string `json:"generation"`
	Version    string `json:"version"`
}

type State struct {
	SchemaVersion  int      `json:"schema_version"`
	LauncherSHA256 string   `json:"launcher_sha256"`
	Current        *Release `json:"current"`
	Pending        *Release `json:"pending,omitempty"`
	Previous       *Release `json:"previous,omitempty"`
}

func Load() (State, error) {
	path, err := StatePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode Prompt Pane install state: %w", err)
	}
	if err := Validate(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func LoadIfPresent() (State, bool, error) {
	state, err := Load()
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	return state, err == nil, err
}

func Save(state State) error {
	if err := Validate(state); err != nil {
		return err
	}
	path, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Prompt Pane data directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Prompt Pane install state: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".install-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create Prompt Pane install state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write Prompt Pane install state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Prompt Pane install state: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return fmt.Errorf("prepare Prompt Pane install state: %w", err)
	}
	if err := filetxn.Replace(temporaryPath, path); err != nil {
		return fmt.Errorf("activate Prompt Pane install state: %w", err)
	}
	return nil
}

func Validate(state State) error {
	if state.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported Prompt Pane install state schema %d", state.SchemaVersion)
	}
	if !validGeneration(state.LauncherSHA256) {
		return fmt.Errorf("Prompt Pane install state has an invalid launcher digest")
	}
	if state.Current == nil {
		return fmt.Errorf("Prompt Pane install state has no current version")
	}
	for _, item := range []struct {
		name    string
		release *Release
	}{
		{name: "current", release: state.Current},
		{name: "pending", release: state.Pending},
		{name: "previous", release: state.Previous},
	} {
		name, release := item.name, item.release
		if release == nil {
			continue
		}
		if err := validateRelease(*release); err != nil {
			return fmt.Errorf("Prompt Pane install state %s version: %w", name, err)
		}
	}
	if sameRelease(state.Current, state.Pending) || sameRelease(state.Current, state.Previous) || sameRelease(state.Pending, state.Previous) {
		return fmt.Errorf("Prompt Pane install state references the same version more than once")
	}
	return nil
}

func SetPending(state State, release Release) (State, bool, error) {
	if err := Validate(state); err != nil {
		return State{}, false, err
	}
	if err := validateRelease(release); err != nil {
		return State{}, false, err
	}
	if state.Current.Generation == release.Generation || state.Pending != nil && state.Pending.Generation == release.Generation {
		return state, false, nil
	}
	state.Pending = releasePointer(release)
	state.Previous = nil
	return state, true, nil
}

func ActivatePending(state State) (State, error) {
	if err := Validate(state); err != nil {
		return State{}, err
	}
	if state.Pending == nil {
		return state, nil
	}
	current := *state.Current
	pending := *state.Pending
	state.Current = releasePointer(pending)
	state.Pending = nil
	state.Previous = releasePointer(current)
	return state, nil
}

func StatePath() (string, error) {
	root, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, stateName), nil
}

func VersionsRoot() (string, error) {
	root, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "versions"), nil
}

func LauncherPath() (string, error) {
	root, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin", executableName), nil
}

func RuntimePath(release Release) (string, error) {
	if err := validateRelease(release); err != nil {
		return "", err
	}
	root, err := VersionsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, release.Generation, executableName), nil
}

func IsLauncherPath(path string) bool {
	launcher, err := LauncherPath()
	if err != nil {
		return false
	}
	left, leftErr := filepath.Abs(path)
	right, rightErr := filepath.Abs(launcher)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func ReleasePathMatches(release Release, path string) bool {
	expected, err := RuntimePath(release)
	if err != nil {
		return false
	}
	left, leftErr := filepath.Abs(path)
	right, rightErr := filepath.Abs(expected)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func PreparedCurrentExecutable(path string) (bool, error) {
	generation := os.Getenv(EnvPreparedGeneration)
	if generation == "" {
		return false, nil
	}
	state, err := Load()
	if err != nil {
		return false, err
	}
	if state.Current.Generation != generation || !ReleasePathMatches(*state.Current, path) {
		return false, fmt.Errorf("prepared Prompt Pane launch does not match the current version")
	}
	if err := VerifyRelease(*state.Current); err != nil {
		return false, err
	}
	return true, nil
}

func ReleaseLabel(release Release) string {
	generation := release.Generation
	if len(generation) > 8 {
		generation = generation[:8]
	}
	return "v" + strings.TrimPrefix(release.Version, "v") + " (" + generation + ")"
}

func sameRelease(left, right *Release) bool {
	return left != nil && right != nil && left.Generation == right.Generation
}

func releasePointer(release Release) *Release {
	copy := release
	return &copy
}

func validateRelease(release Release) error {
	if !validGeneration(release.Generation) {
		return fmt.Errorf("invalid content digest")
	}
	if release.Version == "" || len(release.Version) > 128 {
		return fmt.Errorf("invalid version")
	}
	for _, r := range release.Version {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid version")
		}
	}
	return nil
}

func validGeneration(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
