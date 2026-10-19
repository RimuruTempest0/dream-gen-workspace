$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$env:GOPATH = Join-Path $projectRoot '.tools\gopath'
$env:GOCACHE = Join-Path $projectRoot '.tools\gocache'
$qaModule = Join-Path $projectRoot '.tools\qa-webview2'
if (-not (Test-Path $qaModule)) {
    Copy-Item -LiteralPath (Join-Path $env:GOPATH 'pkg\mod\github.com\wailsapp\go-webview2@v1.0.22') -Destination $qaModule -Recurse
    $chromiumPath = Join-Path $qaModule 'pkg\edge\chromium.go'
    Set-ItemProperty -LiteralPath $chromiumPath -Name IsReadOnly -Value $false
    $text = Get-Content -LiteralPath $chromiumPath -Raw
    $text = $text.Replace('browserArgs := strings.Join(e.AdditionalBrowserArgs, " ")', 'browserArgs := strings.Join(e.AdditionalBrowserArgs, " ") + " --remote-debugging-port=9223"')
    Set-Content -LiteralPath $chromiumPath -Value $text -Encoding utf8
}
Copy-Item -LiteralPath 'go.mod' -Destination '.tools\desktop-qa.mod' -Force
Copy-Item -LiteralPath 'go.sum' -Destination '.tools\desktop-qa.sum' -Force
$replaceLine = "`nreplace github.com/wailsapp/go-webview2 => `"$($qaModule.Replace('\','/'))`"`n"
Add-Content -LiteralPath '.tools\desktop-qa.mod' -Value $replaceLine
New-Item -ItemType Directory -Force -Path '.tools\desktop-qa' | Out-Null
& '.\.tools\go\bin\go.exe' build -modfile .tools/desktop-qa.mod -tags 'production,desktopqa' -ldflags '-H windowsgui' -o '.tools/desktop-qa/ImageWorkbench.exe' .
if ($LASTEXITCODE -ne 0) { throw 'QA build failed' }
# The helper uses the shipping go.mod: the QA debug port belongs to the app window only.
& '.\.tools\go\bin\go.exe' build -ldflags '-H windowsgui' -o '.tools/desktop-qa/product-browser.exe' ./cmd/product-browser
if ($LASTEXITCODE -ne 0) { throw 'product browser build failed' }
if (-not (Test-Path '.tools\desktop-qa\runtime\msedgewebview2.exe')) {
    # A copied or broken link can leave a plain file here; keep it aside and relink.
    if (Test-Path '.tools\desktop-qa\runtime') { Rename-Item '.tools\desktop-qa\runtime' ('runtime-broken-' + (Get-Date -Format 'yyyyMMddHHmmss')) }
    New-Item -ItemType Junction -Path '.tools\desktop-qa\runtime' -Target (Join-Path $projectRoot 'release\ImageWorkbench\runtime') | Out-Null }
$env:IMAGEWORKBENCH_DATA_DIR = Join-Path $projectRoot ('.tools\qa-data-' + [guid]::NewGuid().ToString('N'))
$agentDir = Join-Path $projectRoot '.tools\desktop-qa\agent\claude-code'
New-Item -ItemType Directory -Force -Path $agentDir | Out-Null
Copy-Item -LiteralPath '.tools\claude-code\claude.exe' -Destination (Join-Path $agentDir 'claude.exe') -Force
$qaProcess = Start-Process -FilePath (Join-Path $projectRoot '.tools\desktop-qa\ImageWorkbench.exe') -WindowStyle Hidden -PassThru
$qaProcess.Id | Set-Content '.tools\desktop-qa.pid'
Write-Output "Desktop QA process: $($qaProcess.Id)"
