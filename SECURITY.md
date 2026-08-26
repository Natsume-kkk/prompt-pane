# Security Policy

## 支持范围

当前维护范围为 `v1.2.0` 的 Windows x64、PowerShell、Zellij 和 Codex CLI 组合。官方预编译文件只通过本仓库的 [GitHub Releases](https://github.com/Natsume-kkk/prompt-pane/releases) 提供；安装脚本必须下载同一标签下的可执行文件与摘要，并在替换现有程序前验证 SHA-256。其他平台和 AI provider 不在支持范围内。

## 报告问题

不要在公开 issue 中粘贴真实 prompt、transcript、Hook 原始输入、IPC token、用户名、绝对路径或其他敏感信息。优先使用仓库 Security 页面中的 GitHub Private Vulnerability Reporting；如果页面没有 `Report a vulnerability` 入口，只能在不包含技术细节和敏感数据的 issue 中请求私密联系方式。

## 数据处理承诺

Prompt Pane 的 prompt 只允许出现在 Codex Hook 标准输入、本地认证 IPC、进程内存和终端画面中；用户在 viewer 中显式拖选并松开左键时，当前视口内所选的安全渲染文字还会发送到系统剪贴板。程序不读取剪贴板，拖选不得包含视口外、折叠或旧会话内容。prompt 不得写入日志、配置、运行清单、缓存、遥测、测试 fixture 或网络请求。

任何会话边界都不得读取 App Server 或搜索 Codex 会话目录。`Stop` Hook 只允许按本次 Hook 提供的准确 `session_id` 与 `transcript_path` 逐行筛选当前会话的结构化 usage 元数据；路径为空时静默跳过。为识别用户中断，`UserPromptSubmit` 可以从同一准确 transcript 的当时文件末尾启动短期观察器；观察器只接受匹配 `session_id + turn_id` 的后续结构化 `turn_complete`／`turn_aborted` 类型与中止原因。观察器还可以通过本次运行的认证 IPC 只注册同一准确 `session_id + turn_id`；该通道不得携带 transcript 路径、prompt 或原始记录，并在准确 `Stop`、下一不同轮次、会话边界或工作区 server 关闭时释放观察器。同一键只保留一个有效观察器，重复观察进程立即退出。两条终止路径中的任一条生效后观察器都必须退出；两条路径都不得解析、传输或保留 prompt、回答、推理、工具内容或原始行，不得扫描目录、回退到最近会话文件或建立历史索引。准确 transcript 路径只允许作为 Hook／观察器的短期本地输入，不得进入 IPC、日志、错误、缓存或配置。

认证运行内有效的 `SessionStart` 可以按官方 source 更新会话绑定：主会话结束后的不同 ID `startup` 以及 `resume`／`clear` 清空内存提示词和指标，`compact` 只保留本次运行已接收的记录；主会话 live 时的不同 ID `startup` 只建立进程内临时覆盖层，父提示词和指标暂存到返回父会话或本次进程结束，侧聊指标不得覆盖父指标。只有暂存父 `session_id` 的新 `UserPromptSubmit` 可以恢复父快照，其他已知旧会话迟到事件继续静默丢弃；当前主会话结束后，迟到提示词和指标也必须静默丢弃，直到新的有效 `SessionStart` 到达。未知会话事件、空 ID 和未知 source 仍拒绝。渲染前必须过滤终端控制序列，避免 prompt 操纵终端界面。

`codex.pp` 的安装状态只记录受管快捷入口的绝对路径和可执行文件 SHA-256，不记录 prompt、Codex 参数或会话信息。安装不得覆盖同名非受管文件；卸载前必须再次验证路径、文件名和安装摘要，受管文件被外部修改时拒绝删除。

违反内容不落盘、会话隔离、IPC 认证或终端注入边界的问题视为高优先级安全问题。
