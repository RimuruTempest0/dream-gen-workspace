# Local build preferences only. Never copy this profile into shipping archives.
function Get-ApplicationDataDirectory {
    param([Parameter(Mandatory)][string]$ApplicationDirectory)
    $applicationRoot = [IO.Path]::GetFullPath($ApplicationDirectory)
    $preference = Join-Path $applicationRoot 'data-location.txt'
    $dataRoot = Join-Path $applicationRoot 'data'
    if (Test-Path -LiteralPath $preference -PathType Leaf) {
        $saved = [IO.File]::ReadAllText($preference).Trim()
        if ($saved) {
            $dataRoot = if ([IO.Path]::IsPathRooted($saved)) { $saved } else { Join-Path $applicationRoot $saved }
        }
    }
    return [IO.Path]::GetFullPath($dataRoot)
}

function Assert-LocalProfileData {
    param([Parameter(Mandatory)][string]$DataDirectory)
    if (-not (Test-Path -LiteralPath (Join-Path $DataDirectory 'app.db') -PathType Leaf) -or
        -not (Test-Path -LiteralPath (Join-Path $DataDirectory 'secrets') -PathType Container)) {
        throw 'Remembered local data is unavailable. Restore its directory before building, or explicitly use -NoLocalProfile for a clean build.'
    }
}

function Save-LocalProfileSource {
    param([Parameter(Mandatory)][string]$ProjectRoot, [Parameter(Mandatory)][string]$ApplicationDirectory)
    $applicationRoot = [IO.Path]::GetFullPath($ApplicationDirectory)
    Assert-LocalProfileData (Get-ApplicationDataDirectory $applicationRoot)
    $localRoot = Join-Path $ProjectRoot '.local'
    New-Item -ItemType Directory -Force -Path $localRoot | Out-Null
    [IO.File]::WriteAllText((Join-Path $localRoot 'profile-source.txt'), $applicationRoot, [Text.UTF8Encoding]::new($false))
}

function Get-LocalProfileDataDirectory {
    param([Parameter(Mandatory)][string]$ProjectRoot)
    $sourceFile = Join-Path $ProjectRoot '.local\profile-source.txt'
    if (-not (Test-Path -LiteralPath $sourceFile -PathType Leaf)) { return $null }
    $sourceRoot = [IO.File]::ReadAllText($sourceFile).Trim()
    if (-not $sourceRoot -or -not [IO.Path]::IsPathRooted($sourceRoot)) { throw 'Invalid local profile source directory' }
    $dataRoot = Get-ApplicationDataDirectory $sourceRoot
    Assert-LocalProfileData $dataRoot
    return $dataRoot
}

function Assert-LocalReleaseProfile {
    param([Parameter(Mandatory)][string]$ReleaseDirectory, [Parameter(Mandatory)][string]$DataDirectory)
    Assert-LocalProfileData $DataDirectory
    $existing = Get-ApplicationDataDirectory $ReleaseDirectory
    $preference = Join-Path $ReleaseDirectory 'data-location.txt'
    if (((Test-Path -LiteralPath $preference) -or (Test-Path -LiteralPath (Join-Path $existing 'app.db'))) -and
        -not [string]::Equals($existing, [IO.Path]::GetFullPath($DataDirectory), [StringComparison]::OrdinalIgnoreCase)) {
        throw 'This release already has a different data directory. Choose a new -ReleaseDirectory to keep both profiles intact.'
    }
}

function Set-LocalReleaseProfile {
    param([Parameter(Mandatory)][string]$ProjectRoot, [Parameter(Mandatory)][string]$ReleaseDirectory,
          [Parameter(Mandatory)][string]$DataDirectory)
    Assert-LocalReleaseProfile $ReleaseDirectory $DataDirectory
    New-Item -ItemType Directory -Force -Path $ReleaseDirectory | Out-Null
    [IO.File]::WriteAllText((Join-Path $ReleaseDirectory 'data-location.txt'), [IO.Path]::GetFullPath($DataDirectory), [Text.UTF8Encoding]::new($false))
    # Follow this application on the next build, including subsequent in-app data migrations.
    Save-LocalProfileSource $ProjectRoot $ReleaseDirectory
}
