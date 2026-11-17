# Shows the product-browser verification window for a local mock check page and
# fails if the window stays blank. 0.7.36 and earlier showed an empty white
# window: the page loads while the window is hidden, and WebView2 painted
# nothing after the window appeared. Needs an unlocked desktop; the window is
# brought to the front for about 8 s and the screen area under it is read.
param([Parameter(Mandatory)][string]$Helper, [Parameter(Mandatory)][string]$Runtime)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System; using System.Runtime.InteropServices;
public static class VerifyWindow {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern IntPtr FindWindowEx(IntPtr p, IntPtr a, string c, string t);
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
  public struct RECT { public int L, T, R, B; }
  public struct POINT { public int X, Y; }
  public static IntPtr Find(uint want) {
    IntPtr h = IntPtr.Zero;
    while ((h = FindWindowEx(IntPtr.Zero, h, "ImageWorkbenchProductBrowser", null)) != IntPtr.Zero) {
      uint pid; GetWindowThreadProcessId(h, out pid);
      if (pid == want && IsWindowVisible(h)) return h;
    }
    return IntPtr.Zero;
  }
}
'@

# A check page in solid colours, served on a free local port.
$listener = New-Object System.Net.HttpListener
$port = Get-Random -Minimum 20000 -Maximum 40000
$listener.Prefixes.Add("http://127.0.0.1:$port/")
$listener.Start()
$server = [PowerShell]::Create().AddScript({
    param($l)
    $page = [Text.Encoding]::UTF8.GetBytes('<html><body style="margin:0;background:#1b5e20"><p style="color:#fff;font:40px sans-serif">Click the button below to continue shopping</p></body></html>')
    while ($l.IsListening) {
        try { $c = $l.GetContext() } catch { break }
        $c.Response.ContentType = 'text/html; charset=utf-8'
        $c.Response.OutputStream.Write($page, 0, $page.Length)
        $c.Response.Close()
    }
}).AddArgument($listener)
$null = $server.BeginInvoke()

$profile = Join-Path ([IO.Path]::GetTempPath()) ('pb-verify-' + [guid]::NewGuid())
$psi = New-Object System.Diagnostics.ProcessStartInfo $Helper
$psi.Arguments = "-url http://127.0.0.1:$port/ -runtime `"$Runtime`" -profile `"$profile`" -timeout 40 -challenge-after 1"
$psi.UseShellExecute = $false
$psi.RedirectStandardInput = $true   # stdin EOF makes the helper exit
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$p = [Diagnostics.Process]::Start($psi)
try {
    $h = [IntPtr]::Zero
    for ($i = 0; $i -lt 60 -and $h -eq [IntPtr]::Zero; $i++) {
        Start-Sleep -Milliseconds 500
        if ($p.HasExited) { throw "验证窗口没有出现，辅助程序已退出：$($p.StandardOutput.ReadToEnd())" }
        $h = [VerifyWindow]::Find([uint32]$p.Id)
    }
    if ($h -eq [IntPtr]::Zero) { throw '30 秒内验证窗口没有出现' }
    Start-Sleep -Seconds 5
    $r = New-Object VerifyWindow+RECT
    [VerifyWindow]::GetClientRect($h, [ref]$r) | Out-Null
    $o = New-Object VerifyWindow+POINT
    [VerifyWindow]::ClientToScreen($h, [ref]$o) | Out-Null
    $bmp = New-Object Drawing.Bitmap $r.R, $r.B
    $g = [Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($o.X, $o.Y, 0, 0, $bmp.Size)
    $g.Dispose()
    $green = 0; $total = 0
    for ($y = 5; $y -lt $bmp.Height - 5; $y += 10) {
        for ($x = 5; $x -lt $bmp.Width - 5; $x += 10) {
            $total++
            $c = $bmp.GetPixel($x, $y)
            if ($c.G -gt $c.R + 40 -and $c.G -gt $c.B + 40) { $green++ }
        }
    }
    $bmp.Dispose()
    if ($green -lt $total / 2) { throw "验证窗口是空白的：页面背景只占 $green/$total 个采样点" }
    "verification window shows the page ($green/$total samples)"
} finally {
    if (-not $p.HasExited) { $p.Kill() }
    $listener.Stop()
    Remove-Item -Recurse -Force $profile -ErrorAction SilentlyContinue
}
