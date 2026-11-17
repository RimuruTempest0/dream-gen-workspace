param([string]$SignThumbprint)
$ErrorActionPreference = 'Stop'
# Builds the admin tool (never shipped to customers) into release\admin, with
# the fixed WebView2 runtime from .tools copied beside it (release\admin\runtime).
# Without that folder the tool falls back to the system WebView2 runtime.
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
$fixed = Get-ChildItem '.tools\webview-expanded' -Filter 'msedgewebview2.exe' -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
if ($fixed) {
    # robocopy exit codes below 8 mean success; only changed files are copied.
    robocopy $fixed.Directory.FullName 'release\admin\runtime' /MIR /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw 'Copying the WebView2 runtime failed' }
    $global:LASTEXITCODE = 0
} else { Write-Warning 'No fixed WebView2 runtime under .tools\webview-expanded: the admin tool will use the system WebView2' }
. (Join-Path $PSScriptRoot 'signing.ps1')
$thumbprint = Get-SignThumbprint $SignThumbprint
if ($thumbprint) { Sign-BuildFile 'release\admin\ImageWorkbenchAdmin.exe' $thumbprint } else { Write-UnsignedWarning 'ImageWorkbenchAdmin.exe' }
Get-Item 'release\admin\ImageWorkbenchAdmin.exe' | Select-Object FullName, Length
