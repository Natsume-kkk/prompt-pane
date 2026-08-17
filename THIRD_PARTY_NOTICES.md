# Third-Party Notices

本文件列出 Prompt Pane Windows x64 可执行文件包含的第三方 Go 模块，以及视觉设计使用的第三方内容。版本来自当前 `go.mod`／`go.sum` 和构建产物的 Go module metadata；许可证文本来自对应锁定版本的模块发行内容。

## Go modules included in the executable

| Module | Audited version | License |
|---|---|---|
| [charm.land/bubbletea/v2](https://pkg.go.dev/charm.land/bubbletea/v2@v2.0.8) | `v2.0.8` | MIT |
| [charm.land/lipgloss/v2](https://pkg.go.dev/charm.land/lipgloss/v2@v2.0.6) | `v2.0.6` | MIT |
| [github.com/Microsoft/go-winio](https://pkg.go.dev/github.com/Microsoft/go-winio@v0.6.2) | `v0.6.2` | MIT |
| [github.com/charmbracelet/colorprofile](https://pkg.go.dev/github.com/charmbracelet/colorprofile@v0.4.3) | `v0.4.3` | MIT |
| [github.com/charmbracelet/ultraviolet](https://pkg.go.dev/github.com/charmbracelet/ultraviolet@v0.0.0-20260811164956-006e29f97886) | `v0.0.0-20260811164956-006e29f97886` | MIT |
| [github.com/charmbracelet/x/ansi](https://pkg.go.dev/github.com/charmbracelet/x/ansi@v0.11.8) | `v0.11.8` | MIT |
| [github.com/charmbracelet/x/term](https://pkg.go.dev/github.com/charmbracelet/x/term@v0.2.2) | `v0.2.2` | MIT |
| [github.com/charmbracelet/x/windows](https://pkg.go.dev/github.com/charmbracelet/x/windows@v0.2.2) | `v0.2.2` | MIT |
| [github.com/clipperhouse/displaywidth](https://pkg.go.dev/github.com/clipperhouse/displaywidth@v0.11.0) | `v0.11.0` | MIT |
| [github.com/clipperhouse/uax29/v2](https://pkg.go.dev/github.com/clipperhouse/uax29/v2@v2.7.0) | `v2.7.0` | MIT |
| [github.com/lucasb-eyer/go-colorful](https://pkg.go.dev/github.com/lucasb-eyer/go-colorful@v1.4.1) | `v1.4.1` | MIT |
| [github.com/mattn/go-runewidth](https://pkg.go.dev/github.com/mattn/go-runewidth@v0.0.24) | `v0.0.24` | MIT |
| [github.com/muesli/cancelreader](https://pkg.go.dev/github.com/muesli/cancelreader@v0.2.2) | `v0.2.2` | MIT |
| [github.com/rivo/uniseg](https://pkg.go.dev/github.com/rivo/uniseg@v0.4.7) | `v0.4.7` | MIT |
| [github.com/xo/terminfo](https://pkg.go.dev/github.com/xo/terminfo@v0.0.0-20220910002029-abceb7e1c41e) | `v0.0.0-20220910002029-abceb7e1c41e` | MIT |
| [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync@v0.22.0) | `v0.22.0` | BSD-3-Clause |
| [golang.org/x/sys](https://pkg.go.dev/golang.org/x/sys@v0.47.0) | `v0.47.0` | BSD-3-Clause |

## Visual design source

Prompt Pane 的六套主题色、语义配色、主题选择器八色色板顺序和 Codex 状态栏设计基于 Token Tracker：

- Project: https://github.com/stormzhang/token-tracker
- Audited revision: `ab091fc7cdf1ef5874befce2b5b6410dbf095535`
- License: MIT

## MIT license text

The following copyright notices apply to the MIT-licensed items listed above:

```text
Copyright (c) 2020-2026 Charmbracelet, Inc.
Copyright (c) 2021-2026 Charmbracelet, Inc.
Copyright (c) 2015 Microsoft
Copyright (c) 2020-2024 Charmbracelet, Inc
Copyright (c) 2025 Charmbracelet, Inc
Copyright (c) 2023 Charmbracelet, Inc.
Copyright (c) 2025 Matt Sherman
Copyright (c) 2020 Matt Sherman
Copyright (c) 2013 Lucas Beyer
Copyright (c) 2016 Yasuhiro Matsumoto
Copyright (c) 2022 Erik Geiser and Christian Muehlhaeuser
Copyright (c) 2019 Oliver Kuederle
Copyright (c) 2016 Anmol Sethi
Copyright (c) 2026 stormzhang

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## BSD-3-Clause license text

This license applies to `golang.org/x/sync` and `golang.org/x/sys`:

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
