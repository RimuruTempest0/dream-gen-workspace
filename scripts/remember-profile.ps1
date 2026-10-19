param([Parameter(Mandatory)][string]$ApplicationDirectory)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'local-profile.ps1')
if (-not [IO.Path]::IsPathRooted($ApplicationDirectory)) { $ApplicationDirectory = Join-Path $projectRoot $ApplicationDirectory }
Save-LocalProfileSource $projectRoot $ApplicationDirectory
Write-Output 'Local profile remembered. Future local builds will reuse its data and encrypted credentials; shipping ZIPs stay clean.'
