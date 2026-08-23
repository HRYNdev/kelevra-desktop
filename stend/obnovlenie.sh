#!/usr/bin/env bash
# Стенд обновления: приложение забирает новую версию само и перезапускается.
#
# Проверяется настоящей windows-сборкой под wine, потому что вся суть здесь в
# файловой семантике Windows: запущенный .exe нельзя затереть, но можно
# переименовать. На линуксовой сборке этот шаг ничего не доказывает.
#
# Ход стенда: собираем 0.4.2 и 0.5.0, поднимаем поддельный GitHub со списком
# релизов (внутри — и релиз ЯДРА, чтобы приложение не приняло его за себя),
# запускаем 0.4.2 и смотрим, что дальше живёт 0.5.0.
set -u
KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all} HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
# wine падает («double free or corruption (out)»), если TMPDIR унаследован с
# диска (замерено 23.08, воспроизведено с TMPDIR на ZFS-каталоге): ему нужен
# настоящий /tmp. GOTMPDIR/GOCACHE у go приоритетнее TMPDIR, сборке не мешает.
export TMPDIR=/tmp
S=$KORFN/.stend_obn
PORT=${PORT:-8712}
rm -rf "$S"; mkdir -p "$S/dom" "$S/novaya"

if ! xdpyinfo -display :97 >/dev/null 2>&1; then Xvfb :97 -screen 0 1280x800x24 >/dev/null 2>&1 & sleep 2; fi
export DISPLAY=${DISPLAY:-:97}

versiya() { echo "-X github.com/HRYNdev/kelevra-desktop/internal/podpiska.Versiya=$1"; }
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui $(versiya 0.4.2)" -o "$S/dom/Kelevra.exe" "$KORFN/cmd/kelevra" || exit 1
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui $(versiya 0.5.0)" -o "$S/novaya/Kelevra.exe" "$KORFN/cmd/kelevra" || exit 1

RAZMER=$(stat -c%s "$S/novaya/Kelevra.exe")
cat > "$S/relizy.json" <<JSON
[{"tag_name":"core-v1.14.0-beta.4-1","draft":false,"prerelease":false,
  "assets":[{"name":"sing-box-windows-amd64.zip","browser_download_url":"http://127.0.0.1:$PORT/net","size":9}]},
 {"tag_name":"app-v0.5.0","draft":false,"prerelease":false,
  "assets":[{"name":"Kelevra.exe","browser_download_url":"http://127.0.0.1:$PORT/novaya/Kelevra.exe","size":$RAZMER}]}]
JSON
(cd "$S" && python3 -m http.server "$PORT" >/dev/null 2>&1 &)
sleep 2

ZH="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/kelevra.log"
rm -f "$ZH"
# Запускаем БОЕВЫМ путём (--tiho), а не служебным. 20.08 разведение окна и
# службы на два процесса поставило ветку --sluzhba ДО проверки обновления,
# и стенд, стоявший на KELEVRA_BEZ_OKNA=1, стал щупать дверь, за которой
# обновления нет по замыслу: служба его не проверяет никогда, её поднимает
# уже проверенная копия. --tiho — тот же путь, которым приложение стартует
# из автозапуска Windows: обновление проверяется, окна не открывается.
KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" timeout 90 "$WINE" "$S/dom/Kelevra.exe" --tiho >"$S/run.log" 2>&1 &
sleep 25
pkill -f "$S/dom/Kelevra.exe" 2>/dev/null
# Боевой путь порождает ОТДЕЛЬНЫЙ процесс службы: он переживёт родителя и
# займёт метку копии, из-за чего следующий прогон не поднимется вовсе.
pkill -f "Kelevra.exe --sluzhba" 2>/dev/null
pkill -f "http.server $PORT" 2>/dev/null

bed=0
proverit() { # проверит <что должно быть в журнале> <что это значит>
  if grep -qF "$1" "$ZH" 2>/dev/null; then echo "  ✓ $2"; else echo "  ✗ $2"; bed=1; fi
}
proverit "обновление: вышла версия 0.5.0" "нашло свежую сборку (и не спутало её с релизом ядра)"
proverit "версия 0.5.0 встала, перезапускаюсь" "заменило себя на месте"
proverit "запуск Kelevra 0.5.0" "дальше работает уже новая версия"
if [ "$(md5sum < "$S/dom/Kelevra.exe")" = "$(md5sum < "$S/novaya/Kelevra.exe")" ]; then
  echo "  ✓ файл на диске — это новая сборка"
else
  echo "  ✗ файл на диске остался старым"; bed=1
fi
grep -c "обновление: вышла версия" "$ZH" 2>/dev/null | grep -qx 1 && echo "  ✓ обновление не зациклилось" || { echo "  ✗ обновление повторилось"; bed=1; }

echo "── обновление: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
