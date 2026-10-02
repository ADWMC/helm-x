# Phase A: verify old helmx.exe behavior for P1-P4.
#
# Safety: uses a temporary CODEX_HOME throughout. Never touches the real
# ~/.codex/config.toml. ASCII-only source so any PowerShell reads it correctly.

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$root = 'C:\Users\Administrator\Documents\GitHub\helm-x-wails'
$work = Join-Path $env:TEMP 'helmx-phaseA'
$oldExe = Join-Path $work 'helmx-old.exe'
$fakeExe = Join-Path $work 'fakeupstream.exe'
$results = Join-Path $work 'results'
New-Item -ItemType Directory -Force -Path $results | Out-Null

if (-not (Test-Path $oldExe)) { throw "missing old exe: $oldExe" }
if (-not (Test-Path $fakeExe)) { throw "missing fake upstream: $fakeExe" }

# --- isolated CODEX_HOME ---
$codexHome = Join-Path $work 'codexhome'
New-Item -ItemType Directory -Force -Path $codexHome | Out-Null

$configToml = @(
    'model = "gpt-5.6-terra"'
    'model_provider = "custom"'
    ''
    '[model_providers.custom]'
    'name = "fake"'
    'base_url = "http://127.0.0.1:19000/v1"'
    'wire_api = "responses"'
    'api_key = "test-key-not-real"'
) -join "`n"
Set-Content -Path (Join-Path $codexHome 'config.toml') -Value $configToml -Encoding ASCII

$requestJson = '{"model":"gpt-5.6-terra","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"test request"}]}]}'
$requestBody = Join-Path $work 'request.json'
Set-Content -Path $requestBody -Value $requestJson -Encoding ASCII

function Stop-All {
    Get-Process -Name 'fakeupstream' -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Get-Process -Name 'helmx-old' -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 300
}

function Run-Scenario {
    param([string]$Name, [string]$Scenario)

    Stop-All
    $fakeLog = Join-Path $results "$Name.fake.log"
    $proxyOut = Join-Path $results "$Name.proxy.out"
    $proxyErr = Join-Path $results "$Name.proxy.err"

    $fake = Start-Process -FilePath $fakeExe -ArgumentList @('-addr','127.0.0.1:19000','-scenario',$Scenario) `
        -RedirectStandardError $fakeLog -PassThru -WindowStyle Hidden
    Start-Sleep -Milliseconds 700

    $env:CODEX_HOME = $codexHome
    $proxy = Start-Process -FilePath $oldExe `
        -ArgumentList @('proxy','--listen','1801','--upstream','http://127.0.0.1:19000/v1','--no-retry') `
        -RedirectStandardOutput $proxyOut -RedirectStandardError $proxyErr -PassThru -WindowStyle Hidden
    Start-Sleep -Milliseconds 1800

    $exited = $proxy.HasExited
    $exitCode = if ($exited) { $proxy.ExitCode } else { -1 }

    $out = Join-Path $results "$Name.response.txt"
    $timing = & curl.exe -s -N -o $out `
        -w "http_code=%{http_code} starttransfer=%{time_starttransfer} total=%{time_total}" `
        -X POST 'http://127.0.0.1:1801/v1/responses' `
        -H 'Content-Type: application/json' `
        --data-binary "@$requestBody" 2>&1

    Start-Sleep -Milliseconds 400

    $seen = '(unreachable)'
    try { $seen = (Invoke-WebRequest 'http://127.0.0.1:19000/__seen' -UseBasicParsing -TimeoutSec 5).Content } catch { }
    Set-Content -Path (Join-Path $results "$Name.upstream-seen.json") -Value $seen -Encoding UTF8

    Stop-All

    $respText = ''
    if (Test-Path $out) { $respText = Get-Content $out -Raw -ErrorAction SilentlyContinue }

    [pscustomobject]@{
        Scenario  = $Name
        ProxyDied = $exited
        ProxyExit = $exitCode
        Timing    = ($timing -join ' ')
        Response  = $respText
    }
}

Write-Output "=== Phase A: old helmx.exe behavior ==="
Write-Output "old exe  : $oldExe"
Write-Output "CODEX_HOME: $codexHome (temporary, isolated)"
Write-Output ""

$scenarios = @(
    @{ Name='A-1-p1-5xx-json';   Scenario='p1-5xx-json' },
    @{ Name='A-2-p2-refuse-sse'; Scenario='p2-refuse-sse' },
    @{ Name='A-3-p3-slow-sse';   Scenario='p3-slow-sse' },
    @{ Name='A-4-p4-reordered';  Scenario='p4-reordered' },
    @{ Name='A-5-baseline';      Scenario='normal' }
)

$rows = @()
foreach ($s in $scenarios) {
    Write-Output "--- $($s.Name) ---"
    $r = Run-Scenario -Name $s.Name -Scenario $s.Scenario
    $rows += $r
    Write-Output "  proxyDied=$($r.ProxyDied) exit=$($r.ProxyExit)"
    Write-Output "  $($r.Timing)"
    $preview = if ($r.Response) { $r.Response.Substring(0, [Math]::Min(300, $r.Response.Length)) } else { '(empty)' }
    Write-Output "  resp: $preview"
    Write-Output ""
}

Write-Output "=== summary ==="
$rows | Select-Object Scenario, ProxyDied, Timing | Format-Table -AutoSize | Out-String | Write-Output
Write-Output "artifacts: $results"
