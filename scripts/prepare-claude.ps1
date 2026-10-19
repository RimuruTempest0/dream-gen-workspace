param([string]$Destination = '.tools\claude-code')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot
$target = [IO.Path]::GetFullPath((Join-Path $projectRoot $Destination))
$boundary = [IO.Path]::GetFullPath((Join-Path $projectRoot '.tools')) + [IO.Path]::DirectorySeparatorChar
if (-not $target.StartsWith($boundary,[StringComparison]::OrdinalIgnoreCase)) { throw 'Claude preparation directory must be inside workspace .tools' }
New-Item -ItemType Directory -Force -Path $target | Out-Null
$metadata = Invoke-RestMethod -Uri 'https://registry.npmjs.org/@anthropic-ai/claude-code-win32-x64/2.1.296'
$archive = Join-Path $target 'native.tgz'
Invoke-WebRequest -Uri $metadata.dist.tarball -OutFile $archive
$stream = [IO.File]::OpenRead($archive)
try { $hash = [Security.Cryptography.SHA512]::Create(); $integrity = 'sha512-' + [Convert]::ToBase64String($hash.ComputeHash($stream)); $hash.Dispose() } finally { $stream.Dispose() }
if ($integrity -ne $metadata.dist.integrity) { throw 'Official Claude package integrity mismatch' }
$entries = & tar -tzf $archive
if ($LASTEXITCODE -ne 0) { throw 'Could not list verified Claude archive' }
$exeEntry = $entries | Where-Object { $_ -eq 'package/claude.exe' }
$licenseEntry = $entries | Where-Object { $_ -eq 'package/LICENSE.md' }
if (-not $exeEntry -or -not $licenseEntry) { throw 'Official runtime package is incomplete' }
& tar -xzf $archive -C $target $exeEntry $licenseEntry
if ($LASTEXITCODE -ne 0) { throw 'Claude archive extraction failed' }
Copy-Item -LiteralPath (Join-Path $target 'package\claude.exe') -Destination (Join-Path $target 'claude.exe') -Force
Copy-Item -LiteralPath (Join-Path $target 'package\LICENSE.md') -Destination (Join-Path $target 'LICENSE.md') -Force
$manifest = @{version='2.1.296';platform='windows-x64';source=$metadata.dist.tarball;integrity=$integrity;executableSHA256=(Get-FileHash -LiteralPath (Join-Path $target 'claude.exe') -Algorithm SHA256).Hash}
$manifest | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $target 'version.json') -Encoding utf8
Write-Output 'Verified Claude Code 2.1.296 prepared for portable packaging.'
