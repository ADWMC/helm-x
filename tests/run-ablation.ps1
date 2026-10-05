# run-ablation.ps1 - ablation batch driver (PROMPT-V2-DESIGN.md section 4, stage 1)
# Responsibility: switch prompt_mode per arm, run 50 cases + poxian should_block bucket
# serially, emit tests/abl/<arm>-50.* and tests/abl/<arm>-poxian.*.
#
# Usage: powershell -File run-ablation.ps1                # arms A0..A4
#        powershell -File run-ablation.ps1 -Arms A3       # single arm
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 without a
# BOM as ANSI(GBK); non-ASCII literals get mangled and can break string tokens
# (parse error observed 2026-10-05). Judging is delegated to judge.py via
# helm-x-test-suite.py. Runs must be serial: rollout set-diff judging does not
# support parallel runs.
#
# Safety (2026-10-05 incident): the config file was once seen truncated to 0
# bytes; an empty read plus empty write-back would silently poison all arms.
# Reads and writes are validated on both sides; failures abort the batch.

param(
    [string[]]$Arms = @('A0', 'A1', 'A2', 'A3', 'A4'),
    [int]$Timeout = 60
)

$ErrorActionPreference = 'Stop'
$env:PYTHONIOENCODING = 'utf-8'

$suite  = Join-Path $PSScriptRoot 'helm-x-test-suite.py'
$poxian = Join-Path $PSScriptRoot 'fixtures\poxian-should-block.json'
$outdir = Join-Path $PSScriptRoot 'abl'
New-Item -ItemType Directory -Force -Path $outdir | Out-Null

$cfgPath = Join-Path $env:APPDATA 'helmx.config.json'
$modeMap = @{ A0 = 'default'; A1 = 'abl-a1'; A2 = 'abl-a2'; A3 = 'v2'; A4 = 'abl-a4' }

function Set-PromptMode([string]$mode, [string]$tag) {
    $raw = Get-Content $cfgPath -Raw -Encoding UTF8
    if (-not $raw -or $raw.Length -lt 100 -or $raw -notmatch '"prompt_mode"') {
        throw "[$tag] config read invalid (len=$(if ($raw) { $raw.Length } else { 0 })); abort to protect the ablation."
    }
    $new = $raw -replace '"prompt_mode"\s*:\s*"[^"]*"', ('"prompt_mode": "' + $mode + '"')
    # Tests must inject on EVERY request: codex sends no Session-Id/Thread-Id
    # headers, so all requests share one counter bucket and inject_every=N means
    # only 1 in N requests carries any prompt (verified 2026-10-05). Force 1.
    $new = $new -replace '"inject_every"\s*:\s*\d+', '"inject_every": 1'
    $tmp = "$cfgPath.tmp"
    [System.IO.File]::WriteAllText($tmp, $new, (New-Object System.Text.UTF8Encoding($false)))
    $check = Get-Content $tmp -Raw -Encoding UTF8
    if (-not $check -or $check.Length -lt 100 -or $check -notmatch ('"prompt_mode": "' + [regex]::Escape($mode) + '"') -or $check -notmatch '"inject_every": 1') {
        throw "[$tag] config write verify failed (len=$(if ($check) { $check.Length } else { 0 })); abort."
    }
    Move-Item -Force $tmp $cfgPath
    Write-Host "=== [$tag] prompt_mode=$mode inject_every=1 (config $($check.Length) chars) ==="
}

foreach ($arm in $Arms) {
    if (-not $modeMap.ContainsKey($arm)) { throw "unknown arm: $arm" }
    Set-PromptMode $modeMap[$arm] $arm

    python $suite --timeout $Timeout --arm $arm `
        --output (Join-Path $outdir "$arm-50.md") `
        --save-baseline (Join-Path $outdir "$arm-50.json")

    python $suite --prompts-file $poxian --timeout $Timeout --arm "$arm-poxian" `
        --output (Join-Path $outdir "$arm-poxian.md") `
        --save-baseline (Join-Path $outdir "$arm-poxian.json")
}

Set-PromptMode 'default' 'RESET'
# restore production settings: inject_every=1 (full coverage; the old value 30
# was sparse under the pre-fix semantics and bare requests cause refusals)
$raw = Get-Content $cfgPath -Raw -Encoding UTF8
if ($raw -and $raw.Length -gt 100) {
    $new = $raw -replace '"inject_every"\s*:\s*\d+', '"inject_every": 1'
    [System.IO.File]::WriteAllText($cfgPath, $new, (New-Object System.Text.UTF8Encoding($false)))
}
Write-Host "=== ablation stage 1 complete ==="
