# gogui slim 构建：-trimpath -s -w
#   demo.exe  ~10.5MB(裸) → ~3.8MB(裁剪)
#   monitor.exe / gogui-golden.exe 同理
# 用法: powershell -File build.ps1 [-Out bin]
param([string]$Out = "bin")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$ld = "-s -w"
foreach ($app in @("demo", "monitor", "gogui-golden")) {
    $exe = Join-Path $Out "$app.exe"
    go build -trimpath -ldflags $ld -o $exe "./cmd/$app"
    $kb = [math]::Round((Get-Item $exe).Length / 1KB, 1)
    Write-Host ("built {0,-24} {1,8} KB" -f $exe, $kb)
}
