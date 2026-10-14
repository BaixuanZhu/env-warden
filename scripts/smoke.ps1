# envwarden 全链路冒烟测试（HKCU，自恢复；M2 备份/清理 + M3 快照/diff）
# 用法: ./scripts/smoke.ps1 [-Bin bin/envwarden.exe] [-WorkDir 临时目录]
# 开头先用 clean 归一化基线（CI runner 的 PATH 常带预存死条目），逐字节断言均以基线为准；
# 类型翻转翻到与基线相反的类型（runner 的 Path 常为 REG_EXPAND_SZ）。结束后 HKCU PATH 整体还原到脚本开始前的原始状态。
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

# ---- 0. 归一化基线：清掉机器自带的死条目（CI runner 的 PATH 常有预存死条目），
#         后续所有"逐字节一致"断言都以清理后的基线为准；结束时整体还原到 orig。
[void](RunBin @("clean", "-scope", "user", "-y", "-backup-dir", $bakDir))
$base = PathState
Write-Output ("base: kind=" + $base.Kind + " len=" + $base.Raw.Length)

# ---- 1. 体检 + 快照（基线状态） ----
[void](RunBin @("check", "-snapshot-dir", $snapDir))
$snaps = @(Get-ChildItem $snapDir -Filter 'snapshot-*.json' -ErrorAction SilentlyContinue)
Assert ($snaps.Count -eq 1) ("check 保存了 1 份快照 (got " + $snaps.Count + ")")

# ---- 2. 备份（独立目录，数量断言才确定） ----
$bakDir2 = Join-Path $WorkDir "backup-both"
[void](RunBin @("backup", "-o", $bakDir2))
Assert ((Get-ChildItem $bakDir2 -Filter '*.reg').Count -eq 2) "backup 产出两级 .reg 备份"

# ---- 3. 注入死条目：diff 报新增 ----
$dead = Join-Path $WorkDir ("dead-" + $PID + "\bin")
SetPath ($base.Raw + ';' + $dead) $base.Kind
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 1) ("diff 检出注入条目 exit=1 (got " + $LASTEXITCODE + ")")
Assert ($out -match [regex]::Escape($dead)) "diff 输出包含注入条目"

# ---- 4. dry-run 与取消不改注册表 ----
[void](RunBin @("clean", "-scope", "user", "-dry-run", "-backup-dir", $bakDir))
Assert ((PathState).Raw -eq ($base.Raw + ';' + $dead)) "dry-run 未变更"
$out = 'n' | & $Bin clean -scope user -backup-dir $bakDir 2>&1 | Out-String
Assert ((PathState).Raw -eq ($base.Raw + ';' + $dead)) "取消未变更"

# ---- 5. 清理：移除死条目，逐字节还原到基线 ----
[void](RunBin @("clean", "-scope", "user", "-y", "-backup-dir", $bakDir))
$after = PathState
Assert ($after.Raw -ceq $base.Raw) "clean 后与基线逐字节一致"
Assert ($after.Kind -eq $base.Kind) "clean 后值类型不变"
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 0) ("clean 后 diff 归零 exit=0 (got " + $LASTEXITCODE + ")")

# ---- 6. reg import 还原（验证 .reg 可被注册表引擎导入），再清理 ----
# 取最新的 user 备份：那是 clean 强制备份的"基线+死条目"状态（早于写入发生）
$userReg = Get-ChildItem $bakDir -Filter '*user*.reg' | Sort-Object Name | Select-Object -Last 1
$prevEap = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
reg import $userReg.FullName 2>&1 | Out-Null
$ErrorActionPreference = $prevEap
Assert ($LASTEXITCODE -eq 0) "reg import 接受备份文件"
Assert ((PathState).Raw -eq ($base.Raw + ';' + $dead)) "导入后回到注入状态"
[void](RunBin @("clean", "-scope", "user", "-y", "-backup-dir", $bakDir))
Assert ((PathState).Raw -ceq $base.Raw) "二次清理后逐字节还原到基线"

# ---- 7. 值类型翻转到相反类型：diff 报类型变化 ----
# runner 的 Path 常为 REG_EXPAND_SZ，固定翻到 ExpandString 会成为空操作
$flipKind = [Microsoft.Win32.RegistryValueKind]::ExpandString
if ($base.Kind -eq 'ExpandString') { $flipKind = [Microsoft.Win32.RegistryValueKind]::String }
SetPath $base.Raw $flipKind
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 1) ("类型翻转 diff exit=1 (got " + $LASTEXITCODE + ")")
Assert ($out -match 'REG_SZ' -and $out -match 'REG_EXPAND_SZ') "diff 报告值类型变化"

# ---- 8. 交换前两条：diff 报顺序变化 ----
SetPath $base.Raw $base.Kind
$parts = $base.Raw -split ';'
if ($parts.Count -ge 2) {
    $tmp = $parts[0]; $parts[0] = $parts[1]; $parts[1] = $tmp
    SetPath ($parts -join ';') $base.Kind
    $out = RunBin @("diff", "-snapshot-dir", $snapDir)
    Assert ($LASTEXITCODE -eq 1) ("交换条目 diff exit=1 (got " + $LASTEXITCODE + ")")
    Assert ($out -match '\[顺序\]') "diff 报告顺序变化"

    # ---- 9. reorder 子命令换回顺序（-from 1 -to 2 恰为逆操作），逐字节还原基线 ----
    [void](RunBin @("reorder", "-scope", "user", "-from", "1", "-to", "2", "-y", "-backup-dir", $bakDir))
    $afterReorder = PathState
    Assert ($afterReorder.Raw -ceq $base.Raw) "reorder 后与基线逐字节一致"
    Assert ($afterReorder.Kind -eq $base.Kind) "reorder 后值类型不变"
}

# ---- 10. restore 整键还原：备份基线态，注入垃圾条目，再用备份还原 ----
[void](RunBin @("backup", "-scope", "user", "-o", $bakDir))
$baseBackup = Get-ChildItem $bakDir -Filter '*user*.reg' | Sort-Object Name | Select-Object -Last 1
Assert ($null -ne $baseBackup) "找到基线态备份文件"
SetPath ($base.Raw + ';C:\envwarden-smoke-junk') $base.Kind
[void](RunBin @("restore", "-y", "-backup-dir", $bakDir, $baseBackup.FullName))
$afterRestore = PathState
Assert ($afterRestore.Raw -ceq $base.Raw) "restore 后与基线逐字节一致"
Assert ($afterRestore.Kind -eq $base.Kind) "restore 后值类型不变"

# ---- 11. 回到基线 diff 归零；最后整体还原到脚本开始前的原始状态 ----
SetPath $base.Raw $base.Kind
$out = RunBin @("diff", "-snapshot-dir", $snapDir)
Assert ($LASTEXITCODE -eq 0) ("基线状态 diff 无变化 exit=0 (got " + $LASTEXITCODE + ")")
SetPath $orig.Raw $orig.Kind
$final = PathState
Assert ($final.Raw -ceq $orig.Raw -and $final.Kind -eq $orig.Kind) "HKCU PATH 已还原到脚本开始前的原始状态"

Write-Output ("SMOKE RESULT: " + $(if ($fail -eq 0) { 'ALL PASS' } else { 'FAILED' }))
exit $fail
