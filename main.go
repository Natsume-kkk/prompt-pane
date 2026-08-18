package main

import (
	"fmt"
	"os"

	"github.com/Natsume-kkk/prompt-pane/internal/command"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
)

func main() {
	if err := runcontext.EnsureProcessLifetime(); err != nil {
		fmt.Fprintln(os.Stderr, "prompt-pane:", err)
		os.Exit(1)
	}
	os.Exit(command.New().Execute(invocationArgs(os.Args[0], os.Args[1:])))
}

func invocationArgs(invocation string, args []string) []string {
	if shortcut.IsCodexAlias(invocation) && !isInternalInvocation(args) {
		return append([]string{"codex"}, args...)
	}
	return args
}

func isInternalInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "_agent", "_hook", "_view":
		return true
	default:
		return false
	}
}
