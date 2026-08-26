# Prompt Pane

让 Codex 和你本次运行提交的提示词并排显示在同一个终端工作区里。

![Prompt Pane：Codex 与本次运行的提示词并排显示](docs/assets/prompt-pane-hero.png)

左侧照常使用完整的 Codex CLI，右侧实时保留当前运行的新 prompt。无需切换窗口，也不用在回答和工具输出中反复向上翻找。

## 你会得到什么

- 固定的 70/30 双栏工作区，启动后左侧 Codex 默认获得输入焦点。
- 提交 prompt 后，右侧实时追加原始文本，保留中文、多行、emoji 和重复提交。
- 最新 prompt 处理期间显示与界面语言一致的活动短句和点号动效，任务结束或中断后自动收起。
- 显示当前会话的 token、上下文、Codex 使用额度、模型和 Git 状态。
- 按 `s` 打开设置，可切换界面语言并预览、保存六套内置主题。
- prompt 只在本机处理，不建立历史数据库。

## 安装

支持 Windows x64、Windows PowerShell 5.1／PowerShell 7、Zellij 0.44.3 和 Codex CLI。在 PowerShell 中运行：

```powershell
irm https://raw.githubusercontent.com/Natsume-kkk/prompt-pane/main/scripts/install.ps1 | iex
```

安装命令从 [最新稳定版 GitHub Release](https://github.com/Natsume-kkk/prompt-pane/releases/latest) 下载 Windows x64 程序及其 SHA-256 校验文件；第三方声明由同一 Release 提供。

重复运行同一条命令即可升级。脚本会下载并校验 Windows x64 发布物，再交给 Prompt Pane 写入独立版本目录并配置 Codex 集成；不需要管理员权限，也不会修改 PowerShell Profile、执行策略或系统 `PATH`。

如果升级时已有 Prompt Pane 工作区运行，新版会完成暂存，当前和随后并发打开的工作区继续使用旧版。关闭全部旧工作区后，下一次运行 `codex.pp` 会自动激活新版；不需要重跑升级命令。激活失败时继续使用旧版，不会留下混合组件。首次从旧的单版本安装布局迁移到该机制时，需要按提示关闭所有 Prompt Pane 工作区一次。

<details>
<summary>不允许执行远程脚本，或需要代理时</summary>

先下载并审查脚本，再执行：

```powershell
irm https://raw.githubusercontent.com/Natsume-kkk/prompt-pane/main/scripts/install.ps1 -OutFile .\install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

主程序下载使用当前 PowerShell 的代理和证书设置。Zellij 下载读取 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY`；如果网络只配置了 Windows 系统代理，请设置 `HTTPS_PROXY`，或提前把 Zellij 0.44.3 放入 `PATH`。

```powershell
$env:HTTPS_PROXY = "http://proxy.example:8080"
```

</details>

安装完成后运行：

```powershell
codex.pp
```

首次使用时先提交一条 prompt。如果右侧没有更新，按 `s` 打开设置，再进入帮助；然后在 Codex 中运行 `/hooks`，审查并信任 Prompt Pane Hook 后重新启动 `codex.pp`。

<details>
<summary>从源码构建</summary>

需要提前安装 Codex CLI、Git 和 Go 1.26.6 或更高版本。

```powershell
git clone https://github.com/Natsume-kkk/prompt-pane.git
cd prompt-pane
.\scripts\build.cmd
.\dist\prompt-pane.exe setup codex
```

</details>

## 开始使用

Prompt Pane 不会替换原来的 `codex` 命令。只有 `codex.pp` 会进入双栏工作区，Codex 参数会原样转发：

![Prompt Pane 的真实 70/30 终端布局：左侧运行 Codex，右侧显示当前运行提交的提示词](docs/assets/prompt-pane-workspace.svg)

```powershell
codex.pp
codex.pp resume
```

常用操作：

| 操作 | 按键或鼠标 |
|---|---|
| 选择上一／下一条 prompt | `↑`／`↓` 或 `k`／`j` |
| 提示词翻页 | `PgUp`／`PgDn` 或滚轮 |
| 跳到第一条／最新一条 | `Home`／`End` |
| 展开／折叠长 prompt | `Enter` |
| 打开设置 | `s` |
| 设置中选择／打开 | `↑`／`↓`、`Enter` |
| 帮助／关于中翻页 | `↑`／`↓`、`PgUp`／`PgDn` 或滚轮 |
| 关闭右侧 viewer | `Ctrl+X` |
| 退出整个工作区 | 关闭终端或使用 Zellij 的 `Ctrl+Q` |
| 复制可见文字 | 按住左键拖动，松开后复制 |

关闭 viewer 不会退出左侧 Codex；关闭终端会结束当前 Prompt Pane 工作区及其中的 Codex。如果拖动无法复制，可以按住 `Shift` 使用终端原生选择。

右侧 Zellij 窗格标题固定显示 `PROMPTS`；Prompt Pane 的主题只管理右侧 viewer 内部颜色，不修改 Zellij 外层边框主题。

当前焦点 prompt 或最新处理中 prompt 的编号与全部可见正文整体加粗；处理中 prompt 还会显示尾部动画。两种状态重合时仍使用同一种整体加粗，处理结束后只保留当前焦点样式。

### 显示设置

按 `s` 打开设置，可以统一进入主题、界面语言、帮助和关于页面：

- 主题：提供六套内置主题的真实界面预览与保存。
- 界面语言：尚未保存语言偏好时按 Windows 用户界面语言选择中文或英文，之后可以手动切换并保存；用户原始 prompt、指标、模型和 Git 数据保持原文。
- 帮助：集中说明提示词操作、Git 状态、窗格操作和连接排障。
- 关于：显示 Prompt Pane 版本、完整支持环境和视觉来源。

设置首页以箭头和整体加粗表示当前选中项；主题列表额外使用选择色表示正在预览的主题。

需要减少动态时，在启动本次工作区前设置：

```powershell
$env:PROMPT_PANE_REDUCED_MOTION = "1"
```

该模式固定显示 `...` 并降低文案切换频率。

<p align="center">
  <img src="docs/assets/prompt-pane-themes.png" alt="Prompt Pane 六套主题预览" width="520">
</p>

<details>
<summary>状态栏中的 Git 符号</summary>

状态栏中的 Git 信息采用 `main* +1334 -2199 ?7` 这样的格式：

| 显示 | 含义 |
|---|---|
| `main` | 当前 Git 分支 |
| `*` | 存在已跟踪文件改动 |
| `+N` | 已暂存和未暂存的已跟踪文件相对最近一次提交累计新增行数 |
| `-N` | 已暂存和未暂存的已跟踪文件相对最近一次提交累计删除行数 |
| `?N` | 排除 `.gitignore` 后的未跟踪文件数 |

增删数字是代码行数，不是文件数，也不是分支领先或落后的提交数。按 `s` 打开设置，再进入帮助，也可以查看这些符号的含义。

</details>

## 会话与隐私

- 只显示当前 `codex.pp` 运行期间由 Codex Hook 收到的新 prompt，不显示回答、推理、工具或系统消息。
- 等待文案从内置静态语料中随机选择，不读取 prompt，也不保存当前短句、语体、随机种子或历史。
- 新会话、恢复会话和 `/clear` 会从空白开始；`/compact` 保留本次运行已经显示的 prompt。
- 任务运行期间排队或转向的多条输入会逐条保留，即使 Codex 为它们复用同一 `turn_id`。
- `/side` 和 `/btw` 的内容只临时覆盖右侧，回到主会话后不会混入主 prompt 列表。
- 不扫描 Codex 会话目录，不按最近文件猜测会话，不把 prompt 写入日志、配置、缓存或遥测。
- 并发运行使用独立身份和本地连接，不共享 prompt。

完整安全边界见 [SECURITY.md](SECURITY.md)。

## 排障与移除

安装或启动失败时先运行：

```powershell
& "$env:APPDATA\PromptPane\bin\prompt-pane.exe" doctor
```

错误信息会指出失败的是 GitHub 下载、SHA-256、版本暂存或激活、Zellij、Codex 插件、权限还是 `codex.pp` 冲突，并给出对应处理方向。`doctor` 还会分别显示当前版本和等待激活版本。

移除 Prompt Pane 管理的 Codex 集成：

```powershell
& "$env:APPDATA\PromptPane\bin\prompt-pane.exe" teardown codex
```

该命令不会删除用户原有的 Codex CLI 或已经下载的 Zellij。

## 当前限制

- 只正式支持 Windows x64、PowerShell、Zellij 和 Codex CLI。
- 不能连接已经运行的 Codex。
- 不提供全局历史、搜索、编辑、导出、收藏或同步。
- 不支持 Codex 之外的 AI 命令行工具，也不承诺 WSL、SSH、容器、macOS 或 Linux 可用。
- 字体和像素级渲染由终端控制；部分字体下中文拖选高亮可能出现半格视觉残影，但复制内容仍按完整 Unicode 字素处理。

## 开发与文档

```powershell
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
.\scripts\build.cmd
```

Windows 本机执行 race 检查需要 CGO 可用的 C 编译器；缺少该工具链时，`go test -race ./...` 无法构建，正式发布结果以 Windows CI 的同项检查为准。

- [产品规格](docs/product.md)
- [架构规格](docs/architecture.md)
- [UI 规格](docs/ui.md)
- [贡献指南](CONTRIBUTING.md)

## 许可证

Prompt Pane 使用 [Apache License 2.0](LICENSE)。第三方 Go 模块和视觉设计来源见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
