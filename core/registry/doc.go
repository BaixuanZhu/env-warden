// Package registry 提供对 Windows 注册表两级 Environment 键的读取，
// 以及 regedit 导出 .reg 文件的解析。
//
// 两级键：
//   - 用户级：HKCU\Environment
//   - 系统级：HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment
//
// 职责边界：
//   - 只从注册表读 PATH，保证"新终端视角"，绝不读当前进程环境变量；
//   - PathData.Kind 记录注册表原值类型（REG_SZ / REG_EXPAND_SZ），
//     写回方必须原样保持；
//   - %VAR% 一律不展开（展开判断属于 detect 的职责）；
//   - ParseRegFile 服务于测试夹具与备份比对；
//   - 不做任何检测或过滤判断。
package registry
