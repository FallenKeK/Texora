Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @"
using System; using System.Runtime.InteropServices;
public class M { [DllImport("user32.dll")] public static extern bool SetCursorPos(int x,int y);
 [DllImport("user32.dll")] public static extern void mouse_event(int f,int dx,int dy,int d,int e); }
"@
New-Item -ItemType Directory -Force out | Out-Null
function Shot($n){ $b=[System.Windows.Forms.SystemInformation]::VirtualScreen
 $bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height
 $g=[System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Left,$b.Top,0,0,$bmp.Size)
 $bmp.Save("$PWD\out\$n.png") }
function Click($x,$y){ [M]::SetCursorPos($x,$y); Start-Sleep -Milliseconds 150
 [M]::mouse_event(2,0,0,0,0); Start-Sleep -Milliseconds 60; [M]::mouse_event(4,0,0,0,0); Start-Sleep -Milliseconds 500 }
function Keys($k){ [System.Windows.Forms.SendKeys]::SendWait($k); Start-Sleep -Milliseconds 600 }
# Cmd — команда через палитру (Ctrl+K, начало названия, Enter): не зависит от раскладки окна
function Cmd($q){ Keys "^k"; Keys $q; Keys "{ENTER}"; Start-Sleep -Milliseconds 400 }
function Start-Tx($file){ $p=Start-Process .\Texora.exe -ArgumentList ('"'+(Resolve-Path $file)+'"') -PassThru; Start-Sleep 5; return $p }
