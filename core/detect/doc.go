// Package detect 实现 PATH 体检的四类检测：死条目、重复条目、命令解析、npm prefix 可达性。
//
// 约定：
//   - 输入一律来自 registry 包读取的注册表真实值（新终端视角），而非进程环境变量；
//   - %VAR% 展开用两级注册表环境变量合成的变量表（ComposeVars），不用进程环境；
//   - 条目归一化规则：TrimSpace + 去尾部反斜杠 + 大小写不敏感，用于重复判定；
//   - 命令解析模拟新终端视角：系统 PATH 拼在用户 PATH 之前，同名命令前者胜出，
//     每目录按 PATHEXT 顺序取第一命中，与 where.exe 结果一致；
//   - 检测全部为只读操作，无任何副作用。
package detect
