# Security Policy

## 支持范围

当前维护范围为 `v1.0.0` 的 Windows x64、PowerShell、Zellij 和 Codex CLI 组合。目前尚未提供公开预编译安装包；其他平台和 AI provider 不在支持范围内。

## 报告问题

不要在公开 issue 中粘贴真实 prompt、transcript、Hook 原始输入、IPC token、用户名、绝对路径或其他敏感信息。优先使用仓库 Security 页面中的 GitHub Private Vulnerability Reporting；如果页面没有 `Report a vulnerability` 入口，只能在不包含技术细节和敏感数据的 issue 中请求私密联系方式。

## 数据处理承诺

Prompt Pane 的 prompt 只允许出现在 Codex Hook 标准输入、本地认证 IPC、进程内存和终端画面中；用户在 viewer 中显式拖选并松开左键时，当前视口内所选的安全渲染文字还会发送到系统剪贴板。程序不读取剪贴板，拖选不得包含视口外、折叠或旧会话内容。prompt 不得写入日志、配置、运行清单、缓存、遥测、测试 fixture 或网络请求。

任何会话边界都不得读取 App Server、Hook `transcript_path` 或 Codex 会话目录。认证运行内有效的 `SessionStart` 可以按官方 source 更新会话绑定：不同 ID 的 `startup` 以及 `resume`／`clear` 清空内存提示词，`compact` 只保留本次运行已接收的记录。切换后静默丢弃已知旧会话的迟到事件；当前会话结束后，迟到提示词也必须静默丢弃，直到新的有效 `SessionStart` 到达。未知会话事件、空 ID 和未知 source 仍拒绝。渲染前必须过滤终端控制序列，避免 prompt 操纵终端界面。

`codex.pp` 的安装状态只记录受管快捷入口的绝对路径和可执行文件 SHA-256，不记录 prompt、Codex 参数或会话信息。安装不得覆盖同名非受管文件；卸载前必须再次验证路径、文件名和安装摘要，受管文件被外部修改时拒绝删除。

违反内容不落盘、会话隔离、IPC 认证或终端注入边界的问题视为高优先级安全问题。
