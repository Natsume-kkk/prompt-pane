package command

import (
	"errors"
	"reflect"
	"testing"
)

func TestSetupTransactionRollsBackChangedComponentsInReverseOrder(t *testing.T) {
	pluginErr := errors.New("plugin restore failed")
	var order []string
	transaction := &setupTransaction{
		pluginChanged: true,
		aliasChanged:  true,
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
	if want := []string{"alias", "plugin"}; !reflect.DeepEqual(order, want) {
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
