package command

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSetupTransactionRollsBackChangedComponentsInReverseOrder(t *testing.T) {
	pluginErr := errors.New("plugin restore failed")
	var order []string
	transaction := &setupTransaction{
		pluginChanged:  true,
		aliasChanged:   true,
		installChanged: true,
		restoreInstall: func() error {
			order = append(order, "install")
			return nil
		},
		restoreAlias: func() error {
			order = append(order, "alias")
			return nil
		},
		restorePlugin: func() error {
			order = append(order, "plugin")
			return pluginErr
		},
	}
	err := transaction.rollback()
	if !errors.Is(err, pluginErr) {
		t.Fatalf("rollback error = %v", err)
	}
	if want := []string{"install", "alias", "plugin"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("rollback order = %v, want %v", order, want)
	}
}

func TestSetupTransactionDoesNotRestoreUnchangedComponents(t *testing.T) {
	transaction := &setupTransaction{
		restoreAlias:  func() error { t.Fatal("restored unchanged alias"); return nil },
		restorePlugin: func() error { t.Fatal("restored unchanged plugin"); return nil },
	}
	if err := transaction.rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestSetupTransactionRollbackAfterPreservesOperationError(t *testing.T) {
	operationErr := errors.New("installation failed")
	rollbackErr := errors.New("restore failed")
	transaction := &setupTransaction{
		installChanged: true,
		restoreInstall: func() error { return rollbackErr },
	}
	err := transaction.rollbackAfter(operationErr)
	if !errors.Is(err, operationErr) || !strings.Contains(err.Error(), "installation rollback failed: restore failed") {
		t.Fatalf("combined rollback error = %v", err)
	}
}
