// Package broadcast 负责注册表环境变量变更后的 WM_SETTINGCHANGE 系统广播。
//
// 广播后新开终端立即拿到新值，无需注销；已开着的旧终端仍持有旧值，验证须开新终端。
package broadcast
