# testdata — 测试夹具

存放真实注册表导出的 .reg 夹具（M0 引入）：

- **来源**：`C:\Users\zbxComputer\env-backup-20261013\` 下 2026-10-13 修复前的
  `HKCU-Environment.reg` / `HKLM-Environment.reg` 原始导出。
- **用途**：
  - `check` 验收基线：13 个死条目全部检出、无误报；
  - `clean` 回归基线：清理后存活条目与备份逐字节一致、注册表值类型不变。
- **注意**：夹具含真实路径（含中文路径），属敏感本地数据；仓库若开源需先脱敏重建夹具。
