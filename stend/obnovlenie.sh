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
# Настоящая причина падений wine (TMPDIR не должен быть экспортирован в его
# окружение вообще) и unset для неё — в stend/obshchee.sh, до первого вызова wine.
S=$KORFN/.stend_obn
PORT=${PORT:-8712}
rm -rf "$S"; mkdir -p "$S/dom" "$S/novaya"
. "$KORFN/stend/obshchee.sh"

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
# Прямым ребёнком с пойманным pid и снос по EXIT: уборка строками 87-89 ниже
# работает только на счастливом пути, а past выходит раньше — и сервер
# оставался жить сиротой (см. obnovlenie_fon.sh о том, чем это кончается).
python3 -m http.server "$PORT" --directory "$S" >/dev/null 2>&1 &
PID_SERVERA=$!
trap 'kill -KILL "$PID_SERVERA" 2>/dev/null' EXIT
sleep 2

ZH="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/kelevra.log"
rm -f "$ZH"
# Запускаем БОЕВЫМ путём (--tiho), а не служебным. 20.08 разведение окна и
# службы на два процесса поставило ветку --sluzhba ДО проверки обновления,
# и стенд, стоявший на KELEVRA_BEZ_OKNA=1, стал щупать дверь, за которой
# обновления нет по замыслу: служба его не проверяет никогда, её поднимает
# уже проверенная копия. --tiho — тот же путь, которым приложение стартует
# из автозапуска Windows: обновление проверяется, окна не открывается.
wine_zapusti "$S/run.log" "$ZH" - 25 -- \
  env KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" timeout 90 "$WINE" "$S/dom/Kelevra.exe" --tiho
mertv=$?
# Убить и ДОЖДАТЬСЯ смерти, с эскалацией в KILL.
#
# Голого `pkill -f` (это TERM) тут мало по двум замеренным причинам. Первая:
# боевой путь порождает ОТДЕЛЬНЫЙ процесс службы АСИНХРОННО (main.go,
# podnyatSluzhbuOtdelno) — один выстрел сразу после выхода wine-обёртки может
# уйти раньше, чем ребёнок вообще появился. Вторая: под wine этот Kelevra.exe
# на TERM не реагирует — 26.08 замерено, что после TERM он жил 5с и 20с, а от
# KILL умирал мгновенно.
#
# Почему это не косметика: stend/vse.sh гоняет стенды подряд, и соседи считают
# ЧУЖИЕ живые процессы. 26.08 из-за этих хвостов покраснел polnyy_rezhim.sh
# («процессов Kelevra.exe до старта: 3, площадка нечистая»), сам по себе
# зелёный, — то есть моя приёмка стала случайной из-за уборки в этом файле.
ubit_i_dozhdatsya() { # ubit_i_dozhdatsya <образец для pkill -f>
  local obrazec=$1 i pusto=0
  pkill -TERM -f "$obrazec" 2>/dev/null || true
  # Выходим не по ПЕРВОМУ пустому замеру, а по трём подряд с паузой. Замер
  # 26.08: после первой (успешной!) уборки процесс появился заново с возрастом
  # 11с — служба поднимается асинхронно и может родиться уже ПОСЛЕ того, как
  # мы посмотрели и увидели пусто. Один взгляд тут ничего не доказывает.
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    if pgrep -f "$obrazec" >/dev/null 2>&1; then
      pusto=0
      pkill -KILL -f "$obrazec" 2>/dev/null
    else
      pusto=$((pusto + 1))
      [ "$pusto" -ge 3 ] && return 0
    fi
    sleep 0.5
  done
  # Не молчать: если процесс пережил даже KILL, следующий стенд покраснеет от
  # него, и разбираться будут не с этой строкой, а с невиновным соседом.
  echo "⚠ уборка: процессы «$obrazec» пережили TERM и KILL — площадка останется грязной" >&2
  return 1
}
ubit_i_dozhdatsya "$S/dom/Kelevra.exe" || true
ubit_i_dozhdatsya "Kelevra.exe --sluzhba" || true
ubit_i_dozhdatsya "http.server $PORT" || true
if [ "$mertv" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi

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
