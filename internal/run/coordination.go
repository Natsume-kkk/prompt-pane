package run

import "errors"

var ErrWorkspacesActive = errors.New("running Prompt Pane workspaces must be closed before updating managed components")

type CoordinationLock interface {
	Close() error
}
