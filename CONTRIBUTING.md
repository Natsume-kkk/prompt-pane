# Contributing

提交改动前先阅读 `README.md`，再根据改动范围阅读 `docs/product.md`、`docs/architecture.md` 或 `docs/ui.md`。

## 开发原则

- 先建立可复现测试，再修改行为。
- Hook 和 IPC 测试只使用合成且脱敏的数据；不得提交真实会话。
- 不扫描最新文件或按 cwd 猜测会话。
- 不在日志、issue、PR、截图或测试输出中泄露 prompt、token 和 transcript。
- UI 按显示单元格宽度处理 Unicode，并过滤终端控制序列。
- 新依赖必须说明必要性、维护状态、许可证和 Windows 支持。

## 本地检查

```powershell
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
.\scripts\build.cmd
```

构建统一通过 `scripts\build.cmd` 执行，不直接维护另一套输出路径或 Go 缓存参数。

涉及布局、输入、TUI 或终端恢复时，还必须按 `docs/product.md` 和 `docs/ui.md` 在目标环境完成相应人工验收。

## Commit

使用中文描述和 `init:`、`feat:`、`fix:`、`refactor:`、`docs:`、`chore:` 前缀。说明具体改动、原因和影响范围。
