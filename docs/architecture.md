# Prompt Pane 架构规格

## 架构结论

Prompt Pane 是独立 Go CLI。Zellij 只负责终端内 70/30 布局，Bubble Tea 只负责右侧 TUI，Codex 官方 Hooks 是实时提示词事实源。程序不调用终端品牌专有 API；能够正常运行受支持 Zellij 的终端均在支持边界内。

Prompt Pane 不搜索 `~/.codex/sessions`、不读取 Codex App Server 历史，也不按时间、目录或进程猜测会话。新主会话、恢复会话和清空后的会话都只显示本次运行期间 `UserPromptSubmit` Hook 实际收到的新提示词；上下文压缩保留本次运行已经显示的记录。`/side`／`/btw` 的父提示词与指标只在当前进程内暂存，返回父会话时恢复，侧聊内容随覆盖层结束丢弃。`Stop` Hook 可以按官方提供的准确 `session_id` 与 `transcript_path` 只读解析当前会话的结构化 usage 元数据。由于 Codex 0.149.0 在用户中断时不会运行 `Stop`，`UserPromptSubmit` 还可以为当前准确 `session_id + turn_id` 启动一个短期观察器：观察器同时等待认证 IPC 的精确轮次释放信号，并从 Hook 到达时的 transcript 文件末尾开始只识别后续结构化 `turn_complete`／`turn_aborted` 类型、`turn_id` 与中止原因；任一路径确认该观察已失效后立即退出。两条路径都不得解析或保留 prompt、回复、推理和工具内容，不得扫描目录、回退到最近文件或建立历史索引。

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

`PromptPane\bin\prompt-pane.exe` 与 `codex.pp.exe` 是稳定受管入口。入口根据安装状态选择当前版本化运行程序；`codex.pp.exe` 同时根据自身启动文件名进入 `codex` 用例，所有后续参数仍保持为 argv。入口不替换 `codex.cmd`，实际运行版本继续由 `exec.LookPath("codex")` 发现 Codex 子进程。

1. 稳定入口尝试激活等待版本，再让选定的当前版本校验 Codex、插件和 Zellij；入口在同一更新门内确认版本未变化并登记本次工作区活动。
2. 当前运行版本创建 `run_id`、一次性 token 与当前用户 IPC endpoint。
3. Windows 稳定入口已在任何环境检查、自动修复或外部命令之前进入启用 `KILL_ON_JOB_CLOSE` 的 Job Object；选定版本及其后代继承该约束，完成就绪检查后生成 Zellij layout并前台启动 session，本次会话同时显式使用 `on_force_close=quit`。
4. 左侧运行 `prompt-pane _agent codex -- <codex-args>`，由其原样转发 Codex argv；会话选择与恢复由 Codex 自身处理。
5. 右侧运行 `prompt-pane _view`。
6. Codex Hook 运行 `prompt-pane _hook codex`，从环境取得 endpoint 与 token，从标准输入取得官方事件 JSON。
7. `UserPromptSubmit` 成功后按需运行 `prompt-pane _observe codex <session-id> <turn-id> <offset> <transcript-path>`，只观察该轮后续生命周期。
8. Zellij 退出后只清理本次运行的非敏感临时状态。

Codex 参数保留为 argv，不经过 Shell 字符串。`run_id`、认证 token 与 endpoint 只通过进程环境传递，不写运行清单或日志。

PowerShell、Git、Codex 插件管理和 Zellij 探测／关闭等短时外部命令统一使用有截止时间和输出上限的执行边界；前台 Codex 与 Zellij 属于运行期长进程，由会话生命周期和 Windows Job Object 管理，不套用短命令截止时间。

## 运行身份与状态

状态机：

```text
created -> workspace_started -> session_bound -> live -> ended
                    \-> failed
```

可落盘的短生命周期状态只允许包含 schema version、`run_id`、provider、创建时间、工作目录、结构化 argv、进程标识、生命周期状态和安全错误码。禁止包含 prompt、token 或原始 Hook JSON。

## IPC 协议

Windows `v1.2.0` 使用当前用户命名管道；其他平台接口保留但不承诺可用。每次运行使用不可预测 endpoint 和至少 256-bit token。Hook 原始输入上限为 1 MiB，认证 IPC frame 上限为 8 MiB，为 JSON 转义保留空间；超限只返回不含 payload 的安全错误。

每个请求为有上限的 JSON frame：

```text
version | run_id | token | type | event? | watch?
```

`type` 只允许 `event`、`subscribe` 或内部 `watch_turn`；事件请求携带规范化 `event`，轮次观察请求的 `watch` 只携带准确 `session_id + turn_id`，不得携带 prompt、transcript 路径或原始记录。事件请求返回 `ok`；订阅连接持续接收由 `state`、`prompts`、`notice`、可空 `active_turn_id`、可空 `active_prompt_id` 和指标组成的内存快照；轮次观察连接先接收 `watching` 确认，再在该观察器应退出时接收 `release`。`active_turn_id` 是 provider 轮次关联键，`active_prompt_id` 是 viewer 最新提示词的进程内唯一键，两者不得混用。

事件最小集合：

- `session.started`：规范化 `session_id`、`source` 与只表示当前 transcript 是否缺失的 `ephemeral` 标记；不得把路径本身送入 IPC。
- `prompt.submitted`：`session_id`、`turn_id`、完整 prompt。
- `turn.completed`：`session_id`、`turn_id` 与可空的结构化指标；完成信号不依赖指标是否可用。
- `session.ended`：`session_id`。

server 必须验证版本、大小、token、`run_id`、事件字段和会话绑定。认证失败、超限或状态转换非法时拒绝消息，错误不得包含 payload。

## Codex Hook

插件包含 `SessionStart`、`UserPromptSubmit`、`Stop` 和 `SessionEnd` command hooks。Hook 进程必须：

1. 从标准输入读取单个有界 JSON 对象。
2. 验证 `hook_event_name`、`session_id` 和事件特有字段。
3. 从继承环境读取本次 endpoint、`run_id` 与 token。
4. 通过 IPC 发送规范化事件。
5. 成功时不向 stdout 写 prompt 或额外上下文。

`Stop` Hook 必须验证官方提供的 `turn_id`，并始终发送 `turn.completed`。它只按本次 payload 的准确 `transcript_path` 逐行筛选 `session_meta`、`turn_context` 和 `token_count`；不提供最近文件回退，不把路径、原始行或内容字段送入 IPC。指标适配器同时接受旧版单额度桶和新版按 `limit_id` 分组的多额度桶：多桶结构固定选择键为 `codex` 的默认桶，即使兼容 `rate_limits` 同时携带完整的具名特殊桶也不得覆盖；旧版单桶只在自身 `limit_id == "codex"` 且至少包含一个窗口时接受。同一准确 transcript 内，后续缺少额度或只含具名特殊桶的 `token_count` 不得抹掉最近一条有效默认桶快照；后续有效默认桶完整替换旧快照。额度快照只属于当前主会话，不通过 IPC、磁盘缓存或进程间共享传播到其他会话或工作区。默认桶缺失时不得按模型名称、目录、唯一剩余桶或最近使用情况猜测，必须把额度标记为不可用。额度窗口按上游分钟数规范化并排序，不固定假设 `primary` 为 5h 或 `secondary` 为 7d；到达 `resets_at` 且没有更新快照的窗口必须省略，不得从过期值推断新周期为 `0%`。额度缺失或结构不受支持不得丢弃同一记录中已经验证的 token、上下文、模型和 Git 指标。`transcript_path` 为空或整体指标解析失败时只省略指标，仍发送完成事件并保持 Codex 可用。

规范化 `SessionMetrics` 使用通用额度窗口列表和 `unknown`／`available`／`unavailable` 可用状态，不向 IPC 发送 Codex 原始 `limit_id`、额度桶名称、路径、原始 JSON 或解析错误。当前运行不为额度查询启动 Codex App Server；其账户接口只用于受控开发验证，避免引入实验性长生命周期依赖和跨会话账户数据源。

`UserPromptSubmit` Hook 必须把官方 `turn_id` 放入事件级轮次字段，每次 Hook 调用都发送一条 `prompt.submitted`；同一轮的排队／转向输入可以复用 `turn_id`，server 不得据此去重。事件发送成功后，可以把准确 transcript 路径、提交时文件偏移、`session_id` 和 `turn_id` 交给同一可执行文件的短期内部观察进程。观察进程必须先通过认证 IPC 注册准确 `session_id + turn_id`；server 对同一键只保留一个有效观察器，重复进程立即退出。准确 `Stop` 到达、同会话下一条不同 `turn_id` 的提示词到达、会话边界重置或本次 server 关闭时，server 必须释放对应观察器；同一 `turn_id` 的排队／转向输入不得释放当前观察器。即使释放信号早于观察进程注册，server 也必须根据当前活动轮次让迟到观察器立即退出。

观察进程与 IPC 释放通道并行读取 transcript：先用 `session_meta.id` 验证路径归属，只读取提交偏移之后追加记录的外层生命周期字段；准确匹配的 `turn_aborted` 立即规范化为 `turn.completed`，`turn_complete` 仅作为 `Stop` Hook 未能送达时的延迟兜底。错会话、错轮次、未知类型、内容字段和旧记录全部忽略；IPC 注册失败时仍允许 transcript 兜底，transcript 打开或格式识别失败时仍等待认证 IPC 释放。`_hook` 与 `_observe` 是现有工作区的内部叶子进程，只继承公开入口已经建立的 Windows Job Object，不得各自创建会在 Hook 返回时连带终止观察器的嵌套 `KILL_ON_JOB_CLOSE` Job；观察器在单轮终止、被下一轮替换或工作区退出后结束。启动、解析或 IPC 失败不得阻塞 Codex，也不得输出 transcript 路径或原始记录。

Codex 0.149.0 的输入路径按官方实现归一化：普通用户输入和每条排队／转向输入都会经过 `UserPromptSubmit`；`/plan <请求>` 与 `/side <请求>`／`/btw <请求>` 最终提交请求正文；`/goal` 系列走目标控制面，不经过该 Hook。Prompt Pane 只显示前一类官方事件，不从本地命令历史、目标状态或 transcript 补造控制命令。

Hook 故障不得阻止用户 prompt。非托管 Hook 需要 Codex 信任；程序只能引导用户通过 `/hooks` 审查并信任 Hook，禁止绕过信任。

Codex 会对所有会话加载已安装插件。Hook 只有在三个 Prompt Pane 运行环境变量全部存在时才连接 IPC；三者全无时静默以退出码 0 返回，部分缺失则视为配置错误，避免普通 Codex 会话出现无关告警。

## SessionStart 会话边界

`codex.pp resume` 的参数原样交给 Codex，Prompt Pane 不提前选择或读取会话。Hook 订阅并验证官方 `SessionStart` source：

- `startup`：建立首次绑定；相同 `session_id` 的重复事件保持当前快照。adapter 只检查 `transcript_path` 是否为空并立即丢弃路径：Codex 0.149.0 的 `/side`／`/btw` ephemeral fork 为无 transcript startup，因此不同 `session_id`、`ephemeral=true` 且当前主会话仍为 `live` 时建立临时覆盖层并暂存父提示词与指标；带 transcript 的不同 ID startup 属于 `/new`、普通 `/fork` 等持久主会话，无论旧状态是否 live 都原子清空并重新绑定。
- `resume`：无论 `session_id` 是否变化，都原子清空提示词快照和 UI 阅读状态，并显示只接收后续新提示词的内存提示。
- `clear`：使用与 `resume` 相同的清空语义。
- `compact`：保留提示词快照和 UI 阅读状态，只在 `session_id` 变化时更新绑定。

四种有效 source 都可以在同一认证运行内按上述规则更新 `session_id` 绑定。临时覆盖层只显示自身 prompt，但快照中的指标继续复制父会话最后有效值；覆盖层自身 `turn.completed` 的指标静默丢弃。每条 `prompt.submitted` 都分配新的进程内 `prompt_id` 并按序追加，同时把事件级 `turn_id` 和最新 `prompt_id` 分别设为 active；同一 `turn_id` 的后续提交不得覆盖或合并已有正文，只把等待动效移到最新提示词。只有同一会话且准确匹配 `active_turn_id` 的 `turn.completed` 才能同时清空两个 active 字段，迟到旧轮次只静默确认。父会话在覆盖期间到达的匹配完成事件只更新暂存父状态，不改变侧聊正文。覆盖层收到 `SessionEnd` 时立即恢复父快照；Codex 未发送退出事件时，父 `session_id` 的下一条 `prompt.submitted` 是唯一恢复信号，server 原子恢复父提示词、追加该 prompt，并销毁覆盖层内容。父会话其他迟到事件和普通已知旧会话事件继续静默确认并丢弃，不得覆盖当前状态。`resume`、`clear` 与持久新 `startup` 会丢弃任何暂存父快照并建立新的主绑定。当前主会话进入 `ended` 后，同会话迟到的 prompt 也静默确认并丢弃，只有后续有效 `SessionStart` 可以恢复为 `live`。未知会话事件、空 ID 和未知 source 仍拒绝。任何会话边界的 `transcript_path` 即使存在也不得读取、保留或送入 IPC，adapter 只允许使用其空／非空状态生成 `ephemeral` 布尔值。

## Provider 边界

```go
type UserPrompt struct {
    ID   string
    Text string
}

type Event struct {
    Kind      Kind
    SessionID string
    TurnID    string
    Source    string
    Ephemeral bool
    Prompt    *UserPrompt
    Metrics   *SessionMetrics
}
```

实际实现保持最小。不得为未来 provider 预设动态插件 ABI；新增 provider 时再提取已经真实重复的能力。

## Zellij 管理

- 优先使用 `PATH` 中经过版本检查的 Zellij。
- 缺失时由 `setup` 或首次引导自动下载固定版本，验证 SHA-256，安装到用户级 Prompt Pane 数据目录。
- 不修改 PowerShell Profile、系统 `PATH` 或用户全局 Zellij 配置。
- 临时 layout 使用左 70%、右 30%，左侧初始聚焦并保持原有标题，右侧固定标题为 `PROMPTS`。layout 在非 locked 模式覆盖 `Alt+P` 为单一 `ToggleFocusFullscreen` 动作，使用户聚焦左侧时隐藏／恢复右侧；不组合无顺序保证的多动作，也不关闭或重建 viewer。启动 argv 仅为本次会话覆盖 `mouse_hover_effects=false` 与 `mouse_click_through=true`：前者隐藏窗格边框悬停高亮和 resize 帮助文字，同时保留 `advanced_mouse_actions`、窗格边框与鼠标 resize；后者让首次聚焦点击同时送达目标窗格，使 viewer 可直接开始拖选。两项覆盖适用于 Prompt Pane 创建的整个 Zellij 会话，因此左侧 Codex 的首次聚焦点击也会送达 Codex；程序不启用经过即切换键盘焦点的 `focus_follows_mouse`，不接管 Zellij 主题，也不修改用户全局 Zellij 配置。右侧 viewer command pane 不使用 `close_on_exit`，避免初始化、IPC 或 TUI 失败时吞掉错误；启动器通过本次运行的 `PROMPT_PANE_ZELLIJ_PATH` 传递已发现的 Zellij 绝对路径，viewer 收到 `Ctrl+X` 后由命令编排层使用当前 `ZELLIJ_PANE_ID` 定向关闭右侧窗格。关闭失败时保留 pane 和错误，左侧 Codex 不受影响。
- Prompt Pane 创建的会话同时覆盖 `on_force_close=quit`，用于 Zellij 能收到关闭信号的正常路径。Windows 公开程序入口持有启用 `KILL_ON_JOB_CLOSE` 的匿名 Job Object，环境检查、安装、Zellij、Codex、viewer 及其后代继承同一进程树约束；Hook 和轮次观察器等内部叶子进程沿用该约束，不重复取得短命 Job 所有权。终端直接销毁入口时，操作系统随 Job 句柄关闭结束本次运行的全部子进程。Job 不按进程名或会话名扫描，不修改用户全局配置，也不影响其他 Prompt Pane 或 Zellij 会话。

## 安装事务与活动工作区

- 用户级安装由两个角色组成：`PromptPane\bin\prompt-pane.exe` 与 `codex.pp.exe` 是稳定入口，`PromptPane\versions\<sha256>\prompt-pane.exe` 是不可变运行版本。两类文件都由同一个发布可执行文件复制得到；稳定入口只读取 schema 1 安装状态、调用受管版本并原样转发 argv，不加载 prompt 或会话数据。安装状态只保存版本号、内容摘要以及 `current`、`pending`、`previous` 三个引用，引用必须是 64 位十六进制摘要且只能解析到受管版本目录。
- 每个工作区持有当前用户范围的共享活动锁。显式升级可以在共享锁存在时校验并写入新的不可变版本，再原子更新 `pending`；不得修改 `current`、插件或稳定入口。旧工作区活动期间的其他启动继续解析 `current`，因此同一时刻所有工作区和全局 Codex 插件始终属于同一版本。
- 稳定入口在启动 Codex 前先让 `pending` 版本尝试取得更新门与独占活动锁。没有活动工作区时，等待版本更新 Codex 插件、稳定入口所有权和安装状态，完整验证后把原 `current` 移入 `previous` 并提交新 `current`；存在活动工作区时保持 `pending` 不变并启动原 `current`。稳定入口在组件检查与新工作区登记之间继续使用同一更新门协调，禁止检查后切换的竞态。
- 激活先完成全部路径、权限、冲突和文件占用预检，再暂存受影响组件；Codex 插件、稳定入口、所有权状态和版本指针属于一个事务。稳定入口、快捷入口与安装状态的原子替换和回滚快照共用同一受管文件事务实现。任一步失败时按相反顺序恢复旧组件与旧安装状态并重新验证；回滚成功后可继续启动旧 `current`，回滚失败必须同时报告原始失败与恢复失败，不得声称环境可用。
- 成功激活保留原 `current` 为 `previous`。下一次成功暂存不同版本时清除旧 `previous` 和被替代的 `pending` 引用，并尽力删除未被状态引用的合法版本目录；占用、杀毒软件或权限导致的删除失败只留下无引用残留，不能使升级失败。首次从没有 schema 1 安装状态的单版本布局迁移时，因旧 `codex.pp.exe` 不具备转发能力，必须在任何稳定入口、插件或所有权状态修改前取得独占活动锁，因而只要求这一次先关闭旧工作区。

## TUI

状态为 `ready`、`live`、`ended` 和 `error`。`ready` 表示 Codex 与 viewer 已启动，但尚未由 `SessionStart` 确认 Hook 链路；Codex 可能要到首条 prompt 才创建逻辑 session。UI 从 IPC 获取内存快照和增量事件，默认跟随最新提示词；用户上滚后暂停，回到底部恢复。Bubble Tea 返回实际终端背景颜色时，viewer 和 setup UI 只在进程内保存其 RGB，用于语义文字和文字选区的对比度降级，不写入配置或状态。鼠标拖选只作用于当前视口已经安全渲染的正文或帮助行，松开左键后由 Bubble Tea 生成 OSC 52 写剪贴板序列，经 Zellij 转发给终端；程序不读取剪贴板，不将选择写入 IPC、日志或持久状态。终端不支持 OSC 52 时由用户使用 `Shift` 终端原生选择作为回退。具体排版和键位以 `docs/ui.md` 为准。

## 安装边界

- `scripts/install.ps1` 是 GitHub 分发引导层，不复制 Go 安装事务。它在 PowerShell 5.1／7 中解析目标版本，通过 GitHub Release 固定下载地址获取 Windows x64 可执行文件与 SHA-256，在临时目录校验后直接以绝对路径调用该已验证程序的 `setup codex`。版本暂存、稳定入口、激活和回滚全部由 Go 安装事务拥有。脚本不调用 GitHub API、不安装 Codex、不修改 `PATH`、Profile、执行策略或系统配置；失败时不得替换已有可用程序。
- 引导层的 `Invoke-WebRequest` 使用 PowerShell 当前代理与证书配置，SHA-256 校验直接使用 .NET 加密 API，避免 PowerShell 7 启动 Windows PowerShell 5.1 时继承不兼容模块路径。受管 Zellij 继续由 Go `http.DefaultClient` 下载，只读取 `HTTP_PROXY`／`HTTPS_PROXY`／`NO_PROXY`，不把 Windows 系统代理自动转换为环境变量；`PATH` 中已有准确版本 Zellij 时跳过该下载。两条链路的错误都必须保留组件和阶段语义：主程序与摘要下载指出 Release 资产和 GitHub／代理检查方向，Zellij 请求错误指出 GitHub 可达性及 `HTTPS_PROXY` 或预安装准确版本的处理方向，HTTP 非 200、摘要不匹配、大小超限、写入和替换失败继续使用各自精确错误。
- 默认安装根目录与 `PROMPT_PANE_HOME` 使用同一解析规则；脚本支持显式版本用于复现和诊断，但不直接写安装状态或负责回滚。已经校验的程序负责把自身写入摘要命名的版本目录，并由 Go 事务统一拥有稳定入口、Codex 插件、`codex.pp`、版本状态和 Zellij。
- 公开用例在开始安装或启动工作区前校验 `windows/amd64`；`setup codex` 与 `doctor` 还要发现一个可运行的 PowerShell 5.1 或 7。Hook 的 `commandWindows` 只使用两者共有的调用运算符、环境变量和原生退出码语法。
- Codex 继续通过 `exec.LookPath` 发现，所有路径通过 `filepath` 和结构化 argv 传递，不假定盘符、用户名、npm 包管理器或无空格路径。任何下载和持久安装之前，对 Prompt Pane 数据目录、Codex 配置目录及 `codex.pp` 目标目录执行临时文件写入探测并立即清理。
- `setup codex` 列出需要处理的受管组件后，提取 `plugins/prompt-pane/` 中内嵌的插件，将当前 Windows 可执行文件复制到插件目录，并生成符合 Codex 目录约定的本地 marketplace；Hook 通过 Codex 提供的 `PLUGIN_ROOT` 和 PowerShell 调用运算符调用该副本，并显式透传原生退出码，不依赖系统 `PATH`，安装仍只使用 Codex 官方 plugin 命令且不手工修改 `config.toml`。安装事务完成后，setup 调用与独立 `doctor` 相同的只读环境检查；检查失败则整体返回失败，只有全部通过才输出启动命令。
- `codex.pp` 与兼容入口 `prompt-pane codex` 在创建运行身份和 IPC 前调用同一安装事务；全部组件就绪时不产生安装输出，缺失或过期时列出并自动修复，验证成功后才继续原始 Codex argv。失败时不会启动 Zellij，也不会修改 Hook 信任状态。
- 首次 `setup codex` 将当前已验证程序同时写入版本目录、稳定 `prompt-pane.exe` 与用户 `codex.cmd` 同目录的 `codex.pp.exe`，并记录不含敏感信息的入口所有权路径、入口摘要和版本状态。后续版本变化不要求替换稳定入口；已有同名非受管文件或受管文件被外部修改时停止，不覆盖冲突。稳定入口不修改 `codex.cmd`、PowerShell Profile 或 `PATH`。
- 安装进度由安装用例发布规范化阶段和下载字节数，Bubble Tea 动画只消费进度并渲染；非交互输出使用同一进度事件生成逐行文本。渲染失败不得改变安装事务结果。
- 安装错误沿用同一输出通道，在动画或逐行进度结束后显示失败组件、阶段、原因和单一处理建议；不得包含下载内容、凭据、代理认证信息或本机无关状态。引导脚本尚未开始执行时发生的 GitHub Raw 下载错误只能由调用方显示，公开 README 必须提供对应说明。
- `setup codex` 完成立即激活时自动调用、独立 `doctor` 命令也可调用同一只读检查：覆盖平台、PowerShell、当前运行版本、等待版本、Codex、插件、稳定入口、Hook 信任提示和 Zellij。运行中升级只验证等待版本的摘要与安装状态，完整环境检查推迟到实际激活事务。插件状态优先采用 Codex 官方命令；当目标 Windows 版本错误返回空插件列表时，只回退核对 Prompt Pane 的精确 enabled 配置、当前 cachebuster manifest 和与安装状态中当前版本一致的缓存二进制哈希，不接受仅有目录或旧缓存的状态。`doctor` 即使由等待版本或工作树二进制启动，也不得改用调用者自身摘要核对当前插件；等待版本在同一检查中只验证不可变版本文件和摘要。
- `teardown codex` 只移除本项目安装且摘要仍匹配的 `codex.pp`、Codex 插件和本地 marketplace，并在执行删除前确认目标；托管 Zellij 为后续 provider 共用，首版保留。
- 开发测试使用隔离的临时 Codex home；未经确认不修改真实用户 Codex 配置。

## 主要风险

- Hook 漂移：官方字段 + 版本化 fixture + 未知字段兼容、缺失必需字段拒绝。
- 会话切换串线：只允许认证运行内的有效 `SessionStart` 建立主绑定或临时覆盖层；只有暂存父 `session_id` 的新 `UserPromptSubmit` 可以从覆盖层恢复父快照，其他旧会话及当前主会话结束后的迟到事件静默丢弃，未知会话事件拒绝。
- 并发串线：随机 endpoint、token、`run_id` 和 session 状态机联合验证。
- prompt 泄露：除用户显式拖选后写入系统剪贴板外，无内容日志、无持久化、无遥测、无网络传输；剪贴板只接收当前视口的安全渲染文字。
- 终端注入：渲染前过滤控制序列，固定尺寸 Unicode 测试。
- Windows 兼容：PowerShell、Zellij 与终端之间的 ConPTY、IME、剪贴板、鼠标、resize 和恢复契约是发布门槛；代表性终端人工验证用于发现实现问题，不按终端品牌逐一设置发布门槛。

## 上游依据

- [Codex Hooks](https://learn.chatgpt.com/docs/hooks)
- [Codex Plugins](https://developers.openai.com/plugins/build/plugins)
- [Zellij layouts](https://zellij.dev/documentation/creating-a-layout.html)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [Zellij Windows support](https://zellij.dev/documentation/faq.html#does-zellij-run-on-windows)
