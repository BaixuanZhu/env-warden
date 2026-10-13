// Package backup 负责 Environment 注册表键的 .reg 备份与还原。
//
// 备份格式与 regedit 导出兼容（UTF-16LE + BOM），还原可直接双击导入。
// 任何变更操作前必须先经本包完成备份，该强制顺序由调用方保证并在 CLI 层不可跳过。
package backup
