#!/usr/bin/env bash
# Стенд обновления (Windows/wine): холодный старт находит новую версию сам,
# но .exe подменяет ТОЛЬКО тычок в пузырь — не автоматом.
#
# До 26.08 этот стенд доказывал обратное: что --tiho сам, без единого клика,
# качает свежую сборку, подменяет .exe на месте и перезапускается ею же — это
# и была беда (заказ 26.08 11:33: обновления должны работать как на телефоне —
# приходить сами, а ставиться нажатием, не автоматом). Правка убрала
# автоподмену из холодного старта
# (cmd/kelevra/main.go: obnovitsya() удалён) и оставила программе только
# НАЙТИ и ПРЕДЛОЖИТЬ — ставит теперь только тычок человека в пузырь
# (internal/sluzhba.PostavitNaydennoe, ручка /api/obnovlenie_postavit).
#
# Проверяется настоящей windows-сборкой под wine, потому что установка по
# тычку — та же файловая семантика Windows, что была тут всегда: запущенный
# .exe нельзя затереть, но можно переименовать (obnovlenie.Postavit это не
# трогало и тут не трогается). Линуксовый холодный старт без всякой Windows-
# семантики уже покрыт stend/holodnyy_start_bez_avtopodmeny.sh, а сама ручка
# тычка платформенно-независимо — stend/obnovlenie_postavit.sh; здесь —
# именно связка «--tiho под настоящей Windows → тычок → реальная подмена».
#
# Ход стенда: собираем 0.4.2 и 0.5.0, поднимаем поддельный GitHub со списком
# релизов (внутри — и релиз ЯДРА, чтобы приложение не приняло его за себя),
# запускаем 0.4.2 боевым путём (--tiho), сами бьём по найденной ручке тычка и
# смотрим, что дальше на диске лежит 0.5.0 — но не раньше тычка.
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
MD5_STARAYA_DO=$(md5sum < "$S/dom/Kelevra.exe")
MD5_NOVAYA=$(md5sum < "$S/novaya/Kelevra.exe")

RAZMER=$(stat -c%s "$S/novaya/Kelevra.exe")
cat > "$S/relizy.json" <<JSON
[{"tag_name":"core-v1.14.0-beta.4-1","draft":false,"prerelease":false,
  "assets":[{"name":"sing-box-windows-amd64.zip","browser_download_url":"http://127.0.0.1:$PORT/net","size":9}]},
 {"tag_name":"app-v0.5.0","draft":false,"prerelease":false,
  "assets":[{"name":"Kelevra.exe","browser_download_url":"http://127.0.0.1:$PORT/novaya/Kelevra.exe","size":$RAZMER}]}]
JSON
# Прямым ребёнком с пойманным pid и снос по EXIT: уборка ниже работает только
# на счастливом пути, а past выходит раньше — и сервер оставался жить сиротой
# (см. obnovlenie_fon.sh о том, чем это кончается).
python3 -m http.server "$PORT" --directory "$S" >/dev/null 2>&1 &
PID_SERVERA=$!
trap 'kill -KILL "$PID_SERVERA" 2>/dev/null' EXIT
sleep 2

ZH="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra/kelevra.log"
rm -f "$ZH"
# Запускаем БОЕВЫМ путём (--tiho), а не служебным. 20.08 разведение окна и
# службы на два процесса поставило ветку --sluzhba ДО проверки обновления,
# и стенд, стоявший на KELEVRA_BEZ_OKNA=1, стал щупать дверь, за которой
# обновления нет по замыслу: служба его не проверяет never, её поднимает
# уже проверенная копия. --tiho — тот же путь, которым приложение стартует
# из автозапуска Windows: поиск обновления — дело службы сразу после
# подъёма, окна не открывается.
wine_zapusti "$S/run.log" "$ZH" "служба слушает" 25 -- \
  env KELEVRA_RELIZY="http://127.0.0.1:$PORT/relizy.json" timeout 90 "$WINE" "$S/dom/Kelevra.exe" --tiho
mertv=$?
if [ "$mertv" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  kill -KILL "$PID_SERVERA" 2>/dev/null
  exit 7
fi

bed=0
proverit() { # проверит <что должно быть в журнале> <что это значит>
  if grep -qF "$1" "$ZH" 2>/dev/null; then echo "  ✓ $2"; else echo "  ✗ $2"; bed=1; fi
}
proverit_net() { # проверит_нет <что НЕ должно быть в журнале> <что это значит>
  if grep -qF "$1" "$ZH" 2>/dev/null; then echo "  ✗ $2"; bed=1; else echo "  ✓ $2"; fi
}
proverit "фоновая проверка обновления: найдена версия 0.5.0" "нашла свежую сборку сама, без клика (и не спутала её с релизом ядра)"
proverit_net "версия 0.5.0 встала, перезапускаюсь" "холодный старт САМ ничего не поставил (старой строки автоподмены больше нет)"
if [ "$(md5sum < "$S/dom/Kelevra.exe")" = "$MD5_STARAYA_DO" ]; then
  echo "  ✓ .exe на диске не подменился холодным стартом"
else
  echo "  ✗ .exe на диске подменился без тычка человека"; bed=1
fi

# --- тычок в пузырь: та самая настоящая Windows-семантика (rename поверх
# запущенного .exe), теперь единственный путь к установке. -------------------
URL=$(grep -o 'http://[^ ]*' "$ZH" 2>/dev/null | head -1)
if [ -z "$URL" ]; then
  echo "  ✗ не нашёл адрес службы в журнале — тычок проверить не смог"
  bed=1
else
  TYCHOK=$(curl -s -o "$S/tychok.json" -w '%{http_code}' --max-time 60 -X POST "${URL}api/obnovlenie_postavit") || TYCHOK="000"
  if [ "$TYCHOK" = "200" ]; then
    echo "  ✓ тычок POST api/obnovlenie_postavit → 200"
  else
    echo "  ✗ тычок POST api/obnovlenie_postavit → $TYCHOK ($(cat "$S/tychok.json" 2>/dev/null))"; bed=1
  fi
  # Реальная подмена под Windows идёт асинхронно (см. PostavitNaydennoe): даём
  # время на скачивание (localhost, ~8МБ) и rename поверх своего же .exe.
  POSTAVILOS=0
  for _ in $(seq 1 30); do
    [ "$(md5sum < "$S/dom/Kelevra.exe" 2>/dev/null)" = "$MD5_NOVAYA" ] && { POSTAVILOS=1; break; }
    sleep 1
  done
  if [ "$POSTAVILOS" -eq 1 ]; then
    echo "  ✓ после тычка .exe на диске — это новая сборка (Windows rename поверх запущенного файла отработал)"
  else
    echo "  ✗ после тычка .exe на диске остался старым"; bed=1
  fi
fi

# Убить и ДОЖДАТЬСЯ смерти, с эскалацией в KILL — ПОСЛЕ всех проверок: тычок
# выше сам просит службу перезапуститься новой копией, гасить раньше нечем
# мерить.
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
ubit_i_dozhdatsya "Kelevra.exe --tiho" || true
ubit_i_dozhdatsya "http.server $PORT" || true

echo "── обновление: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
