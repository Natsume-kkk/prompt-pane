package main

import (
	"fmt"
	"os"

	"github.com/Natsume-kkk/prompt-pane/internal/command"
	"github.com/Natsume-kkk/prompt-pane/internal/launcher"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
)

func main() {
	if err := runcontext.EnsureProcessLifetime(); err != nil {
		fmt.Fprintln(os.Stderr, "prompt-pane:", err)
		os.Exit(1)
	}
	args := invocationArgs(os.Args[0], os.Args[1:])
	managed, err := launcher.IsManagedInvocation(os.Args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "prompt-pane:", err)
		os.Exit(1)
	}
	if managed {
		os.Exit(launcher.New().Execute(args))
	}
	os.Exit(command.New().Execute(args))
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
	case "_agent", "_hook", "_view", "_prepare", "_activate":
		return true
	default:
		return false
	}
}
