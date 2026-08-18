package command

import (
	"errors"

	"github.com/Natsume-kkk/prompt-pane/internal/provider/codex"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
)

type setupTransaction struct {
	restorePlugin func() error
	restoreAlias  func() error
	discardPlugin func() error
	pluginChanged bool
	aliasChanged  bool
}

func captureSetupTransaction(codexPath string, pluginChange, aliasChange bool) (*setupTransaction, error) {
	transaction := &setupTransaction{}
	var err error
	if pluginChange {
		plugin, captureErr := codex.CaptureInstallation(codexPath)
		err = captureErr
		if err != nil {
			return nil, err
		}
		transaction.restorePlugin = plugin.Restore
		transaction.discardPlugin = plugin.Discard
	}
	if aliasChange {
		alias, captureErr := shortcut.CaptureInstallation(codexPath)
		err = captureErr
		if err != nil {
			_ = transaction.discard()
			return nil, err
		}
		transaction.restoreAlias = alias.Restore
	}
	return transaction, nil
}

func (t *setupTransaction) rollback() error {
	if t == nil {
		return nil
	}
	var rollbackErr error
	if t.aliasChanged && t.restoreAlias != nil {
		rollbackErr = errors.Join(rollbackErr, t.restoreAlias())
	}
	if t.pluginChanged && t.restorePlugin != nil {
		rollbackErr = errors.Join(rollbackErr, t.restorePlugin())
	}
	return rollbackErr
}

func (t *setupTransaction) discard() error {
	if t == nil || t.discardPlugin == nil {
		return nil
	}
	return t.discardPlugin()
}
