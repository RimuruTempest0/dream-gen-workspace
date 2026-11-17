# Builds the product-browser helper and runs the Go tests that drive it against
# local mock pages (no real site, isolated temporary data). A verification
# window appears for a few seconds during one test.
# Covered: script-rendered pages, redirects, human checks (auto, and a shown window
# that must display the page, not stay blank),
# blank-page timeout, dropped connections, unreachable sites, and the read-only
# guards: new windows and popups blocked, downloads cancelled, navigation to
# anything but http/https cancelled (Chromium itself refuses file:// from a page),
# while the product is still read. Password autosave, form autofill and every
# permission request are switched off in cmd/product-browser/guards_windows.go;
# the profile folder is separate (data\product-browser) and the helper exits once
# the page and its images are read.
param([string]$RuntimeSource = '')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$go = 'go'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { $go = Join-Path $projectRoot '.tools\go\bin\go.exe' }
if (Test-Path '.tools\gopath') { $env:GOPATH = Join-Path $projectRoot '.tools\gopath'; $env:GOCACHE = Join-Path $projectRoot '.tools\gocache' }
if (-not $RuntimeSource) {
    $RuntimeSource = @(
        (Join-Path $projectRoot 'release\ImageWorkbench\runtime'),
        (Get-ChildItem (Join-Path $projectRoot '.tools\webview-expanded') -Directory -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty FullName)
    ) | Where-Object { $_ -and (Test-Path (Join-Path $_ 'msedgewebview2.exe')) } | Select-Object -First 1
}
if (-not $RuntimeSource) { throw '未找到 WebView2 固定版运行时，请用 -RuntimeSource 指定' }
$helper = Join-Path $projectRoot '.tools\product-browser-test\product-browser.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $helper) | Out-Null
& $go build -ldflags '-H windowsgui' -o $helper ./cmd/product-browser
if ($LASTEXITCODE -ne 0) { throw 'helper build failed' }
$env:IMAGEWORKBENCH_TEST_BROWSER = $helper
$env:IMAGEWORKBENCH_TEST_WEBVIEW2 = (Resolve-Path $RuntimeSource).Path
& $go test ./internal/core -run 'RealHelper' -count=1 -v -timeout 300s
if ($LASTEXITCODE -ne 0) { throw 'product browser tests failed' }
# The tests above only see the "verify" event; this one checks the shown window
# actually displays the check page (0.7.36 and earlier showed it blank).
& (Join-Path $PSScriptRoot 'test-verify-window.ps1') -Helper $helper -Runtime $env:IMAGEWORKBENCH_TEST_WEBVIEW2
