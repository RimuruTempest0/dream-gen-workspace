param([Parameter(Mandatory=$true)][string]$Target,[switch]$Close)
$ErrorActionPreference='Stop'
$resolved=[IO.Path]::GetFullPath($Target).TrimEnd('\')
$shell=New-Object -ComObject Shell.Application
$found=$false
foreach($window in @($shell.Windows())){
    try {
        $url=[uri]$window.LocationURL
        if($url.IsFile -and $url.LocalPath.TrimEnd('\') -eq $resolved){
            $found=[bool]$window.Visible
            if($Close){$window.Quit()}
        }
    } catch {}
}
if($found){'visible'}else{'absent'}
