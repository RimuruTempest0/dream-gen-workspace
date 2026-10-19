$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'local-profile.ps1')
$projectRoot = Split-Path -Parent $PSScriptRoot
$fixture = Join-Path $projectRoot ('.tools\profile-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixture -Force | Out-Null
function Assert-True([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}
function New-TestData([string]$Directory) {
    New-Item -ItemType Directory -Path (Join-Path $Directory 'secrets') -Force | Out-Null
    [IO.File]::WriteAllText((Join-Path $Directory 'app.db'), 'test-database')
    [IO.File]::WriteAllText((Join-Path $Directory 'secrets\fixture.bin'), 'encrypted-test-placeholder')
}
function Assert-Rejected([scriptblock]$Action, [string]$Message) {
    $rejected = $false
    try { & $Action } catch { $rejected = $true }
    Assert-True $rejected $Message
}

Assert-True ($null -eq (Get-LocalProfileDataDirectory $fixture)) 'Fresh checkouts must have no local profile'
$oldRelease = Join-Path $fixture 'old release'
$originalData = Join-Path $oldRelease 'data'
New-TestData $originalData
Save-LocalProfileSource $fixture $oldRelease
Assert-True ((Get-LocalProfileDataDirectory $fixture) -eq $originalData) 'Registration failed'

$newRelease = Join-Path $fixture 'new release'
Set-LocalReleaseProfile $fixture $newRelease (Get-LocalProfileDataDirectory $fixture)
Assert-True ((Get-ApplicationDataDirectory $newRelease) -eq $originalData) 'New release lost the saved configuration'
Assert-True (-not (Test-Path -LiteralPath (Join-Path $newRelease 'data'))) 'Profile was copied instead of reused'
$pointer = [IO.File]::ReadAllBytes((Join-Path $newRelease 'data-location.txt'))
Assert-True ($pointer[0] -ne 0xEF) 'Go cannot resolve a UTF-8 BOM in the pointer'

# A subsequent in-app move may save a relative preference; next build must follow it.
$migratedData = Join-Path $newRelease 'migrated data'
New-TestData $migratedData
[IO.File]::WriteAllText((Join-Path $newRelease 'data-location.txt'), " migrated data`n")
Assert-True ((Get-LocalProfileDataDirectory $fixture) -eq $migratedData) 'Changed data location was ignored'
$nextRelease = Join-Path $fixture 'next release'
Set-LocalReleaseProfile $fixture $nextRelease (Get-LocalProfileDataDirectory $fixture)
Assert-True ((Get-ApplicationDataDirectory $nextRelease) -eq $migratedData) 'Third version lost migrated data'
Set-LocalReleaseProfile $fixture $nextRelease $migratedData

$conflict = Join-Path $fixture 'other release'
New-TestData (Join-Path $conflict 'data')
Assert-Rejected { Set-LocalReleaseProfile $fixture $conflict $migratedData } 'Existing user data was silently replaced'
Assert-True (-not (Test-Path -LiteralPath (Join-Path $conflict 'data-location.txt'))) 'Rejected build changed target configuration'
[IO.File]::WriteAllText((Join-Path $conflict 'data-location.txt'), (Join-Path $fixture 'missing data'))
Assert-Rejected { Set-LocalReleaseProfile $fixture $conflict $migratedData } 'Existing broken preference was silently replaced'

# Package the same explicit shipping entries used by build.ps1, with private files present.
foreach ($directory in @('runtime', 'agent')) {
    New-Item -ItemType Directory -Path (Join-Path $nextRelease $directory) | Out-Null
    [IO.File]::WriteAllText((Join-Path $nextRelease "$directory\fixture.txt"), 'runtime')
}
foreach ($file in @('ImageWorkbench.exe', 'product-browser.exe', '使用说明.txt', 'version.json')) {
    [IO.File]::WriteAllText((Join-Path $nextRelease $file), 'shipping-fixture')
}
$shipping = @('ImageWorkbench.exe', 'product-browser.exe', 'runtime', 'agent', '使用说明.txt', 'version.json') | ForEach-Object { Join-Path $nextRelease $_ }
$archivePath = Join-Path $fixture 'shipping.zip'
Compress-Archive -LiteralPath $shipping -DestinationPath $archivePath
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::OpenRead($archivePath)
try {
    $entries = @($archive.Entries | ForEach-Object { $_.FullName })
    Assert-True (-not ($entries -match 'data-location|app\.db|secrets|\.local|product-browser[\\/]')) 'Shipping archive exposed local profile'
    Assert-True ($entries -contains 'product-browser.exe') 'Shipping archive is missing the product browser helper'
} finally { $archive.Dispose() }

[IO.File]::WriteAllText((Join-Path $nextRelease 'data-location.txt'), (Join-Path $fixture 'missing data'))
Assert-Rejected { Get-LocalProfileDataDirectory $fixture } 'Missing data must fail rather than create empty configuration'
Write-Output 'PASS: profile reuse, repeated versions, migrated location, existing-data protection, missing-data protection, BOM-free pointer, clean shipping ZIP'
