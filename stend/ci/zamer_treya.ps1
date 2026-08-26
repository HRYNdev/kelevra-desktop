# ЗАМЕР, а не проверка. Отвечает на один вопрос: можно ли на раннере GitHub Actions
# доказывать НАСТОЯЩИЙ windows-путь трея — тот, что у нас закрыт заглушкой на линуксе
# и наполовину не работает под wine (пузырь рисуется, клик по пузырю не доходит).
#
# Ничего не утверждает заранее и никого не красит: печатает факты и итог. Решение,
# что с этим делать, принимается по его выводу, а не до него.

$ErrorActionPreference = 'Continue'
function Fakt($imya, $znachenie) { "{0,-34} {1}" -f ($imya + ':'), $znachenie }

'=== ЗАМЕР: живой трей на раннере ==='
Fakt 'ОС' (Get-CimInstance Win32_OperatingSystem).Caption
Fakt 'пользователь' "$env:USERNAME"
Fakt 'интерактивная сессия' ([Environment]::UserInteractive)
Fakt 'id сессии процесса' ([System.Diagnostics.Process]::GetCurrentProcess().SessionId)

$explorer = Get-Process explorer -ErrorAction SilentlyContinue
Fakt 'explorer.exe запущен' ($null -ne $explorer)
if (-not $explorer) {
    'explorer.exe не найден — пробую поднять оболочку сам'
    Start-Process explorer.exe -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 8
    $explorer = Get-Process explorer -ErrorAction SilentlyContinue
    Fakt 'explorer.exe после запуска' ($null -ne $explorer)
}

Add-Type -Namespace Okna -Name Api -MemberDefinition @'
[DllImport("user32.dll", SetLastError=true, CharSet=CharSet.Unicode)]
public static extern IntPtr FindWindowW(string cls, string name);
'@
$tray = [Okna.Api]::FindWindowW('Shell_TrayWnd', $null)
Fakt 'окно Shell_TrayWnd' $(if ($tray -ne [IntPtr]::Zero) { "есть ($tray)" } else { 'НЕТ' })

# Самое честное место замера: WinForms NotifyIcon зовёт ровно Shell_NotifyIconW,
# тот же вызов, что и наш trey_windows.go.
$znachok = $null
$postavlen = $false
$balun = $false
try {
    Add-Type -AssemblyName System.Windows.Forms, System.Drawing
    $znachok = New-Object System.Windows.Forms.NotifyIcon
    $znachok.Icon = [System.Drawing.SystemIcons]::Information
    $znachok.Text = 'zamer'
    $znachok.Visible = $true
    $postavlen = $znachok.Visible
    if ($postavlen) {
        $znachok.BalloonTipTitle = 'zamer'
        $znachok.BalloonTipText  = 'проверка пузыря на раннере'
        $znachok.ShowBalloonTip(4000)
        Start-Sleep -Seconds 3
        $balun = $true
    }
} catch {
    Fakt 'Shell_NotifyIcon отказ' $_.Exception.Message
} finally {
    if ($znachok) { $znachok.Visible = $false; $znachok.Dispose() }
}
Fakt 'значок в трее поставлен' $postavlen
Fakt 'пузырь показан без ошибки' $balun

$wv = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
$wvv = (Get-ItemProperty -Path $wv -Name pv -ErrorAction SilentlyContinue).pv
Fakt 'WebView2 Runtime' $(if ($wvv) { $wvv } else { 'НЕ УСТАНОВЛЕН' })

''
if ($tray -ne [IntPtr]::Zero -and $postavlen) {
    'ИТОГ: раннер держит настоящий трей. Клик по пузырю (NIN_BALLOONUSERCLICK) можно'
    '      доказывать здесь — то, что под wine недоказуемо.'
} elseif ($postavlen) {
    'ИТОГ: значок ставится, но окна оболочки нет. Пузырь под вопросом, клик по пузырю'
    '      скорее всего недоказуем — нужна живая Windows с рабочим столом.'
} else {
    'ИТОГ: трея на раннере НЕТ. Windows-путь трея здесь не доказать; для клика по'
    '      пузырю нужна отдельная Windows с сеансом рабочего стола.'
}
exit 0
