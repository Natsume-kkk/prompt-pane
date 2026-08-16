package plugins

import "embed"

// Content contains the exact plugin tree installed by setup.
//
//go:embed all:prompt-pane
var Content embed.FS
