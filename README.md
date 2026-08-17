# Prompt Pane

Prompt Pane 在同一个终端工作区中并排运行 Codex CLI 与本次运行的用户提示词：左侧继续使用 Codex，右侧实时查看自己提交过的 prompt。

```text
┌──────────────────────────────────────┬──────────────────┐
│ Codex CLI                            │ 1  first prompt  │
│                                      │                  │
│                                      │ [LIVE]    h help │
└──────────────────────────────────────┴──────────────────┘
```

## 当前版本

当前源码版本为 `v1.1.0`，支持 Windows x64。仓库已准备 PowerShell 一键安装脚本和发布物 SHA-256 契约；在 GitHub Release 正式创建前，外部一键安装命令仍不可用，当前请使用下方源码构建方式。

| 项目 | 支持范围 |
|---|---|
| 操作系统 | Windows x64 |
| Shell | Windows PowerShell 5.1 或 PowerShell 7 |
| AI CLI | Codex CLI |
| 工作区 | Zellij 0.44.3 |
| 源码构建 | Go 1.26 或更高版本 |

当前验证基线为 Go 1.26.5、Codex CLI 0.147.0 和 Zellij 0.44.3。终端只需能够正常运行受支持的 Zellij，不限定品牌。

## 快速开始

公开 GitHub Release 后，推荐在 PowerShell 5.1 或 7 中运行一条命令完成当前用户级安装；重复执行同一命令即可升级：

```powershell
irm https://raw.githubusercontent.com/Natsume-kkk/prompt-pane/main/scripts/install.ps1 | iex
```

如果组织策略不允许内联脚本，可先下载、审查，再以单次进程级策略运行：

```powershell
irm https://raw.githubusercontent.com/Natsume-kkk/prompt-pane/main/scripts/install.ps1 -OutFile .\install.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

脚本只支持 Windows x64，会直接下载 Release 的 `prompt-pane.exe` 与 `prompt-pane.exe.sha256`，校验 SHA-256 后原子更新到当前用户目录，再运行 `setup codex`。它不要求管理员权限，不修改 PowerShell Profile、执行策略或系统 `PATH`。在 Release 尚未创建或仓库仍不可公开访问时，这条命令会失败，不应作为当前可用入口宣传。

当前源码安装方式如下。

克隆仓库并构建：

```powershell
git clone https://github.com/Natsume-kkk/prompt-pane.git
cd prompt-pane
.\scripts\build.cmd
```

安装或刷新 Codex 集成：

```powershell
.\dist\prompt-pane.exe setup codex
```

`setup codex` 会：

- 检查 Windows、PowerShell 和 Codex；
- 按用户级安装并校验 Zellij 0.44.3；
- 通过 Codex 官方插件机制安装 Prompt Pane Hook；
- 创建受管入口 `codex.pp.exe`；
- 完成后自动运行只读诊断。

它不会替换原有的 `codex` 命令，也不会修改 PowerShell Profile 或系统 `PATH`。

首次运行时先提交一条 prompt；如果右侧仍未显示，界面会在约 10 秒后直接提示打开 `/hooks`。在 Codex 中审查 Prompt Pane Hook，然后重新启动：

```powershell
codex.pp
```

## 使用

日常入口：

```powershell
codex.pp
```

Codex 参数会原样转发：

```powershell
codex.pp resume
```

兼容入口仍然可用：

```powershell
.\dist\prompt-pane.exe codex -- <codex-args>
```

普通 `codex` 命令保持原有行为，不会进入 Prompt Pane 工作区。

### Viewer 操作

| 操作 | 按键或鼠标 |
|---|---|
| 选择上一／下一条提示词 | `↑`、`↓`、`k`、`j` |
| 翻页 | `PgUp`、`PgDn`、滚轮 |
| 跳到第一条／最新一条 | `Home`、`End` |
| 展开／折叠当前长提示词 | `Enter` |
| 折叠全部长提示词 | `c` |
| 打开帮助 | `h` |
| 关闭帮助 | `h` 或 `Esc` |
| 在帮助页预览／保存主题 | `↑`／`↓`、`Enter` |
| 关闭右侧 viewer | `Ctrl+X` |
| 选择提示词 | 左键单击 |
| 选择并复制可见文字 | 按住左键拖动，松开后复制 |

`Ctrl+X` 只关闭右侧 viewer，左侧 Codex 会继续运行。终端不支持 OSC 52 时，可以按住 `Shift` 使用终端原生选择和复制。

### 状态栏与主题

每次 Codex 回答完成后，viewer 会更新当前会话的累计 token、上下文占用、5 小时／7 天限额、模型以及宽度允许时的 Git 分支状态。首次回答结束前，指标区显示 `Metrics available after first response`，不把尚未提交 prompt 的正常状态描述为正在等待回答；已有指标中的未知字段显示 `--`，不用 `0` 或空进度条代替未知值。日常状态区固定为标题与指标两行，默认 70/30 窗格使用 Token Tracker 同款 `█`／`░` 8 格短进度条，超宽窗格增加到 12 格；空间不足时优先保留 5 小时／7 天限额并逐步隐藏次要信息，不把状态区扩展成多行仪表盘。

进入 `/side` 或 `/btw` 时，viewer 正文临时切换为侧聊提示词，状态栏继续保留父对话最后一次有效指标且不接受侧聊指标覆盖。Codex 未提供侧聊退出 Hook 时，关闭侧聊后正文会暂时停留在侧聊最后快照；提交下一条父对话提示词后，viewer 恢复原父提示词、追加新提示词并丢弃侧聊内容。

内置主题为 `mocha`、`latte`、`frappe`、`macchiato`、`nord` 和 `dracula`，色值与语义映射来自 Token Tracker。默认 `auto` 会在可确认浅色背景时使用 `latte`，否则使用 `mocha`，但不会作为多余选项显示在主题列表中。帮助页统一以 `Help` 为标题，依次显示连接排障（仅适用时）、viewer 操作、导航、prompt 操作、主题与语义预览、与当前工作区直接相关的 Zellij 默认操作，最后以精简 `About` 说明版本、技术基础、Token Tracker 视觉来源和支持环境；宽度允许时会用 `current`／`recommended` 区分当前与自动推荐主题，自定义 Zellij 键位可能与默认提示不同。固定页脚使用 `↑`／`↓` 实时预览、`Enter` 保存。帮助正文、未选中主题名称和普通提示词编号使用正常前景色。也可用 `PROMPT_PANE_THEME` 临时覆盖，`NO_COLOR` 会关闭颜色。

## 会话行为

Prompt Pane 只显示当前 `codex.pp` 运行期间由 Codex Hook 收到的新提示词。

- 新会话、恢复会话和 `/clear` 会清空右侧，只显示边界之后的新提示词。
- `/compact` 保留本次运行已经显示的提示词。
- `/side`／`/btw` 只临时覆盖正文并保留父提示词与状态栏；返回后的第一条父提示词恢复父列表，侧聊内容不保存。
- 会话结束后，迟到事件不会重新激活或修改右侧记录。
- 相同文本的不同提交会分别保留。
- 回答、推理、工具活动和系统内容不会显示。

## 工作原理

```text
codex.pp
   │
   ├─ Zellij ── Codex CLI
   │
   └─ Viewer ◀── authenticated local IPC ◀── Codex Hook
```

Zellij 负责 70/30 窗格，Bubble Tea 负责右侧 TUI。Prompt Pane 只在本次 Zellij 会话中关闭边框悬停高亮和 resize 帮助文字，仍保留窗格边框与鼠标 resize；首次聚焦点击会同时传入目标窗格，因此右侧 viewer 无需预先聚焦即可开始拖选。程序不启用鼠标经过即切换焦点，也不修改用户全局 Zellij 配置。每次运行都有独立的 `run_id`、本地 endpoint 和一次性 token；Hook 事件必须与本次运行及 Codex `session_id` 精确匹配。

## 隐私与安全

- 不扫描 `~/.codex/sessions`，不读取 App Server，也不按最近文件猜测会话。
- `Stop` Hook 只按 Codex 提供的当前会话准确 `transcript_path` 筛选结构化用量元数据；不读取、传输或保存其中的 prompt、回答、推理和工具内容。
- prompt 只经过 Hook 标准输入、当前用户本地 IPC、进程内存和终端画面。
- 不把 prompt 写入日志、配置、缓存、遥测或网络请求。
- 鼠标可直接在未聚焦的 viewer 中开始拖选，只复制当前视口中已经安全渲染的文字；程序不会读取剪贴板。
- 显示前过滤 ANSI、CSI、OSC 等终端控制序列。
- 并发运行使用独立 endpoint、token 和会话状态，不共享 prompt。

更多信息见 [安全策略](SECURITY.md)。

## 诊断和移除

```powershell
.\dist\prompt-pane.exe doctor
.\dist\prompt-pane.exe teardown codex
```

`doctor` 检查平台、PowerShell、程序路径、Codex、插件、`codex.pp` 和 Zellij。`teardown codex` 只移除由当前项目管理且摘要仍匹配的 Codex 插件、快捷入口和本地 marketplace，不删除托管 Zellij。

退出码：

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 环境或运行失败 |
| `2` | 命令用法错误 |

## 常见问题

### 右侧一直显示 `[READY]`

`[READY]` 表示 Codex 与 viewer 已启动，但尚未由 `SessionStart` 确认 Hook 链路。Codex 可能要到第一条 prompt 才创建逻辑 session。

如果提交 prompt 后仍无更新：

1. 按 `h` 查看排障页。
2. 在左侧 Codex 中运行 `/hooks`。
3. 审查并信任 Prompt Pane Hook。
4. 退出当前工作区并重新运行 `codex.pp`。

### 安装或启动失败

先运行：

```powershell
.\dist\prompt-pane.exe doctor
```

诊断结果会区分环境前置条件和可由 `setup codex` 修复的受管组件。

## 已知限制

- 仅支持 Windows x64、PowerShell、Zellij 和 Codex CLI。
- 不能连接已经运行的 Codex。
- 不提供全局历史、搜索、编辑、删除、导出、收藏或同步。
- 不支持 Codex 之外的 AI provider。
- 某些终端字体与渲染组合下，中文拖选高亮可能出现半格视觉残影；逻辑选区和复制内容仍按完整 Unicode 字素处理。
- 字体、字号、字重、字间距和像素行高由终端控制。

## 开发

```powershell
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
.\scripts\build.cmd
```

构建必须通过 `scripts\build.cmd`，产物写入忽略的 `dist\prompt-pane.exe`。Windows 上运行 race 测试需要可用的 C 编译器。

设计文档：

- [产品规格](docs/product.md)
- [架构规格](docs/architecture.md)
- [UI 规格](docs/ui.md)
- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)

## 许可证

Prompt Pane 使用 [Apache License 2.0](LICENSE)。

主题色和状态栏设计包含来自 Token Tracker 的 MIT 许可内容，详见 [第三方声明](THIRD_PARTY_NOTICES.md)。
