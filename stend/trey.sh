#!/usr/bin/env bash
# Стенд трея: доказывает живьём (настоящая windows-сборка под wine), что
# служебный режим (--sluzhba) поднимает значок в системном трее, а не
# остаётся невидимкой, как после разведения окна и службы на два процесса
# (b924080, стенд stend/razdvoenie.sh).
#
# Диагноз, который закрывает этот файл: человек закрыл окно — служба (ядро +
# прокси + HTTP) продолжает жить отдельным процессом, но понять, что защита
# включена, нечем, а выключить её нечем тоже. Трей (cmd/kelevra/trey_windows.go)
# — единственное окно в мир, которое у службы теперь есть.
#
# Что стенд ПОКАЗЫВАЕТ по журналу kelevra.log:
#   а) поток трея встал и создал СВОЁ (message-only) окно —
#      «трей: поток встал, окно трея создано»;
#   б) HICON собран настоящим CreateIconFromResourceEx из вшитого znachok.ico,
#      номер хендла не ноль — «трей: значок собран из znachok.ico ... HICON=0x…»;
#   в) Shell_NotifyIconW(NIM_ADD) позван, и его результат записан честно —
#      «трей: Shell_NotifyIconW(NIM_ADD) -> true|false». В wine без explorer.exe
#      он законно может вернуть false — это не провал щупа, провал — молчание
#      (строки нет вовсе).
#
# Проверка выключением (обязательна по договору со стендом): скрипт умеет
# --lomat — временно подменяет znachok.ico на пустой файл, пересобирает и
# показывает, что стенд краснеет (нет строки «значок собран», есть
# «беру системный значок»), затем восстанавливает файл и остаётся собранным
# нормально. Обычный (без --lomat) прогон это делает автоматически как
# часть себя же — см. РАЗДЕЛ 2 ниже — и в конце всегда возвращает
# znachok.ico на место, даже при ошибке (trap).
#
# Чего стенд НЕ ПРОВЕРЯЕТ по-настоящему (границы честно названы):
#   · настоящий explorer.exe и то, отрисовывается ли значок реальному
#     человеку в реальном трее — wine его не поднимает, тут его нет вовсе;
#   · правый клик/меню/двойной клик — под wine без окна рабочего стола некому
#     сгенерировать WM_RBUTTONUP/WM_LBUTTONDBLCLK; проверяются только старт
#     потока, сборка значка и сам факт вызова Shell_NotifyIconW;
#   · «Выход» гасит ядро и снимает прокси — это уже покрыто путём
#     zhdatSignal(stop) в стенде razdvoenie.sh/windows.sh (тот же select, тот
#     же код), сюда развёрнутый живой клик по «Выход» не входит.
set -u
KORFN=$(cd "$(dirname "$0")/.." && pwd)
WINE=${WINE:-/usr/lib/wine/wine64}
export WINEPREFIX=${WINEPREFIX:-$KORFN/.wine}
export WINEDEBUG=${WINEDEBUG:--all}
export HOME=${HOME:-/root}
export LANG=${LANG:-C.UTF-8} LC_ALL=${LC_ALL:-C.UTF-8}
# wine падает («double free or corruption (out)»), если TMPDIR унаследован с
# диска (замерено 23.08, воспроизведено с TMPDIR на ZFS-каталоге): ему нужен
# настоящий /tmp. GOTMPDIR/GOCACHE у go приоритетнее TMPDIR, сборке не мешает.
export TMPDIR=/tmp
STEND=$KORFN/.stend_trey
mkdir -p "$STEND" "$WINEPREFIX"
. "$KORFN/stend/obshchee.sh"

command -v go >/dev/null 2>&1 || export PATH="$PATH:/usr/local/go/bin"

if [ ! -x "$WINE" ]; then
  echo "нет wine ($WINE): apt-get install -y --no-install-recommends wine64" >&2
  exit 2
fi

if ! xdpyinfo -display :97 >/dev/null 2>&1; then
  Xvfb :97 -screen 0 1280x800x24 >/dev/null 2>&1 &
  sleep 2
fi
export DISPLAY=${DISPLAY:-:97}

ZNACHOK="$KORFN/cmd/kelevra/znachok.ico"
ZNACHOK_BEKAP="$STEND/znachok.ico.bekap"
cp "$ZNACHOK" "$ZNACHOK_BEKAP"
# Что бы ни случилось дальше (в том числе Ctrl+C или падение стенда) —
# znachok.ico не входит в список файлов, которые эта работа имеет право
# менять навсегда, поэтому файл обязан вернуться на место.
trap 'cp "$ZNACHOK_BEKAP" "$ZNACHOK"' EXIT

PAPKA="$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/Kelevra"
ZHURNAL="$PAPKA/kelevra.log"
bed=0

sobrat() {
  GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o "$STEND/Kelevra.exe" "$KORFN/cmd/kelevra"
}

# zapustitISnyatZhurnal(pometka) — поднимает --sluzhba под wine на 12 секунд,
# отдаёт журнал (только новые строки) и гасит процесс. KELEVRA_BEZ_OBNOVLENIYA
# тут ни при чём (--sluzhba обновление не проверяет вовсе, см. main.go), но
# держим переменные окружения теми же, что у соседних стендов ради единого
# рецепта.
zapustitISnyatZhurnal() { # возврат 77 (через $?) — прибор мёртв, см. stend/obshchee.sh
  pkill -f "Kelevra.exe" 2>/dev/null; sleep 1
  wine_zapusti "$STEND/sluzhba_$1.log" "$ZHURNAL" "трей:\|служба слушает" 13 -- \
    timeout 15 "$WINE" "$STEND/Kelevra.exe" --sluzhba
  local rc=$? pid=$WINE_ZAPUSTI_PID
  sleep 1
  cat "$ZHURNAL" 2>/dev/null
  kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
  pkill -f "Kelevra.exe" 2>/dev/null
  return "$rc"
}

echo "── РАЗДЕЛ 1: живой значок (настоящий znachok.ico) ──"
echo "── сборка Kelevra.exe (windows/amd64) ──"
if ! sobrat 2>&1; then
  echo "  НЕ СОБРАЛСЯ"; exit 1
fi

zhurnal1=$(zapustitISnyatZhurnal "zhivoy")
if [ "$?" -eq 77 ]; then
  echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
  exit 7
fi
echo "$zhurnal1" | sed 's/^/  /' | tail -12

stroka_okno=$(printf '%s\n' "$zhurnal1" | grep -c "трей: поток встал, окно трея создано")
stroka_znachok=$(printf '%s\n' "$zhurnal1" | grep -oE "трей: значок собран из znachok\.ico.*HICON=0x[0-9a-f]+")
stroka_notify=$(printf '%s\n' "$zhurnal1" | grep -oE "трей: Shell_NotifyIconW\(NIM_ADD\) -> (true|false)")

if [ "$stroka_okno" -ge 1 ] 2>/dev/null; then
  echo "  (а) зелёный: поток трея встал, окно создано"
else
  echo "  (а) КРАСНЫЙ: строки «поток встал, окно трея создано» нет"
  bed=1
fi

if [ -n "$stroka_znachok" ]; then
  hicon=$(printf '%s' "$stroka_znachok" | grep -oE "0x[0-9a-f]+")
  if [ "$hicon" != "0x0" ]; then
    echo "  (б) зелёный: HICON собран настоящим CreateIconFromResourceEx, $stroka_znachok"
  else
    echo "  (б) КРАСНЫЙ: HICON=0x0 — CreateIconFromResourceEx отказал на исправном значке"
    bed=1
  fi
else
  echo "  (б) КРАСНЫЙ: строки «значок собран из znachok.ico» нет (см. журнал выше — либо отказ, либо системный значок)"
  bed=1
fi

if [ -n "$stroka_notify" ]; then
  echo "  (в) зелёный: $stroka_notify (честный результат — под wine без explorer.exe тут допустим и false)"
else
  echo "  (в) КРАСНЫЙ: строки «Shell_NotifyIconW(NIM_ADD) ->» нет вовсе — это и есть провал щупа (молчание, не false)"
  bed=1
fi

echo "── РАЗДЕЛ 2: проверка выключением (ломаем znachok.ico) ──"
printf '' > "$ZNACHOK"
echo "  znachok.ico временно заменён на 0 байт (были: $(stat -c%s "$ZNACHOK_BEKAP") байт)"
if ! sobrat 2>&1; then
  echo "  НЕ СОБРАЛСЯ на пустом значке — это отдельная беда, не то, что проверяем"; bed=1
else
  zhurnal2=$(zapustitISnyatZhurnal "lomanyy")
  if [ "$?" -eq 77 ]; then
    echo "⚫ ПРИБОР МЁРТВ: wine не запустил exe (ни одной строки в логе) — продукт НЕ проверялся"
    exit 7
  fi
  echo "$zhurnal2" | sed 's/^/  /' | tail -12
  otkaz=$(printf '%s\n' "$zhurnal2" | grep -c "трей: не удалось собрать значок из znachok\.ico.*беру системный значок")
  uspeh_na_lomanom=$(printf '%s\n' "$zhurnal2" | grep -c "трей: значок собран из znachok\.ico")
  if [ "$otkaz" -ge 1 ] 2>/dev/null && [ "$uspeh_na_lomanom" -eq 0 ]; then
    echo "  зелёный: на пустом значке щуп КРАСНЕЕТ там, где должен — «значок собран» пропадает, вместо неё честный откат на системный"
  else
    echo "  КРАСНЫЙ: щуп не отличил битый значок от живого (не сработала проверка выключением) — сам щуп не годен"
    bed=1
  fi
  # Служба обязана остаться на ногах даже с битым значком: значок — облик,
  # не право на жизнь. Тот же select по строке (а), что и в разделе 1.
  if printf '%s\n' "$zhurnal2" | grep -q "трей: поток встал, окно трея создано"; then
    echo "  зелёный: поток трея всё равно встал (отказ значка не уронил трей)"
  else
    echo "  КРАСНЫЙ: поток трея не встал вовсе на битом значке — отказ значка утащил за собой трей"
    bed=1
  fi
fi

echo "── возвращаю настоящий znachok.ico ──"
cp "$ZNACHOK_BEKAP" "$ZNACHOK"
if cmp -s "$ZNACHOK_BEKAP" "$ZNACHOK"; then
  echo "  зелёный: znachok.ico восстановлен байт в байт"
else
  echo "  КРАСНЫЙ: znachok.ico не восстановился"; bed=1
fi

pkill -f "Kelevra.exe" 2>/dev/null

echo "── итог: $([ $bed -eq 0 ] && echo ЗЕЛЁНЫЙ || echo КРАСНЫЙ) ──"
exit $bed
