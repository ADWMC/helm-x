# 构建可交付的 helmx.exe（图标 + 清单 + GUI 子系统）
#
# 为什么需要这个脚本：`wails3 task build` 在本仓库坏在图标生成那一步
# （build/darwin 被清理掉后，icons 任务无条件写 darwin/icons.icns 而失败），
# 而裸 `go build` 不会带 Windows 图标资源 —— 图标与清单都来自 .syso。
#
# 用法：
#   pwsh -File scripts/build.ps1
#
# 产物：bin/helmx.exe（GUI 子系统，双击不出控制台；命令行子命令仍可用，
# 由 console_windows.go 里的 AttachConsole 接管 stdout）。

$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    Write-Host '[1/4] 前端构建（vue-tsc + vite）'
    Push-Location frontend
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "前端构建失败（exit $LASTEXITCODE）" }
    Pop-Location

    Write-Host '[2/4] 生成 Windows 资源（图标 + 清单 + 版本）'
    wails3 generate syso -arch amd64 -out helmx.syso `
        -icon build/windows/icon.ico `
        -manifest build/windows/wails.exe.manifest `
        -info build/windows/info.json
    if ($LASTEXITCODE -ne 0) { throw "syso 生成失败（exit $LASTEXITCODE）" }

    Write-Host '[3/4] 链接 exe'
    # -a 是必需的：Go 的构建缓存不跟踪 .syso 变化，不加会复用旧归档、图标进不去
    go build -a -trimpath -buildvcs=false `
        -ldflags '-w -s -H windowsgui' -o bin/helmx.exe .
    if ($LASTEXITCODE -ne 0) { throw "链接失败（exit $LASTEXITCODE）" }

    # wails 的 syso 把版本资源写成中性语言（0x0000），Windows 属性页读不到；
    # 这里改成 0x0409 并同步块内键，见 tools/verlang。
    Write-Host '[4/4] 修正版本资源语言'
    go run ./tools/verlang -file bin/helmx.exe
    if ($LASTEXITCODE -ne 0) { throw "版本资源修正失败（exit $LASTEXITCODE）" }

    Write-Host '完成：bin/helmx.exe'
} finally {
    Pop-Location
}
