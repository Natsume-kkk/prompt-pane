package main

import (
	"reflect"
	"testing"
)

func TestCodexAliasPreservesArguments(t *testing.T) {
	arguments := []string{"resume", "thread id with spaces", "--model", "gpt-test"}
	got := invocationArgs(`C:\Users\tester\AppData\Roaming\npm\codex.pp.exe`, arguments)
	want := []string{"codex", "resume", "thread id with spaces", "--model", "gpt-test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(arguments, []string{"resume", "thread id with spaces", "--model", "gpt-test"}) {
		t.Fatal("source arguments were modified")
	}
}

func TestCodexAliasPreservesInternalInvocations(t *testing.T) {
	for _, arguments := range [][]string{
		{"_agent", "codex", "--", "resume"},
		{"_hook", "codex"},
		{"_observe", "codex", "thr", "turn", "0", `C:\synthetic\current.jsonl`},
		{"_view"},
		{"_prepare", "codex"},
		{"_activate", "codex"},
	} {
		if got := invocationArgs(`C:\Users\tester\AppData\Roaming\npm\codex.pp.exe`, arguments); !reflect.DeepEqual(got, arguments) {
			t.Fatalf("arguments = %#v, want %#v", got, arguments)
		}
	}
}

func TestHookAndObserverInheritWorkspaceProcessLifetime(t *testing.T) {
	for _, arguments := range [][]string{{"_hook", "codex"}, {"_observe", "codex", "thr", "turn", "0", "current.jsonl"}} {
		if ownsProcessLifetime(arguments) {
			t.Fatalf("internal leaf unexpectedly owns a process lifetime: %#v", arguments)
		}
	}
	for _, arguments := range [][]string{{"codex"}, {"setup", "codex"}, {"_agent", "codex"}, {"_view"}} {
		if !ownsProcessLifetime(arguments) {
			t.Fatalf("lifetime owner was skipped: %#v", arguments)
		}
	}
}

func TestNormalInvocationIsUnchanged(t *testing.T) {
	arguments := []string{"doctor"}
	if got := invocationArgs("prompt-pane.exe", arguments); !reflect.DeepEqual(got, arguments) {
		t.Fatalf("arguments = %#v", got)
	}
}
