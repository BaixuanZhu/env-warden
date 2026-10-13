// Package snapshot 保存体检快照并对比两次快照间的 PATH 差异。
//
// 回答"上次体检以来 PATH 变了什么"：新增、删除、顺序调整（顺序即命令解析优先级）。
package snapshot
