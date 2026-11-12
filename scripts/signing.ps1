# Optional Authenticode signing for scripts/build.ps1 and scripts/build-admin.ps1.
# Pass -SignThumbprint (SHA-1 thumbprint of a code-signing certificate in the
# current user's or machine's certificate store) or set IMAGEWORKBENCH_SIGN_THUMBPRINT.
# Without one the build continues unsigned with a warning. signtool.exe comes from
# the Windows SDK (or set IMAGEWORKBENCH_SIGNTOOL to its path).
function Get-SignThumbprint([string]$Thumbprint) {
    if ($Thumbprint) { return $Thumbprint.Trim() }
    if ($env:IMAGEWORKBENCH_SIGN_THUMBPRINT) { return $env:IMAGEWORKBENCH_SIGN_THUMBPRINT.Trim() }
    return ''
}

function Find-SignTool {
    if ($env:IMAGEWORKBENCH_SIGNTOOL -and (Test-Path -LiteralPath $env:IMAGEWORKBENCH_SIGNTOOL)) { return $env:IMAGEWORKBENCH_SIGNTOOL }
    $onPath = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($onPath) { return $onPath.Source }
    $kits = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $found = Get-ChildItem -LiteralPath $kits -Filter signtool.exe -Recurse -ErrorAction SilentlyContinue |
        Where-Object { $_.Directory.Name -eq 'x64' } | Sort-Object { $_.Directory.Parent.Name } | Select-Object -Last 1
    if ($found) { return $found.FullName }
    throw 'signtool.exe not found: install the Windows SDK signing tools or set IMAGEWORKBENCH_SIGNTOOL'
}

# Sign-BuildFile signs a file with a timestamp and checks the result.
function Sign-BuildFile([string]$Path, [string]$Thumbprint, [string]$TimestampURL = 'http://timestamp.digicert.com') {
    $tool = Find-SignTool
    & $tool sign /sha1 $Thumbprint /fd sha256 /tr $TimestampURL /td sha256 $Path
    if ($LASTEXITCODE -ne 0) { throw "Signing failed: $Path" }
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid') { throw "Signature check failed for ${Path}: $($signature.Status)" }
    Write-Output "Signed: $Path ($($signature.SignerCertificate.Subject))"
}

function Write-UnsignedWarning([string]$What) {
    Write-Warning "$What is not code-signed (no -SignThumbprint / IMAGEWORKBENCH_SIGN_THUMBPRINT). Windows SmartScreen will show 'unknown publisher'."
}
