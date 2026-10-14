# envwarden 全链路冒烟测试（HKCU，自恢复；M2 备份/清理 + M3 快照/diff）
# 用法: ./scripts/smoke.ps1 [-Bin bin/envwarden.exe] [-WorkDir 临时目录]
# 断言只依赖 diff 的确定性输出，不依赖机器基线 PATH 是否健康；结束后 HKCU PATH 逐字节还原。
param(
    [string]$Bin = (Join-Path $PSScriptRoot "..\bin\envwarden.exe"),
    [string]$WorkDir = (Join-Path ([IO.Path]::GetTempPath()) ("envwarden-smoke-" + $PID))
)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$Bin = [IO.Path]::GetFullPath($Bin)
if (-not (Test-Path $Bin)) { Write-Output ("FAIL: binary not found: " + $Bin); exit 1 }
$bakDir = Join-Path $WorkDir "backups"
$snapDir = Join-Path $WorkDir "snapshots"
$fail = 0

function PathState {
    $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
    $raw = $k.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    $kind = $k.GetValueKind('Path')
    $k.Close()
    @{ Raw = $raw; Kind = $kind }
}

function SetPath($raw, $kind) {
    $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    $k.SetValue('Path', $raw, $kind)
    $k.Close()
}

function RunBin($argList) {
    & $Bin @argList 2>&1 | Out-String
}

function Assert($cond, $label) {
    if ($cond) { Write-Output ("PASS: " + $label) }
    else { Write-Output ("FAIL: " + $label); $script:fail = 1 }
}

$orig = PathState
Write-Output ("orig: kind=" + $orig.Kind + " len=" + $orig.Raw.Length + " bin=" + $Bin)

# ---- 1. 体检 + 快照 ----
[void](RunBin @("check", "-snapshot-dir", $snapDir))
$snaps = @(Get-ChildItem $snapDir -Filter 'snapshot-*.json' -ErrorAction SilentlyContinue)
Assert ($snaps.Count -eq 1) ("check 保存了 1 份快照 (got " + $snaps.Count + ")")

# ---- 2. 备份 ----
[void](RunBin @("backup", "-o", $bakDir))
Assert ((Get-ChildItem $bakDir -Filter '*.reg').Count -eq 2) "backup 产出两级 .reg 备份"

# ---- 3. 注入死条目：check 检出、diff 报新增 ----
$dead = Join-Path $WorkDir ("dead-" + $PID + "\bin")
SetPath ($orig.Raw + ';' + $dead) $orig.Kind
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 1) ("diff 检出注入条目 exit=1 (got " + $LASTEXITCODE + ")")
Assert ($out -match [regex]::Escape($dead)) "diff 输出包含注入条目"

# ---- 4. dry-run 与取消不改注册表 ----
[void](RunBin @("clean", "-scope", "user", "-dry-run", "-backup-dir", $bakDir))
Assert ((PathState).Raw -eq ($orig.Raw + ';' + $dead)) "dry-run 未变更"
$out = 'n' | & $Bin clean -scope user -backup-dir $bakDir 2>&1 | Out-String
Assert ((PathState).Raw -eq ($orig.Raw + ';' + $dead)) "取消未变更"

# ---- 5. 清理：移除死条目，逐字节还原 ----
[void](RunBin @("clean", "-scope", "user", "-y", "-backup-dir", $bakDir))
$after = PathState
Assert ($after.Raw -ceq $orig.Raw) "clean 后与原始值逐字节一致"
Assert ($after.Kind -eq $orig.Kind) "clean 后值类型不变"
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 0) ("clean 后 diff 归零 exit=0 (got " + $LASTEXITCODE + ")")

# ---- 6. reg import 还原（验证 .reg 可被注册表引擎导入），再清理 ----
# 取最新的 user 备份：那是 clean 强制备份的"原始+死条目"状态（早于写入发生）
$userReg = Get-ChildItem $bakDir -Filter '*user*.reg' | Sort-Object Name | Select-Object -Last 1
$prevEap = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
reg import $userReg.FullName 2>&1 | Out-Null
$ErrorActionPreference = $prevEap
Assert ($LASTEXITCODE -eq 0) "reg import 接受备份文件"
Assert ((PathState).Raw -eq ($orig.Raw + ';' + $dead)) "导入后回到注入状态"
[void](RunBin @("clean", "-scope", "user", "-y", "-backup-dir", $bakDir))
Assert ((PathState).Raw -ceq $orig.Raw) "二次清理后逐字节还原"

# ---- 7. 值类型翻转：diff 报类型变化 ----
SetPath $orig.Raw ([Microsoft.Win32.RegistryValueKind]::ExpandString)
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 1) ("类型翻转 diff exit=1 (got " + $LASTEXITCODE + ")")
Assert ($out -match 'REG_SZ' -and $out -match 'REG_EXPAND_SZ') "diff 报告值类型变化"

# ---- 8. 交换前两条：diff 报顺序变化 ----
SetPath $orig.Raw $orig.Kind
$parts = $orig.Raw -split ';'
if ($parts.Count -ge 2) {
    $tmp = $parts[0]; $parts[0] = $parts[1]; $parts[1] = $tmp
    SetPath ($parts -join ';') $orig.Kind
    $out = RunBin @("diff", "-snapshot-dir", $snapDir)
    Assert ($LASTEXITCODE -eq 1) ("交换条目 diff exit=1 (got " + $LASTEXITCODE + ")")
    Assert ($out -match '\[顺序\]') "diff 报告顺序变化"
}

# ---- 9. 全部还原，最终断言 ----
SetPath $orig.Raw $orig.Kind
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 0) ("最终 diff 无变化 exit=0 (got " + $LASTEXITCODE + ")")
$final = PathState
Assert ($final.Raw -ceq $orig.Raw -and $final.Kind -eq $orig.Kind) "HKCU PATH 逐字节还原且类型不变"

Write-Output ("SMOKE RESULT: " + $(if ($fail -eq 0) { 'ALL PASS' } else { 'FAILED' }))
exit $fail
