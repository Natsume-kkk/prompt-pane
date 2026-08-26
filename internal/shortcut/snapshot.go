package shortcut

import "github.com/Natsume-kkk/prompt-pane/internal/filetxn"

type InstallationSnapshot = filetxn.Snapshot

func CaptureInstallation(codexPath string) (*InstallationSnapshot, error) {
	target, err := Target(codexPath)
	if err != nil {
		return nil, err
	}
	stateFile, err := statePath()
	if err != nil {
		return nil, err
	}
	return filetxn.Capture(target, stateFile)
}
