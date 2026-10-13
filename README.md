# env-warden

> 给 Windows 环境变量做体检、开处方、留病历的"家庭医生"。

Windows 环境变量管理工具，核心是 PATH 的体检、诊断与安全清理。形态为 Go 单二进制 `envwarden`：CLI 与 Wails 桌面 GUI 共享同一个纯 Go 核心包。

## 状态

项目奠基阶段。子命令规划：

| 子命令 | 功能 |
|---|---|
| `check` | PATH 体检：死条目、重复、命令解析、npm prefix 可达性 |
| `backup` | 导出两级 Environment 键为 .reg 备份（还原 = 双击） |
| `clean` | 精确清理死条目（变更前强制自动备份） |
| `diff` | 对比两次体检快照 |

## 构建

```bash
go build -o bin/envwarden.exe ./cmd/envwarden
go test ./...
```

要求 Go ≥ 1.26，Windows。

## 目录

- `core/` — 纯 Go 核心：registry / detect / backup / mutate / broadcast / snapshot
- `cmd/envwarden/` — CLI 入口
- `internal/cli/` — 子命令实现
- `gui/` — Wails 桌面应用（M4）
- `testdata/` — 真实 .reg 测试夹具

开发规范见 [AGENTS.md](AGENTS.md)。
