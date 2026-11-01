$ErrorActionPreference = 'Stop'
# Builds the admin tool (never shipped to customers) into release\admin.
# It uses the system WebView2 runtime that Windows 10/11 include.
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
if (Test-Path '.tools\go\bin\go.exe') { $env:PATH = (Join-Path $projectRoot '.tools\go\bin') + ';' + $env:PATH }
if (Test-Path '.tools\gopath') { $env:GOPATH = Join-Path $projectRoot '.tools\gopath'; $env:GOCACHE = Join-Path $projectRoot '.tools\gocache' }
Push-Location frontend
if (Get-Command node -ErrorAction SilentlyContinue) { node node_modules\vite\bin\vite.js build --config vite.admin.config.ts }
else { & (Join-Path $projectRoot '.tools\node.ps1') node_modules\vite\bin\vite.js build --config vite.admin.config.ts }
$uiExit = $LASTEXITCODE
Pop-Location
if ($uiExit -ne 0) { throw 'Admin UI build failed' }
New-Item -ItemType Directory -Force -Path 'release\admin' | Out-Null
go build -tags production -trimpath -ldflags '-H windowsgui -s -w' -o 'release\admin\ImageWorkbenchAdmin.exe' ./cmd/admin
if ($LASTEXITCODE -ne 0) { throw 'Admin build failed' }
Get-Item 'release\admin\ImageWorkbenchAdmin.exe' | Select-Object FullName, Length
