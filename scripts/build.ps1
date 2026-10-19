param([string]$RuntimeSource, [string]$ClaudeSource = '.tools\claude-code', [string]$Version = '0.3.8', [string]$ReleaseDirectory, [switch]$NoLocalProfile)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
. (Join-Path $PSScriptRoot 'local-profile.ps1')
if (-not $ReleaseDirectory) { $ReleaseDirectory = "release\ImageWorkbench-$Version" }
$releaseRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot $ReleaseDirectory))
$releaseBoundary = [IO.Path]::GetFullPath((Join-Path $projectRoot 'release')) + [IO.Path]::DirectorySeparatorChar
if (-not $releaseRoot.StartsWith($releaseBoundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Release directory must remain inside workspace release directory' }
$localDataRoot = if (-not $NoLocalProfile) { Get-LocalProfileDataDirectory $projectRoot } else { $null }
if ($localDataRoot) { Assert-LocalReleaseProfile $releaseRoot $localDataRoot }
$env:CI = 'true'
if (Test-Path '.tools\go\bin\go.exe') { $env:PATH = (Join-Path $projectRoot '.tools\go\bin') + ';' + $env:PATH }
if (Test-Path '.tools\gopath') { $env:GOPATH = Join-Path $projectRoot '.tools\gopath'; $env:GOCACHE = Join-Path $projectRoot '.tools\gocache' }
$wailsCommand = if (Test-Path '.tools\bin\wails.exe') { Join-Path $projectRoot '.tools\bin\wails.exe' } else { (Get-Command wails -ErrorAction Stop).Source }
if (Get-Command pnpm -ErrorAction SilentlyContinue) {
    pnpm --dir frontend install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed' }
    pnpm --dir frontend run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
} elseif ((Get-Command node -ErrorAction SilentlyContinue) -and (Test-Path 'frontend\node_modules\vite')) {
    # Without pnpm, build from the already installed dependencies (lockfile is not re-verified).
    Write-Warning 'pnpm not found; building the frontend with the installed node_modules'
    Push-Location frontend
    node node_modules\typescript\bin\tsc -b
    if ($LASTEXITCODE -eq 0) { node node_modules\vite\bin\vite.js build }
    $frontendExit = $LASTEXITCODE
    Pop-Location
    if ($frontendExit -ne 0) { throw 'Frontend build failed' }
} else { throw 'Frontend build needs pnpm, or node with installed frontend\node_modules' }
& $wailsCommand build -s -o "ImageWorkbench-$Version.exe"
if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }
# Product page reader used when a shop blocks direct requests (cmd/product-browser).
go build -trimpath -ldflags '-H windowsgui -s -w' -o 'build\bin\product-browser.exe' ./cmd/product-browser
if ($LASTEXITCODE -ne 0) { throw 'Product browser build failed' }
New-Item -ItemType Directory -Force -Path $releaseRoot | Out-Null
if (-not $RuntimeSource) {
    $candidate = Get-ChildItem '.tools\webview-expanded' -Filter 'msedgewebview2.exe' -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($candidate) { $RuntimeSource = $candidate.Directory.FullName }
}
if (-not $RuntimeSource) { throw 'Pass -RuntimeSource with extracted Microsoft Fixed Version x64 runtime' }
$runtimeExe = Join-Path $RuntimeSource 'msedgewebview2.exe'
$signature = Get-AuthenticodeSignature -LiteralPath $runtimeExe
if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notlike '*Microsoft Corporation*') { throw 'Microsoft runtime signature validation failed' }
$runtimeTarget = Join-Path $releaseRoot 'runtime'
$targetExe=Join-Path $runtimeTarget 'msedgewebview2.exe'
$refreshRuntime=(-not (Test-Path $targetExe)) -or ((Get-FileHash $targetExe).Hash -ne (Get-FileHash $runtimeExe).Hash)
if($refreshRuntime){
    if(Test-Path $runtimeTarget){
        $expectedTarget=[IO.Path]::GetFullPath((Join-Path $releaseRoot 'runtime'))
        if([IO.Path]::GetFullPath($runtimeTarget) -ne $expectedTarget -or (Get-Item $runtimeTarget).Attributes -band [IO.FileAttributes]::ReparsePoint){throw 'Unsafe runtime target'}
        Remove-Item -LiteralPath $runtimeTarget -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $runtimeTarget | Out-Null
    Copy-Item -Path (Join-Path $RuntimeSource '*') -Destination $runtimeTarget -Recurse -Force
}
Copy-Item -LiteralPath (Join-Path $projectRoot "build\bin\ImageWorkbench-$Version.exe") -Destination (Join-Path $releaseRoot 'ImageWorkbench.exe') -Force
Copy-Item -LiteralPath (Join-Path $projectRoot 'build\bin\product-browser.exe') -Destination (Join-Path $releaseRoot 'product-browser.exe') -Force
Copy-Item -LiteralPath 'docs\使用说明.txt' -Destination (Join-Path $releaseRoot '使用说明.txt') -Force
$claudeManifest = Get-Content -LiteralPath (Join-Path $ClaudeSource 'version.json') -Raw | ConvertFrom-Json
$claudeExe = Join-Path $ClaudeSource 'claude.exe'
if ($claudeManifest.version -ne '2.1.296' -or (Get-FileHash -LiteralPath $claudeExe -Algorithm SHA256).Hash -ne $claudeManifest.executableSHA256) { throw 'Claude runtime version/hash validation failed' }
$agentTarget = Join-Path $releaseRoot 'agent\claude-code'
New-Item -ItemType Directory -Force -Path $agentTarget | Out-Null
foreach ($file in @('claude.exe','version.json','LICENSE.md')) { Copy-Item -LiteralPath (Join-Path $ClaudeSource $file) -Destination (Join-Path $agentTarget $file) -Force }
$manifest = @{version=$Version;platform='windows-x64';runtime=(Get-Item $runtimeExe).VersionInfo.FileVersion;executableSHA256=(Get-FileHash (Join-Path $releaseRoot 'ImageWorkbench.exe')).Hash;productBrowserSHA256=(Get-FileHash (Join-Path $releaseRoot 'product-browser.exe')).Hash;runtimeSHA256=(Get-FileHash (Join-Path $runtimeTarget 'msedgewebview2.exe')).Hash;claudeVersion=$claudeManifest.version;claudeSHA256=$claudeManifest.executableSHA256}
$manifest | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $releaseRoot 'version.json') -Encoding utf8
# Only explicit shipping files; data/cache and development fixtures never enter the ZIP.
$shipping = @((Join-Path $releaseRoot 'ImageWorkbench.exe'),(Join-Path $releaseRoot 'product-browser.exe'),$runtimeTarget,(Join-Path $releaseRoot 'agent'),(Join-Path $releaseRoot '使用说明.txt'),(Join-Path $releaseRoot 'version.json'))
Compress-Archive -LiteralPath $shipping -DestinationPath (Join-Path $projectRoot "release\ImageWorkbench-portable-x64-$Version.zip") -Force
if ($localDataRoot) {
    Set-LocalReleaseProfile $projectRoot $releaseRoot $localDataRoot
    Write-Output 'Local release reuses your saved connections and encrypted keys. The shipping ZIP contains no personal data or data-location.txt.'
}
Get-Item (Join-Path $projectRoot "release\ImageWorkbench-portable-x64-$Version.zip") | Select-Object FullName,Length
