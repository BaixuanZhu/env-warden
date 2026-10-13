// Package mutate 实现对 PATH 的安全变更：只精确删除声明的条目，其余逐字保留。
//
// 红线：永不整段重写 PATH；只动调用方明确声明的条目；写回委托 registry 完成以保持值类型。
package mutate
