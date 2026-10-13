// Package cli 实现 envwarden 的子命令（check / backup / clean / diff）。
//
// 每个子命令一个文件：解析参数、调用 core 包、渲染中文输出；不包含任何检测或变更逻辑。
package cli
