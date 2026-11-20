$ErrorActionPreference = 'Stop'
# Builds and starts an isolated admin tool for desktop QA: its own data folder, a
# stand-in source folder (packages never land in the real release folder), and
# the QA WebView2 with remote debugging on port 9223. Stop the app QA first.
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
Copy-Item -LiteralPath 'go.mod' -Destination '.tools\admin-qa.mod' -Force
Copy-Item -LiteralPath 'go.sum' -Destination '.tools\admin-qa.sum' -Force
Add-Content -LiteralPath '.tools\admin-qa.mod' -Value "`nreplace github.com/wailsapp/go-webview2 => `"$($qaModule.Replace('\','/'))`"`n"
Push-Location frontend
& (Join-Path $projectRoot '.tools\node.ps1') node_modules\vite\bin\vite.js build --config vite.admin.config.ts
$uiExit = $LASTEXITCODE
Pop-Location
if ($uiExit -ne 0) { throw 'Admin UI build failed' }
New-Item -ItemType Directory -Force -Path '.tools\admin-qa' | Out-Null
& '.\.tools\go\bin\go.exe' build -modfile .tools/admin-qa.mod -tags 'production,desktopqa' -ldflags '-H windowsgui' -o '.tools/admin-qa/ImageWorkbenchAdmin.exe' ./cmd/admin
if ($LASTEXITCODE -ne 0) { throw 'Admin QA build failed' }
$id = [guid]::NewGuid().ToString('N')
$repo = Join-Path $projectRoot ".tools\admin-qa-repo-$id"
New-Item -ItemType Directory -Force -Path (Join-Path $repo 'scripts') | Out-Null
Set-Content -LiteralPath (Join-Path $repo 'scripts\build.ps1') -Value '# stand-in: the QA admin build uses a fake builder' -Encoding utf8
Set-Content -LiteralPath (Join-Path $repo 'wails.json') -Value '{ "info": { "productVersion": "0.7.34" } }' -Encoding utf8
# A real Git repository as the admin tool reads it: the mass-market line on main
# at 0.7.34, built in its own build folder next to it (<repo>-build). release/ is
# ignored, as in the real repository. (The e-commerce line was retired in admin 1.12.0.)
New-Item -ItemType Directory -Force -Path (Join-Path $repo 'docs') | Out-Null
[IO.File]::WriteAllText((Join-Path $repo 'docs\CHANGELOG.md'), "# Changelog`n`n## 0.7.34 - QA creative build`n`nQA creative notes, first paragraph.`n`n- first item`n`n### Verification`n- not for people using the app`n")
[IO.File]::WriteAllText((Join-Path $repo '.gitignore'), "release/`n")
git -C $repo init -q -b main
if ($LASTEXITCODE -ne 0) { throw 'git init failed for the admin QA source folder' }
git -C $repo add -A
git -C $repo -c user.name=qa -c user.email=qa@example.invalid commit -q -m 'QA creative source'
if ($LASTEXITCODE -ne 0) { throw 'git commit failed for the admin QA source folder' }
$env:IMAGEWORKBENCH_ADMIN_DATA = Join-Path $projectRoot ".tools\admin-qa-data-$id"
$env:IMAGEWORKBENCH_ADMIN_REPO = $repo
$runtime = Get-ChildItem (Join-Path $projectRoot '.tools\webview-expanded') -Filter 'msedgewebview2.exe' -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
# IMAGEWORKBENCH_QA_SYSTEM_WEBVIEW=1 uses the system WebView2 instead (when the fixed runtime cannot start here).
if ($runtime -and $env:IMAGEWORKBENCH_QA_SYSTEM_WEBVIEW -ne '1') { $env:IMAGEWORKBENCH_ADMIN_WEBVIEW = $runtime.Directory.FullName }
[IO.File]::WriteAllText((Join-Path $projectRoot '.tools\admin-qa-repo.txt'), $repo)
# IMAGEWORKBENCH_QA_NO_START=1: build and prepare only; the caller starts the program.
if ($env:IMAGEWORKBENCH_QA_NO_START -eq '1') { Write-Output 'Admin QA prepared, not started'; return }
$p = Start-Process -FilePath (Join-Path $projectRoot '.tools\admin-qa\ImageWorkbenchAdmin.exe') -WindowStyle Hidden -PassThru
$p.Id | Set-Content '.tools\admin-qa.pid'
[IO.File]::WriteAllText((Join-Path $projectRoot '.tools\admin-qa-repo.txt'), $repo)
Write-Output "Admin QA process: $($p.Id)"
