# AGENTS.md

## Project overview

env-warden 是 Windows 环境变量管理工具，核心是 PATH 的体检、诊断与安全清理——把一次真实的多工具链冲突事故（旧 npx 抢占命令名、npm prefix 失配、13 条死路径）的人工排查全流程产品化。形态为 Go 单二进制：`envwarden` CLI + Wails 桌面 GUI，两者共享同一个纯 Go 核心包。

## Setup commands

- Install: `scoop install go`（Go ≥ 1.26，Windows）
- Build: `go build ./...`（编译检查；此产物的 `gui` 子命令只会弹 Wails tag 提示框）
- Build 发布二进制（CLI + GUI 可用）: `go build -tags production -o bin/envwarden.exe ./cmd/envwarden`
- Test: `go test ./...`
- Vet: `go vet ./...`
- 格式检查：`gofmt -l .`（必须无输出）
- 依赖变更后：`go mod tidy`

第三方依赖原则：优先标准库；`golang.org/x/sys`（注册表 / Win32 API）与 `github.com/wailsapp/wails/v2`（GUI，交接文档已定决策）是预期内的主要第三方依赖，新增依赖须说明理由。CLI 参数解析用标准库 `flag` 子命令风格，不引入 cobra 之类的框架。GUI 前端为无构建步骤的原生 HTML/JS/CSS（内嵌于 `gui/frontend/dist`），不引入 node 工具链。

## Code style

- 所有代码经 `gofmt`/`goimports` 格式化。
- 所有导出标识符（包 / 类型 / 函数 / 方法 / 常量）必须有 Go doc 注释，且以标识符名开头（如 `// ReadPath ...`）；注释只写代码表达不了的"为什么"与约束，不复述实现。
- 错误处理：core 包对外错误用包级哨兵 / 自定义错误类型，包装一律 `%w`，调用方用 `errors.Is`/`errors.As`；禁止吞错（`_ = err`）。
- core 包禁止 panic 逃生，错误一律返回。
- 单元测试用表驱动风格（table-driven tests）。
- 命名与语言：标识符、错误消息用英文（错误消息小写开头）；代码注释、CLI 用户可见输出用中文。

## Architecture

`core/` 是纯 Go 核心包，禁止依赖任何 GUI / CLI 框架，必须可独立单测；`cmd/envwarden`（CLI）与 `gui/`（Wails）都是薄壳，只做参数解析与结果渲染。这样分层的原因：检测与变更逻辑是产品价值所在，壳必须可替换。

核心数据流：

- 体检链：`registry`（读两级 PATH，新终端视角）→ `detect`（死条目 / 重复 / 命令解析 / npm prefix）→ 报告。
- 变更链（提交流水线在 `internal/ops`，CLI 与 GUI 共用，顺序固定：复检 → 强制备份 → 类型保持写入 → 写后校验 → 广播）：`mutate`（精确过滤 / 排列校验的重排）→ 写回前强制 `backup`（.reg 导出）→ `registry` 类型保持写入 → `broadcast`（WM_SETTINGCHANGE）。`restore` 从 .reg 备份整键还原（值类型以备份为准）。
- `snapshot` 保存体检快照，供 `diff` 对比"上次体检以来 PATH 变了什么"。

## Hard constraints

（源自真实事故，违反任何一条即为 bug，不可协商）

1. 写回注册表必须保持原值类型：REG_SZ 与 REG_EXPAND_SZ 禁止互换，否则 `%VAR%` 展开损坏。
2. 禁止使用 `setx` 写 PATH——1024 字符静默截断。
3. 任何变更操作前强制自动备份为 .reg，备份不可跳过。
4. 只精确匹配删除用户明确声明的条目，其余逐字保留；永不整段重写 PATH。
5. HKLM 写入需要 UAC 提权；未提权时明确报错，不得静默失败或降级。
6. 编码：注册表 API 返回 UTF-16，进程内部一律 UTF-8；导出 .reg 遵循 regedit 格式（UTF-16LE + BOM）。
7. 判断"新终端视角"必须读注册表，绝不使用当前进程的 PATH 环境变量——旧终端持有旧值。
8. 命令解析按 Windows 实际顺序：系统 PATH 拼在用户 PATH 之前，同名命令前者胜出。

## Security considerations

- 本工具能写 HKCU/HKLM 的 Environment 键，等价于持久修改系统配置：变更范围必须仅限用户明确声明的条目（见 Hard constraints 3、4）。
- HKLM 写入走 UAC 提权流程；日常只读操作不得要求管理员权限。
- 纯本地工具：不联网、不收集不上传任何数据，依赖树保持最小。
- .reg 备份含用户真实路径信息，不得进入版本库；`testdata/` 下作为测试夹具的真实导出除外（仓库若开源需先脱敏）。

## Known gotchas

- PowerShell 5.1 按 GBK 解析 UTF-8 无 BOM 脚本会乱码（历史事故成因之一）；Go 侧写任何供 PowerShell 消费的文件时注意 BOM 策略。
- `WM_SETTINGCHANGE` 广播后，已开着的旧终端仍持有旧 PATH，验证必须开新终端。
- 用户 PATH 远超 1024 字符，任何"顺手 setx"都会静默截断。
- 中文路径（如 `F:\微信web开发者工具\dll`）是真实测试场景而非边角案例，夹具与测试必须覆盖。
- Win11 23H2 自带 WebView2，Wails GUI 无需额外运行时安装。
- `REG_EXPAND_SZ` 误存为 `REG_SZ` 后 `%VAR%` 不再展开，且无任何报错——这是静默损坏。

## Testing instructions

- `go test ./...`；core 每个包必须有单元测试，行为以测试为准。
- `testdata/` 放真实 .reg 导出夹具（2026-10-13 修复前的 HKCU/HKLM 原始状态）：`check` 的验收基线是 13 个死条目全部检出、无误报。
- `clean` 的回归基线：清理后死条目清零、存活条目与备份逐字节一致、注册表值类型不变。
- 涉及真实注册表写入 / HKLM 提权的集成验证手动执行，不在单测内提权。
