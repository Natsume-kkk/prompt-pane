package install

import "github.com/Natsume-kkk/prompt-pane/internal/filetxn"

type Snapshot = filetxn.Snapshot

func Capture() (*Snapshot, error) {
	statePath, err := StatePath()
	if err != nil {
		return nil, err
	}
	launcherPath, err := LauncherPath()
	if err != nil {
		return nil, err
	}
	return filetxn.Capture(launcherPath, statePath)
}
