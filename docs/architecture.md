# Prompt Pane 架构规格

## 架构结论

Prompt Pane 是独立 Go CLI。Zellij 只负责终端内 70/30 布局，Bubble Tea 只负责右侧 TUI，Codex 官方 Hooks 是实时提示词事实源。程序不调用终端品牌专有 API；能够正常运行受支持 Zellij 的终端均在支持边界内。

Prompt Pane 不搜索 `~/.codex/sessions`、不读取 Codex App Server 历史，也不按时间、目录或进程猜测会话。新会话、恢复会话和清空后的会话都只显示本次运行期间 `UserPromptSubmit` Hook 实际收到的新提示词；上下文压缩保留本次运行已经显示的记录。`Stop` Hook 可以按官方提供的准确 `session_id` 与 `transcript_path` 只读解析当前会话的结构化 usage 元数据，但不得读取或保留 prompt、回复、推理和工具内容。

## 组件与依赖方向

```text
Public CLI
  └─ Command use cases
      ├─ Run controller
      │   ├─ Local IPC
      │   └─ In-memory prompt store
      ├─ Zellij adapter ──> managed or PATH Zellij
      └─ Provider boundary
          └─ Codex adapter
              ├─ Embedded Codex plugin
              └─ Hook event decoder

In-memory prompt store ──> Bubble Tea UI
```

依赖方向要求：UI 只接收规范化事件；Codex adapter 不依赖终端或 UI；Zellij adapter 不解析会话；prompt store 不持久化。

## 进程模型

```powershell
prompt-pane codex -- <codex-args>
codex.pp <codex-args>
```

`codex.pp.exe` 是当前 Prompt Pane 可执行文件的受管副本。程序根据自身启动文件名进入 `codex` 用例，所有后续参数仍保持为 argv；它不替换 `codex.cmd`，Codex 子进程继续由 `exec.LookPath("codex")` 发现。

1. 校验 Codex、插件和 Zellij。
2. 创建 `run_id`、一次性 token 与当前用户 IPC endpoint。
3. 生成仅属于本次运行的 Zellij layout并前台启动 Zellij session。
4. 左侧运行 `prompt-pane _agent codex -- <codex-args>`，由其原样转发 Codex argv；会话选择与恢复由 Codex 自身处理。
5. 右侧运行 `prompt-pane _view`。
6. Codex Hook 运行 `prompt-pane _hook codex`，从环境取得 endpoint 与 token，从标准输入取得官方事件 JSON。
7. Zellij 退出后只清理本次运行的非敏感临时状态。

Codex 参数保留为 argv，不经过 Shell 字符串。`run_id`、认证 token 与 endpoint 只通过进程环境传递，不写运行清单或日志。

## 运行身份与状态

状态机：

```text
created -> workspace_started -> session_bound -> live -> ended
                    \-> failed
```

可落盘的短生命周期状态只允许包含 schema version、`run_id`、provider、创建时间、工作目录、结构化 argv、进程标识、生命周期状态和安全错误码。禁止包含 prompt、token 或原始 Hook JSON。

## IPC 协议

Windows `v1.1.0` 使用当前用户命名管道；其他平台接口保留但不承诺可用。每次运行使用不可预测 endpoint 和至少 256-bit token。Hook 原始输入上限为 1 MiB，认证 IPC frame 上限为 8 MiB，为 JSON 转义保留空间；超限只返回不含 payload 的安全错误。

每个请求为有上限的 JSON frame：

```text
version | run_id | token | type | event?
```

`type` 只允许 `event` 或 `subscribe`；只有事件请求携带规范化 `event`。事件请求返回 `ok`，订阅连接持续接收由 `state`、`prompts` 和 `notice` 组成的内存快照。

事件最小集合：

- `session.started`：规范化 `session_id` 与 `source`。
- `prompt.submitted`：`session_id`、`turn_id`、完整 prompt。
- `session.ended`：`session_id`。
- `metrics.updated`：`session_id` 与可空的累计 token、上下文窗口、5h／7d 限额、模型、推理强度及项目 Git 状态。

server 必须验证版本、大小、token、`run_id`、事件字段和会话绑定。认证失败、超限或状态转换非法时拒绝消息，错误不得包含 payload。

## Codex Hook

插件包含 `SessionStart`、`UserPromptSubmit`、`Stop` 和 `SessionEnd` command hooks。Hook 进程必须：

1. 从标准输入读取单个有界 JSON 对象。
2. 验证 `hook_event_name`、`session_id` 和事件特有字段。
3. 从继承环境读取本次 endpoint、`run_id` 与 token。
4. 通过 IPC 发送规范化事件。
5. 成功时不向 stdout 写 prompt 或额外上下文。

`Stop` Hook 只按本次 payload 的准确 `transcript_path` 逐行筛选 `session_meta`、`turn_context` 和 `token_count`；不提供最近文件回退，不把路径、原始行或内容字段送入 IPC。指标解析失败时静默跳过本次更新并保持 Codex 可用。

Hook 故障不得阻止用户 prompt。非托管 Hook 需要 Codex 信任；程序只能引导用户通过 `/hooks` 审查，禁止绕过信任。

Codex 会对所有会话加载已安装插件。Hook 只有在三个 Prompt Pane 运行环境变量全部存在时才连接 IPC；三者全无时静默以退出码 0 返回，部分缺失则视为配置错误，避免普通 Codex 会话出现无关告警。

## SessionStart 会话边界

`codex.pp resume` 的参数原样交给 Codex，Prompt Pane 不提前选择或读取会话。Hook 订阅并验证官方 `SessionStart` source：

- `startup`：建立首次绑定；相同 `session_id` 的重复事件保持当前快照，不同 `session_id` 表示进入新会话并原子清空提示词快照、`turn_id` 去重集合和 UI 阅读状态。
- `resume`：无论 `session_id` 是否变化，都原子清空提示词快照、`turn_id` 去重集合和 UI 阅读状态，并显示只接收后续新提示词的内存提示。
- `clear`：使用与 `resume` 相同的清空语义。
- `compact`：保留提示词快照、去重集合和 UI 阅读状态，只在 `session_id` 变化时更新绑定。

四种有效 source 都可以在同一认证运行内按上述规则更新 `session_id` 绑定。重绑后迟到的已知旧会话 prompt 与 `SessionEnd` 事件静默确认并丢弃，避免产生 Hook 失败且不得覆盖当前会话状态；返回曾经绑定过的会话时恢复其当前身份，先前其他会话继续视为旧会话。当前会话进入 `ended` 后，同会话迟到的 prompt 也静默确认并丢弃，只有后续有效 `SessionStart` 可以恢复为 `live`。未知会话事件、空 ID 和未知 source 仍拒绝。任何会话边界的 `transcript_path` 即使存在也不得读取。

## Provider 边界

```go
type UserPrompt struct {
    ID   string
    Text string
}

type Event struct {
    Kind      Kind
    SessionID string
    Source    string
    Prompt    *UserPrompt
}
```

实际实现保持最小。不得为未来 provider 预设动态插件 ABI；新增 provider 时再提取已经真实重复的能力。

## Zellij 管理

- 优先使用 `PATH` 中经过版本检查的 Zellij。
- 缺失时由 `setup` 或首次引导自动下载固定版本，验证 SHA-256，安装到用户级 Prompt Pane 数据目录。
- 不修改 PowerShell Profile、系统 `PATH` 或用户全局 Zellij 配置。
- 临时 layout 使用左 70%、右 30%，左侧初始聚焦，关闭不必要的 Zellij UI。右侧 viewer command pane 不使用 `close_on_exit`，避免初始化、IPC 或 TUI 失败时吞掉错误；启动器通过本次运行的 `PROMPT_PANE_ZELLIJ_PATH` 传递已发现的 Zellij 绝对路径，viewer 收到 `Ctrl+X` 后由命令编排层使用当前 `ZELLIJ_PANE_ID` 定向关闭右侧窗格。关闭失败时保留 pane 和错误，左侧 Codex 不受影响。

## TUI

状态为 `ready`、`live`、`ended` 和 `error`。`ready` 表示 Codex 与 viewer 已启动，但尚未由 `SessionStart` 确认 Hook 链路；Codex 可能要到首条 prompt 才创建逻辑 session。UI 从 IPC 获取内存快照和增量事件，默认跟随最新提示词；用户上滚后暂停，回到底部恢复。鼠标拖选只作用于当前视口已经安全渲染的正文或帮助行，松开左键后由 Bubble Tea 生成 OSC 52 写剪贴板序列，经 Zellij 转发给终端；程序不读取剪贴板，不将选择写入 IPC、日志或持久状态。终端不支持 OSC 52 时由用户使用 `Shift` 终端原生选择作为回退。具体排版和键位以 `docs/ui.md` 为准。

## 安装边界

- 公开用例在开始安装或启动工作区前校验 `windows/amd64`；`setup codex` 与 `doctor` 还要发现一个可运行的 PowerShell 5.1 或 7。Hook 的 `commandWindows` 只使用两者共有的调用运算符、环境变量和原生退出码语法。
- Codex 继续通过 `exec.LookPath` 发现，所有路径通过 `filepath` 和结构化 argv 传递，不假定盘符、用户名、npm 包管理器或无空格路径。任何下载和持久安装之前，对 Prompt Pane 数据目录、Codex 配置目录及 `codex.pp` 目标目录执行临时文件写入探测并立即清理。
- `setup codex` 列出需要处理的受管组件后，提取 `plugins/prompt-pane/` 中内嵌的插件，将当前 Windows 可执行文件复制到插件目录，并生成符合 Codex 目录约定的本地 marketplace；Hook 通过 Codex 提供的 `PLUGIN_ROOT` 和 PowerShell 调用运算符调用该副本，并显式透传原生退出码，不依赖系统 `PATH`，安装仍只使用 Codex 官方 plugin 命令且不手工修改 `config.toml`。安装事务完成后，setup 调用与独立 `doctor` 相同的只读环境检查；检查失败则整体返回失败，只有全部通过才输出启动命令。
- `codex.pp` 与兼容入口 `prompt-pane codex` 在创建运行身份和 IPC 前调用同一安装事务；全部组件就绪时不产生安装输出，缺失或过期时列出并自动修复，验证成功后才继续原始 Codex argv。失败时不会启动 Zellij，也不会修改 Hook 信任状态。
- `setup codex` 还将当前可执行文件复制为与用户 `codex.cmd` 同目录的 `codex.pp.exe`，并在 Prompt Pane 用户数据目录记录不含敏感信息的所有权路径和摘要。已有同名非受管文件或受管文件被外部修改时停止，不覆盖冲突；不修改 `codex.cmd`、PowerShell Profile 或 `PATH`。
- 安装进度由安装用例发布规范化阶段和下载字节数，Bubble Tea 动画只消费进度并渲染；非交互输出使用同一进度事件生成逐行文本。渲染失败不得改变安装事务结果。
- `setup codex` 安装后自动调用、独立 `doctor` 命令也可调用同一只读检查：覆盖平台、PowerShell、程序路径、Codex、插件、`codex.pp`、Hook 信任提示和 Zellij。插件状态优先采用 Codex 官方命令；当目标 Windows 版本错误返回空插件列表时，只回退核对 Prompt Pane 的精确 enabled 配置、当前 cachebuster manifest 和与当前程序一致的缓存二进制哈希，不接受仅有目录或旧缓存的状态。
- `teardown codex` 只移除本项目安装且摘要仍匹配的 `codex.pp`、Codex 插件和本地 marketplace，并在执行删除前确认目标；托管 Zellij 为后续 provider 共用，首版保留。
- 开发测试使用隔离的临时 Codex home；未经确认不修改真实用户 Codex 配置。

## 主要风险

- Hook 漂移：官方字段 + 版本化 fixture + 未知字段兼容、缺失必需字段拒绝。
- 会话切换串线：只允许认证运行内的有效 `SessionStart` 按 source 重绑并决定清空或保留；已知旧会话及当前会话结束后的迟到事件静默丢弃，未知会话事件拒绝。
- 并发串线：随机 endpoint、token、`run_id` 和 session 状态机联合验证。
- prompt 泄露：除用户显式拖选后写入系统剪贴板外，无内容日志、无持久化、无遥测、无网络传输；剪贴板只接收当前视口的安全渲染文字。
- 终端注入：渲染前过滤控制序列，固定尺寸 Unicode 测试。
- Windows 兼容：PowerShell、Zellij 与终端之间的 ConPTY、IME、剪贴板、鼠标、resize 和恢复契约是发布门槛；代表性终端人工验证用于发现实现问题，不按终端品牌逐一设置发布门槛。

## 上游依据

- [Codex Hooks](https://learn.chatgpt.com/docs/hooks)
- [Codex Plugins](https://learn.chatgpt.com/docs/plugins)
- [Zellij layouts](https://zellij.dev/documentation/creating-a-layout.html)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [Zellij Windows support](https://zellij.dev/documentation/faq.html#does-zellij-run-on-windows)
