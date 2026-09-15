# 模拟 RustDesk 安卓端发往 Windows 被控端的输入。
# 用法: .\test_inject.ps1 -Mode unicode -Text "1234"      (模拟安卓端：Unicode 文本注入)
#       .\test_inject.ps1 -Mode vk      -Text "1234"      (模拟桌面端：真实虚拟键码)
param(
    [ValidateSet("unicode", "vk")] [string]$Mode = "unicode",
    [string]$Text = "1234",
    [int]$Extra = 100,   # enigo / RustDesk 的 ENIGO_INPUT_EXTRA_VALUE
    [int]$Hold  = 30
)

# 命令行传空格容易被吃掉，用 sp 代替
if ($Text -eq "sp") { $Text = " " }

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

[StructLayout(LayoutKind.Sequential)]
public struct KEYBDINPUT {
    public ushort wVk;
    public ushort wScan;
    public uint   dwFlags;
    public uint   time;
    public IntPtr dwExtraInfo;
}

[StructLayout(LayoutKind.Sequential, Size = 40)]
public struct INPUT {
    public uint type;
    public KEYBDINPUT ki;
}

public class Injector {
    public const uint KEYEVENTF_KEYUP    = 0x0002;
    public const uint KEYEVENTF_UNICODE  = 0x0004;
    public const uint KEYEVENTF_SCANCODE = 0x0008;

    [DllImport("user32.dll", SetLastError = true)]
    public static extern uint SendInput(uint nInputs, INPUT[] pInputs, int cbSize);

    [DllImport("user32.dll")]
    public static extern ushort VkKeyScanW(char ch);

    [DllImport("user32.dll")]
    public static extern uint MapVirtualKeyW(uint code, uint mapType);

    public static void Send(ushort vk, ushort scan, uint flags, IntPtr extra) {
        INPUT[] inputs = new INPUT[1];
        inputs[0].type = 1; // INPUT_KEYBOARD
        inputs[0].ki.wVk = vk;
        inputs[0].ki.wScan = scan;
        inputs[0].ki.dwFlags = flags;
        inputs[0].ki.time = 0;
        inputs[0].ki.dwExtraInfo = extra;
        uint r = SendInput(1, inputs, Marshal.SizeOf(typeof(INPUT)));
        if (r != 1) {
            Console.WriteLine("SendInput 失败, GetLastError=" + Marshal.GetLastWin32Error());
        }
    }
}
"@ -ErrorAction Stop

$extra = [IntPtr]$Extra

foreach ($c in $Text.ToCharArray()) {
    if ($Mode -eq "unicode") {
        # RustDesk 安卓端路径：KEYEVENTF_UNICODE，wVk=0，wScan=Unicode 码元
        [Injector]::Send(0, [uint16][int]$c, [Injector]::KEYEVENTF_UNICODE, $extra)
        Start-Sleep -Milliseconds $Hold
        [Injector]::Send(0, [uint16][int]$c, ([Injector]::KEYEVENTF_UNICODE -bor [Injector]::KEYEVENTF_KEYUP), $extra)
    } else {
        # RustDesk 桌面端路径：真实虚拟键码
        $vk = [Injector]::VkKeyScanW($c) -band 0xFF
        $sc = [Injector]::MapVirtualKeyW($vk, 0)
        [Injector]::Send([uint16]$vk, [uint16]$sc, 0, $extra)
        Start-Sleep -Milliseconds $Hold
        [Injector]::Send([uint16]$vk, [uint16]$sc, [Injector]::KEYEVENTF_KEYUP, $extra)
    }
    Start-Sleep -Milliseconds 80
}
Write-Output "已发送 [$Mode] 文本: $Text (extra=$Extra)"
