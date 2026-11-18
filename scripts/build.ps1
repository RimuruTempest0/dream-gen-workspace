param([string]$RuntimeSource, [string]$ClaudeSource = '.tools\claude-code', [string]$Version = '0.7.38', [string]$ReleaseDirectory, [switch]$NoLocalProfile, [string]$LicensePublicKey, [string]$ZipPath, [string]$UpdateURL, [string]$SignThumbprint, [string]$ProductLine, [string]$UpdatePublicKey)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
. (Join-Path $PSScriptRoot 'local-profile.ps1')
. (Join-Path $PSScriptRoot 'signing.ps1')
$thumbprint = Get-SignThumbprint $SignThumbprint
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
# Product page reader used when a shop blocks direct requests (cmd/product-browser).
# Built first: the app embeds its hash and refuses to start a different file.
go build -trimpath -ldflags '-H windowsgui -s -w' -o 'build\bin\product-browser.exe' ./cmd/product-browser
if ($LASTEXITCODE -ne 0) { throw 'Product browser build failed' }
# Signed before hashing: the app checks the file it will actually find.
if ($thumbprint) { Sign-BuildFile 'build\bin\product-browser.exe' $thumbprint } else { Write-UnsignedWarning 'product-browser.exe' }
$claudeManifest = Get-Content -LiteralPath (Join-Path $ClaudeSource 'version.json') -Raw | ConvertFrom-Json
$claudeExe = Join-Path $ClaudeSource 'claude.exe'
if ($claudeManifest.version -ne '2.1.296' -or (Get-FileHash -LiteralPath $claudeExe -Algorithm SHA256).Hash -ne $claudeManifest.executableSHA256) { throw 'Claude runtime version/hash validation failed' }
$browserHash = (Get-FileHash -LiteralPath 'build\bin\product-browser.exe' -Algorithm SHA256).Hash
$ldflags = "-X imageworkbench/internal/core.expectedClaudeSHA256=$($claudeManifest.executableSHA256) -X imageworkbench/internal/core.expectedProductBrowserSHA256=$browserHash"
# Customer packages (built by the admin tool) trust its signing key for customer.dat.
if ($LicensePublicKey) { $ldflags += " -X imageworkbench/internal/core.licensePublicKey=$LicensePublicKey" }
# A signed build checks its own signature before trusting customer.dat.
if ($thumbprint) { $ldflags += " -X imageworkbench/internal/core.expectSigned=1" }
# Where the app reads the signed update.json (customer builds with a signing key only).
if ($UpdateURL) {
    if ($UpdateURL -notmatch '^https?://[^\s]+$') { throw 'UpdateURL must be an http(s) address' }
    $ldflags += " -X imageworkbench/internal/core.updateManifestURL=$UpdateURL"
}
# A product line without a licence (the admin tool's "发布大众版"): the app runs
# without customer.dat, and takes updates signed with -UpdatePublicKey and
# announced for this line only (docs/adr/0001).
if ($ProductLine) {
    if ($ProductLine -notmatch '^[a-z][a-z0-9-]{1,31}$') { throw 'ProductLine must be a short lower-case name' }
    if ($LicensePublicKey) { throw 'A product-line build has no licence: do not pass -LicensePublicKey' }
    if (-not $UpdatePublicKey -or -not $UpdateURL) { throw 'A product-line build needs -UpdatePublicKey and -UpdateURL' }
    $ldflags += " -X imageworkbench/internal/core.productLine=$ProductLine -X imageworkbench/internal/core.updatePublicKey=$UpdatePublicKey"
} elseif ($UpdatePublicKey) { throw '-UpdatePublicKey is for product-line builds (-ProductLine)' }
$buildArgs = @('build', '-s', '-o', "ImageWorkbench-$Version.exe", '-ldflags', $ldflags)
& $wailsCommand @buildArgs
if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }
if ($thumbprint) { Sign-BuildFile (Join-Path $projectRoot "build\bin\ImageWorkbench-$Version.exe") $thumbprint } else { Write-UnsignedWarning 'ImageWorkbench.exe' }
New-Item -ItemType Directory -Force -Path $releaseRoot | Out-Null
if (-not $RuntimeSource) {
    $candidate = Get-ChildItem '.tools\webview-expanded' -Filter 'msedgewebview2.exe' -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($candidate) { $RuntimeSource = $candidate.Directory.FullName }
}
if (-not $RuntimeSource) { throw 'Pass -RuntimeSource with extracted Microsoft Fixed Version x64 runtime' }
$runtimeExe = Join-Path $RuntimeSource 'msedgewebview2.exe'
# The fixed WebView2 runtime is not updated by Windows; ship a recent one.
$runtimeAge = (Get-Date) - (Get-Item -LiteralPath $runtimeExe).LastWriteTime
if ($runtimeAge.TotalDays -gt 90) {
    Write-Warning ("WebView2 Fixed Version runtime {0} is {1:N0} days old. Download the current x64 Fixed Version from https://developer.microsoft.com/microsoft-edge/webview2/ (see README, 'WebView2 运行时更新'), extract it and pass -RuntimeSource." -f (Get-Item $runtimeExe).VersionInfo.FileVersion, $runtimeAge.TotalDays)
}
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
$agentTarget = Join-Path $releaseRoot 'agent\claude-code'
New-Item -ItemType Directory -Force -Path $agentTarget | Out-Null
foreach ($file in @('claude.exe','version.json','LICENSE.md')) { Copy-Item -LiteralPath (Join-Path $ClaudeSource $file) -Destination (Join-Path $agentTarget $file) -Force }
$manifest = @{version=$Version;platform='windows-x64';runtime=(Get-Item $runtimeExe).VersionInfo.FileVersion;executableSHA256=(Get-FileHash (Join-Path $releaseRoot 'ImageWorkbench.exe')).Hash;productBrowserSHA256=(Get-FileHash (Join-Path $releaseRoot 'product-browser.exe')).Hash;runtimeSHA256=(Get-FileHash (Join-Path $runtimeTarget 'msedgewebview2.exe')).Hash;claudeVersion=$claudeManifest.version;claudeSHA256=$claudeManifest.executableSHA256}
if ($ProductLine) { $manifest.productLine = $ProductLine }
$manifest | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $releaseRoot 'version.json') -Encoding utf8
# Only explicit shipping files; data/cache and development fixtures never enter the ZIP.
$shipping = @((Join-Path $releaseRoot 'ImageWorkbench.exe'),(Join-Path $releaseRoot 'product-browser.exe'),$runtimeTarget,(Join-Path $releaseRoot 'agent'),(Join-Path $releaseRoot '使用说明.txt'),(Join-Path $releaseRoot 'version.json'))
# Customer builds (admin tool) pass their own ZIP path so the generic package is not replaced.
$zipPath = if ($ZipPath) { [IO.Path]::GetFullPath((Join-Path $projectRoot $ZipPath)) } else { Join-Path $projectRoot "release\ImageWorkbench-portable-x64-$Version.zip" }
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $zipPath) | Out-Null
Compress-Archive -LiteralPath $shipping -DestinationPath $zipPath -Force
# The ZIP must never carry your LLM / image relay connections or keys: those stay only
# in the local data directory used by the release folder. Any finding deletes the ZIP.
go build -o 'build\bin\packcheck.exe' ./scripts/packcheck
if ($LASTEXITCODE -ne 0) { Remove-Item -LiteralPath $zipPath -Force; throw 'Package privacy check build failed' }
$checkArgs = @('-zip', $zipPath)
if ($localDataRoot) { $checkArgs += @('-data', $localDataRoot) }
& (Join-Path $projectRoot 'build\bin\packcheck.exe') @checkArgs
if ($LASTEXITCODE -ne 0) { Remove-Item -LiteralPath $zipPath -Force; throw 'Shipping ZIP contained local data or connection details and was deleted' }
# Program update package: the program and its notes, plus runtime/agent only when they
# changed since the previous build next to this one (the app refuses an update that
# lacks a component it needs, so an older install then uses the full package).
$previous = Get-ChildItem -LiteralPath (Split-Path -Parent $releaseRoot) -Directory -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -ne $releaseRoot -and (Test-Path (Join-Path $_.FullName 'version.json')) } |
    ForEach-Object { try { $v = Get-Content -LiteralPath (Join-Path $_.FullName 'version.json') -Raw | ConvertFrom-Json; [pscustomobject]@{ Info = $v; Order = [version]$v.version } } catch { } } |
    Where-Object { $_.Order -lt [version]$Version } | Sort-Object Order | Select-Object -Last 1
$updateItems = @((Join-Path $releaseRoot 'ImageWorkbench.exe'),(Join-Path $releaseRoot 'product-browser.exe'),(Join-Path $releaseRoot '使用说明.txt'),(Join-Path $releaseRoot 'version.json'))
if (-not $previous -or $previous.Info.runtimeSHA256 -ne $manifest.runtimeSHA256) { $updateItems += $runtimeTarget }
# A product line's update package is public and never carries the bundled Claude
# Code: when it changed, the app refuses the update and asks for the full package.
if (-not $ProductLine -and (-not $previous -or $previous.Info.claudeSHA256 -ne $manifest.claudeSHA256)) { $updateItems += (Join-Path $releaseRoot 'agent') }
# Next to the shipping ZIP: ImageWorkbench-portable-x64-<version>-update.zip (admin builds: build-check-update.zip, renamed by the admin tool).
$updateZip = $zipPath -replace '\.zip$', '-update.zip'
Compress-Archive -LiteralPath $updateItems -DestinationPath $updateZip -Force
$checkArgs = @('-zip', $updateZip)
if ($localDataRoot) { $checkArgs += @('-data', $localDataRoot) }
& (Join-Path $projectRoot 'build\bin\packcheck.exe') @checkArgs
if ($LASTEXITCODE -ne 0) { Remove-Item -LiteralPath $updateZip -Force; throw 'Update package contained local data or connection details and was deleted' }
$from = if ($previous) { "since $($previous.Info.version)" } else { 'no previous build found' }
Write-Output ("Update package ({0}): {1} item(s), {2}" -f $from, $updateItems.Count, $updateZip)
if ($localDataRoot) {
    Set-LocalReleaseProfile $projectRoot $releaseRoot $localDataRoot
    Write-Output 'Local release reuses your saved connections and encrypted keys. The shipping ZIP contains no personal data or data-location.txt.'
}
Get-Item $zipPath | Select-Object FullName,Length
