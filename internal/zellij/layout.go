package zellij

import (
	"fmt"
	"strconv"
	"strings"
)

func Layout(executable string, codexArgs []string) string {
	var agentArgs []string
	agentArgs = append(agentArgs, "_agent", "codex")
	if len(codexArgs) > 0 {
		agentArgs = append(agentArgs, "--")
		agentArgs = append(agentArgs, codexArgs...)
	}
	return fmt.Sprintf(`layout {
    pane split_direction="vertical" {
        pane size="70%%" command=%s focus=true {
            args %s
        }
        pane size="30%%" name=" " command=%s {
            args "_view"
        }
    }
}
`, quote(executable), quoteArgs(agentArgs), quote(executable))
}

func quote(value string) string {
	return strconv.Quote(value)
}

func quoteArgs(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = quote(value)
	}
	return strings.Join(quoted, " ")
}
